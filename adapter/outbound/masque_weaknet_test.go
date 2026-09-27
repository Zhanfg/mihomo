package outbound

import (
	"testing"
	"time"
)

func TestMasqueLoopRetryDelay(t *testing.T) {
	cases := []struct {
		failures int
		want     time.Duration
	}{
		{0, 0},
		{1, 25 * time.Millisecond},
		{2, 50 * time.Millisecond},
		{3, 100 * time.Millisecond},
		{6, 800 * time.Millisecond},
		{20, 800 * time.Millisecond},
	}

	for _, tc := range cases {
		if got := masqueLoopRetryDelay(tc.failures); got != tc.want {
			t.Fatalf("failures=%d delay=%v want=%v", tc.failures, got, tc.want)
		}
	}
}
