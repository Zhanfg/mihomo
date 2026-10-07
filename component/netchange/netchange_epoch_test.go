package netchange

import (
	"slices"
	"testing"

	"github.com/metacubex/mihomo/component/netstate"
)

func TestNetworkChangeReapsOldEpochBeforeProviderProbe(t *testing.T) {
	r := &recorder{}
	n := newTestNotifier(r, nil)
	n.work.reapConnections = func(epoch uint64) int {
		if epoch != netstate.CurrentEpoch() {
			t.Fatalf("reaper epoch=%d current=%d", epoch, netstate.CurrentEpoch())
		}
		r.record("reap")
		return 2
	}

	waitDone(t, n.notify())

	if events := r.snapshot(); !slices.Equal(events, []string{"flush", "reset", "reap", "check"}) {
		t.Fatalf("unexpected fan-out: %v", events)
	}
}
