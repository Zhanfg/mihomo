package smart

import (
	"encoding/json"
	"testing"
	"time"

	C "github.com/metacubex/mihomo/constant"

	"github.com/stretchr/testify/require"
)

// seedHostStatus installs a HostStatus straight into both caches, the way
// TestCheckHostStatusConcurrentUpdate does: the global write queue runs
// asynchronous flushes and is not a stable fixture.
func seedHostStatus(t *testing.T, group, config, wildcardTarget string, hs *HostStatus) string {
	t.Helper()
	InitCache()
	InitQueue()
	db = nil
	hostStatusCache.Clear()
	dbResultCache.Clear()

	data, err := json.Marshal(hs)
	require.NoError(t, err)
	cachePath := FormatDBKey(KeyTypeHostFailures, config, group, wildcardTarget)
	hs.initOnce.Do(func() {})
	hostStatusCache.Set(cachePath, hs)
	dbResultCache.Set(FormatDBKey(KeyTypeHostFailures, config, group), map[string][]byte{
		cachePath: data,
	})
	return cachePath
}

func blockedNode(node, host string, expiresIn time.Duration) *CodeNodeSet {
	return &CodeNodeSet{
		Nodes:     map[string]int64{node: time.Now().Add(expiresIn).Unix()},
		NodeHosts: map[string]string{node: host},
	}
}

// A blocked node is dropped outright at dial time, so a recovery probe is the
// only thing that can return it to service before the 24-hour TTL. The sweep
// used to be built from Codes[2] alone -- the one code reached by probing --
// which left code 3 (dial or close failure) and codes 4, 5 and 6 (zero
// traffic, low weight, packet loss) excluded for a full day with no way back.
func TestCheckHostStatusProbesEveryRecoverableCode(t *testing.T) {
	const (
		group          = "group"
		config         = "config"
		wildcardTarget = "example.com"
	)
	seedHostStatus(t, group, config, wildcardTarget, &HostStatus{Codes: map[int]*CodeNodeSet{
		2: blockedNode("node-abnormal", "a.example.com", time.Hour),
		3: blockedNode("node-dial", "b.example.com", time.Hour),
		4: blockedNode("node-zero-traffic", "c.example.com", time.Hour),
		5: blockedNode("node-low-weight", "d.example.com", time.Hour),
		6: blockedNode("node-loss", "e.example.com", time.Hour),
	}})

	store := &Store{}
	result, err := store.CheckHostStatus(group, config, 1_000)
	require.NoError(t, err)

	probes := result[wildcardTarget]
	for node, host := range map[string]string{
		"node-abnormal":     "a.example.com",
		"node-dial":         "b.example.com",
		"node-zero-traffic": "c.example.com",
		"node-low-weight":   "d.example.com",
		"node-loss":         "e.example.com",
	} {
		require.Equalf(t, host, probes[node], "%s is blocked with no way back before the TTL", node)
	}
}

// Code 1 is the dashboard's manual block. It is deliberately permanent and
// must never be probed back into service behind the user's back.
func TestCheckHostStatusNeverProbesAManualBlock(t *testing.T) {
	const (
		group          = "group"
		config         = "config"
		wildcardTarget = "manual.example.com"
	)
	manual := blockedNode("node-manual", "manual.example.com", time.Hour)
	manual.Nodes["node-manual"] = 0 // TTL 0 means permanent
	seedHostStatus(t, group, config, wildcardTarget, &HostStatus{Codes: map[int]*CodeNodeSet{
		1: manual,
	}})

	store := &Store{}
	result, err := store.CheckHostStatus(group, config, 1_000)
	require.NoError(t, err)
	require.Empty(t, result[wildcardTarget], "a manual block was queued for a recovery probe")
}

// A probe needs somewhere to aim. Recording the host only for code 2 meant
// that even once the other codes were swept there would be nothing to test.
func TestUpdateHostStatusRecordsTheHostForEveryBlockingCode(t *testing.T) {
	const (
		group          = "group"
		config         = "config"
		wildcardTarget = "example.com"
		node           = "node-a"
	)
	for _, blockCode := range []int64{2, 4, 5, 6} {
		seedHostStatus(t, group, config, wildcardTarget, &HostStatus{})

		store := &Store{}
		metadata := &C.Metadata{Host: "probe.example.com"}
		store.UpdateHostStatus(group, config, wildcardTarget, metadata, node, 3, 1_000, true, true, blockCode)

		cached, ok := hostStatusCache.Get(FormatDBKey(KeyTypeHostFailures, config, group, wildcardTarget))
		require.True(t, ok)
		codeSet := cached.Codes[int(blockCode)]
		require.NotNilf(t, codeSet, "code %d recorded no block at all", blockCode)
		require.Equalf(t, "probe.example.com", codeSet.NodeHosts[node],
			"code %d blocked a node without recording where to probe it", blockCode)
	}
}

// Code 3 only blocks once the failures pile up; until then it is a counter,
// and a counter is not something to probe.
func TestUpdateHostStatusRecordsTheHostWhenCode3Blocks(t *testing.T) {
	const (
		group          = "group"
		config         = "config"
		wildcardTarget = "example.com"
		node           = "node-a"
		maxFailedTimes = 2
	)
	seedHostStatus(t, group, config, wildcardTarget, &HostStatus{})

	store := &Store{}
	cachePath := FormatDBKey(KeyTypeHostFailures, config, group, wildcardTarget)
	metadata := &C.Metadata{Host: "probe.example.com"}

	store.UpdateHostStatus(group, config, wildcardTarget, metadata, node, maxFailedTimes, 1_000, true, true, 3)
	cached, ok := hostStatusCache.Get(cachePath)
	require.True(t, ok)
	require.Empty(t, cached.Codes[3].Nodes, "a single failure blocked the node outright")

	store.UpdateHostStatus(group, config, wildcardTarget, metadata, node, maxFailedTimes, 1_000, true, true, 3)
	cached, ok = hostStatusCache.Get(cachePath)
	require.True(t, ok)
	require.NotEmpty(t, cached.Codes[3].Nodes, "the node never blocked despite reaching the failure limit")
	require.Equal(t, "probe.example.com", cached.Codes[3].NodeHosts[node],
		"code 3 blocked a node without recording where to probe it")
}

// A connection that completes cleanly is the positive evidence a block was
// waiting for, and it has to undo every code the node collected -- except the
// manual one, which is the user's decision and not the network's.
func TestUpdateHostStatusClearsEveryRecoverableBlockOnSuccess(t *testing.T) {
	const (
		group          = "group"
		config         = "config"
		wildcardTarget = "example.com"
		node           = "node-a"
	)
	codes := map[int]*CodeNodeSet{}
	for _, code := range []int{1, 2, 3, 4, 5, 6} {
		codes[code] = blockedNode(node, "probe.example.com", time.Hour)
	}
	codes[1].Nodes[node] = 0
	seedHostStatus(t, group, config, wildcardTarget, &HostStatus{Codes: codes})

	store := &Store{}
	metadata := &C.Metadata{Host: "probe.example.com"}
	store.UpdateHostStatus(group, config, wildcardTarget, metadata, node, 3, 1_000, false, true, 0)

	cached, ok := hostStatusCache.Get(FormatDBKey(KeyTypeHostFailures, config, group, wildcardTarget))
	require.True(t, ok)
	for _, code := range []int{2, 3, 4, 5, 6} {
		if codeSet := cached.Codes[code]; codeSet != nil {
			require.NotContainsf(t, codeSet.Nodes, node, "a clean close left the code %d block in place", code)
		}
	}
	require.NotNil(t, cached.Codes[1], "a clean close lifted the user's manual block")
	require.Contains(t, cached.Codes[1].Nodes, node, "a clean close lifted the user's manual block")
}

// A recovery probe that fails re-blocks the node, and the probe is the only
// thing that ever re-blocks a node it has just tested. Minting a fresh TTL
// there turns a bounded exclusion into a permanent one: a host that answers a
// bare GET with a banned status fails every probe, and the probe comes round
// every four hours, so the pair is never released at all.
func TestUpdateHostStatusReblockNeverExtendsTheDeadline(t *testing.T) {
	const (
		group          = "group"
		config         = "config"
		wildcardTarget = "example.com"
		node           = "node-a"
	)
	original := time.Now().Add(2 * time.Hour).Unix()
	codes := map[int]*CodeNodeSet{4: {
		Nodes:     map[string]int64{node: original},
		NodeHosts: map[string]string{node: "probe.example.com"},
	}}
	seedHostStatus(t, group, config, wildcardTarget, &HostStatus{Codes: codes})

	store := &Store{}
	metadata := &C.Metadata{Host: "probe.example.com"}
	// What a failed recovery probe does: re-block as code 2.
	store.UpdateHostStatus(group, config, wildcardTarget, metadata, node, 3, 1_000, true, true, 2)

	cached, ok := hostStatusCache.Get(FormatDBKey(KeyTypeHostFailures, config, group, wildcardTarget))
	require.True(t, ok)
	require.NotNil(t, cached.Codes[2])
	require.Equalf(t, original, cached.Codes[2].Nodes[node],
		"the re-block moved the deadline to %d; the node is now excluded for another full TTL and the probe will do this again in four hours",
		cached.Codes[2].Nodes[node])
}

// A first block still gets the full TTL -- the clamp only ever holds a
// deadline back, it never shortens a new one.
func TestUpdateHostStatusFirstBlockGetsTheFullTTL(t *testing.T) {
	const (
		group          = "group"
		config         = "config"
		wildcardTarget = "example.com"
		node           = "node-a"
	)
	seedHostStatus(t, group, config, wildcardTarget, &HostStatus{})

	store := &Store{}
	metadata := &C.Metadata{Host: "probe.example.com"}
	before := time.Now().Add(HostFailureNodeTTL).Unix()
	store.UpdateHostStatus(group, config, wildcardTarget, metadata, node, 3, 1_000, true, true, 4)

	cached, ok := hostStatusCache.Get(FormatDBKey(KeyTypeHostFailures, config, group, wildcardTarget))
	require.True(t, ok)
	require.GreaterOrEqual(t, cached.Codes[4].Nodes[node], before)
}

// An expired block is not a block: nothing is excluding the node, so probing it
// can only mint a new one for a node that is serving fine. Nothing sweeps
// expired entries for a target that went quiet, so they would otherwise sit
// there being probed forever.
func TestCheckHostStatusSkipsALapsedBlock(t *testing.T) {
	const (
		group          = "group"
		config         = "config"
		wildcardTarget = "example.com"
	)
	codes := map[int]*CodeNodeSet{
		4: blockedNode("node-lapsed", "lapsed.example.com", -time.Hour),
		5: blockedNode("node-live", "live.example.com", time.Hour),
	}
	seedHostStatus(t, group, config, wildcardTarget, &HostStatus{Codes: codes})

	store := &Store{}
	result, err := store.CheckHostStatus(group, config, 1_000)
	require.NoError(t, err)

	probes := result[wildcardTarget]
	require.NotContains(t, probes, "node-lapsed", "a block that already expired was queued for a probe")
	require.Equal(t, "live.example.com", probes["node-live"], "the live block stopped being probed")
}
