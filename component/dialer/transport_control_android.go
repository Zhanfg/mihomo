//go:build android

package dialer

import (
	"context"
	"syscall"

	"golang.org/x/sys/unix"
)

const androidTCPUserTimeoutMS = 30_000

// defaultTransportControl makes an actively black-holed TCP tunnel fail in a
// bounded time instead of waiting through minutes of kernel retransmissions.
// It is best-effort: unsupported/vendor kernels must never make a dial fail.
func defaultTransportControl(_ context.Context, network, _ string, c syscall.RawConn) error {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil
	}
	return c.Control(func(fd uintptr) {
		_ = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_USER_TIMEOUT, androidTCPUserTimeoutMS)
	})
}
