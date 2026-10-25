package smart

import (
	"sync"
	"testing"
	"time"
)
func TestProbeLifecycleIsolationAndClearing(t *testing.T) {
	var p ProbeThrottle
	now := time.Unix(1700000000, 0)
	p.MarkPending("site1", "node1", now)
	if got := p.PendingFor("site1", "node1", now.Add(2*time.Second)); got != 2*time.Second { t.Fatalf("pending: %s", got) }
	if p.PendingFor("site2", "node1", now.Add(time.Second)) != 0 { t.Fatal("cross target leak") }
	if p.AllowNode("site1", "node1", now.Add(3*time.Second)) { t.Fatal("duplicate pending probe") }
	p.MarkUnproven("site1", "node1", now.Add(3*time.Second))
	if !p.Unproven("site1", "node1") { t.Fatal("unproven state lost") }
	p.ClearPending("site1", "node1")
	if p.PendingFor("site1", "node1", now.Add(5*time.Second)) != 0 { t.Fatal("pending not cleared") }
	p.ClearUnproven("site1", "node1")
	if p.Unproven("site1", "node1") { t.Fatal("unproven not cleared") }
}
func TestProbeLifecycleRace(t *testing.T) {
	var p ProbeThrottle
	var wg sync.WaitGroup
	now := time.Now()
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.MarkPending("target", "node", now)
			_ = p.PendingFor("target", "node", now.Add(time.Second))
			p.MarkUnproven("target", "node", now)
			_ = p.Unproven("target", "node")
			p.ClearPending("target", "node")
			p.ClearUnproven("target", "node")
		}()
	}
	wg.Wait()
}
