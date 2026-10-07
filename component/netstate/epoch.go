// Package netstate exposes a tiny process-wide network epoch and a cached
// description of the current default interface. It intentionally contains no
// probing logic: callers can invalidate link-scoped evidence without waking the
// radio or opening sockets.
package netstate

import (
	"sync"
	"sync/atomic"

	"github.com/metacubex/mihomo/component/iface"
)

type LocalProfile struct {
	Epoch     uint64
	Known     bool
	Interface string
	MTU       int
	IPv4      bool
	IPv6      bool
}

var (
	epoch atomic.Uint64

	profileMu       sync.Mutex
	defaultIfName   string
	cachedEpoch     uint64
	cachedLocal     LocalProfile
)

func CurrentEpoch() uint64 {
	value := epoch.Load()
	if value == 0 {
		return 1
	}
	return value
}

func Advance() uint64 {
	value := epoch.Add(1)
	if value == 1 {
		// zero means "not initialized" to old callers; keep published epochs
		// strictly positive.
		value = epoch.Add(1)
	}
	profileMu.Lock()
	cachedEpoch = 0
	profileMu.Unlock()
	return value
}

// SetDefaultInterface records the platform monitor's current default interface.
// It does not advance the epoch by itself; the same monitor calls netchange.Notify
// once for the actual transition, keeping one epoch per network change.
func SetDefaultInterface(name string) {
	profileMu.Lock()
	if defaultIfName != name {
		defaultIfName = name
		cachedEpoch = 0
	}
	profileMu.Unlock()
}

func Local() LocalProfile {
	current := CurrentEpoch()
	profileMu.Lock()
	defer profileMu.Unlock()
	if cachedEpoch == current {
		return cachedLocal
	}

	profile := LocalProfile{Epoch: current, Interface: defaultIfName}
	if defaultIfName != "" {
		if networkInterface, err := iface.ResolveInterface(defaultIfName); err == nil && networkInterface != nil {
			profile.Known = true
			profile.MTU = networkInterface.MTU
			for _, prefix := range networkInterface.Addresses {
				if !prefix.IsValid() {
					continue
				}
				addr := prefix.Addr().Unmap()
				profile.IPv4 = profile.IPv4 || addr.Is4()
				profile.IPv6 = profile.IPv6 || addr.Is6()
			}
		}
	}

	cachedEpoch = current
	cachedLocal = profile
	return profile
}
