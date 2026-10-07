package legacybridge

import (
	"net/netip"
	"reflect"
	"testing"

	"github.com/metacubex/mihomo/component/cfir"
	C "github.com/metacubex/mihomo/constant"
)

func TestMetadataRoundTripPreservesLegacySemantics(t *testing.T) {
	original := &C.Metadata{
		NetWork:       C.TCP,
		Type:          C.VLESS,
		SrcIP:         netip.MustParseAddr("10.0.0.2"),
		SrcPort:       43123,
		DstIP:         netip.MustParseAddr("203.0.113.8"),
		DstPort:       443,
		Host:          "example.test",
		InName:        "tun0",
		InUser:        "user",
		Uid:           10234,
		Process:       "browser",
		ProcessPath:   "/app/browser",
		SpecialProxy:  "AUTO",
		SpecialRules:  "rule-set",
		RemoteDst:     "edge.example.test",
		DSCP:          46,
		SniffHost:     "sniff.example.test",
		SrcIPASN:      "AS64500",
		DstIPASN:      "AS64501",
		SrcGeoIP:      []string{"PRIVATE"},
		DstGeoIP:      []string{"US"},
		SmartBlock:    "",
		SmartTarget:   "example.test",
		WildcardTarget: "*.example.test",
	}
	generation := cfir.Generation{Network: 7, Path: 3, Socket: 11}

	flow, err := FromMetadata(original, generation)
	if err != nil {
		t.Fatal(err)
	}
	if flow.Generation != generation {
		t.Fatalf("generation lost: %#v", flow.Generation)
	}
	if flow.Security.AttestedByCore {
		t.Fatal("legacy bridge must not fabricate a security attestation")
	}

	roundTrip, err := ToMetadata(flow)
	if err != nil {
		t.Fatal(err)
	}
	if roundTrip.NetWork != original.NetWork ||
		roundTrip.Type != original.Type ||
		roundTrip.SrcIP != original.SrcIP ||
		roundTrip.DstIP != original.DstIP ||
		roundTrip.SrcPort != original.SrcPort ||
		roundTrip.DstPort != original.DstPort ||
		roundTrip.Host != original.Host ||
		roundTrip.Uid != original.Uid ||
		roundTrip.Process != original.Process ||
		roundTrip.ProcessPath != original.ProcessPath ||
		roundTrip.DSCP != original.DSCP ||
		roundTrip.SniffHost != original.SniffHost ||
		roundTrip.SmartTarget != original.SmartTarget ||
		roundTrip.WildcardTarget != original.WildcardTarget {
		t.Fatalf("metadata round-trip mismatch:\nwant=%#v\ngot=%#v", original, roundTrip)
	}
	if !reflect.DeepEqual(roundTrip.SrcGeoIP, original.SrcGeoIP) ||
		!reflect.DeepEqual(roundTrip.DstGeoIP, original.DstGeoIP) {
		t.Fatal("derived geo metadata was not preserved by extension round-trip")
	}
}

func TestMetadataUDPMapsToDatagramPrimitive(t *testing.T) {
	metadata := &C.Metadata{
		NetWork: C.UDP,
		DstIP:   netip.MustParseAddr("198.51.100.2"),
		DstPort: 53,
	}
	flow, err := FromMetadata(metadata, cfir.Generation{})
	if err != nil {
		t.Fatal(err)
	}
	if flow.Primitive != cfir.PrimitiveDatagram {
		t.Fatalf("primitive=%v want datagram", flow.Primitive)
	}
}
