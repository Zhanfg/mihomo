package adapter

import (
	"net/netip"
	"testing"
	"time"

	C "github.com/metacubex/mihomo/constant"
)

type vectorStubProxy struct {
	stubProxy
	history []C.DelayHistory
	alive   bool
	udp     bool
}

func (p vectorStubProxy) AliveForTestUrl(string) bool { return p.alive }
func (p vectorStubProxy) DelayHistoryForTestUrl(string) []C.DelayHistory {
	return p.history
}
func (p vectorStubProxy) SupportUDP() bool { return p.udp }

func TestNodeVectorScorePrefersMatchingIPFamily(t *testing.T) {
	base := NodeVectorProfile{}
	for i := range base.Vector {
		base.Vector[i] = 0.8
	}
	v4 := base
	v4.Vector[vectorIPv4] = 1
	v4.Vector[vectorIPv6] = 0
	v6 := base
	v6.Vector[vectorIPv4] = 0
	v6.Vector[vectorIPv6] = 1

	meta6 := &C.Metadata{DstIP: netip.MustParseAddr("2606:4700:4700::1111"), NetWork: C.TCP}
	if got6, got4 := NodeVectorScore(v6, meta6), NodeVectorScore(v4, meta6); got6 <= got4 {
		t.Fatalf("IPv6 target must prefer IPv6 profile: v6=%f v4=%f", got6, got4)
	}

	meta4 := &C.Metadata{DstIP: netip.MustParseAddr("1.1.1.1"), NetWork: C.TCP}
	if got4, got6 := NodeVectorScore(v4, meta4), NodeVectorScore(v6, meta4); got4 <= got6 {
		t.Fatalf("IPv4 target must prefer IPv4 profile: v4=%f v6=%f", got4, got6)
	}
}

func TestBuildNodeVectorReusesCapabilityMetadata(t *testing.T) {
	capabilityCache.Range(func(k, _ any) bool {
		capabilityCache.Delete(k)
		return true
	})

	p := vectorStubProxy{
		stubProxy: stubProxy{name: "node", type_: C.Http, provider: "main-a"},
		alive:     true,
		udp:       true,
		history: []C.DelayHistory{
			{Time: time.Now(), Delay: 48},
			{Time: time.Now(), Delay: 52},
			{Time: time.Now(), Delay: 50},
		},
	}
	state := capabilityStateForProxy(p)
	seedEntry := func(e *capabilityEntry, ip, country, asn, vendor string) {
		e.mu.Lock()
		e.known = true
		e.ok = true
		e.expire = time.Now().Add(time.Hour)
		e.exitIP = netip.MustParseAddr(ip)
		e.country = country
		e.asn = asn
		e.vendor = vendor
		e.mu.Unlock()
	}
	seedEntry(&state.ipv4, "1.1.1.1", "US", "13335", "Cloudflare")
	seedEntry(&state.ipv6, "2606:4700:4700::1111", "US", "13335", "Cloudflare")
	state.udp.mu.Lock()
	state.udp.known = true
	state.udp.ok = true
	state.udp.expire = time.Now().Add(time.Hour)
	state.udp.mu.Unlock()

	profile := BuildNodeVector(p, "https://example.test", 1.0)
	if profile.Protocol != C.Http.String() || profile.Provider != "main-a" {
		t.Fatalf("unexpected categorical metadata: protocol=%q provider=%q", profile.Protocol, profile.Provider)
	}
	if profile.Country4 != "US" || profile.Country6 != "US" ||
		profile.ASN4 != "13335" || profile.ASN6 != "13335" ||
		profile.Vendor4 != "Cloudflare" || profile.Vendor6 != "Cloudflare" {
		t.Fatalf("capability metadata was not reused: %+v", profile)
	}
	if profile.Vector[vectorFamilyCountryConsistency] != 1 ||
		profile.Vector[vectorFamilyNetworkConsistency] != 1 {
		t.Fatalf("same-country/same-ASN dual stack must score as consistent: %+v", profile.Vector)
	}
	if profile.Vector[vectorReliability] <= 0.5 ||
		profile.Vector[vectorLatency] <= 0.8 ||
		profile.Vector[vectorStability] <= 0.8 {
		t.Fatalf("healthy low-jitter history should produce a strong vector: %+v", profile.Vector)
	}
}

func TestNodeVectorScoreRewardsUDPForUDPContext(t *testing.T) {
	base := NodeVectorProfile{}
	for i := range base.Vector {
		base.Vector[i] = 0.7
	}
	noUDP := base
	noUDP.Vector[vectorUDP] = 0
	withUDP := base
	withUDP.Vector[vectorUDP] = 1

	meta := &C.Metadata{NetWork: C.UDP}
	if good, bad := NodeVectorScore(withUDP, meta), NodeVectorScore(noUDP, meta); good <= bad {
		t.Fatalf("UDP context must reward measured UDP capability: good=%f bad=%f", good, bad)
	}
}
