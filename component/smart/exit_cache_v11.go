package smart

import (
	"encoding/json"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// KeyTypeExit uses an isolated keyspace so node statistics, Bandit weights,
// cache flush and model persistence remain backward compatible.
const (
	KeyTypeExit = "exit"
	exitIdentityTTL = 6 * time.Hour
	exitIdentityMaxEntries = 4096
)

type ExitIdentity struct {
	IP string
	Region string
	ASN string
	Observed int64
}

type exitCacheKey struct {
	Config string
	Group string
	Node string
	IPv6 bool
}
func (k exitCacheKey) storageKey() string {
	family := "v4"
	if k.IPv6 { family = "v6" }
	return FormatDBKey(KeyTypeExit, k.Config, k.Group, k.Node, family)
}

type ExitCache struct {
	mu sync.RWMutex
	persistMu sync.Mutex
	items map[exitCacheKey]ExitIdentity
	store *Store
}

func NewExitCache(store *Store) *ExitCache {
	return &ExitCache{items: make(map[exitCacheKey]ExitIdentity), store: store}
}

func validExitIP(ip netip.Addr, ipv6 bool) bool {
	return ip.IsValid() && ip.Is6() == ipv6 &&
		ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback()
}

// Observe uses only already measured current-network evidence. Never probe
// or change node weights here. Writes occur off the Smart dial hot path.
func (c *ExitCache) Observe(config, group, node string, ipv6 bool, ip netip.Addr, region string, now time.Time) {
	if c == nil || config == "" || group == "" || node == "" { return }
	if ip.IsValid() {
		ip = ip.Unmap()
		if !validExitIP(ip, ipv6) { return }
	}
	region = strings.ToUpper(strings.TrimSpace(region))
	if !ip.IsValid() && region == "" { return }
	entry := ExitIdentity{Region: region, Observed: now.Unix()}
	if ip.IsValid() { entry.IP = ip.String() }
	k := exitCacheKey{config, group, node, ipv6}

	c.mu.Lock()
	if previous, ok := c.items[k]; ok &&
		previous.IP == entry.IP && previous.Region == entry.Region &&
		now.Unix()-previous.Observed >= 0 &&
		now.Unix()-previous.Observed < int64(exitIdentityTTL.Seconds()) {
		c.mu.Unlock()
		return
	}
	if len(c.items) >= exitIdentityMaxEntries {
		var oldest exitCacheKey
		var oldestAt int64 = 1<<63 - 1
		for key, existing := range c.items {
			if existing.Observed < oldestAt {
				oldest, oldestAt = key, existing.Observed
			}
		}
		delete(c.items, oldest)
	}
	c.items[k] = entry
	c.mu.Unlock()

	if c.store != nil {
		data, err := json.Marshal(entry)
		if err == nil {
			go func() {
				c.persistMu.Lock()
				defer c.persistMu.Unlock()
				// Last observation wins even if goroutines are scheduled out of order.
				c.mu.RLock()
				current := c.items[k]
				equal := current == entry
				c.mu.RUnlock()
				if equal { _ = c.store.DBBatchPutItem(k.storageKey(), data) }
			}()
		}
	}
}

// Cached returns only observations from this process. A restart or handover
// requires the existing capability subsystem to confirm current exit identity.
func (c *ExitCache) Cached(config, group, node string, ipv6 bool, now time.Time) (ExitIdentity, bool) {
	if c == nil { return ExitIdentity{}, false }
	k := exitCacheKey{config,group,node,ipv6}
	c.mu.RLock()
	v, found := c.items[k]
	c.mu.RUnlock()
	if !found || v.Observed <= 0 ||
		now.Unix()-v.Observed < 0 ||
		now.Unix()-v.Observed >= int64(exitIdentityTTL.Seconds()) {
		return ExitIdentity{}, false
	}
	return v, true
}

// Persisted is diagnostics-only. It never populates the live routing cache,
// so yesterday's country/IP cannot be used to judge today's node.
func (c *ExitCache) Persisted(config, group, node string, ipv6 bool, now time.Time) (ExitIdentity, bool) {
	if c == nil || c.store == nil { return ExitIdentity{}, false }
	key := exitCacheKey{config,group,node,ipv6}
	data, err := c.store.DBViewGetItem(key.storageKey())
	if err != nil { return ExitIdentity{}, false }
	var entry ExitIdentity
	if json.Unmarshal(data, &entry) != nil ||
		entry.Observed <= 0 ||
		now.Unix()-entry.Observed < 0 ||
		now.Unix()-entry.Observed >= int64(exitIdentityTTL.Seconds()) {
		return ExitIdentity{}, false
	}
	if entry.IP != "" {
		ip, err := netip.ParseAddr(entry.IP)
		if err != nil || !validExitIP(ip, ipv6) { return ExitIdentity{}, false }
	}
	return entry, true
}
