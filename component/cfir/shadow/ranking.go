package shadow

import (
	"github.com/metacubex/mihomo/component/cfir"
	"github.com/metacubex/mihomo/component/cfir/legacybridge"
	C "github.com/metacubex/mihomo/constant"
)

type RankingMismatch uint64

const (
	RankingMismatchNone RankingMismatch = 0
	RankingMismatchNoEligibleCandidate RankingMismatch = 1 << iota
	RankingMismatchLegacyFirstIneligible
	RankingMismatchFilteredWinnerChanged
)

type RankingResult struct {
	Intent        cfir.ExecutionIntent
	LegacyFirst   legacybridge.LegacyDecision
	PlannerFirst  legacybridge.LegacyDecision
	RankedCount   int
	EligibleCount int
	Mismatch      RankingMismatch
}

func (r RankingResult) Equal() bool { return r.Mismatch == RankingMismatchNone }

// ObserveRankedCandidates consumes the exact ranking snapshot produced by the
// live Smart selector. It never calls Smart selection itself, avoiding observer
// effects from a second cache/exploration pass.
func ObserveRankedCandidates(metadata *C.Metadata, ranked []C.Proxy, generation cfir.Generation) (RankingResult, error) {
	result := RankingResult{RankedCount: len(ranked)}
	if len(ranked) == 0 {
		result.Mismatch |= RankingMismatchNoEligibleCandidate
		return result, nil
	}

	first, err := legacybridge.ProjectProxyDecision(ranked[0], metadata)
	if err != nil {
		return result, err
	}
	result.LegacyFirst = first
	intent, err := legacybridge.IntentForLegacyDecision(metadata, generation, cfir.RouteActionForward)
	if err != nil {
		return result, err
	}
	result.Intent = intent

	for _, proxy := range ranked {
		candidate, err := legacybridge.ProjectProxyDecision(proxy, metadata)
		if err != nil {
			return result, err
		}
		if !decisionEligible(intent, candidate) {
			continue
		}
		result.EligibleCount++
		if result.PlannerFirst.LeafName == "" {
			result.PlannerFirst = candidate
		}
	}

	if result.EligibleCount == 0 {
		result.Mismatch |= RankingMismatchNoEligibleCandidate
		return result, nil
	}
	if !decisionEligible(intent, first) {
		result.Mismatch |= RankingMismatchLegacyFirstIneligible
	}
	if result.PlannerFirst.LeafName != first.LeafName ||
		result.PlannerFirst.Protocol != first.Protocol {
		result.Mismatch |= RankingMismatchFilteredWinnerChanged
	}
	return result, nil
}
