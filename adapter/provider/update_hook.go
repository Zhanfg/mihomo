package provider

import "sync/atomic"

// ProviderUpdateHook is an optional lifecycle notification for clients. It
// fires after a proxy-provider's proxy set is updated and its lock is released.
// It covers scheduled and explicit updates without adding work to packet paths.
type ProviderUpdateHook func(name string)

var providerUpdateObserver atomic.Pointer[ProviderUpdateHook]

func SetProviderUpdateHook(fn func(name string)) {
	if fn == nil {
		providerUpdateObserver.Store(nil)
		return
	}
	hook := ProviderUpdateHook(fn)
	providerUpdateObserver.Store(&hook)
}

func notifyProviderUpdate(name string) {
	if hook := providerUpdateObserver.Load(); hook != nil {
		(*hook)(name)
	}
}
