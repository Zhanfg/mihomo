package cfir

import (
	"context"
	"testing"
)

type testRanker struct {
	score map[string]CandidateScore
}

func (r testRanker) ScoreCandidates(_ context.Context, _ ExecutionIntent, candidates []PlanCandidate) ([]ScoredCandidate, error) {
	result := make([]ScoredCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, ScoredCandidate{Candidate: candidate, Score: r.score[candidate.Key()]})
	}
	return result, nil
}

type injectingRanker struct{}

func (injectingRanker) ScoreCandidates(_ context.Context, _ ExecutionIntent, candidates []PlanCandidate) ([]ScoredCandidate, error) {
	result := make([]ScoredCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, ScoredCandidate{Candidate: candidate, Score: CandidateScore{Utility: 1}})
	}
	if len(candidates) > 0 {
		illegal := candidates[0]
		illegal.Protocol.Instance = "injected"
		result[0].Candidate = illegal
	}
	return result, nil
}

func TestRankerCannotReintroduceRejectedCandidate(t *testing.T) {
	candidate := PlanCandidate{
		Protocol: ProtocolCandidate{Protocol: "p", Instance: "node"},
	}
	_, err := RankPreparedCandidates(
		context.Background(),
		ExecutionIntent{Action: RouteActionForward, Primitive: PrimitiveStream},
		injectingRanker{},
		[]PlanCandidate{candidate},
	)
	if err == nil {
		t.Fatal("ranker must not be able to introduce an unapproved candidate")
	}
}

func TestRankPreparedCandidatesUsesDeterministicUtilityConfidenceUncertaintyOrder(t *testing.T) {
	a := PlanCandidate{Protocol: ProtocolCandidate{Protocol: "p", Instance: "a"}}
	b := PlanCandidate{Protocol: ProtocolCandidate{Protocol: "p", Instance: "b"}}
	c := PlanCandidate{Protocol: ProtocolCandidate{Protocol: "p", Instance: "c"}}
	ranker := testRanker{score: map[string]CandidateScore{
		a.Key(): {Utility: 1, Confidence: 0.8, Uncertainty: 0.1},
		b.Key(): {Utility: 2, Confidence: 0.5, Uncertainty: 0.4},
		c.Key(): {Utility: 1, Confidence: 0.9, Uncertainty: 0.8},
	}}
	got, err := RankPreparedCandidates(
		context.Background(),
		ExecutionIntent{Action: RouteActionForward, Primitive: PrimitiveStream},
		ranker,
		[]PlanCandidate{a, b, c},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Candidate.Protocol.Instance != "b" ||
		got[1].Candidate.Protocol.Instance != "c" ||
		got[2].Candidate.Protocol.Instance != "a" {
		t.Fatalf("unexpected order: %#v", got)
	}
}


type capturingRanker struct {
	seen []PlanCandidate
}

func (r *capturingRanker) ScoreCandidates(_ context.Context, _ ExecutionIntent, candidates []PlanCandidate) ([]ScoredCandidate, error) {
	r.seen = append(r.seen[:0], candidates...)
	out := make([]ScoredCandidate, 0, len(candidates))
	for i, candidate := range candidates {
		out = append(out, ScoredCandidate{
			Candidate: candidate,
			Score: CandidateScore{
				Utility: float64(len(candidates) - i),
				Confidence: 0.5,
				Uncertainty: 0.5,
			},
		})
	}
	return out, nil
}

func TestRegistryRankCandidatesNeverExposesRejectedCandidate(t *testing.T) {
	registry := NewRegistry()
	caps := NewCapabilitySet(CapabilityPathMigration)
	for _, descriptor := range []ProtocolDescriptor{
		{
			ID: "quic-ok", MinCFIR: CurrentVersion,
			Primitives: PrimitiveSet(PrimitiveDatagram),
			Capabilities: caps,
		},
		{
			ID: "tcp-only", MinCFIR: CurrentVersion,
			Primitives: PrimitiveSet(PrimitiveStream),
			Capabilities: caps,
		},
	} {
		if err := registry.RegisterProtocol(testProtocol{descriptor: descriptor}); err != nil {
			t.Fatal(err)
		}
	}

	intent := ExecutionIntent{
		Action: RouteActionForward,
		Primitive: PrimitiveDatagram,
		Requirements: CapabilityRequirement{Standard: []StandardCapability{
			CapabilityPathMigration,
		}},
	}

	valid := PlanCandidate{Protocol: ProtocolCandidate{
		Protocol: "quic-ok",
		Instance: "good",
		Capabilities: caps,
	}}
	invalid := PlanCandidate{Protocol: ProtocolCandidate{
		Protocol: "tcp-only",
		Instance: "bad",
		Capabilities: caps,
	}}

	ranker := &capturingRanker{}
	got, err := registry.RankCandidates(context.Background(), intent, ranker, []PlanCandidate{invalid, valid})
	if err != nil {
		t.Fatal(err)
	}
	if len(ranker.seen) != 1 || ranker.seen[0].Protocol.Instance != "good" {
		t.Fatalf("ranker saw rejected candidate: %#v", ranker.seen)
	}
	if len(got) != 1 || got[0].Candidate.Protocol.Instance != "good" {
		t.Fatalf("unexpected ranked result: %#v", got)
	}
}
