package smart

import "time"

// Pending and unproven states must never become permanent node failures.
func (t *ProbeThrottle) MarkPending(target, node string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ensureMaps()
	key := probeKey(target, node)
	state := t.nodes[key]
	if state == nil {
		state = &probeNodeState{lastProbe: now}
		t.nodes[key] = state
	}
	state.pendingSince = now
	state.unprovenSince = time.Time{}
}

func (t *ProbeThrottle) ClearPending(target, node string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if state := t.nodes[probeKey(target, node)]; state != nil {
		state.pendingSince = time.Time{}
	}
}

func (t *ProbeThrottle) PendingFor(target, node string, now time.Time) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	if state := t.nodes[probeKey(target, node)]; state != nil && !state.pendingSince.IsZero() {
		if duration := now.Sub(state.pendingSince); duration > 0 {
			return duration
		}
	}
	return 0
}

func (t *ProbeThrottle) MarkUnproven(target, node string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ensureMaps()
	key := probeKey(target, node)
	state := t.nodes[key]
	if state == nil {
		state = &probeNodeState{lastProbe: now}
		t.nodes[key] = state
	}
	state.unprovenSince = now
}

func (t *ProbeThrottle) ClearUnproven(target, node string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if state := t.nodes[probeKey(target, node)]; state != nil {
		state.unprovenSince = time.Time{}
	}
}

func (t *ProbeThrottle) Unproven(target, node string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.nodes[probeKey(target, node)]
	return state != nil && !state.unprovenSince.IsZero()
}
