package statistic

import "sync/atomic"

// requestJoinHook is an optional consumer-facing callback invoked only when a
// connection tracker is registered, never in the packet read/write path.
// Desktop clients such as Bettbox need this to preserve their request history
// and real-time request events without polling or patching the data plane.
type requestJoinHook func(Tracker)

var activeRequestJoinHook atomic.Pointer[requestJoinHook]

// SetRequestNotifyHook installs a lifecycle callback. A nil callback disables
// notifications. Updates and tracker registrations are race-safe.
func SetRequestNotifyHook(fn func(Tracker)) {
	if fn == nil {
		activeRequestJoinHook.Store(nil)
		return
	}
	hook := requestJoinHook(fn)
	activeRequestJoinHook.Store(&hook)
}

func notifyRequestJoin(tracker Tracker) {
	if hook := activeRequestJoinHook.Load(); hook != nil {
		(*hook)(tracker)
	}
}
