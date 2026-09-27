package netchange

import (
	"testing"

	P "github.com/metacubex/mihomo/constant/provider"
)

type scheduledStubProvider struct {
	P.ProxyProvider
	scheduled int
}

func (p *scheduledStubProvider) ScheduleHealthCheck() bool {
	p.scheduled++
	return true
}

func TestRequestScheduledHealthCheckAndroidOnly(t *testing.T) {
	p := &scheduledStubProvider{}
	if requestScheduledHealthCheck(false, p) {
		t.Fatal("desktop path must not replace immediate compatibility check")
	}
	if p.scheduled != 0 {
		t.Fatalf("desktop unexpectedly scheduled %d checks", p.scheduled)
	}
	if !requestScheduledHealthCheck(true, p) {
		t.Fatal("Android should use provider scheduler when available")
	}
	if p.scheduled != 1 {
		t.Fatalf("scheduled=%d want=1", p.scheduled)
	}
}
