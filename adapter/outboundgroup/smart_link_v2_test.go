package outboundgroup

import (
	"testing"

	"github.com/metacubex/mihomo/component/linkprofile"
	"github.com/metacubex/mihomo/component/smart"
)

func TestStabilizeSmartOrderKeepsNearWinner(t *testing.T) {
	names := []string{"new", "old", "third"}
	weights := []float64{1.00, 0.91, 0.70}

	stabilizeSmartOrder(names, weights, "old", smartSwitchMargin)

	if names[0] != "old" || weights[0] != 0.91 {
		t.Fatalf("order=%v weights=%v, expected old winner to remain first", names, weights)
	}
}

func TestStabilizeSmartOrderAllowsMaterialImprovement(t *testing.T) {
	names := []string{"new", "old"}
	weights := []float64{1.00, 0.75}

	stabilizeSmartOrder(names, weights, "old", smartSwitchMargin)

	if names[0] != "new" {
		t.Fatalf("order=%v, expected materially better node to win", names)
	}
}

func TestStabilizeSmartOrderRejectsUnusableCurrent(t *testing.T) {
	names := []string{"new", "old"}
	weights := []float64{0.50, smart.AllowedWeight - 0.01}

	stabilizeSmartOrder(names, weights, "old", smartSwitchMargin)

	if names[0] != "new" {
		t.Fatalf("order=%v, unusable current winner must not be retained", names)
	}
}

func TestSmartDialBatchBoundsForWeakLink(t *testing.T) {
	width := smartParallelism()
	if width <= 0 {
		t.Fatalf("invalid smart parallelism %d", width)
	}

	// Weak-link iteration 0 always races only two candidates. Later iterations
	// use the platform budget: Android=3, desktop/Linux=5 today.
	begin, end := smartDialBatchBoundsForLink(10, 0, false, true)
	if begin != 0 || end != 2 {
		t.Fatalf("iteration=0 got=(%d,%d) want=(0,2)", begin, end)
	}

	iteration := 1
	expectedBegin := 2
	for expectedBegin < 10 {
		expectedEnd := expectedBegin + width
		if expectedEnd > 10 {
			expectedEnd = 10
		}
		begin, end = smartDialBatchBoundsForLink(10, iteration, false, true)
		if begin != expectedBegin || end != expectedEnd {
			t.Fatalf("iteration=%d width=%d got=(%d,%d) want=(%d,%d)",
				iteration, width, begin, end, expectedBegin, expectedEnd)
		}
		expectedBegin = expectedEnd
		iteration++
	}

	begin, end = smartDialBatchBoundsForLink(10, iteration, false, true)
	if begin != 0 || end != 0 {
		t.Fatalf("exhausted iteration=%d got=(%d,%d), want=(0,0)", iteration, begin, end)
	}
}

func TestSmartDialBatchBoundsHedgesNominalFirstPath(t *testing.T) {
	begin, end := smartDialBatchBoundsForLink(6, 0, false, false)
	if begin != 0 || end != 2 {
		t.Fatalf("nominal first batch=(%d,%d), want (0,2)", begin, end)
	}

	begin, end = smartDialBatchBoundsForLink(6, 1, false, false)
	if begin != 2 {
		t.Fatalf("nominal second batch begins at %d, want 2", begin)
	}
}

func TestSmartDialBatchBoundsPinnedRemainsSerial(t *testing.T) {
	begin, end := smartDialBatchBoundsForLink(5, 2, true, true)
	if begin != 2 || end != 3 {
		t.Fatalf("pinned weak batch=(%d,%d), want (2,3)", begin, end)
	}
}


func TestSwitchMarginDistinguishesNodeFailureFromCommonModeWeakness(t *testing.T) {
	weak := linkprofile.Assessment{
		Condition:    linkprofile.ConditionWeak,
		SwitchMargin: 0.08,
	}
	if got := switchMarginForPath(weak, false); got != 0.08 {
		t.Fatalf("node-specific weak margin=%v, want 0.08", got)
	}
	if got := switchMarginForPath(weak, true); got != commonModeWeakSwitchMargin {
		t.Fatalf("common-mode weak margin=%v, want %v", got, commonModeWeakSwitchMargin)
	}
}

func TestSwitchMarginCommonModeNeverMakesWinnerLessStable(t *testing.T) {
	for _, assessment := range []linkprofile.Assessment{
		{Condition: linkprofile.ConditionUnknown},
		{Condition: linkprofile.ConditionHealthy, SwitchMargin: 0.14},
		{Condition: linkprofile.ConditionConstrained, SwitchMargin: 0.12},
		{Condition: linkprofile.ConditionWeak, SwitchMargin: 0.08},
		{Condition: linkprofile.ConditionUnstable, SwitchMargin: 0.05},
	} {
		normal := switchMarginForPath(assessment, false)
		common := switchMarginForPath(assessment, true)
		if common < normal {
			t.Fatalf("condition=%s common=%v normal=%v", assessment.Condition, common, normal)
		}
	}
}
