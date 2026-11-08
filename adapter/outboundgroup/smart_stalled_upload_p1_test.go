package outboundgroup

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestP1StalledSweepParksUntilNewUpload(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var mu sync.Mutex
		var signal chan struct{}
		arm := func() <-chan struct{} {
			mu.Lock()
			defer mu.Unlock()
			if signal == nil { signal = make(chan struct{}) }
			return signal
		}
		fire := func() {
			mu.Lock()
			defer mu.Unlock()
			if signal != nil { close(signal); signal = nil }
		}
		var scans atomic.Int32
		var pending atomic.Bool
		changed := make(chan struct{})
		background := func() (bool, <-chan struct{}) { return false, changed }
		go runStalledSweep(ctx, 15*time.Second, 0, background, arm, func() bool {
			scans.Add(1)
			return pending.Load()
		})
		time.Sleep(15*time.Second)
		synctest.Wait()
		if got := scans.Load(); got != 1 { t.Fatalf("initial scans=%d", got) }
		time.Sleep(time.Hour)
		synctest.Wait()
		if got := scans.Load(); got != 1 { t.Fatalf("idle scanner woke %d times", got) }
		pending.Store(true)
		fire()
		synctest.Wait()
		time.Sleep(15*time.Second)
		synctest.Wait()
		if got := scans.Load(); got != 2 { t.Fatalf("post-upload scans=%d", got) }
		time.Sleep(30*time.Second)
		synctest.Wait()
		if got := scans.Load(); got != 4 { t.Fatalf("pending scans=%d", got) }
		pending.Store(false)
		time.Sleep(15*time.Second)
		synctest.Wait()
		time.Sleep(time.Hour)
		synctest.Wait()
		if got := scans.Load(); got != 5 { t.Fatalf("sweeps did not re-park: %d", got) }
	})
}
