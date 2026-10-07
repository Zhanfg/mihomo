package shadow

import (
	"testing"

	"github.com/metacubex/mihomo/component/cfir"
	C "github.com/metacubex/mihomo/constant"
)

func TestSnapshotRankerPreservesExactSmartOrderAfterLegalityFilter(t *testing.T) {
	illegal := &fakeProxy{name: "tcp-only", kind: C.Vless, udp: false}
	first := &fakeProxy{name: "hy2-a", kind: C.Hysteria2, udp: true}
	second := &fakeProxy{name: "hy2-b", kind: C.Hysteria2, udp: true}

	got, err := RankSnapshotThroughCFIR(
		decisionMetadata(C.UDP),
		[]C.Proxy{illegal, first, second},
		cfir.Generation{Network: 5},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ranked=%d want 2", len(got))
	}
	if got[0].Candidate.Protocol.Instance != "hy2-a" ||
		got[1].Candidate.Protocol.Instance != "hy2-b" {
		t.Fatalf("unexpected order: %#v", got)
	}
}

func TestSnapshotRankerNeverReintroducesIllegalCandidate(t *testing.T) {
	illegal := &fakeProxy{name: "tcp-only", kind: C.Vless, udp: false}
	got, err := RankSnapshotThroughCFIR(
		decisionMetadata(C.UDP),
		[]C.Proxy{illegal},
		cfir.Generation{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("illegal candidate survived CFIR rank bridge: %#v", got)
	}
}


func TestMaterializeSnapshotPlanCarriesSelectedInstance(t *testing.T) {
	illegal := &fakeProxy{name: "tcp-only", kind: C.Vless, udp: false}
	selected := &fakeProxy{name: "hy2-a", kind: C.Hysteria2, udp: true}
	backup := &fakeProxy{name: "hy2-b", kind: C.Hysteria2, udp: true}
	generation := cfir.Generation{Network: 9, Path: 3, Socket: 2}

	plan, err := MaterializeSnapshotPlan(
		decisionMetadata(C.UDP),
		[]C.Proxy{illegal, selected, backup},
		generation,
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Protocol != "hysteria2" || plan.Instance != "hy2-a" {
		t.Fatalf("unexpected plan target: %#v", plan)
	}
	if plan.Primitive != cfir.PrimitiveDatagram || plan.Generation != generation {
		t.Fatalf("unexpected plan semantics: %#v", plan)
	}
	if plan.Action != cfir.RouteActionForward {
		t.Fatalf("unexpected action: %v", plan.Action)
	}
}
