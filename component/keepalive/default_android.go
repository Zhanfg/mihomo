//go:build android

package keepalive

import "time"

// Mobile NATs routinely reap idle TCP mappings. These defaults keep long-lived
// proxy sessions alive without a busy watchdog or application-level heartbeat.
// Explicit config always wins through EffectiveKeepAlive*.
func platformKeepAliveIdle() time.Duration     { return 60 * time.Second }
func platformKeepAliveInterval() time.Duration { return 20 * time.Second }
