package statistic

import "testing"

func TestShouldCloseForNetworkEpoch(t *testing.T) {
	tests := []struct {
		name    string
		info    *TrackerInfo
		current uint64
		want    bool
	}{
		{name: "nil info", info: nil, current: 3, want: false},
		{name: "legacy untagged", info: &TrackerInfo{}, current: 3, want: false},
		{name: "current epoch", info: &TrackerInfo{NetworkEpoch: 3}, current: 3, want: false},
		{name: "future epoch", info: &TrackerInfo{NetworkEpoch: 4}, current: 3, want: false},
		{name: "stale epoch", info: &TrackerInfo{NetworkEpoch: 2}, current: 3, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldCloseForNetworkEpoch(tc.info, tc.current); got != tc.want {
				t.Fatalf("shouldCloseForNetworkEpoch()=%v want=%v", got, tc.want)
			}
		})
	}
}
