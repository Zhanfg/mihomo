package outboundgroup

import (
	"math"
	"testing"

	"github.com/metacubex/mihomo/component/linkprofile"
)

func TestGreedyCandidateAndExplorationPolicyCannotDiverge(t *testing.T) {
	for _, condition := range []linkprofile.Condition{
		linkprofile.ConditionUnknown, linkprofile.ConditionHealthy,
		linkprofile.ConditionConstrained, linkprofile.ConditionWeak,
		linkprofile.ConditionUnstable,
	} {
		for _, epochChanged := range []bool{false, true} {
			for _, failure := range []bool{false, true} {
				assessment := linkprofile.Assessment{Condition: condition}
				budget, alpha := greedyPolicyForState(assessment, epochChanged, failure)
				if budget != greedySelectionBudget(assessment, epochChanged, failure) ||
					alpha != greedyExplorationAlpha(assessment, epochChanged, failure) {
					t.Fatalf("split policy for %v epoch=%v failure=%v", condition, epochChanged, failure)
				}
				recovery := epochChanged || failure ||
					condition == linkprofile.ConditionWeak || condition == linkprofile.ConditionUnstable
				if recovery && (budget != greedyRecoveryBudget || alpha != 0) {
					t.Fatalf("recovery must be broad but nonexploratory: budget=%d alpha=%g", budget, alpha)
				}
			}
		}
	}
}

func TestGreedyMalformedPathStressIsBounded(t *testing.T) {
	for _, stress := range []float64{math.NaN(), math.Inf(-1), -10} {
		a := linkprofile.Assessment{Condition: linkprofile.ConditionUnstable, Stress: stress}
		if got := adjustedGreedyWeight(1, smartCountryMatch, a); got != 1 {
			t.Fatalf("invalid stress %v degraded rank to %v", stress, got)
		}
		if got := adjustedGreedyDelay(100, smartCountryMatch, a); got != 100 {
			t.Fatalf("invalid stress %v changed delay to %d", stress, got)
		}
	}
	a := linkprofile.Assessment{Condition: linkprofile.ConditionUnstable, Stress: math.Inf(1)}
	if got := adjustedGreedyWeight(1, smartCountryMatch, a); math.Abs(got-0.86) > 1e-12 {
		t.Fatalf("positive infinite stress must saturate: %v", got)
	}
	if got := adjustedGreedyDelay(100, smartCountryMatch, a); got != 200 {
		t.Fatalf("positive infinite stress must saturate delay: %d", got)
	}
}
