package outboundgroup

import (
	"math"
	"testing"

	"github.com/metacubex/mihomo/component/linkprofile"
)

func TestAdjustedCountryWeightIsElastic(t *testing.T) {
	const base = 1.0
	if got := adjustedCountryWeight(base, smartCountryMatch); got != base {
		t.Fatalf("match changed weight: %v", got)
	}
	if got := adjustedCountryWeight(base, smartCountryUnknown); math.Abs(got-0.98) > 1e-9 {
		t.Fatalf("unknown weight=%v", got)
	}
	if got := adjustedCountryWeight(base, smartCountryMismatch); math.Abs(got-0.88) > 1e-9 {
		t.Fatalf("mismatch weight=%v", got)
	}

	// Country alignment is a preference, not a hard wall: a materially stronger
	// mismatched path must still be able to beat a weak aligned one.
	if adjustedCountryWeight(1.30, smartCountryMismatch) <= adjustedCountryWeight(1.00, smartCountryMatch) {
		t.Fatal("elastic mismatch can never overcome affinity")
	}
}

func TestAdjustedCountryDelayIsBounded(t *testing.T) {
	if got := adjustedCountryDelay(100, smartCountryMatch); got != 100 {
		t.Fatalf("match delay=%d", got)
	}
	if got := adjustedCountryDelay(100, smartCountryUnknown); got != 115 {
		t.Fatalf("unknown delay=%d", got)
	}
	if got := adjustedCountryDelay(100, smartCountryMismatch); got != 220 {
		t.Fatalf("mismatch delay=%d", got)
	}
	if got := adjustedCountryDelay(math.MaxUint16-10, smartCountryMismatch); got != math.MaxUint16 {
		t.Fatalf("overflow not saturated: %d", got)
	}
}

func TestAdjustedGreedyCostUsesBoundedLiveStress(t *testing.T) {
	healthy := linkprofile.Assessment{Condition: linkprofile.ConditionHealthy, Stress: 0}
	unstable := linkprofile.Assessment{Condition: linkprofile.ConditionUnstable, Stress: 1}

	if got := adjustedGreedyWeight(1, smartCountryMatch, healthy); math.Abs(got-1) > 1e-9 {
		t.Fatalf("healthy weight=%v", got)
	}
	if got := adjustedGreedyWeight(1, smartCountryMatch, unstable); math.Abs(got-0.86) > 1e-9 {
		t.Fatalf("unstable weight=%v", got)
	}
	if got := adjustedGreedyDelay(100, smartCountryMatch, unstable); got != 200 {
		t.Fatalf("unstable delay=%d", got)
	}

	// Even a fully stressed path is penalized, not excluded.
	if adjustedGreedyWeight(1.3, smartCountryMismatch, unstable) <= 0 {
		t.Fatal("greedy cost accidentally hard-excluded candidate")
	}
}
