package smart

import (
	"container/heap"
	"sync"
)

const activeTargetHardLimit = 4096

type activeTargetKey struct {
	target string
	udp    bool
}

type activeTargetGroup struct {
	mu         sync.Mutex
	active     map[activeTargetKey]int64
	dirty      map[activeTargetKey]int64
	ring       [activeTargetHardLimit]activeTargetKey
	ringUsed   int
	ringCursor int
}

var activeTargetGroups sync.Map // FormatDBKey("active", config, group) -> *activeTargetGroup

func activityGroupKey(config, group string) string {
	return FormatDBKey("active", config, group)
}

func activityGroupFor(config, group string) *activeTargetGroup {
	key := activityGroupKey(config, group)
	if value, ok := activeTargetGroups.Load(key); ok {
		return value.(*activeTargetGroup)
	}
	created := &activeTargetGroup{
		active: make(map[activeTargetKey]int64),
		dirty:  make(map[activeTargetKey]int64),
	}
	value, _ := activeTargetGroups.LoadOrStore(key, created)
	return value.(*activeTargetGroup)
}

// TouchActiveTarget is the O(1) hot-path replacement for discovering active
// targets by rescanning every persisted Smart record. The index is deliberately
// runtime-local: after a restart, the first real connection repopulates it and
// a cache miss can still load that target directly from bbolt.
func (s *Store) TouchActiveTarget(group, config, target string, isUDP bool, lastUsed int64) {
	if target == "" {
		return
	}
	g := activityGroupFor(config, group)
	k := activeTargetKey{target: target, udp: isUDP}

	g.mu.Lock()
	if _, exists := g.active[k]; !exists {
		if g.ringUsed < activeTargetHardLimit {
			g.ring[g.ringUsed] = k
			g.ringUsed++
		} else {
			// Fixed FIFO-ish admission ring: every new one-shot target replaces
			// exactly one old slot. This makes Touch O(1) even under an endless
			// random-subdomain stream instead of scanning all 4096 entries on
			// every insertion after saturation.
			victim := g.ring[g.ringCursor]
			delete(g.active, victim)
			delete(g.dirty, victim)
			g.ring[g.ringCursor] = k
			g.ringCursor++
			if g.ringCursor == activeTargetHardLimit {
				g.ringCursor = 0
			}
		}
	}
	g.active[k] = lastUsed
	g.dirty[k] = lastUsed
	g.mu.Unlock()
}

func topActiveTargets(values map[activeTargetKey]int64, limit int) []ActiveTarget {
	if limit <= 0 || len(values) == 0 {
		return nil
	}

	h := &targetMinHeap{}
	heap.Init(h)
	for key, lastUsed := range values {
		heap.Push(h, ActiveTarget{Target: key.target, IsUDP: key.udp, LastUsed: lastUsed})
		if h.Len() > limit {
			heap.Pop(h)
		}
	}

	result := make([]ActiveTarget, h.Len())
	for i := len(result) - 1; i >= 0; i-- {
		result[i] = heap.Pop(h).(ActiveTarget)
	}
	return result
}

func (s *Store) snapshotActiveTargets(group, config string, limit int) []ActiveTarget {
	value, ok := activeTargetGroups.Load(activityGroupKey(config, group))
	if !ok {
		return nil
	}
	g := value.(*activeTargetGroup)
	g.mu.Lock()
	defer g.mu.Unlock()
	return topActiveTargets(g.active, limit)
}

// takeDirtyTargets returns only targets that changed since the previous
// prefetch cycle. This turns periodic prefetch from O(all persisted targets)
// into O(targets that actually saw traffic in this interval).
func (s *Store) takeDirtyTargets(group, config string, limit int) []ActiveTarget {
	value, ok := activeTargetGroups.Load(activityGroupKey(config, group))
	if !ok {
		return nil
	}
	g := value.(*activeTargetGroup)
	g.mu.Lock()
	defer g.mu.Unlock()

	result := topActiveTargets(g.dirty, limit)
	for _, item := range result {
		delete(g.dirty, activeTargetKey{target: item.Target, udp: item.IsUDP})
	}
	return result
}

func clearTargetActivity(level, config, group string) {
	switch level {
	case "all":
		activeTargetGroups.Range(func(key, _ any) bool {
			activeTargetGroups.Delete(key)
			return true
		})
	case "config":
		prefix := FormatDBKey("active", config) + "/"
		activeTargetGroups.Range(func(key, _ any) bool {
			if k, ok := key.(string); ok && len(k) >= len(prefix) && k[:len(prefix)] == prefix {
				activeTargetGroups.Delete(key)
			}
			return true
		})
	case "group":
		activeTargetGroups.Delete(activityGroupKey(config, group))
	}
}
