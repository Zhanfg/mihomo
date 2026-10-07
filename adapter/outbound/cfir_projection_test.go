package outbound

import (
	"testing"

	"github.com/metacubex/mihomo/component/cfir"
	C "github.com/metacubex/mihomo/constant"
)

func testProjectionBase(name string, kind C.AdapterType, udp bool) *Base {
	return NewBase(BaseOption{Name: name, Type: kind, UDP: udp})
}

func TestCFIRHysteria2ProjectionIsSelfDescribingButNotSecurityAttested(t *testing.T) {
	h := &Hysteria2{
		Base: testProjectionBase("hy2-node", C.Hysteria2, true),
		option: &Hysteria2Option{
			Ports: "443,8443",
			Obfs: "salamander",
			RealmOpts: Hysteria2RealmOption{Enable: true},
		},
	}
	projection := h.CFIRProtocolProjection()
	if err := projection.Validate(); err != nil {
		t.Fatal(err)
	}
	if projection.Family.ID != "hysteria2" || projection.Instance.Instance != "hy2-node" {
		t.Fatalf("unexpected projection identity: %#v", projection)
	}
	for _, primitive := range []cfir.Primitive{
		cfir.PrimitiveStream,
		cfir.PrimitiveDatagram,
		cfir.PrimitiveSession,
	} {
		if !projection.Instance.Primitives.Supports(primitive) {
			t.Fatalf("HY2 instance missing primitive %s", primitive)
		}
	}
	if !projection.Instance.Capabilities.HasStandard(cfir.CapabilityMultiplex) ||
		!projection.Instance.Capabilities.HasExtension("hysteria2.port-hopping") ||
		!projection.Instance.Capabilities.HasExtension("hysteria2.obfs.salamander") {
		t.Fatalf("HY2 instance capabilities incomplete: %#v", projection.Instance.Capabilities)
	}
	if projection.Instance.Security.AttestedByCore {
		t.Fatal("configured TLS must not be treated as completed runtime attestation")
	}
}

func TestCFIRVLESSProjectionKeepsUDPAtInstanceScope(t *testing.T) {
	tcpOnly := &Vless{
		Base: testProjectionBase("vless-tcp", C.Vless, false),
		option: &VlessOption{
			TLS: true,
			Network: "xhttp",
			PacketAddr: true,
		},
	}
	projection := tcpOnly.CFIRProtocolProjection()
	if err := projection.Validate(); err != nil {
		t.Fatal(err)
	}
	if !projection.Family.Primitives.Supports(cfir.PrimitiveDatagram) {
		t.Fatal("VLESS family should describe possible UDP support")
	}
	if projection.Instance.Primitives.Supports(cfir.PrimitiveDatagram) {
		t.Fatal("udp:false VLESS instance inherited family datagram support")
	}
	if !projection.Instance.Capabilities.HasStandard(cfir.CapabilityPacketAddress) {
		t.Fatal("packet-addr instance capability missing")
	}
	if !projection.Instance.Capabilities.HasExtension("vless.transport.xhttp") ||
		!projection.Instance.Capabilities.HasExtension("vless.security.tls-configured") {
		t.Fatalf("VLESS configured extensions missing: %#v", projection.Instance.Capabilities.Extensions())
	}
	if projection.Instance.Security.AttestedByCore {
		t.Fatal("VLESS TLS configuration must not self-attest runtime security")
	}

	udp := &Vless{
		Base: testProjectionBase("vless-udp", C.Vless, true),
		option: &VlessOption{UDP: true, XUDP: true},
	}
	udpProjection := udp.CFIRProtocolProjection()
	if err := udpProjection.Validate(); err != nil {
		t.Fatal(err)
	}
	if !udpProjection.Instance.Primitives.Supports(cfir.PrimitiveDatagram) ||
		!udpProjection.Instance.Capabilities.HasExtension("vless.xudp") {
		t.Fatalf("VLESS UDP instance evidence missing: %#v", udpProjection.Instance)
	}
}

func TestCFIRWireGuardProjectionPreservesL3AndOptionalUDP(t *testing.T) {
	wg := &WireGuard{
		Base: testProjectionBase("wg-node", C.WireGuard, false),
		option: WireGuardOption{
			IPStack: IPStackOption{Mode: ipStackMips, CongestionController: "bbr3"},
			RemoteDnsResolve: true,
			Dns: []string{"1.1.1.1"},
		},
	}
	projection := wg.CFIRProtocolProjection()
	if err := projection.Validate(); err != nil {
		t.Fatal(err)
	}
	if !projection.Instance.Primitives.Supports(cfir.PrimitivePacket) ||
		!projection.Instance.Primitives.Supports(cfir.PrimitiveStream) {
		t.Fatalf("WireGuard L3/stream evidence missing: %#v", projection.Instance.Primitives)
	}
	if projection.Instance.Primitives.Supports(cfir.PrimitiveDatagram) {
		t.Fatal("udp:false WireGuard instance inherited datagram support")
	}
	if !projection.Instance.Capabilities.HasStandard(cfir.CapabilityCongestionControl) ||
		!projection.Instance.Capabilities.HasStandard(cfir.CapabilityNativeResolve) ||
		!projection.Instance.Capabilities.HasExtension("wireguard.ip-stack.mips") {
		t.Fatalf("WireGuard instance capability projection incomplete: %#v", projection.Instance.Capabilities)
	}
	if projection.Instance.Security.AttestedByCore {
		t.Fatal("WireGuard configuration alone must not claim completed handshake attestation")
	}
}

func TestCFIRMasqueProjectionDifferentiatesH2AndH3L4(t *testing.T) {
	h2 := &Masque{
		Base: testProjectionBase("masque-h2", C.Masque, true),
		option: MasqueOption{Network: "h2", UDP: true},
	}
	h2Projection := h2.CFIRProtocolProjection()
	if err := h2Projection.Validate(); err != nil {
		t.Fatal(err)
	}
	if !h2Projection.Instance.Capabilities.HasExtension("masque.transport.h2") {
		t.Fatal("MASQUE H2 transport evidence missing")
	}
	if h2Projection.Instance.Capabilities.HasStandard(cfir.CapabilityCongestionControl) {
		t.Fatal("H2 MASQUE must not inherit QUIC congestion-controller configuration")
	}

	h3 := &Masque{
		Base: testProjectionBase("masque-h3-l4", C.Masque, false),
		option: MasqueOption{
			Network: "h3-l4proxy",
			CongestionController: "bbr",
			IPStack: IPStackOption{Mode: ipStackMips},
		},
	}
	h3Projection := h3.CFIRProtocolProjection()
	if err := h3Projection.Validate(); err != nil {
		t.Fatal(err)
	}
	if !h3Projection.Instance.Capabilities.HasExtension("masque.transport.h3") ||
		!h3Projection.Instance.Capabilities.HasExtension("masque.mode.h3-l4proxy") ||
		!h3Projection.Instance.Capabilities.HasStandard(cfir.CapabilityCongestionControl) {
		t.Fatalf("MASQUE H3 L4 evidence incomplete: %#v", h3Projection.Instance.Capabilities)
	}
	if !h3Projection.Instance.Primitives.Supports(cfir.PrimitivePacket) ||
		!h3Projection.Instance.Primitives.Supports(cfir.PrimitiveSession) {
		t.Fatalf("MASQUE packet/session semantics missing: %#v", h3Projection.Instance.Primitives)
	}
	if h3Projection.Instance.Security.AttestedByCore {
		t.Fatal("MASQUE TLS configuration must not self-attest negotiated security")
	}
}
