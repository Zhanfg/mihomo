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

// ApplySocketProtect invokes the active socket-protect function, if any. It is
// exposed so socket-creation paths outside this package (for example inbound
// listener control chains) can register their sockets with the eBPF inbound.
func ApplySocketProtect(network, address string, c syscall.RawConn) error {
	if fn, loaded := DefaultSocketProtect.Load().(protectFn); loaded && fn != nil {
		if err := fn(context.Background(), network, address, c); err != nil {
			return err
		}
	}
	socketProtectOwners.RLock()
	hooks := make([]protectFn, 0, len(socketProtectOwners.hooks))
	for _, fn := range socketProtectOwners.hooks {
		hooks = append(hooks, fn)
	}
	socketProtectOwners.RUnlock()
	for _, fn := range hooks {
		if err := fn(context.Background(), network, address, c); err != nil {
			return err
		}
	}
	return nil
}
