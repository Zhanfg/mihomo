package shadow

import (
	"testing"

	"github.com/metacubex/mihomo/component/cfir"
	C "github.com/metacubex/mihomo/constant"
)

func TestExactRankingShadowKeepsLegalWinner(t *testing.T) {
	first := &fakeProxy{name: "hy2-a", kind: C.Hysteria2, udp: true}
	second := &fakeProxy{name: "hy2-b", kind: C.Hysteria2, udp: true}
	result, err := ObserveRankedCandidates(
		decisionMetadata(C.UDP),
		[]C.Proxy{first, second},
		cfir.Generation{Network: 3},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Equal() || result.EligibleCount != 2 || result.PlannerFirst.LeafName != "hy2-a" {
		t.Fatalf("unexpected ranking result: %#v", result)
	}
}

func TestExactRankingShadowFiltersIllegalWinnerWithoutRerankingSmart(t *testing.T) {
	illegal := &fakeProxy{name: "tcp-only", kind: C.Vless, udp: false}
	legal := &fakeProxy{name: "hy2", kind: C.Hysteria2, udp: true}
	result, err := ObserveRankedCandidates(
		decisionMetadata(C.UDP),
		[]C.Proxy{illegal, legal},
		cfir.Generation{},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := RankingMismatchLegacyFirstIneligible | RankingMismatchFilteredWinnerChanged
	if result.Mismatch&want != want {
		t.Fatalf("mismatch=0x%x want flags=0x%x", uint64(result.Mismatch), uint64(want))
	}
	if result.PlannerFirst.LeafName != "hy2" || result.EligibleCount != 1 {
		t.Fatalf("unexpected planner result: %#v", result)
	}
}
