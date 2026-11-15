package dialer

import (
	"context"
	"sync"
	"sync/atomic"
	"syscall"
)

type protectFn = func(ctx context.Context, network, address string, c syscall.RawConn) error

// DefaultSocketProtect holds an optional socket-protect function that is
// appended to every control chain in this package. It is used to register
// eBPF socket cookies so that the eBPF inbound does not capture sockets
// created by mihomo itself. It is an append-only hook and never replaces the
// bind/mark/TFO controls.
var DefaultSocketProtect atomic.Value // holds protectFn or nil

// Multiple eBPF inbounds may be active in one process. Each needs to mark
// its own map; unregistering one inbound must not disable another.
var socketProtectOwners = struct {
	sync.RWMutex
	nextID uint64
	hooks map[uint64]protectFn
}{hooks: make(map[uint64]protectFn)}

// RegisterSocketProtectOwner installs a scoped protect callback and returns
// an idempotent cleanup function for this owner. Call cleanup before closing
// its eBPF map; other owners remain registered.
func RegisterSocketProtectOwner(fn protectFn) func() {
	if fn == nil {
		return func() {}
	}
	socketProtectOwners.Lock()
	socketProtectOwners.nextID++
	id := socketProtectOwners.nextID
	socketProtectOwners.hooks[id] = fn
	socketProtectOwners.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			socketProtectOwners.Lock()
			delete(socketProtectOwners.hooks, id)
			socketProtectOwners.Unlock()
		})
	}
}

// RegisterSocketProtectFunc installs a socket-protect function. Pass nil to
// disable. The previous function is replaced, not chained.
func RegisterSocketProtectFunc(fn protectFn) {
	if fn == nil {
		UnregisterSocketProtectFunc()
		return
	}
	DefaultSocketProtect.Store(fn)
}

// UnregisterSocketProtectFunc removes the active socket-protect function.
func UnregisterSocketProtectFunc() {
	DefaultSocketProtect.Store((protectFn)(nil))
}

// A connection attempt owns the rollback callbacks for any cookie-map entries
// inserted by its ControlContext hook. Only failed dials roll these back.
type socketProtectScopeKey struct{}
type socketProtectScope struct {
	access sync.Mutex
	undo []func()
}

func withSocketProtectScope(ctx context.Context) (context.Context, *socketProtectScope) {
	socketProtectOwners.RLock()
	active := len(socketProtectOwners.hooks) != 0
	socketProtectOwners.RUnlock()
	if !active { return ctx, nil } // avoid allocations in the ordinary fast path
	scope := &socketProtectScope{}
	return context.WithValue(ctx, socketProtectScopeKey{}, scope), scope
}

// RegisterSocketProtectRollback is a no-op outside a scoped dial attempt.
func RegisterSocketProtectRollback(ctx context.Context, cleanup func()) {
	if ctx == nil || cleanup == nil { return }
	scope, ok := ctx.Value(socketProtectScopeKey{}).(*socketProtectScope)
	if !ok { return }
	scope.access.Lock()
	scope.undo = append(scope.undo, cleanup)
	scope.access.Unlock()
}

func (s *socketProtectScope) Rollback() {
	if s == nil { return }
	s.access.Lock()
	undo := s.undo
	s.undo = nil
	s.access.Unlock()
	for i := len(undo) - 1; i >= 0; i-- { undo[i]() }
}

// ApplySocketProtect preserves the original context-free listener API.
func ApplySocketProtect(network, address string, c syscall.RawConn) error {
	return ApplySocketProtectContext(context.Background(), network, address, c)
}

// ApplySocketProtectContext propagates only the dial-scoped cleanup context;
// vendor/legacy hook signatures remain unchanged.
func ApplySocketProtectContext(ctx context.Context, network, address string, c syscall.RawConn) error {
	if fn, loaded := DefaultSocketProtect.Load().(protectFn); loaded && fn != nil {
		if err := fn(ctx, network, address, c); err != nil { return err }
	}
	socketProtectOwners.RLock()
	hooks := make([]protectFn, 0, len(socketProtectOwners.hooks))
	for _, fn := range socketProtectOwners.hooks { hooks = append(hooks, fn) }
	socketProtectOwners.RUnlock()
	for _, fn := range hooks {
		if err := fn(ctx, network, address, c); err != nil { return err }
	}
	return nil
}
