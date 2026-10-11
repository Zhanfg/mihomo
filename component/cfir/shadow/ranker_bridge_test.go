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


func TestScoredSnapshotCarriesRealSmartUtilityStages(t *testing.T) {
	first := &fakeProxy{name: "hy2-a", kind: C.Hysteria2, udp: true}
	second := &fakeProxy{name: "hy2-b", kind: C.Hysteria2, udp: true}
	scores := map[string]cfir.CandidateScore{
		"hy2-a": {
			LearnedUtility:  0.72,
			ExploredUtility: 0.78,
			Utility:         0.74,
			Confidence:      0.80,
			Uncertainty:     0.20,
		},
		"hy2-b": {
			LearnedUtility:  0.61,
			ExploredUtility: 0.66,
			Utility:         0.63,
			Confidence:      0.65,
			Uncertainty:     0.35,
		},
	}

	got, err := RankSnapshotThroughCFIRWithScores(
		decisionMetadata(C.UDP),
		[]C.Proxy{first, second},
		cfir.Generation{Network: 6},
		scores,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Candidate.Protocol.Instance != "hy2-a" {
		t.Fatalf("unexpected scored order: %#v", got)
	}
	if got[0].Score.LearnedUtility != 0.72 ||
		got[0].Score.ExploredUtility != 0.78 ||
		got[0].Score.Utility != 0.74 ||
		got[0].Score.Uncertainty != 0.20 {
		t.Fatalf("Smart score stages were not preserved: %#v", got[0].Score)
	}
}

func TestScoredSnapshotRejectsPartialScoreCoverage(t *testing.T) {
	first := &fakeProxy{name: "hy2-a", kind: C.Hysteria2, udp: true}
	second := &fakeProxy{name: "hy2-b", kind: C.Hysteria2, udp: true}

	_, err := RankSnapshotThroughCFIRWithScores(
		decisionMetadata(C.UDP),
		[]C.Proxy{first, second},
		cfir.Generation{},
		map[string]cfir.CandidateScore{
			"hy2-a": {
				LearnedUtility:  0.7,
				ExploredUtility: 0.7,
				Utility:         0.7,
				Confidence:      0.8,
				Uncertainty:     0.2,
			},
		},
	)
	if err == nil {
		t.Fatal("partial real-score snapshot must not mix with synthetic rank scores")
	}
}

func TestScoredSnapshotUsesFinalUtilityForRanking(t *testing.T) {
	first := &fakeProxy{name: "hy2-a", kind: C.Hysteria2, udp: true}
	second := &fakeProxy{name: "hy2-b", kind: C.Hysteria2, udp: true}
	scores := map[string]cfir.CandidateScore{
		"hy2-a": {
			LearnedUtility:  0.90,
			ExploredUtility: 0.90,
			Utility:         0.60,
			Confidence:      0.9,
			Uncertainty:     0.1,
		},
		"hy2-b": {
			LearnedUtility:  0.70,
			ExploredUtility: 0.82,
			Utility:         0.75,
			Confidence:      0.7,
			Uncertainty:     0.3,
		},
	}

	got, err := RankSnapshotThroughCFIRWithScores(
		decisionMetadata(C.UDP),
		[]C.Proxy{first, second},
		cfir.Generation{},
		scores,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Candidate.Protocol.Instance != "hy2-b" {
		t.Fatalf("final request utility did not control Ranker order: %#v", got)
	}
}
