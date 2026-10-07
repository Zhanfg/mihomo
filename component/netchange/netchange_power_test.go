package netchange

import (
	"slices"
	"testing"
	"time"
)

func TestNetworkSettleDelayPolicy(t *testing.T) {
	if got := networkSettleDelay(false); got != 0 {
		t.Fatalf("desktop settle=%v, want 0", got)
	}
	if got := networkSettleDelay(true); got != 8*time.Second {
		t.Fatalf("android settle=%v, want 8s", got)
	}
}

func TestSettleWindowCoalescesFlappingChanges(t *testing.T) {
	r := &recorder{}
	n := newTestNotifier(r, nil)

	// Do not make correctness depend on sub-20ms scheduler precision. Windows
	// ARM hosted runners can oversleep a 5ms wall-clock delay by more than the
	// old 20ms settle window under matrix load, turning three legitimately
	// separated events into a test-only failure. Back-to-back notifications
	// exercise the same supersede/debounce contract deterministically.
	n.settleDelay = 100 * time.Millisecond

	first := n.notify()
	second := n.notify()
	third := n.notify()

	waitDone(t, first)
	waitDone(t, second)
	waitDone(t, third)

	if events := r.snapshot(); !slices.Equal(events, []string{"flush", "reset", "check"}) {
		t.Fatalf("flapping changes should collapse to one fan-out: %v", events)
	}
}
