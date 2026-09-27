// Package netchange fans out the work that has to happen when the default
// network interface changes. Dropping the interface and resolver caches alone
// is not enough: a urltest, fallback or smart group keeps routing to whichever
// node won on the *previous* link until its health check interval elapses, so
// every provider is re-probed as part of the same notification.
package netchange

import (
	"context"
	"runtime"
	"sync"
	"time"

	"github.com/metacubex/mihomo/common/batch"
	"github.com/metacubex/mihomo/component/iface"
	"github.com/metacubex/mihomo/component/netstate"
	"github.com/metacubex/mihomo/component/resolver"
	P "github.com/metacubex/mihomo/constant/provider"
	"github.com/metacubex/mihomo/log"
	"github.com/metacubex/mihomo/tunnel"
)

// A provider health check already fans out over its own proxies. Android uses
// one provider at a time: after a radio handover the expensive work is the
// provider's own per-node fan-out, so stacking several providers only creates a
// CPU/socket/radio burst without making Smart's selected-node recovery faster.
func providerConcurrency() int {
	if runtime.GOOS == "android" {
		return 1
	}
	return 4
}

func networkSettleDelay(android bool) time.Duration {
	if android {
		return 8 * time.Second
	}
	return 0
}

// fanOut are the stages of a network change, kept as fields instead of direct
// calls so tests can drive the sequencing without a real resolver or providers.
type fanOut struct {
	flushCache      func()
	resetConnection func()
	providers       func() map[string]P.ProxyProvider
}

type notifier struct {
	access      sync.Mutex
	cancel      context.CancelFunc // supersedes the fan-out started last
	work        fanOut
	settleDelay time.Duration
	// runAccess serialises the fan-out bodies. A superseded run can still be
	// inside a provider check when its replacement starts, and two resets
	// racing each other would rebuild connections on half-torn-down state.
	runAccess sync.Mutex
}

var defaultNotifier = &notifier{
	settleDelay: networkSettleDelay(runtime.GOOS == "android"),
	work: fanOut{
	flushCache:      iface.FlushCache,
	resetConnection: resolver.ResetConnection,
	providers:       tunnel.Providers,
}}

// Notify reports that the default interface changed. A flapping link
// supersedes the running fan-out instead of stacking another one on top of it.
func Notify() {
	netstate.Advance()
	defaultNotifier.notify()
}

// notify returns a channel closed once this run's goroutine exits, which is
// what the tests wait on instead of sleeping.
func (n *notifier) notify() <-chan struct{} {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	n.access.Lock()
	previous := n.cancel
	n.cancel = cancel
	work := n.work
	n.access.Unlock()
	if previous != nil {
		previous()
	}
	go func() {
		defer close(done)
		defer cancel()
		n.run(ctx, work)
	}()
	return done
}

func (n *notifier) run(ctx context.Context, work fanOut) {
	if n.settleDelay > 0 {
		timer := time.NewTimer(n.settleDelay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}

	n.runAccess.Lock()
	defer n.runAccess.Unlock()
	if ctx.Err() != nil {
		return
	}
	work.flushCache()
	if ctx.Err() != nil {
		return
	}
	work.resetConnection()
	if ctx.Err() != nil {
		return
	}
	recheckProviders(ctx, work.providers)
}

type scheduledHealthChecker interface {
	ScheduleHealthCheck() bool
}

// recheckProviders keeps desktop's immediate compatibility behavior. Android
// first requests the provider's own coalesced/background-aware scheduler; only
// providers without an automatic health-check loop fall back to synchronous
// HealthCheck. A trigger received while power is paused is retained by the
// HealthCheck state machine and runs after resume.
func recheckProviders(ctx context.Context, source func() map[string]P.ProxyProvider) {
	if source == nil {
		return
	}
	providers := source()
	if len(providers) == 0 {
		return
	}
	log.Debugln("[NetChange] re-checking %d providers after default interface changed", len(providers))
	b, _ := batch.New[struct{}](ctx, batch.WithConcurrencyNum[struct{}](providerConcurrency()))
	for name, provider := range providers {
		b.Go(name, func() (struct{}, error) {
			if ctx.Err() != nil {
				return struct{}{}, nil
			}
			if runtime.GOOS == "android" {
				if scheduler, ok := provider.(scheduledHealthChecker); ok && scheduler.ScheduleHealthCheck() {
					return struct{}{}, nil
				}
			}
			// Compatibility fallback for providers without an automatic health
			// loop. This path remains serialized on Android.
			provider.HealthCheck()
			return struct{}{}, nil
		})
	}
	_ = b.Wait()
}
