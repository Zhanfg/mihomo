package dialer

import (
	"context"
	"net"
	"syscall"
)

type controlFn = func(ctx context.Context, network, address string, c syscall.RawConn) error

// Nested control wrappers must register eBPF bypass only once, after vendor
// routing/bind/TFO controls have finished modifying the socket.
type socketProtectNestedKey struct{}

func addControlToListenConfig(lc *net.ListenConfig, fn controlFn) {
	llc := *lc
	lc.Control = func(network, address string, c syscall.RawConn) (err error) {
		switch {
		case llc.Control != nil:
			if err = llc.Control(network, address, c); err != nil {
				return
			}
		}
		if err = fn(context.Background(), network, address, c); err != nil {
			return
		}
		return ApplySocketProtect(network, address, c)
	}
}

func addControlToDialer(d *net.Dialer, fn controlFn) {
	ld := *d
	d.ControlContext = func(ctx context.Context, network, address string, c syscall.RawConn) (err error) {
		nested := ctx.Value(socketProtectNestedKey{}) != nil
		if !nested {
			ctx = context.WithValue(ctx, socketProtectNestedKey{}, true)
		}
		switch {
		case ld.ControlContext != nil:
			if err = ld.ControlContext(ctx, network, address, c); err != nil {
				return
			}
		case ld.Control != nil:
			if err = ld.Control(network, address, c); err != nil {
				return
			}
		}
		if err = fn(ctx, network, address, c); err != nil {
			return
		}
		if nested {
			return nil
		}
		return ApplySocketProtectContext(ctx, network, address, c)
	}
}
