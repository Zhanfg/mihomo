package smart

import "testing"

func TestRouteEvidenceEpochPolicy(t *testing.T) {
	tests := []struct {
		name    string
		stored  uint64
		current uint64
		want    bool
	}{
		{name: "bootstrap accepts legacy", stored: 0, current: 1, want: true},
		{name: "bootstrap accepts old tagged", stored: 9, current: 1, want: true},
		{name: "handover rejects legacy", stored: 0, current: 2, want: false},
		{name: "handover rejects prior network", stored: 2, current: 3, want: false},
		{name: "handover accepts current network", stored: 3, current: 3, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := routeEvidenceCurrentAt(tc.stored, tc.current); got != tc.want {
				t.Fatalf("routeEvidenceCurrentAt(%d,%d)=%v want=%v", tc.stored, tc.current, got, tc.want)
			}
		})
	}
}

func TestRouteEpochWeightTypeSeparatedByTransport(t *testing.T) {
	if RouteEpochWeightType(false) == RouteEpochWeightType(true) {
		t.Fatal("TCP and UDP route epochs must be independent")
	}
}
