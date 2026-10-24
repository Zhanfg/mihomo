package smart

import (
	"net/netip"
	"strings"
	"sync"
	"time"
)

// Region suspicion requires distinct measured public exits and independent
// evidence that the same target answers from another region. It never blocks
// sockets or modifies Bandit/LightGBM weights by itself.
const (
	exitWitnessWindow = 10 * time.Minute
	exitSuspectTTL = 20 * time.Minute
	exitWitnessMaxTargets = 4096
	exitWitnessMaxIPsPerRegion = 16
)

type exitTargetWitness struct {
	failures map[string]map[string]int64
	controls map[string]int64
	suspected map[string]int64
	updated int64
}

type ExitRegionWatcher struct {
	mu sync.Mutex
	targets map[string]*exitTargetWitness
	lastSweep time.Time
}

func NewExitRegionWatcher() *ExitRegionWatcher {
	return &ExitRegionWatcher{targets: make(map[string]*exitTargetWitness)}
}

func normalizeExitRegion(region string) string {
	region = strings.ToUpper(strings.TrimSpace(region))
	if len(region) != 2 || region[0] < 'A' || region[0] > 'Z' ||
		region[1] < 'A' || region[1] > 'Z' {
		return ""
	}
	return region
}

func validExitWitnessIP(raw string) bool {
	ip, err := netip.ParseAddr(raw)
	return err == nil && ip.IsValid() && ip.IsGlobalUnicast() && !ip.IsPrivate()
}

// Observe reports true only when a previously unproven regional refusal gains
// sufficient corroboration: two distinct public IP exits from that region
// refused the target AND a different region had a recent successful control.
// A regional success clears suspicion for that region immediately.
func (w *ExitRegionWatcher) Observe(target, region, publicIP string, success bool, now time.Time) bool {
	if w == nil || target == "" || !validExitWitnessIP(publicIP) {
		return false
	}
	region = normalizeExitRegion(region)
	if region == "" {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.targets == nil {
		w.targets = make(map[string]*exitTargetWitness)
	}
	if w.lastSweep.IsZero() || now.Sub(w.lastSweep) >= time.Minute || len(w.targets) >= exitWitnessMaxTargets {
		for key, state := range w.targets {
			if now.UnixNano()-state.updated > int64(exitSuspectTTL) {
				delete(w.targets, key)
			}
		}
		w.lastSweep = now
	}
	state := w.targets[target]
	if state == nil {
		if len(w.targets) >= exitWitnessMaxTargets {
			return false
		}
		state = &exitTargetWitness{
			failures: make(map[string]map[string]int64),
			controls: make(map[string]int64),
			suspected: make(map[string]int64),
		}
		w.targets[target] = state
	}
	state.updated = now.UnixNano()
	cutoff := now.Add(-exitWitnessWindow).UnixNano()
	for r, ips := range state.failures {
		for ip, at := range ips {
			if at < cutoff {
				delete(ips, ip)
			}
		}
		if len(ips) == 0 {
			delete(state.failures, r)
		}
	}
	for r, at := range state.controls {
		if at < cutoff {
			delete(state.controls, r)
		}
	}
	for r, until := range state.suspected {
		if until <= now.UnixNano() {
			delete(state.suspected, r)
		}
	}

	if success {
		state.controls[region] = now.UnixNano()
		delete(state.failures, region)
		delete(state.suspected, region)
	} else {
		ips := state.failures[region]
		if ips == nil {
			ips = make(map[string]int64)
			state.failures[region] = ips
		}
		if len(ips) < exitWitnessMaxIPsPerRegion || ips[publicIP] != 0 {
			ips[publicIP] = now.UnixNano()
		}
	}

	newlySuspected := false
	for failureRegion, ips := range state.failures {
		if len(ips) < 2 || state.suspected[failureRegion] > now.UnixNano() {
			continue
		}
		for controlRegion := range state.controls {
			if controlRegion != failureRegion {
				state.suspected[failureRegion] = now.Add(exitSuspectTTL).UnixNano()
				newlySuspected = true
				break
			}
		}
	}
	return newlySuspected
}

func (w *ExitRegionWatcher) Suspected(target, region string, now time.Time) bool {
	if w == nil || target == "" {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	state := w.targets[target]
	return state != nil && state.suspected[normalizeExitRegion(region)] > now.UnixNano()
}
