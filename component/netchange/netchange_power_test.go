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
	n.settleDelay = 20 * time.Millisecond

	first := n.notify()
	time.Sleep(5 * time.Millisecond)
	second := n.notify()
	time.Sleep(5 * time.Millisecond)
	third := n.notify()

	waitDone(t, first)
	waitDone(t, second)
	waitDone(t, third)

	if events := r.snapshot(); !slices.Equal(events, []string{"flush", "reset", "check"}) {
		t.Fatalf("flapping changes should collapse to one fan-out: %v", events)
	}
}
