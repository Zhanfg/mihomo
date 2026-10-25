package smart

import (
	"context"
	"github.com/metacubex/http"
	"testing"
	"time"

	C "github.com/metacubex/mihomo/constant"
)

func TestMobileContinuitySpeculativeTLSDoesNotProbe(t *testing.T) {
	meta := &C.Metadata{Host: "example.org", DstPort: 443}
	for _, test := range []struct {
		up, down float64
		dur int64
		want bool
	}{
		{0, 0, 100, false},
		{0.0001, 0, 6000, false},
		{0.01, 0, 100, false},
		{0.01, 0, 8000, true},
		{0.01, 0.04, 8000, false},
	} {
		if got := ResponseProbeEligibleForTraffic(meta, test.up, test.down, test.dur, false); got != test.want {
			t.Fatalf("up=%f down=%f duration=%d: got %v, want %v",test.up,test.down,test.dur,got,test.want)
		}
	}
	if ResponseProbeEligibleForTraffic(meta, .01, 0, 8000, true) {
		t.Fatal("UDP must never trigger HTTPS probe")
	}
	if ResponseProbeEligibleForTraffic(nil, .01, 0, 8000, false) {
		t.Fatal("nil metadata must not trigger HTTPS probe")
	}
}

func TestProbeTimeoutIsUnprovenNotQuarantine(t *testing.T) {
	v := ClassifyProbeError(context.DeadlineExceeded)
	if v.Action != VerdictIgnore {
		t.Fatalf("timeout created a hard block: %+v",v)
	}
	// Keep HTTP status information available as soft evidence for the
	// region/host watchers, but never use that evidence to force closures.
	v = ClassifyResponse(http.StatusForbidden, http.Header{}, time.Now())
	if v.Action != VerdictRecord {
		t.Fatalf("forbidden response should remain observable: %+v",v)
	}
}
