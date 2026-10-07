package shadow

import (
	"context"
	"net/netip"
	"testing"

	"github.com/metacubex/mihomo/common/utils"
	"github.com/metacubex/mihomo/component/cfir"
	C "github.com/metacubex/mihomo/constant"
)

type fakeProxy struct {
	name       string
	kind       C.AdapterType
	udp        bool
	uot        bool
	l3         bool
	info       C.ProxyInfo
	next       C.Proxy
}

func (p *fakeProxy) Name() string { return p.name }
func (p *fakeProxy) Type() C.AdapterType { return p.kind }
func (p *fakeProxy) Addr() string { return "" }
func (p *fakeProxy) SupportUDP() bool { return p.udp }
func (p *fakeProxy) ProxyInfo() C.ProxyInfo { return p.info }
func (p *fakeProxy) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }
func (p *fakeProxy) DialContext(context.Context, *C.Metadata) (C.Conn, error) { return nil, C.ErrNotSupport }
func (p *fakeProxy) ListenPacketContext(context.Context, *C.Metadata) (C.PacketConn, error) { return nil, C.ErrNotSupport }
func (p *fakeProxy) SupportUOT() bool { return p.uot }
func (p *fakeProxy) IsL3Protocol(*C.Metadata) bool { return p.l3 }
func (p *fakeProxy) Unwrap(*C.Metadata, bool) C.Proxy { return p.next }
func (p *fakeProxy) Close() error { return nil }
func (p *fakeProxy) Adapter() C.ProxyAdapter { return p }
func (p *fakeProxy) AliveForTestUrl(string) bool { return true }
func (p *fakeProxy) DelayHistory() []C.DelayHistory { return nil }
func (p *fakeProxy) DelayHistoryForTestUrl(string) []C.DelayHistory { return nil }
func (p *fakeProxy) ExtraDelayHistories() map[string]C.ProxyState { return nil }
func (p *fakeProxy) LastDelayForTestUrl(string) uint16 { return 0 }
func (p *fakeProxy) URLTest(context.Context, string, utils.IntRanges[uint16]) (uint16, error) { return 0, C.ErrNotSupport }
func (p *fakeProxy) StatusTest(context.Context, string) (uint16, bool, error) { return 0, false, C.ErrNotSupport }

var _ C.Proxy = (*fakeProxy)(nil)

func decisionMetadata(network C.NetWork) *C.Metadata {
	return &C.Metadata{
		NetWork: network,
		SrcIP: netip.MustParseAddr("10.0.0.2"),
		SrcPort: 12345,
		DstIP: netip.MustParseAddr("203.0.113.4"),
		DstPort: 443,
	}
}

func TestDecisionShadowUnwrapsGroupWithoutConfusingItWithProtocol(t *testing.T) {
	leaf := &fakeProxy{name: "hy2-node", kind: C.Hysteria2, udp: true}
	group := &fakeProxy{name: "smart", kind: C.Smart, next: leaf}

	result, err := ObserveDecision(decisionMetadata(C.UDP), group, cfir.Generation{Network: 7})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Equal() {
		t.Fatalf("unexpected mismatch: 0x%x", uint64(result.Mismatch))
	}
	if result.Legacy.LogicalTarget != "smart" || result.Legacy.LeafName != "hy2-node" ||
		result.Legacy.Protocol != "hysteria2" {
		t.Fatalf("unexpected projection: %#v", result.Legacy)
	}
}

func TestDecisionShadowRejectsUDPLeafWithoutUDPSemantics(t *testing.T) {
	leaf := &fakeProxy{name: "tcp-node", kind: C.Vless, udp: false}
	result, err := ObserveDecision(decisionMetadata(C.UDP), leaf, cfir.Generation{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mismatch&DecisionMismatchPrimitive == 0 {
		t.Fatalf("expected primitive mismatch, got 0x%x", uint64(result.Mismatch))
	}
}

func TestDecisionShadowModelsDirectAsRouteActionNotProtocol(t *testing.T) {
	direct := &fakeProxy{name: "DIRECT", kind: C.Direct}
	result, err := ObserveDecision(decisionMetadata(C.TCP), direct, cfir.Generation{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Equal() || result.Legacy.Action != cfir.RouteActionDirect || result.Legacy.Protocol != "" {
		t.Fatalf("unexpected direct decision: %#v", result)
	}
}

