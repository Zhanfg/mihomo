package outboundgroup

import (
	"testing"

	"github.com/metacubex/mihomo/component/smart"
)

func TestStabilizeSmartOrderKeepsNearWinner(t *testing.T) {
	names := []string{"new", "old", "third"}
	weights := []float64{1.00, 0.91, 0.70}

	stabilizeSmartOrder(names, weights, "old")

	if names[0] != "old" || weights[0] != 0.91 {
		t.Fatalf("order=%v weights=%v, expected old winner to remain first", names, weights)
	}
}

func TestStabilizeSmartOrderAllowsMaterialImprovement(t *testing.T) {
	names := []string{"new", "old"}
	weights := []float64{1.00, 0.75}

	stabilizeSmartOrder(names, weights, "old")

	if names[0] != "new" {
		t.Fatalf("order=%v, expected materially better node to win", names)
	}
}

func TestStabilizeSmartOrderRejectsUnusableCurrent(t *testing.T) {
	names := []string{"new", "old"}
	weights := []float64{0.50, smart.AllowedWeight - 0.01}

	stabilizeSmartOrder(names, weights, "old")

	if names[0] != "new" {
		t.Fatalf("order=%v, unusable current winner must not be retained", names)
	}
}

func TestSmartDialBatchBoundsForWeakLink(t *testing.T) {
	cases := []struct {
		iteration int
		begin     int
		end       int
	}{
		{0, 0, 2},
		{1, 2, 5},
		{2, 5, 8},
		{3, 8, 10},
		{4, 0, 0},
	}

	for _, tc := range cases {
		begin, end := smartDialBatchBoundsForLink(10, tc.iteration, false, true)
		if begin != tc.begin || end != tc.end {
			t.Fatalf("iteration=%d got=(%d,%d) want=(%d,%d)", tc.iteration, begin, end, tc.begin, tc.end)
		}
	}
}

func TestSmartDialBatchBoundsPinnedRemainsSerial(t *testing.T) {
	begin, end := smartDialBatchBoundsForLink(5, 2, true, true)
	if begin != 2 || end != 3 {
		t.Fatalf("pinned weak batch=(%d,%d), want (2,3)", begin, end)
	}
}
