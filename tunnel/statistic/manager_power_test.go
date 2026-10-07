package statistic

import (
	"testing"
	"time"
)

func stopRateLoop(t *testing.T, stop chan struct{}, done <-chan struct{}) {
	t.Helper()
	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("rate sampler did not stop")
	}
}

func TestRateSamplerDoesNotWakeWhileIdle(t *testing.T) {
	m := &Manager{}
	stop := make(chan struct{})
	done := make(chan struct{})
	samples := make(chan struct{}, 4)

	go func() {
		defer close(done)
		m.runRateLoop(5*time.Millisecond, stop, func(_, _ int64) {
			samples <- struct{}{}
		})
	}()

	select {
	case <-samples:
		t.Fatal("idle manager sampled without traffic")
	case <-time.After(30 * time.Millisecond):
	}
	stopRateLoop(t, stop, done)
}

func TestRateSamplerWakesClearsAndParksAgain(t *testing.T) {
	m := &Manager{}
	stop := make(chan struct{})
	done := make(chan struct{})
	type sample struct{ up, down int64 }
	samples := make(chan sample, 8)

	go func() {
		defer close(done)
		m.runRateLoop(5*time.Millisecond, stop, func(up, down int64) {
			samples <- sample{up, down}
		})
	}()

	m.PushUploaded(7)
	m.PushDownloaded(3)

	select {
	case got := <-samples:
		if got != (sample{7, 3}) {
			t.Fatalf("first sample=%+v want={7 3}", got)
		}
	case <-time.After(time.Second):
		t.Fatal("traffic did not wake rate sampler")
	}

	// One quiet interval clears the public blip, then the loop must park.
	select {
	case got := <-samples:
		if got != (sample{}) {
			t.Fatalf("clear sample=%+v want zero", got)
		}
	case <-time.After(time.Second):
		t.Fatal("rate sampler did not publish zero after traffic stopped")
	}

	select {
	case got := <-samples:
		t.Fatalf("parked sampler kept ticking: %+v", got)
	case <-time.After(30 * time.Millisecond):
	}

	if up, down := m.Now(); up != 0 || down != 0 {
		t.Fatalf("Now() after park=(%d,%d), want zero", up, down)
	}
	if up, down := m.Total(); up != 7 || down != 3 {
		t.Fatalf("Total()=(%d,%d), want (7,3)", up, down)
	}

	// A later burst must re-arm from a fully parked state.
	m.PushUploaded(11)
	select {
	case got := <-samples:
		if got.up != 11 || got.down != 0 {
			t.Fatalf("rearmed sample=%+v want={11 0}", got)
		}
	case <-time.After(time.Second):
		t.Fatal("parked rate sampler did not re-arm")
	}

	stopRateLoop(t, stop, done)
}

func TestRateSamplerConcurrentWakeCannotLoseBytes(t *testing.T) {
	m := &Manager{}
	stop := make(chan struct{})
	done := make(chan struct{})
	samples := make(chan int64, 64)

	go func() {
		defer close(done)
		m.runRateLoop(2*time.Millisecond, stop, func(up, down int64) {
			samples <- up + down
		})
	}()

	const bursts = 40
	var pushed int64
	for i := 0; i < bursts; i++ {
		m.PushUploaded(1)
		m.PushDownloaded(2)
		pushed += 3
		if i%5 == 0 {
			time.Sleep(time.Millisecond)
		}
	}

	deadline := time.After(time.Second)
	var sampled int64
	for sampled < pushed {
		select {
		case n := <-samples:
			sampled += n
		case <-deadline:
			t.Fatalf("sampled=%d pushed=%d; wake race lost traffic", sampled, pushed)
		}
	}
	if sampled != pushed {
		t.Fatalf("sampled=%d pushed=%d", sampled, pushed)
	}
	if up, down := m.Total(); up+down != pushed {
		t.Fatalf("totals=%d pushed=%d", up+down, pushed)
	}

	stopRateLoop(t, stop, done)
}


func TestTrafficWakeIsOneEdgePerActiveBurst(t *testing.T) {
	m := &Manager{}
	wake := m.TrafficWake()

	m.PushUploaded(1)
	select {
	case <-wake:
	case <-time.After(time.Second):
		t.Fatal("first traffic did not publish activity edge")
	}

	// Without a quiet transition the sampler is already active, so another
	// packet must not turn the maintenance channel into a per-packet queue.
	m.PushDownloaded(1)
	select {
	case <-wake:
		t.Fatal("continuous traffic published a second activity edge")
	case <-time.After(20 * time.Millisecond):
	}
}


func TestReclaimActiveFromPendingPublishesMaintenanceEdge(t *testing.T) {
	m := &Manager{}
	wake := m.TrafficWake()

	// Reproduce the idle-transition handoff deterministically: the sampler has
	// already cleared rateActive, a producer has already added bytes, but that
	// producer has not yet reached wakeRateLoop.
	m.rateActive.Store(false)
	m.uploadTemp.Add(9)

	if !m.reclaimActiveFromPending() {
		t.Fatal("pending bytes did not reclaim active state")
	}
	select {
	case <-wake:
	case <-time.After(time.Second):
		t.Fatal("sampler reclaimed activity without publishing maintenance edge")
	}

	// The producer arriving afterwards sees active=true and must not create a
	// duplicate edge.
	m.wakeRateLoop()
	select {
	case <-wake:
		t.Fatal("handoff produced duplicate maintenance edge")
	case <-time.After(20 * time.Millisecond):
	}
}
