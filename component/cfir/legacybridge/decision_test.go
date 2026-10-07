package legacybridge

import (
	"testing"

	"github.com/metacubex/mihomo/component/cfir"
	C "github.com/metacubex/mihomo/constant"
)

func TestAdapterTypeMappingSeparatesRouteActionFromWireProtocol(t *testing.T) {
	for _, tc := range []struct {
		adapter C.AdapterType
		action  cfir.RouteAction
		proto   cfir.ProtocolID
	}{
		{C.Direct, cfir.RouteActionDirect, ""},
		{C.Reject, cfir.RouteActionReject, ""},
		{C.Dns, cfir.RouteActionDNS, ""},
		{C.Vless, cfir.RouteActionForward, "vless"},
		{C.Hysteria2, cfir.RouteActionForward, "hysteria2"},
		{C.Masque, cfir.RouteActionForward, "masque"},
		{C.WireGuard, cfir.RouteActionForward, "wireguard"},
	} {
		if got := RouteActionForAdapterType(tc.adapter); got != tc.action {
			t.Fatalf("%s action=%v want=%v", tc.adapter, got, tc.action)
		}
		got, ok := ProtocolIDForAdapterType(tc.adapter)
		if tc.proto == "" {
			if ok {
				t.Fatalf("%s unexpectedly mapped to protocol %s", tc.adapter, got)
			}
		} else if !ok || got != tc.proto {
			t.Fatalf("%s protocol=%q ok=%v want=%q", tc.adapter, got, ok, tc.proto)
		}
	}
}

func TestLegacyDescriptorIsConservativeAndCapabilityDriven(t *testing.T) {
	descriptor, ok := DescriptorForLegacyLeaf(
		C.Hysteria2,
		true,
		true,
		false,
		C.ProxyInfo{SMUX: true, TFO: true, MPTCP: true},
	)
	if !ok {
		t.Fatal("known adapter was not projected")
	}
	if !descriptor.Primitives.Supports(cfir.PrimitiveStream) ||
		!descriptor.Primitives.Supports(cfir.PrimitiveDatagram) {
		t.Fatalf("unexpected primitives: %v", descriptor.Primitives)
	}
	if !descriptor.Capabilities.HasStandard(cfir.CapabilityMultiplex) {
		t.Fatal("SMUX capability not preserved")
	}
	if descriptor.Security != (cfir.SecurityProfile{}) {
		t.Fatalf("legacy adapter name must not fabricate security evidence: %#v", descriptor.Security)
	}
	extension, _ := cfir.ParseExtensionID("mihomo.uot.reliable-datagram")
	if !descriptor.Capabilities.HasExtension(extension) {
		t.Fatal("UOT extension capability not projected")
	}
}

func TestIntentUsesFlowPrimitiveRatherThanProtocolName(t *testing.T) {
	metadata := &C.Metadata{NetWork: C.UDP}
	intent, err := IntentForLegacyDecision(metadata, cfir.Generation{Network: 4}, cfir.RouteActionForward)
	if err != nil {
		t.Fatal(err)
	}
	if intent.Primitive != cfir.PrimitiveDatagram || intent.Generation.Network != 4 {
		t.Fatalf("unexpected intent: %#v", intent)
	}
}
