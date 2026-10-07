//go:build aix || android || darwin || dragonfly || freebsd || illumos || ios || linux || netbsd || openbsd || solaris

package callback

import (
	"errors"
	"syscall"
)

func isICMPRefusal(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET)
}
