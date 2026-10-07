//go:build !android

package dialer

import (
	"context"
	"syscall"
)

func defaultTransportControl(context.Context, string, string, syscall.RawConn) error {
	return nil
}
