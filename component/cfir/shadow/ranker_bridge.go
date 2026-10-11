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
// Ranker ABI without asking Smart to make a second decision. Callers that do
// not have score evidence get a strictly order-preserving fallback score.
func RankSnapshotThroughCFIR(metadata *C.Metadata, ranked []C.Proxy, generation cfir.Generation) ([]cfir.ScoredCandidate, error) {
	return RankSnapshotThroughCFIRWithScores(metadata, ranked, generation, nil)
}

// RankSnapshotThroughCFIRWithScores consumes scores produced by the same live
// Smart selection pass. The map is keyed by concrete proxy instance name.
// A non-nil score snapshot must cover every admitted candidate: mixing learned
// utilities with fabricated rank utilities would put unlike scales in one
// ordering. Call RankSnapshotThroughCFIR for pin/fallback/rank-only snapshots.
func RankSnapshotThroughCFIRWithScores(
	metadata *C.Metadata,
	ranked []C.Proxy,
	generation cfir.Generation,
	instanceScores map[string]cfir.CandidateScore,
) ([]cfir.ScoredCandidate, error) {
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
		candidate := cfir.PlanCandidate{Protocol: decision.Instance}
		key := candidate.Key()
		if _, exists := scores[key]; exists {
			continue
		}

		var score cfir.CandidateScore
		if instanceScores == nil {
			score = cfir.CandidateScore{
				Utility:             total - float64(index),
				Confidence:          0.5,
				Uncertainty:         0.5,
				ResidualUncertainty: 1,
			}
		} else {
			var ok bool
			score, ok = instanceScores[decision.LeafName]
			if !ok {
				return nil, fmt.Errorf(
					"cfir shadow: scored snapshot is incomplete; missing %s",
					decision.LeafName,
				)
			}
		}
		if err := score.Validate(); err != nil {
			return nil, fmt.Errorf("cfir shadow: invalid Smart score for %s: %w", decision.LeafName, err)
		}
		candidates = append(candidates, candidate)
		scores[key] = score
	}
	return cfir.RankPreparedCandidates(context.Background(), intent, SnapshotRanker{scores: scores}, candidates)
}


type shadowProtocolAdapter struct {
	descriptor cfir.ProtocolDescriptor
}

func (a shadowProtocolAdapter) Descriptor() cfir.ProtocolDescriptor {
	return a.descriptor
}

func MaterializeSnapshotPlan(metadata *C.Metadata, ranked []C.Proxy, generation cfir.Generation) (cfir.ExecutionPlan, error) {
	scored, err := RankSnapshotThroughCFIR(metadata, ranked, generation)
	if err != nil {
		return cfir.ExecutionPlan{}, err
	}
	if len(scored) == 0 {
		return cfir.ExecutionPlan{}, nil
	}
	return materializeRankedSnapshotPlan(metadata, ranked, generation, scored)
}

func materializeRankedSnapshotPlan(
	metadata *C.Metadata,
	ranked []C.Proxy,
	generation cfir.Generation,
	scored []cfir.ScoredCandidate,
) (cfir.ExecutionPlan, error) {
	selected := scored[0].Candidate.Protocol
	var selectedDecision legacybridge.LegacyDecision
	for _, proxy := range ranked {
		decision, err := legacybridge.ProjectProxyDecision(proxy, metadata)
		if err != nil {
			return cfir.ExecutionPlan{}, err
		}
		if decision.Resolved &&
			decision.Protocol == selected.Protocol &&
			decision.LeafName == selected.Instance {
			selectedDecision = decision
			break
		}
	}
	if !selectedDecision.Resolved {
		return cfir.ExecutionPlan{}, fmt.Errorf(
			"cfir shadow: selected candidate %s/%s has no legacy projection",
			selected.Protocol, selected.Instance,
		)
	}

	intent, err := legacybridge.IntentForLegacyDecision(metadata, generation, cfir.RouteActionForward)
	if err != nil {
		return cfir.ExecutionPlan{}, err
	}
	if platform, platformErr := cfir.RuntimePlatform(); platformErr == nil {
		intent.Platform = platform
	}

	registry := cfir.NewRegistry()
	if err := registry.RegisterProtocol(shadowProtocolAdapter{descriptor: selectedDecision.Family}); err != nil {
		return cfir.ExecutionPlan{}, err
	}
	return registry.MaterializeExecutionPlan(intent, scored[0])
}


func MaterializeSnapshotPlanWithScores(
	metadata *C.Metadata,
	ranked []C.Proxy,
	generation cfir.Generation,
	instanceScores map[string]cfir.CandidateScore,
) (cfir.ExecutionPlan, error) {
	scored, err := RankSnapshotThroughCFIRWithScores(metadata, ranked, generation, instanceScores)
	if err != nil {
		return cfir.ExecutionPlan{}, err
	}
	if len(scored) == 0 {
		return cfir.ExecutionPlan{}, nil
	}
	return materializeRankedSnapshotPlan(metadata, ranked, generation, scored)
}
