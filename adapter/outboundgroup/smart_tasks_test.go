package outboundgroup

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/mihomo/component/power"
	"github.com/metacubex/mihomo/component/smart"
)

func TestSmartTaskReadinessStopsPollingWhilePaused(t *testing.T) {
	power.SetDevicePaused(true)
	defer power.SetDevicePaused(false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var checks atomic.Int32
	var ready atomic.Bool
	done := make(chan struct{})
	ran := make(chan struct{}, 1)
	go func() {
		defer close(done)
		runSmartTaskSchedule(ctx, []smartScheduledTask{{runOnce: true, run: func() { ran <- struct{}{} }}}, func() bool { checks.Add(1); return ready.Load() }, time.Millisecond, func() time.Duration { return 0 })
	}()
	time.Sleep(30 * time.Millisecond)
	if n := checks.Load(); n > 1 {
		t.Fatalf("readiness polled %d times while paused", n)
	}
	ready.Store(true)
	power.SetDevicePaused(false)
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("readiness did not resume")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not exit")
	}
}

func TestSmartTaskSchedulePausesBackgroundWork(t *testing.T) {
	power.SetDevicePaused(true)
	defer power.SetDevicePaused(false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runs := make(chan struct{}, 10)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runSmartTaskSchedule(ctx, []smartScheduledTask{{
			initialDelay: time.Millisecond, interval: 5 * time.Millisecond,
			run: func() { runs <- struct{}{} },
		}}, func() bool { return true }, time.Millisecond, func() time.Duration { return 0 })
	}()
	select {
	case <-runs:
		t.Fatal("maintenance ran while device paused")
	case <-time.After(20 * time.Millisecond):
	}
	power.SetDevicePaused(false)
	select {
	case <-runs:
	case <-time.After(time.Second):
		t.Fatal("maintenance did not resume")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
}

func TestSmartTaskSchedulePreventsOverlap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var calls atomic.Int32
	var active atomic.Int32
	var maxActive atomic.Int32

	task := smartScheduledTask{
		initialDelay: 0,
		interval:     5 * time.Millisecond,
		name:         "slow",
		run: func() {
			calls.Add(1)
			current := active.Add(1)
			for {
				old := maxActive.Load()
				if current <= old || maxActive.CompareAndSwap(old, current) {
					break
				}
			}
			started <- struct{}{}
			<-release
			active.Add(-1)
		},
	}

	done := make(chan struct{})
	go func() {
		runSmartTaskSchedule(ctx, []smartScheduledTask{task}, func() bool { return true }, time.Millisecond, func() time.Duration { return 0 })
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("scheduled task did not start")
	}
	time.Sleep(30 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("slow task overlapped: calls=%d", got)
	}
	if got := maxActive.Load(); got != 1 {
		t.Fatalf("maximum concurrent executions=%d, want 1", got)
	}

	close(release)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop after cancellation")
	}
}

func TestSmartTaskScheduleCancelsWhileWaitingForTunnel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runSmartTaskSchedule(ctx, []smartScheduledTask{{
			initialDelay: time.Hour,
			interval:     time.Hour,
			name:         "never",
			run:          func() { t.Error("task ran while tunnel was stopped") },
		}}, func() bool { return false }, 5*time.Millisecond, func() time.Duration { return 0 })
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler ignored cancellation while waiting for tunnel")
	}
}

func TestSmartTaskScheduleBatchesSharedDeadlines(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ran := make(chan string, 2)
	tasks := []smartScheduledTask{
		{initialDelay: 0, interval: time.Hour, name: "first", run: func() { ran <- "first" }, runOnce: true},
		{initialDelay: 0, interval: time.Hour, name: "second", run: func() { ran <- "second" }, runOnce: true},
	}
	done := make(chan struct{})
	go func() {
		runSmartTaskSchedule(ctx, tasks, func() bool { return true }, time.Millisecond, func() time.Duration { return 5 * time.Millisecond })
		close(done)
	}()

	for i := 0; i < len(tasks); i++ {
		select {
		case <-ran:
		case <-time.After(time.Second):
			t.Fatal("tasks with a shared deadline were not dispatched together")
		}
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("run-once schedule did not finish")
	}
}

func TestSmartGlobalRegistrySurvivesGroupReplacement(t *testing.T) {
	registry := &smartGlobalTaskRegistry{}
	store := smart.NewStore(nil)
	oldGroup := &Smart{store: store, configName: "old"}
	newGroup := &Smart{store: store, configName: "new"}

	run := registry.acquire(oldGroup)
	if got := registry.acquire(newGroup); got != run {
		t.Fatal("replacement group started a second global task run")
	}
	registry.release(oldGroup, run)

	select {
	case <-run.ctx.Done():
		t.Fatal("closing the old group canceled global tasks owned by the replacement")
	default:
	}
	groups := run.snapshotGroupsByConfig()
	if len(groups) != 1 || groups[0] != newGroup {
		t.Fatalf("global task retained a closed first group: %#v", groups)
	}

	registry.release(newGroup, run)
	select {
	case <-run.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("last group did not stop global tasks")
	}
}

func TestSmartBackgroundGateRejectsWorkAfterClose(t *testing.T) {
	var s Smart
	if !s.beginBackgroundWork() {
		t.Fatal("open group rejected statistics")
	}

	waiting := make(chan struct{})
	go func() {
		s.waitBackgroundWork()
		close(waiting)
	}()
	select {
	case <-waiting:
		t.Fatal("wait returned before accepted statistics completed")
	case <-time.After(10 * time.Millisecond):
	}

	s.disableBackgroundWork()
	if s.beginBackgroundWork() {
		t.Fatal("closing group accepted new statistics")
	}
	s.finishBackgroundWork()
	select {
	case <-waiting:
	case <-time.After(time.Second):
		t.Fatal("wait did not observe accepted statistics completion")
	}
}

func TestSmartCloseIsConcurrentAndIdempotent(t *testing.T) {
	store := smart.NewStore(nil)
	ctx, cancel := context.WithCancel(context.Background())
	s := &Smart{store: store, ctx: ctx, cancel: cancel}
	s.global = globalSmartTasks.acquire(s)

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < cap(errs); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.Close()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("close did not cancel group context")
	}
	if s.global != nil {
		t.Fatal("close retained global task ownership")
	}
}

func TestResumeDelayNeverExceedsTheSettleDelay(t *testing.T) {
	defer func(previous time.Duration) { smartResumeSettleDelay = previous }(smartResumeSettleDelay)
	smartResumeSettleDelay = 30 * time.Second

	for _, testCase := range []struct {
		name   string
		period time.Duration
		want   time.Duration
	}{
		{name: "the host status sweep", period: 30 * time.Minute, want: 30 * time.Second},
		{name: "a period shorter than the settle delay", period: 5 * time.Second, want: 5 * time.Second},
		{name: "a degenerate period", period: 0, want: time.Millisecond},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := resumeDelayFor(testCase.period); got != testCase.want {
				t.Fatalf("resumeDelayFor(%v) = %v, want %v", testCase.period, got, testCase.want)
			}
		})
	}
}

// A task that came due while the device was paused used to be pushed a whole
// period into the future on resume, so a device that pauses more often than the
// task's interval postponed it forever. The interval here is an hour: under the
// old behaviour this test would time out rather than fail.
func TestSmartTaskScheduleRunsWhatCameDueWhilePaused(t *testing.T) {
	defer func(previous time.Duration) { smartResumeSettleDelay = previous }(smartResumeSettleDelay)
	smartResumeSettleDelay = time.Millisecond

	power.SetDevicePaused(true)
	defer power.SetDevicePaused(false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runs := make(chan struct{}, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runSmartTaskSchedule(ctx, []smartScheduledTask{{
			initialDelay: time.Millisecond, interval: time.Hour,
			run: func() { runs <- struct{}{} },
		}}, func() bool { return true }, time.Millisecond, func() time.Duration { return 0 })
	}()

	// Let the task fall due while the scheduler is parked on the pause.
	time.Sleep(20 * time.Millisecond)
	select {
	case <-runs:
		t.Fatal("maintenance ran while the device was paused")
	default:
	}

	power.SetDevicePaused(false)
	select {
	case <-runs:
	case <-time.After(2 * time.Second):
		t.Fatal("a task that came due while paused was deferred a whole period, so a device that pauses often never runs it")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
}


func TestSmartActivityScheduleStaysParkedWithoutTraffic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	activity := make(chan struct{}, 1)
	var activeUntil atomic.Int64
	var runs atomic.Int32
	done := make(chan struct{})

	recent := func(now time.Time) bool {
		return now.UnixNano() < activeUntil.Load()
	}
	go func() {
		defer close(done)
		runSmartActivityTaskSchedule(
			ctx,
			[]smartScheduledTask{{
				initialDelay: time.Millisecond,
				interval:     5 * time.Millisecond,
				name:         "idle",
				run:          func() { runs.Add(1) },
			}},
			func() bool { return true },
			recent,
			activity,
			time.Millisecond,
			func() time.Duration { return 0 },
		)
	}()

	time.Sleep(30 * time.Millisecond)
	if got := runs.Load(); got != 0 {
		t.Fatalf("idle activity scheduler ran %d tasks without traffic", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("idle activity scheduler did not stop")
	}
}

func TestSmartActivityScheduleWakesOnTrafficAndParksAgain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	activity := make(chan struct{}, 1)
	var activeUntil atomic.Int64
	var runs atomic.Int32
	ran := make(chan struct{}, 16)
	done := make(chan struct{})

	recent := func(now time.Time) bool {
		return now.UnixNano() < activeUntil.Load()
	}
	go func() {
		defer close(done)
		runSmartActivityTaskSchedule(
			ctx,
			[]smartScheduledTask{{
				initialDelay: time.Millisecond,
				interval:     5 * time.Millisecond,
				name:         "active",
				run: func() {
					runs.Add(1)
					select {
					case ran <- struct{}{}:
					default:
					}
				},
			}},
			func() bool { return true },
			recent,
			activity,
			time.Millisecond,
			func() time.Duration { return 0 },
		)
	}()

	// Let the task become overdue while completely parked.
	time.Sleep(15 * time.Millisecond)
	activeUntil.Store(time.Now().Add(35 * time.Millisecond).UnixNano())
	activity <- struct{}{}

	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("real traffic did not wake overdue maintenance")
	}

	// Once the activity window expires, no more group task executions occur.
	time.Sleep(50 * time.Millisecond)
	before := runs.Load()
	time.Sleep(30 * time.Millisecond)
	if after := runs.Load(); after != before {
		t.Fatalf("scheduler kept running after activity expired: before=%d after=%d", before, after)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("activity scheduler did not stop")
	}
}

func TestSmartActivitySignalsDoNotPostponeDueMaintenance(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	activity := make(chan struct{}, 1)
	var activeUntil atomic.Int64
	activeUntil.Store(time.Now().Add(time.Second).UnixNano())
	ran := make(chan struct{}, 1)
	done := make(chan struct{})

	go func() {
		defer close(done)
		runSmartActivityTaskSchedule(
			ctx,
			[]smartScheduledTask{{
				initialDelay: 15 * time.Millisecond,
				interval:     time.Hour,
				name:         "deadline",
				runOnce:      true,
				run:          func() { ran <- struct{}{} },
			}},
			func() bool { return true },
			func(now time.Time) bool { return now.UnixNano() < activeUntil.Load() },
			activity,
			time.Millisecond,
			func() time.Duration { return 0 },
		)
	}()

	spamDone := make(chan struct{})
	go func() {
		defer close(spamDone)
		for i := 0; i < 20; i++ {
			select {
			case activity <- struct{}{}:
			default:
			}
			time.Sleep(time.Millisecond)
		}
	}()

	select {
	case <-ran:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("traffic signals kept postponing a due maintenance task")
	}
	<-spamDone

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("run-once activity schedule did not finish")
	}
}


func TestTrafficDrivenSweepDoesNothingWithoutTraffic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	activity := make(chan struct{}, 1)
	var active atomic.Bool
	var sweeps atomic.Int32
	done := make(chan struct{})

	go func() {
		defer close(done)
		runTrafficDrivenSweep(ctx, activity, active.Load, func() {
			sweeps.Add(1)
		}, 5*time.Millisecond, 8*time.Millisecond)
	}()

	time.Sleep(30 * time.Millisecond)
	if got := sweeps.Load(); got != 0 {
		t.Fatalf("idle stalled sweep ran %d times", got)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("idle stalled sweep did not stop")
	}
}

func TestTrafficDrivenSweepChecksShortBurstOnceThenParks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	activity := make(chan struct{}, 1)
	var active atomic.Bool
	var sweeps atomic.Int32
	ran := make(chan struct{}, 4)
	done := make(chan struct{})

	go func() {
		defer close(done)
		runTrafficDrivenSweep(ctx, activity, active.Load, func() {
			sweeps.Add(1)
			ran <- struct{}{}
		}, 5*time.Millisecond, 8*time.Millisecond)
	}()

	// The activity edge remains meaningful even if the one-second rate sampler
	// has already gone quiet before the stall deadline arrives.
	activity <- struct{}{}
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("short traffic burst did not trigger delayed stalled sweep")
	}

	time.Sleep(25 * time.Millisecond)
	if got := sweeps.Load(); got != 1 {
		t.Fatalf("short burst kept periodic sweep alive: %d runs", got)
	}
	cancel()
	<-done
}

func TestTrafficDrivenSweepKeepsCadenceOnlyWhileTrafficActive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	activity := make(chan struct{}, 1)
	var active atomic.Bool
	active.Store(true)
	ran := make(chan struct{}, 8)
	done := make(chan struct{})

	go func() {
		defer close(done)
		runTrafficDrivenSweep(ctx, activity, active.Load, func() {
			ran <- struct{}{}
		}, 5*time.Millisecond, 8*time.Millisecond)
	}()

	activity <- struct{}{}
	for i := 0; i < 2; i++ {
		select {
		case <-ran:
		case <-time.After(time.Second):
			t.Fatal("continuous traffic did not keep stalled sweep cadence")
		}
	}

	active.Store(false)
	// The already-armed next check runs once, observes quiet, then parks.
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("final quiet-transition sweep did not run")
	}
	select {
	case <-ran:
		t.Fatal("stalled sweep kept ticking after traffic became idle")
	case <-time.After(25 * time.Millisecond):
	}

	cancel()
	<-done
}
