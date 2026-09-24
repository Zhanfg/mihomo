package dialer

import (
	"context"
	"net"
	"syscall"
	"sync/atomic"
)

// SocketControl
// never change type traits because it's used in CMFA
type SocketControl func(network, address string, conn syscall.RawConn) error

// DefaultSocketHook
// never change type traits because it's used in CMFA
var DefaultSocketHook SocketControl

type socketHookHolder struct {
	hook SocketControl
}

var additionalSocketHook atomic.Pointer[socketHookHolder]

// SetAdditionalSocketHook installs an additive socket hook without changing
// DefaultSocketHook semantics used by CMFA. Socket creation is a hot path, so
// readers use a lock-free atomic pointer while configuration changes remain
// rare.
func SetAdditionalSocketHook(hook SocketControl) {
	if hook == nil {
		additionalSocketHook.Store(nil)
		return
	}
	additionalSocketHook.Store(&socketHookHolder{hook: hook})
}

func runAdditionalSocketHook(network, address string, conn syscall.RawConn) error {
	holder := additionalSocketHook.Load()
	if holder == nil {
		return nil
	}
	return holder.hook(network, address, conn)
}

func additionalSocketHookToDialer(dialer *net.Dialer) {
	addControlToDialer(dialer, func(ctx context.Context, network, address string, c syscall.RawConn) error {
		return runAdditionalSocketHook(network, address, c)
	})
}

func additionalSocketHookToListenConfig(lc *net.ListenConfig) {
	addControlToListenConfig(lc, func(ctx context.Context, network, address string, c syscall.RawConn) error {
		return runAdditionalSocketHook(network, address, c)
	})
}

func socketHookToToDialer(dialer *net.Dialer) {
	addControlToDialer(dialer, func(ctx context.Context, network, address string, c syscall.RawConn) error {
		return DefaultSocketHook(network, address, c)
	})
}

func socketHookToListenConfig(lc *net.ListenConfig) {
	addControlToListenConfig(lc, func(ctx context.Context, network, address string, c syscall.RawConn) error {
		return DefaultSocketHook(network, address, c)
	})
}
