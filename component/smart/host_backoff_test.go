package smart

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestHostBackoffDistinctNodesAndRecovery(t *testing.T) {
	var b hostBackoff
	now := time.Unix(500000, 0)
	const config, group, host = "conf", "smart", "Example.COM."

	if b.note(config, group, host, "node-1", "rate limited", now) {
		t.Fatal("node-specific rate limit must not hold the whole host")
	}
	if b.note(config, group, host, "node-1", "forbidden", now) {
		t.Fatal("one node is not independent evidence")
	}
	if b.note(config, group, host, "node-1", "forbidden", now.Add(time.Second)) {
		t.Fatal("repeated reports from one node must not reach the threshold")
	}
	if b.note(config, group, host, "node-2", "forbidden", now.Add(2*time.Second)) {
		t.Fatal("two distinct nodes must not hold host")
	}
	if b.held(config, group, host, now.Add(2*time.Second)) {
		t.Fatal("host unexpectedly held before third node")
	}
	if !b.note(config, group, host, "node-3", "origin error", now.Add(3*time.Second)) {
		t.Fatal("three distinct node refusals must hold host")
	}
	if !b.held(config, group, "example.com", now.Add(5*time.Second)) {
		t.Fatal("host normalization or held state broken")
	}
	if b.held(config, group, host, now.Add(hostBackoffTTL+4*time.Second)) {
		t.Fatal("host hold must expire")
	}
	if !b.note(config, group, host, "node-4", "forbidden", now.Add(hostBackoffTTL+5*time.Second)) {
		t.Fatal("first refusal after expired probation must re-arm hold")
	}
	b.answer(config, group, "EXAMPLE.COM")
	if b.held(config, group, host, now.Add(hostBackoffTTL+6*time.Second)) {
		t.Fatal("successful answer must clear host backoff")
	}
	if b.note(config, group, host, "node-1", "forbidden", now.Add(hostBackoffTTL+7*time.Second)) {
		t.Fatal("evidence must start fresh after successful answer")
	}
}

func TestHostBackoffIsolationAndStaleEvidence(t *testing.T) {
	var b hostBackoff
	now := time.Unix(500000, 0)
	for _, node := range []string{"a", "b"} {
		if b.note("conf", "group1", "same.example", node, "forbidden", now) {
			t.Fatal("premature hold")
		}
	}
	if b.note("conf", "group2", "same.example", "c", "forbidden", now) {
		t.Fatal("group isolation broken")
	}
	if b.note("other", "group1", "same.example", "d", "forbidden", now) {
		t.Fatal("config isolation broken")
	}
	if b.note("conf", "group1", "same.example", "c", "forbidden", now.Add(hostBackoffWindow+time.Second)) {
		t.Fatal("old evidence must age out of the window")
	}
	if b.held("conf", "group1", "same.example", now.Add(hostBackoffWindow+time.Second)) {
		t.Fatal("stale evidence incorrectly held host")
	}
}

func TestHostBackoffBoundedStorage(t *testing.T) {
	var b hostBackoff
	now := time.Unix(500000, 0)
	for i := range hostBackoffMaxEntries + 128 {
		b.note("conf", "smart", fmt.Sprintf("host-%d.example", i), "n1", "forbidden", now)
	}
	if len(b.entries) > hostBackoffMaxEntries {
		t.Fatalf("unbounded host memory: %d entries", len(b.entries))
	}
}

func TestHostBackoffConcurrentAccess(t *testing.T) {
	var b hostBackoff
	now := time.Unix(500000, 0)
	var wg sync.WaitGroup
	for i := range 64 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			node := fmt.Sprintf("node-%d", i)
			b.note("conf", "smart", "same.example", node, "forbidden", now)
			b.held("conf", "smart", "same.example", now)
			if i%2 == 0 {
				b.answer("other", "smart", "same.example")
			}
		}(i)
	}
	wg.Wait()
}
