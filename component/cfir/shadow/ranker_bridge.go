package shadow

import (
	"context"
	"fmt"

	"github.com/metacubex/mihomo/component/cfir"
	"github.com/metacubex/mihomo/component/cfir/legacybridge"
	C "github.com/metacubex/mihomo/constant"
)

type SnapshotRanker struct {
	scores map[string]cfir.CandidateScore
}

func (r SnapshotRanker) ScoreCandidates(_ context.Context, _ cfir.ExecutionIntent, candidates []cfir.PlanCandidate) ([]cfir.ScoredCandidate, error) {
	result := make([]cfir.ScoredCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		score, ok := r.scores[candidate.Key()]
		if !ok {
			return nil, fmt.Errorf("cfir shadow: missing snapshot score for %q", candidate.Key())
		}
		result = append(result, cfir.ScoredCandidate{Candidate: candidate, Score: score})
	}
	return result, nil
}

// RankSnapshotThroughCFIR converts the exact live Smart order into the generic
// Ranker ABI without asking Smart to make a second decision. Utility is an
// order-preserving shadow score only; it is not yet Smart's internal model
// weight. The purpose is to prove candidate identity/legality/order parity
// before extracting real model utility into a pure scorer.
func RankSnapshotThroughCFIR(metadata *C.Metadata, ranked []C.Proxy, generation cfir.Generation) ([]cfir.ScoredCandidate, error) {
	intent, err := legacybridge.IntentForLegacyDecision(metadata, generation, cfir.RouteActionForward)
	if err != nil {
		return nil, err
	}
	if len(ranked) == 0 {
		return nil, nil
	}

	candidates := make([]cfir.PlanCandidate, 0, len(ranked))
	scores := make(map[string]cfir.CandidateScore, len(ranked))
	total := float64(len(ranked))
	for index, proxy := range ranked {
		decision, err := legacybridge.ProjectProxyDecision(proxy, metadata)
		if err != nil {
			return nil, err
		}
		if !decisionEligible(intent, decision) {
			continue
		}
		candidate := cfir.PlanCandidate{
			Protocol: decision.Instance,
		}
		key := candidate.Key()
		if _, exists := scores[key]; exists {
			continue
		}
		// Strictly monotonic utility preserves the actual Smart snapshot order.
		// Confidence/uncertainty remain neutral until Smart exports real values.
		score := cfir.CandidateScore{
			Utility:    total - float64(index),
			Confidence: 0.5,
			Uncertainty: 0.5,
		}
		candidates = append(candidates, candidate)
		scores[key] = score
	}
	return cfir.RankPreparedCandidates(context.Background(), intent, SnapshotRanker{scores: scores}, candidates)
}
