package dialer

import (
	"context"
	"net"
	"syscall"
	"sync"
)

// SocketControl
// never change type traits because it's used in CMFA
type SocketControl func(network, address string, conn syscall.RawConn) error

// DefaultSocketHook
// never change type traits because it's used in CMFA
var DefaultSocketHook SocketControl

var (
	additionalSocketHookMu sync.RWMutex
	additionalSocketHook   SocketControl
)

// SetAdditionalSocketHook installs an additive socket hook without changing
// DefaultSocketHook semantics used by CMFA. The hook is applied after normal
// interface/routing controls and is intended for features such as eBPF
// socket-cookie self bypass.
func SetAdditionalSocketHook(hook SocketControl) {
	additionalSocketHookMu.Lock()
	additionalSocketHook = hook
	additionalSocketHookMu.Unlock()
}

func runAdditionalSocketHook(network, address string, conn syscall.RawConn) error {
	additionalSocketHookMu.RLock()
	hook := additionalSocketHook
	additionalSocketHookMu.RUnlock()
	if hook == nil {
		return nil
	}
	return hook(network, address, conn)
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
