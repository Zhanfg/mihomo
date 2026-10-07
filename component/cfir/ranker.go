package cfir

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
)

type PlanCandidate struct {
	Protocol  ProtocolCandidate
	Backend   string
	Transport TransportPolicy
}

func (c PlanCandidate) Key() string {
	return string(c.Protocol.Protocol) + "\x00" + c.Protocol.Instance + "\x00" + c.Backend
}

type CandidateScore struct {
	Utility     float64
	Confidence  float64
	Uncertainty float64
}

func (s CandidateScore) Validate() error {
	if math.IsNaN(s.Utility) || math.IsInf(s.Utility, 0) {
		return errors.New("cfir: candidate utility must be finite")
	}
	if math.IsNaN(s.Confidence) || math.IsInf(s.Confidence, 0) || s.Confidence < 0 || s.Confidence > 1 {
		return errors.New("cfir: candidate confidence must be finite and within [0,1]")
	}
	if math.IsNaN(s.Uncertainty) || math.IsInf(s.Uncertainty, 0) || s.Uncertainty < 0 || s.Uncertainty > 1 {
		return errors.New("cfir: candidate uncertainty must be finite and within [0,1]")
	}
	return nil
}

type ScoredCandidate struct {
	Candidate PlanCandidate
	Score     CandidateScore
}

// Ranker is intentionally narrower than Smart's internal model. It receives
// only candidates already admitted by CFIR and cannot re-introduce rejected
// protocols/backends.
type Ranker interface {
	ScoreCandidates(ctx context.Context, intent ExecutionIntent, candidates []PlanCandidate) ([]ScoredCandidate, error)
}

func (r *Registry) PreparePlanCandidates(intent ExecutionIntent, candidates []PlanCandidate) ([]PlanCandidate, error) {
	if err := intent.Validate(); err != nil {
		return nil, err
	}
	if intent.Action != RouteActionForward {
		return nil, nil
	}

	result := make([]PlanCandidate, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if _, err := r.ValidateProtocolCandidate(intent, candidate.Protocol); err != nil {
			continue
		}
		if candidate.Backend != "" {
			backend, ok := r.Backend(candidate.Backend)
			if !ok {
				continue
			}
			descriptor := backend.Descriptor()
			if intent.Platform != PlatformInvalid && !descriptor.Platforms.Supports(intent.Platform) {
				continue
			}
			if !intent.BackendRequirements.SatisfiedBy(descriptor.Capabilities) {
				continue
			}
		} else if len(intent.BackendRequirements.Standard) > 0 || len(intent.BackendRequirements.Extensions) > 0 {
			continue
		}
		key := candidate.Key()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, candidate)
	}
	return result, nil
}

func RankPreparedCandidates(ctx context.Context, intent ExecutionIntent, ranker Ranker, candidates []PlanCandidate) ([]ScoredCandidate, error) {
	if ranker == nil {
		return nil, errors.New("cfir: nil ranker")
	}
	scored, err := ranker.ScoreCandidates(ctx, intent, slices.Clone(candidates))
	if err != nil {
		return nil, err
	}
	if len(scored) != len(candidates) {
		return nil, fmt.Errorf("cfir: ranker returned %d candidates for %d inputs", len(scored), len(candidates))
	}

	allowed := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		allowed[candidate.Key()] = struct{}{}
	}
	seen := make(map[string]struct{}, len(scored))
	for _, item := range scored {
		key := item.Candidate.Key()
		if _, ok := allowed[key]; !ok {
			return nil, fmt.Errorf("cfir: ranker introduced unapproved candidate %q", key)
		}
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("cfir: ranker duplicated candidate %q", key)
		}
		seen[key] = struct{}{}
		if err := item.Score.Validate(); err != nil {
			return nil, err
		}
	}

	slices.SortStableFunc(scored, func(a, b ScoredCandidate) int {
		if a.Score.Utility > b.Score.Utility {
			return -1
		}
		if a.Score.Utility < b.Score.Utility {
			return 1
		}
		if a.Score.Confidence > b.Score.Confidence {
			return -1
		}
		if a.Score.Confidence < b.Score.Confidence {
			return 1
		}
		if a.Score.Uncertainty < b.Score.Uncertainty {
			return -1
		}
		if a.Score.Uncertainty > b.Score.Uncertainty {
			return 1
		}
		if a.Candidate.Key() < b.Candidate.Key() {
			return -1
		}
		if a.Candidate.Key() > b.Candidate.Key() {
			return 1
		}
		return 0
	})
	return scored, nil
}
