package outboundgroup

import (
	"math"
	"net/netip"
	"testing"

	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/component/linkprofile"
	C "github.com/metacubex/mihomo/constant"
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


func TestGreedySelectionBudget(t *testing.T) {
	tests := []struct {
		name          string
		condition     linkprofile.Condition
		epochChanged  bool
		recentFailure bool
		want          int
	}{
		{"healthy", linkprofile.ConditionHealthy, false, false, greedyHealthyBudget},
		{"constrained", linkprofile.ConditionConstrained, false, false, greedyNormalBudget},
		{"unknown", linkprofile.ConditionUnknown, false, false, greedyNormalBudget},
		{"weak", linkprofile.ConditionWeak, false, false, greedyRecoveryBudget},
		{"unstable", linkprofile.ConditionUnstable, false, false, greedyRecoveryBudget},
		{"handover", linkprofile.ConditionHealthy, true, false, greedyRecoveryBudget},
		{"recent failure", linkprofile.ConditionHealthy, false, true, greedyRecoveryBudget},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := greedySelectionBudget(linkprofile.Assessment{Condition: tc.condition}, tc.epochChanged, tc.recentFailure)
			if got != tc.want {
				t.Fatalf("budget=%d want=%d", got, tc.want)
			}
		})
	}
}


func TestCountryGreedyFitFromSnapshot(t *testing.T) {
	snapshot := adapter.CachedCapabilitySnapshot{
		IPv4Known:     true,
		IPv4Available: true,
		IPv4Country:   "US",
		IPv6Known:     true,
		IPv6Available: true,
		IPv6Country:   "JP",
	}

	v4 := &C.Metadata{DstIP: netip.MustParseAddr("1.1.1.1")}
	if fit := countryGreedyFitFromSnapshot(v4, snapshot, "US"); fit != smartCountryMatch {
		t.Fatalf("IPv4 fit=%v, want match", fit)
	}
	if fit := countryGreedyFitFromSnapshot(v4, snapshot, "JP"); fit != smartCountryMismatch {
		t.Fatalf("IPv4 fit=%v, want mismatch", fit)
	}

	v6 := &C.Metadata{DstIP: netip.MustParseAddr("2001:db8::1")}
	if fit := countryGreedyFitFromSnapshot(v6, snapshot, "JP"); fit != smartCountryMatch {
		t.Fatalf("IPv6 fit=%v, want match", fit)
	}

	unknownFamily := &C.Metadata{Host: "example.com"}
	if fit := countryGreedyFitFromSnapshot(unknownFamily, snapshot, "US"); fit != smartCountryMatch {
		t.Fatalf("family-unknown fit=%v, want either-family match", fit)
	}
}

func TestAutoIPFamilyMismatchFromSnapshot(t *testing.T) {
	v6 := &C.Metadata{DstIP: netip.MustParseAddr("2001:db8::1")}
	missing := adapter.CachedCapabilitySnapshot{IPv6Known: true, IPv6Available: false}
	if !autoIPFamilyMismatchFromSnapshot(v6, missing) {
		t.Fatal("confirmed IPv6 mismatch was not detected")
	}
	unknown := adapter.CachedCapabilitySnapshot{}
	if autoIPFamilyMismatchFromSnapshot(v6, unknown) {
		t.Fatal("unknown IPv6 capability must preserve availability")
	}
}


func TestGreedyExplorationAlpha(t *testing.T) {
	tests := []struct {
		name          string
		condition     linkprofile.Condition
		epochChanged  bool
		recentFailure bool
		want          float64
	}{
		{"healthy", linkprofile.ConditionHealthy, false, false, 0.06},
		{"constrained", linkprofile.ConditionConstrained, false, false, 0.02},
		{"unknown", linkprofile.ConditionUnknown, false, false, 0.03},
		{"weak", linkprofile.ConditionWeak, false, false, 0},
		{"unstable", linkprofile.ConditionUnstable, false, false, 0},
		{"handover", linkprofile.ConditionHealthy, true, false, 0},
		{"recent failure", linkprofile.ConditionHealthy, false, true, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := greedyExplorationAlpha(linkprofile.Assessment{Condition: tc.condition}, tc.epochChanged, tc.recentFailure)
			if math.Abs(got-tc.want) > 1e-12 {
				t.Fatalf("alpha=%v want=%v", got, tc.want)
			}
		})
	}
}
