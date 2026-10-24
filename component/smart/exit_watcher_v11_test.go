package smart

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestExitRegionNeedsDistinctExitsAndCrossRegionControl(t *testing.T) {
	w := NewExitRegionWatcher()
	now := time.Unix(1700000000, 0)
	if w.Observe("target", "US", "1.1.1.1", false, now) {
		t.Fatal("one IP refusal")
	}
	if w.Observe("target", "us", "1.1.1.1", false, now.Add(time.Second)) {
		t.Fatal("duplicate IP counted twice")
	}
	if w.Observe("target", "US", "8.8.8.8", false, now.Add(2*time.Second)) {
		t.Fatal("no cross-region control")
	}
	if w.Suspected("target", "US", now) {
		t.Fatal("unconfirmed suspicion")
	}
	if !w.Observe("target", "JP", "9.9.9.9", true, now.Add(3*time.Second)) {
		t.Fatal("independent witness did not trigger region suspicion")
	}
	if !w.Suspected("target", "us", now.Add(4*time.Second)) {
		t.Fatal("region not suspected")
	}
	if w.Suspected("other", "US", now.Add(4*time.Second)) {
		t.Fatal("target isolation violated")
	}
	if w.Observe("target", "JP", "9.9.9.9", true, now.Add(5*time.Second)) {
		t.Fatal("duplicate success must not re-raise suspicion")
	}
	if w.Observe("target", "US", "1.1.1.1", true, now.Add(6*time.Second)) {
		t.Fatal("successful suspected region must clear, not raise")
	}
	if w.Suspected("target", "US", now.Add(7*time.Second)) {
		t.Fatal("region success failed to clear suspicion")
	}
}
func TestExitRegionTTLAndInvalidEvidence(t *testing.T) {
	w := NewExitRegionWatcher()
	now := time.Unix(1700000000, 0)
	w.Observe("target", "US", "1.1.1.1", false, now)
	w.Observe("target", "US", "8.8.8.8", false, now)
	if w.Observe("target", "JP", "9.9.9.9", true, now.Add(exitWitnessWindow+time.Second)) {
		t.Fatal("stale refusal treated as current evidence")
	}
	w.Observe("target", "US", "192.168.1.9", false, now)
	if len(w.targets["target"].failures["US"]) != 0 {
		t.Fatal("invalid private exit evidence counted")
	}
}
func TestExitRegionBoundedAndConcurrent(t *testing.T) {
	w := NewExitRegionWatcher()
	now := time.Unix(1700000000, 0)
	var wg sync.WaitGroup
	for i:=0;i<32;i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j:=0;j<150;j++ {
				name := fmt.Sprintf("target-%d-%d", i, j)
				w.Observe(name, "US", "1.1.1.1", false, now.Add(time.Duration(j)*time.Millisecond))
				w.Suspected(name, "US", now)
			}
		}(i)
	}
	wg.Wait()
	if len(w.targets)>exitWitnessMaxTargets {
		t.Fatalf("unbounded watcher: %d",len(w.targets))
	}
}
