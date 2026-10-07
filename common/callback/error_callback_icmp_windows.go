//go:build windows

package callback

import (
	"errors"
	"syscall"
)

func isICMPRefusal(err error) bool {
	return errors.Is(err, syscall.WSAECONNRESET) ||
		errors.Is(err, syscall.Errno(10061)) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET)
}
