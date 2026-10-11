package shadow

import (
	"net"
	"net/netip"
	"testing"

	"github.com/metacubex/mihomo/component/cfir"
	C "github.com/metacubex/mihomo/constant"
)

func completeMetadata() *C.Metadata {
	return &C.Metadata{
		NetWork:       C.TCP,
		Type:          C.VLESS,
		SrcIP:         netip.MustParseAddr("10.0.0.2"),
		DstIP:         netip.MustParseAddr("203.0.113.9"),
		SrcGeoIP:      []string{},
		DstGeoIP:      []string{"US"},
		SrcIPASN:      "AS64500",
		DstIPASN:      "AS64501",
		SrcPort:       42001,
		DstPort:       443,
		InIP:          netip.MustParseAddr("10.0.0.1"),
		InPort:        7890,
		InName:        "tun0",
		InUser:        "user",
		RematchName:   "final-rule",
		Host:          "example.test",
		Uid:           10234,
		Process:       "browser",
		ProcessPath:   "/app/browser",
		SpecialProxy:  "AUTO",
		SpecialRules:  "ruleset",
		RemoteDst:     "edge.example.test",
		DSCP:          46,
		UUID:          "flow-1",
		SmartBlock:    "degraded",
		SmartTarget:   "example.test",
		WildcardTarget:"*.example.test",
		RawSrcAddr:    &net.TCPAddr{IP: net.ParseIP("10.0.0.2"), Port: 42001},
		RawDstAddr:    &net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 443},
		SniffHost:     "sniff.example.test",
	}
}

func TestObserveMetadataRoundTripIsSemanticallyLossless(t *testing.T) {
	metadata := completeMetadata()
	generation := cfir.Generation{Network: 9, Path: 4, Socket: 2}
	result, err := ObserveMetadata(metadata, generation)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Equal() {
		t.Fatalf("unexpected mismatch mask: 0x%x", uint64(result.Mismatch))
	}
	if result.Flow.Generation != generation {
		t.Fatalf("generation=%#v want %#v", result.Flow.Generation, generation)
	}
	if result.Flow.Security.AttestedByCore {
		t.Fatal("shadow bridge fabricated security attestation")
	}
	if result.RoundTrip.SrcGeoIP == nil {
		t.Fatal("empty-but-known GeoIP slice collapsed to nil")
	}
}

func TestNilAndEmptyGeoEvidenceAreDifferent(t *testing.T) {
	a := completeMetadata()
	b := completeMetadata()
	a.SrcGeoIP = nil
	b.SrcGeoIP = []string{}
	if got := CompareMetadata(a, b); got&MismatchGeoASN == 0 {
		t.Fatal("nil vs empty GeoIP must remain semantically distinct")
	}
}

func TestMismatchDomainsAreIndependent(t *testing.T) {
	a := completeMetadata()
	b := completeMetadata()
	b.Host = "other.test"
	b.Process = "other"
	b.DSCP = 0
	got := CompareMetadata(a, b)
	want := MismatchDestination | MismatchIdentity | MismatchRouting
	if got != want {
		t.Fatalf("mismatch=0x%x want=0x%x", uint64(got), uint64(want))
	}
}

func TestCountersTrackShadowHealth(t *testing.T) {
	var counters Counters
	ok, err := ObserveMetadata(completeMetadata(), cfir.Generation{})
	counters.Record(ok, err)
	bad := ok
	bad.Mismatch = MismatchDestination
	counters.Record(bad, nil)
	counters.Record(Result{}, net.ErrClosed)

	got := counters.Snapshot()
	if got.Observed != 3 || got.Matched != 1 || got.Mismatch != 1 || got.Errors != 1 {
		t.Fatalf("unexpected counters: %#v", got)
	}
}

func TestRoundTripCorpus(t *testing.T) {
	for i := 1; i <= 256; i++ {
		m := completeMetadata()
		m.SrcPort = uint16(10000 + i)
		m.DstPort = uint16(1 + (i * 251 % 65534))
		if i%2 == 0 {
			m.NetWork = C.UDP
			m.RawSrcAddr = &net.UDPAddr{IP: net.ParseIP("10.0.0.2"), Port: int(m.SrcPort)}
			m.RawDstAddr = &net.UDPAddr{IP: net.ParseIP("203.0.113.9"), Port: int(m.DstPort)}
		}
		if i%3 == 0 {
			m.Host = ""
		}
		if i%5 == 0 {
			m.DstGeoIP = nil
		} else if i%7 == 0 {
			m.DstGeoIP = []string{}
		}
		result, err := ObserveMetadata(m, cfir.Generation{Network: uint64(i)})
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if !result.Equal() {
			t.Fatalf("case %d mismatch=0x%x", i, uint64(result.Mismatch))
		}
	}
}
