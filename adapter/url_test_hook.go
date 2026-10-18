package adapter

import "sync/atomic"

// URLTestHook is optional and fires after a proxy delay test completes,
// including tests scheduled by providers. It does not run in the dial data path.
type URLTestHook func(url, proxyName string, delay uint16)

var urlTestObserver atomic.Pointer[URLTestHook]

// SetURLTestHook installs or removes an optional frontend observer.
func SetURLTestHook(fn func(url, proxyName string, delay uint16)) {
	if fn == nil {
		urlTestObserver.Store(nil)
		return
	}
	hook := URLTestHook(fn)
	urlTestObserver.Store(&hook)
}

func notifyURLTestHook(url, proxyName string, delay uint16) {
	if hook := urlTestObserver.Load(); hook != nil {
		(*hook)(url, proxyName, delay)
	}
}
