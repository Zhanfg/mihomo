package statistic

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestRequestNotifyHookLifecycle(t *testing.T) {
	defer SetRequestNotifyHook(nil)
	var called atomic.Int64
	SetRequestNotifyHook(func(Tracker) { called.Add(1) })
	notifyRequestJoin(nil)
	if got := called.Load(); got != 1 {
		t.Fatalf("expected one join notification, got %d", got)
	}
	SetRequestNotifyHook(nil)
	notifyRequestJoin(nil)
	if got := called.Load(); got != 1 {
		t.Fatalf("disabled hook emitted event: %d", got)
	}
}

func TestRequestNotifyHookConcurrentRegistration(t *testing.T) {
	defer SetRequestNotifyHook(nil)
	var calls atomic.Int64
	hook := func(Tracker) { calls.Add(1) }
	const iterations = 200
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			SetRequestNotifyHook(hook)
			SetRequestNotifyHook(nil)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			notifyRequestJoin(nil)
		}
	}()
	wg.Wait()
	// Notification counts depend on scheduling. The race detector validates
	// the concurrent publication and dereference instead of an exact count.
	if calls.Load() > iterations {
		t.Fatal("unexpected duplicate notifications")
	}
}
