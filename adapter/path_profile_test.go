package adapter

import (
	"net/netip"
	"testing"
	"time"

	"github.com/metacubex/mihomo/component/linkprofile"
	"github.com/metacubex/mihomo/component/netstate"
)

func TestTunnelPathEvidenceExpiresOnNetworkEpoch(t *testing.T) {
	p := stub("epoch-path")
	netstate.Advance()

	factor := ObserveTunnelPath(p, linkprofile.TunnelMetrics{
		RTTMs: 900, RTTVarMs: 450, LossRate: 0.08,
		Unacked: 30, Lost: 4, Cwnd: 10,
	})
	if factor >= 1 {
		t.Fatalf("weak path factor=%v, want <1", factor)
	}
	if got := TunnelPathFactorForProxy(p); got != factor {
		t.Fatalf("factor snapshot=%v want=%v", got, factor)
	}

	profile := PathProfileForProxy(p, false)
	if !profile.TunnelFresh || profile.TunnelSamples != 1 {
		t.Fatalf("profile=%+v", profile)
	}

	netstate.Advance()
	if got := TunnelPathFactorForProxy(p); got != 1 {
		t.Fatalf("stale epoch factor=%v, want neutral 1", got)
	}
	profile = PathProfileForProxy(p, false)
	if profile.TunnelFresh {
		t.Fatalf("old tunnel evidence survived network epoch: %+v", profile)
	}
}

func TestPathProfileConfidenceRewardsCorroboratedEgress(t *testing.T) {
	p := stub("attested-path")
	state := capabilityStateForProxy(p)
	epoch := netstate.CurrentEpoch()
	state.ipv4.mu.Lock()
	state.ipv4.known = true
	state.ipv4.ok = true
	state.ipv4.epoch = epoch
	state.ipv4.expire = time.Now().Add(time.Hour)
	state.ipv4.exitIP = netip.MustParseAddr("1.1.1.1")
	state.ipv4.sources = 2
	state.ipv4.consistent = true
	state.ipv4.mu.Unlock()

	profile := PathProfileForProxy(p, false)
	if !profile.Egress.Fresh || profile.Egress.Sources != 2 || !profile.Egress.Consistent {
		t.Fatalf("egress=%+v", profile.Egress)
	}
	if profile.Confidence < 0.5 {
		t.Fatalf("confidence=%v, expected corroborated evidence", profile.Confidence)
	}
}


func TestUnavailableUDPDoesNotExposeOldMapping(t *testing.T) {
	p := stub("udp-old-mapping")
	state := capabilityStateForProxy(p)
	epoch := netstate.CurrentEpoch()

	state.udp.mu.Lock()
	state.udp.known = true
	state.udp.ok = false
	state.udp.epoch = epoch
	state.udp.expire = time.Now().Add(time.Hour)
	state.udp.exitIP = netip.MustParseAddr("203.0.113.44")
	state.udp.mu.Unlock()

	profile := PathProfileForProxy(p, false)
	if !profile.UDPKnown || profile.UDPAvailable {
		t.Fatalf("unexpected UDP verdict: known=%v available=%v", profile.UDPKnown, profile.UDPAvailable)
	}
	if profile.UDPEgressIP.IsValid() {
		t.Fatalf("failed UDP probe exposed stale mapping %v", profile.UDPEgressIP)
	}
}


func TestEgressProfileHidesPreviousEpochIdentity(t *testing.T) {
	p := stub("stale-egress-profile")
	state := capabilityStateForProxy(p)
	epoch := netstate.CurrentEpoch()

	state.ipv4.mu.Lock()
	state.ipv4.known = true
	state.ipv4.ok = true
	state.ipv4.epoch = epoch
	state.ipv4.expire = time.Now().Add(time.Hour)
	state.ipv4.exitIP = netip.MustParseAddr("192.0.2.55")
	state.ipv4.country = "US"
	state.ipv4.asn = "AS64510"
	state.ipv4.asnOrg = "example"
	state.ipv4.sources = 2
	state.ipv4.consistent = true
	state.ipv4.mu.Unlock()

	fresh := PathProfileForProxy(p, false)
	if !fresh.Egress.Known || !fresh.Egress.Fresh || !fresh.Egress.IP.IsValid() {
		t.Fatalf("fresh egress identity missing: %+v", fresh.Egress)
	}

	netstate.Advance()
	stale := PathProfileForProxy(p, false)
	if stale.Egress.Known || stale.Egress.Fresh || stale.Egress.IP.IsValid() {
		t.Fatalf("previous-epoch identity leaked into current profile: %+v", stale.Egress)
	}
	if stale.Egress.Country != "" || stale.Egress.ASN != "" || stale.Egress.ASNOrg != "" ||
		stale.Egress.Sources != 0 || stale.Egress.Consistent || stale.Egress.Divergent {
		t.Fatalf("stale identity metadata leaked: %+v", stale.Egress)
	}
}

func TestExpiredEgressIdentityIsUnknown(t *testing.T) {
	p := stub("expired-egress-profile")
	state := capabilityStateForProxy(p)

	state.ipv4.mu.Lock()
	state.ipv4.known = true
	state.ipv4.ok = true
	state.ipv4.epoch = netstate.CurrentEpoch()
	state.ipv4.expire = time.Now().Add(-time.Second)
	state.ipv4.exitIP = netip.MustParseAddr("198.51.100.90")
	state.ipv4.country = "DE"
	state.ipv4.sources = 2
	state.ipv4.consistent = true
	state.ipv4.mu.Unlock()

	profile := PathProfileForProxy(p, false)
	if profile.Egress.Known || profile.Egress.IP.IsValid() || profile.Egress.Sources != 0 {
		t.Fatalf("expired egress evidence should be unknown: %+v", profile.Egress)
	}
}
