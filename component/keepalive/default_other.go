//go:build !android

package keepalive

import "time"

func platformKeepAliveIdle() time.Duration     { return 0 }
func platformKeepAliveInterval() time.Duration { return 0 }

func platformKeepAliveCount() int { return 0 }
