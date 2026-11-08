package log

import (
	"fmt"
	"testing"
	"time"
)

type logFormatProbe struct{ calls int }
func (p *logFormatProbe) String() string { p.calls++; return "probe" }

// An info-only dashboard cannot enable debug event formatting.
func TestP1InfoDashboardCannotEnableDebugFormatting(t *testing.T) {
	old := Level()
	SetLevel(INFO)
	defer SetLevel(old)
	sub := SubscribeLevel(INFO)
	defer UnSubscribe(sub)
	if DebugEnabled() {
		t.Fatal("info-only subscriber enabled debug formatting")
	}
	probe := &logFormatProbe{}
	Debugln("%s", probe)
	if probe.calls != 0 {
		t.Fatalf("disabled debug message formatted %d times", probe.calls)
	}
}

// A slow dashboard is allowed to miss logs, but may not backpressure the core.
func TestP1SlowLogSubscriberDoesNotBlockCore(t *testing.T) {
	old := Level()
	SetLevel(SILENT)
	defer SetLevel(old)
	sub := SubscribeLevel(INFO)
	defer UnSubscribe(sub)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 800; i++ {
			Infoln("p1-slow-subscriber-%d", i)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("slow dashboard log subscriber blocked core publication")
	}
	_ = fmt.Sprint("ok")
}
