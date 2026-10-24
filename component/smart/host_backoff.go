package smart

import (
	"strings"
	"sync"
	"time"
)

// The host-refusal gate complements V11's node-level probe throttle. A site-wide
// refusal observed across distinct nodes is not reliable evidence against any
// particular proxy.
const (
	hostBackoffMinNodes   = 3
	hostBackoffMaxEntries = 4096
	hostBackoffWindow     = time.Hour
	hostBackoffTTL        = 30 * time.Minute
)

type hostBackoffKey struct {
	config string
	group  string
	host   string
}

type hostBackoffState struct {
	nodes     map[string]int64
	updated   int64
	until     int64
	probation bool
}

type hostBackoff struct {
	mu      sync.RWMutex
	entries map[hostBackoffKey]hostBackoffState
}

var hostBackoffTable hostBackoff

func hostRefusalEvidence(reason string) bool {
	switch reason {
	case "region unavailable", "forbidden", "origin error",
		"unknown certificate authority", "certificate hostname mismatch":
		return true
	}
	return false
}

func normalizedProbeHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func (t *hostBackoff) note(config, group, host, node, reason string, now time.Time) bool {
	host = normalizedProbeHost(host)
	if host == "" || node == "" || !hostRefusalEvidence(reason) {
		return false
	}
	key := hostBackoffKey{config: config, group: group, host: host}
	nowNano := now.UnixNano()

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.entries == nil {
		t.entries = make(map[hostBackoffKey]hostBackoffState)
	}
	state, exists := t.entries[key]
	if state.until > nowNano {
		return true
	}
	// After an unanswered host-wide hold expires, a single new refusal
	// re-arms the hold. Any successful answer explicitly clears probation.
	if state.probation {
		state.probation = false
		state.nodes = nil
		state.updated = nowNano
		state.until = now.Add(hostBackoffTTL).UnixNano()
		t.entries[key] = state
		return true
	}
	if !exists && len(t.entries) >= hostBackoffMaxEntries {
		t.pruneLocked(nowNano)
		if len(t.entries) >= hostBackoffMaxEntries {
			// Cap memory rather than unboundedly recording new hosts.
			return false
		}
	}
	if state.nodes == nil {
		state.nodes = make(map[string]int64)
	}
	state.nodes[node] = nowNano
	for seen, at := range state.nodes {
		if nowNano-at > int64(hostBackoffWindow) {
			delete(state.nodes, seen)
		}
	}
	state.updated = nowNano
	if len(state.nodes) >= hostBackoffMinNodes {
		state.nodes = nil
		state.probation = true
		state.until = now.Add(hostBackoffTTL).UnixNano()
	}
	t.entries[key] = state
	return state.until > nowNano
}

func (t *hostBackoff) held(config, group, host string, now time.Time) bool {
	host = normalizedProbeHost(host)
	if host == "" {
		return false
	}
	key := hostBackoffKey{config: config, group: group, host: host}
	t.mu.RLock()
	state := t.entries[key]
	t.mu.RUnlock()
	return state.until > now.UnixNano()
}

func (t *hostBackoff) answer(config, group, host string) {
	host = normalizedProbeHost(host)
	if host == "" {
		return
	}
	t.mu.Lock()
	delete(t.entries, hostBackoffKey{config: config, group: group, host: host})
	t.mu.Unlock()
}

func (t *hostBackoff) pruneLocked(nowNano int64) {
	for key, state := range t.entries {
		if nowNano-state.updated > int64(hostBackoffWindow) {
			delete(t.entries, key)
		}
	}
}

// NoteHostRefusal records host-level evidence across distinct nodes. It returns
// true if the host is already held or has now reached the independent-node
// threshold; callers must not penalize another node for that response.
func NoteHostRefusal(config, group, host, node, reason string) bool {
	return hostBackoffTable.note(config, group, host, node, reason, time.Now())
}

// HostBackedOff avoids spending network, CPU and model work probing a site
// already known to refuse multiple independent nodes.
func HostBackedOff(config, group, host string) bool {
	return hostBackoffTable.held(config, group, host, time.Now())
}

// NoteHostAnswer clears host-wide refusal evidence when any node can answer.
func NoteHostAnswer(config, group, host string) {
	hostBackoffTable.answer(config, group, host)
}
