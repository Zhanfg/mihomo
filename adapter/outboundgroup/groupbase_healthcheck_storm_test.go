package outboundgroup

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	C "github.com/metacubex/mihomo/constant"
)

func TestGroupDialFailureWindowRestartsFromOne(t *testing.T) {
	gb := NewGroupBase(GroupBaseOption{MaxFailedTimes: 3, TestTimeout: 5000})
	start := time.Unix(1000, 0)
	if gb.recordFailedDial(start) || gb.recordFailedDial(start.Add(time.Second)) {
		t.Fatal("fewer than three failures must not start health checking")
	}
	// Previous failures expired; this is a new window's first failure.
	if gb.recordFailedDial(start.Add(6 * time.Second)) {
		t.Fatal("expired window must not count stale failures")
	}
	if gb.recordFailedDial(start.Add(7 * time.Second)) {
		t.Fatal("second failure must not reach threshold")
	}
	if !gb.recordFailedDial(start.Add(8 * time.Second)) {
		t.Fatal("third failure in the new window must reach threshold")
	}
}

func TestGroupAutomaticHealthCheckCooldown(t *testing.T) {
	gb := NewGroupBase(GroupBaseOption{})
	start := time.Unix(1000, 0)
	gb.failedTimes = 25
	if !gb.reserveAutomaticHealthCheck(start) {
		t.Fatal("first health-check request should be accepted")
	}
	if gb.failedTimes != 0 {
		t.Fatal("accepted request should reset the failure burst")
	}
	if gb.reserveAutomaticHealthCheck(start) ||
		gb.reserveAutomaticHealthCheck(start.Add(autoHealthCheckCooldown-time.Nanosecond)) {
		t.Fatal("repeat requests during the cooldown must be dropped")
	}
	if !gb.reserveAutomaticHealthCheck(start.Add(autoHealthCheckCooldown)) {
		t.Fatal("request should be accepted when the cooldown expires")
	}
}

func TestGroupAutomaticHealthCheckInProgress(t *testing.T) {
	gb := NewGroupBase(GroupBaseOption{})
	start := time.Unix(1000, 0)
	gb.failedTesting.Store(true)
	if gb.reserveAutomaticHealthCheck(start) {
		t.Fatal("a running health check must suppress new requests")
	}
	gb.failedTesting.Store(false)
	if !gb.reserveAutomaticHealthCheck(start) {
		t.Fatal("a suppressed request must not consume the cooldown")
	}
}

func TestGroupAutomaticHealthCheckConcurrentReservations(t *testing.T) {
	gb := NewGroupBase(GroupBaseOption{})
	start := time.Unix(1000, 0)
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for i := 0; i < 128; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if gb.reserveAutomaticHealthCheck(start) {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("128 competing failure reports caused %d full checks, want 1", accepted.Load())
	}
}

func TestGroupDialFailureRefusedTriggersFirstCheck(t *testing.T) {
	gb := NewGroupBase(GroupBaseOption{MaxFailedTimes: 5})
	called := make(chan struct{}, 2)
	gb.onDialFailed(C.Shadowsocks, errors.New("connection refused"), func() { called <- struct{}{} })
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("connection refused should trigger an immediate health check")
	}
	gb.onDialFailed(C.Shadowsocks, errors.New("connection refused"), func() { called <- struct{}{} })
	select {
	case <-called:
		t.Fatal("immediate repeat connection refused should be debounced")
	case <-time.After(50 * time.Millisecond):
	}
}
