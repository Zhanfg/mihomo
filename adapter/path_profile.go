package adapter

import (
	"math"
	"net/netip"
	"sync"
	"time"

	"github.com/metacubex/mihomo/component/linkprofile"
	"github.com/metacubex/mihomo/component/mmdb"
	"github.com/metacubex/mihomo/component/netstate"
	C "github.com/metacubex/mihomo/constant"
)

const tunnelEvidenceTTL = 15 * time.Minute

type pathRuntime struct {
	mu      sync.Mutex
	epoch   uint64
	updated time.Time
	samples uint32
	tunnel  linkprofile.TunnelMetrics
	factor  float64
}

type EgressPathProfile struct {
	Known      bool
	Fresh      bool
	IP         netip.Addr
	IPv6       bool
	Country    string
	ASN        string
	ASNOrg     string
	Sources    uint8
	Consistent bool
	Divergent  bool
	ExpiresAt  time.Time
}

type ProxyPathProfile struct {
	Epoch        uint64
	Local        netstate.LocalProfile
	Tunnel           linkprofile.TunnelMetrics
	TunnelFresh      bool
	TunnelFactor     float64
	TunnelSamples    uint32
	TunnelAssessment linkprofile.Assessment
	Egress       EgressPathProfile
	UDPKnown        bool
	UDPAvailable    bool
	UDPEgressIP     netip.Addr
	TransportSplit  bool
	Confidence      float64
}

// ObserveTunnelPath records passive phone->proxy evidence from the socket that
// carried real traffic. It is intentionally transient and network-epoch scoped:
// Wi-Fi measurements never survive into a cellular path or process restart.
func ObserveTunnelPath(p C.Proxy, sample linkprofile.TunnelMetrics) float64 {
	if p == nil {
		return 1
	}
	state := capabilityStateForProxy(p)
	currentEpoch := netstate.CurrentEpoch()
	now := time.Now()

	state.path.mu.Lock()
	defer state.path.mu.Unlock()
	if state.path.epoch != currentEpoch {
		state.path.epoch = currentEpoch
		state.path.updated = time.Time{}
		state.path.samples = 0
		state.path.tunnel = linkprofile.TunnelMetrics{}
		state.path.factor = 1
	}

	if state.path.samples == 0 {
		state.path.tunnel = sample
	} else {
		const alpha = 0.25
		state.path.tunnel.RTTMs = linkprofile.EWMA(state.path.tunnel.RTTMs, sample.RTTMs, alpha)
		state.path.tunnel.RTTVarMs = linkprofile.EWMA(state.path.tunnel.RTTVarMs, sample.RTTVarMs, alpha)
		state.path.tunnel.LossRate = state.path.tunnel.LossRate*(1-alpha) + sample.LossRate*alpha
		state.path.tunnel.Unacked = sample.Unacked
		state.path.tunnel.Lost = sample.Lost
		state.path.tunnel.Cwnd = sample.Cwnd
	}
	state.path.samples++
	state.path.updated = now
	state.path.factor = linkprofile.QualityFactor(state.path.tunnel)
	return state.path.factor
}

func tunnelPathSnapshot(state *capabilityState, now time.Time, epoch uint64) (linkprofile.TunnelMetrics, float64, uint32, bool) {
	state.path.mu.Lock()
	defer state.path.mu.Unlock()
	fresh := state.path.epoch == epoch && !state.path.updated.IsZero() && now.Sub(state.path.updated) <= tunnelEvidenceTTL
	if !fresh {
		return linkprofile.TunnelMetrics{}, 1, 0, false
	}
	return state.path.tunnel, state.path.factor, state.path.samples, true
}

func TunnelPathAssessmentForProxy(p C.Proxy) linkprofile.Assessment {
	if p == nil {
		return linkprofile.Assess(linkprofile.TunnelMetrics{}, 0)
	}
	state := capabilityStateForProxy(p)
	metrics, _, samples, fresh := tunnelPathSnapshot(state, time.Now(), netstate.CurrentEpoch())
	if !fresh {
		return linkprofile.Assess(linkprofile.TunnelMetrics{}, 0)
	}
	return linkprofile.Assess(metrics, samples)
}

func TunnelPathFactorForProxy(p C.Proxy) float64 {
	if p == nil {
		return 1
	}
	state := capabilityStateForProxy(p)
	_, factor, _, fresh := tunnelPathSnapshot(state, time.Now(), netstate.CurrentEpoch())
	if !fresh || factor <= 0 {
		return 1
	}
	return factor
}

func egressPathSnapshot(entry *capabilityEntry, ipv6 bool, now time.Time, epoch uint64) EgressPathProfile {
	entry.mu.Lock()
	profile := EgressPathProfile{
		Known:      entry.known && entry.ok,
		Fresh:      entry.known && entry.ok && entry.epoch == epoch && now.Before(entry.expire),
		IP:         entry.exitIP,
		IPv6:       ipv6,
		Country:    entry.country,
		Sources:    entry.sources,
		Consistent: entry.consistent,
		Divergent:  entry.sources >= 2 && !entry.consistent,
		ExpiresAt:  entry.expire,
	}
	entry.mu.Unlock()

	if !profile.IP.IsValid() {
		return profile
	}
	if profile.Country == "" {
		if codes, err := mmdb.LookupCodeOptional(C.Path.MMDB(), profile.IP.AsSlice()); err == nil && len(codes) > 0 {
			profile.Country = codes[0]
		}
	}
	if asn, org, err := mmdb.LookupASNOptional(C.Path.ASN(), profile.IP.AsSlice()); err == nil {
		profile.ASN, profile.ASNOrg = asn, org
	}
	return profile
}

func PathProfileForProxy(p C.Proxy, ipv6 bool) ProxyPathProfile {
	epoch := netstate.CurrentEpoch()
	profile := ProxyPathProfile{
		Epoch:        epoch,
		Local:        netstate.Local(),
		TunnelFactor: 1,
	}
	if p == nil {
		return profile
	}

	state := capabilityStateForProxy(p)
	now := time.Now()
	profile.Tunnel, profile.TunnelFactor, profile.TunnelSamples, profile.TunnelFresh =
		tunnelPathSnapshot(state, now, epoch)
	profile.TunnelAssessment = linkprofile.Assess(profile.Tunnel, profile.TunnelSamples)
	if !profile.TunnelFresh {
		profile.TunnelAssessment = linkprofile.Assess(linkprofile.TunnelMetrics{}, 0)
	}

	entry := &state.ipv4
	if ipv6 {
		entry = &state.ipv6
	}
	profile.Egress = egressPathSnapshot(entry, ipv6, now, epoch)

	state.udp.mu.Lock()
	profile.UDPKnown = state.udp.known && state.udp.epoch == epoch && now.Before(state.udp.expire)
	profile.UDPAvailable = profile.UDPKnown && state.udp.ok
	if profile.UDPKnown {
		profile.UDPEgressIP = state.udp.exitIP
	}
	state.udp.mu.Unlock()
	if profile.Egress.IP.IsValid() && profile.UDPEgressIP.IsValid() &&
		profile.Egress.IP.Is6() == profile.UDPEgressIP.Is6() &&
		profile.Egress.IP != profile.UDPEgressIP {
		// This is not automatically malicious: many providers use distinct TCP
		// and UDP NAT pools. It is still important fingerprint evidence and must
		// be visible instead of pretending the node has one universal exit.
		profile.TransportSplit = true
	}

	confidence := 0.0
	if profile.Local.Known {
		confidence += 0.10
	}
	if profile.TunnelFresh {
		confidence += 0.25
	}
	if profile.Egress.Fresh && profile.Egress.IP.IsValid() {
		confidence += 0.25
	}
	switch {
	case profile.Egress.Sources >= 2 && profile.Egress.Consistent:
		confidence += 0.25
	case profile.Egress.Sources >= 1:
		confidence += 0.10
	}
	if profile.Egress.Country != "" {
		confidence += 0.05
	}
	if profile.Egress.ASN != "" {
		confidence += 0.05
	}
	if profile.UDPKnown {
		confidence += 0.05
	}
	if profile.UDPEgressIP.IsValid() {
		confidence += 0.05
	}
	if profile.TransportSplit {
		confidence -= 0.05
	}
	if profile.Egress.Divergent {
		confidence -= 0.15
	}
	profile.Confidence = math.Max(0, math.Min(1, confidence))
	return profile
}
