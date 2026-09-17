package outboundgroup

import (
	"errors"
	"testing"

	"github.com/metacubex/mihomo/adapter/outbound"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/tunnel/statistic"
)

// sweepTracker is the smallest thing statistic.Manager will index and
// closeSameConnection will act on.
type sweepTracker struct {
	id     string
	info   *statistic.TrackerInfo
	chain  C.Chain
	closed bool
}

func newSweepTracker(id, target, node, group string) *sweepTracker {
	return &sweepTracker{
		id:    id,
		chain: C.Chain{node, group},
		info: &statistic.TrackerInfo{
			Metadata: &C.Metadata{UUID: id, SmartTarget: target, SmartBlock: "normal"},
		},
	}
}

func (t *sweepTracker) ID() string                    { return t.id }
func (t *sweepTracker) Close() error                  { t.closed = true; return nil }
func (t *sweepTracker) Info() *statistic.TrackerInfo  { return t.info }
func (t *sweepTracker) Chains() C.Chain               { return t.chain }
func (t *sweepTracker) ProviderChains() C.Chain       { return nil }
func (t *sweepTracker) AppendToChains(C.ProxyAdapter) {}
func (t *sweepTracker) RemoteDestination() string     { return "" }
func (t *sweepTracker) block() string                 { return t.info.Metadata.SmartBlock }

var _ statistic.Tracker = (*sweepTracker)(nil)

func sweepGroup(name string) *Smart {
	return &Smart{GroupBase: &GroupBase{Base: outbound.NewBase(outbound.BaseOption{Name: name})}}
}

// The non-force sweep closes every connection in the bucket that is not on the
// winning node. Those victims used to be closed without the marker the force
// sweep sets, so unlike force-closed connections they ran the full quality
// check on the way out -- and a connection killed moments after it was
// established has moved no bytes, which the zero-traffic rule reads as a broken
// node and answers with a 24-hour block. Closing a connection is this group's
// own decision; it is not evidence about the node that carried it.
func TestSweepMarksEveryConnectionItCloses(t *testing.T) {
	const target = "RuleSet [Proxy]"
	const group = "smart-group"
	s := sweepGroup(group)

	winner := newSweepTracker("winner", target, "node-a", group)
	loser := newSweepTracker("loser", target, "node-b", group)
	for _, tracker := range []*sweepTracker{winner, loser} {
		statistic.DefaultManager.Join(tracker)
		defer statistic.DefaultManager.Leave(tracker)
	}

	dialer := &C.Metadata{UUID: "dialer", SmartTarget: target}
	s.closeSameConnection(dialer, "node-a", target, "", false)

	if !loser.closed {
		t.Fatal("the sweep left a connection on a losing node open")
	}
	if loser.block() != "degraded" {
		t.Fatalf("a connection the group closed itself is marked %q, so its close is read as the node's fault", loser.block())
	}
	if winner.closed {
		t.Fatal("the sweep closed a connection already on the winning node")
	}
	if winner.block() != "normal" {
		t.Fatalf("an untouched connection was marked %q", winner.block())
	}
}

// The force sweep is the one that answers a degrade, and it takes the whole
// bucket -- winning node included.
func TestForceSweepTakesTheWholeTargetAndMarksIt(t *testing.T) {
	const target = "RuleSet [Proxy]"
	const group = "smart-group"
	s := sweepGroup(group)

	onWinner := newSweepTracker("on-winner", target, "node-a", group)
	elsewhere := newSweepTracker("elsewhere", target, "node-b", group)
	for _, tracker := range []*sweepTracker{onWinner, elsewhere} {
		statistic.DefaultManager.Join(tracker)
		defer statistic.DefaultManager.Leave(tracker)
	}

	dialer := &C.Metadata{UUID: "dialer", SmartTarget: target}
	s.closeSameConnection(dialer, "node-a", target, "", true)

	for _, tracker := range []*sweepTracker{onWinner, elsewhere} {
		if !tracker.closed {
			t.Fatalf("force sweep spared %q", tracker.id)
		}
		if tracker.block() != "degraded" {
			t.Fatalf("force sweep left %q marked %q", tracker.id, tracker.block())
		}
	}
}

// A connection belonging to another group shares the bucket but not the blame.
func TestSweepIgnoresConnectionsOutsideTheGroup(t *testing.T) {
	const target = "RuleSet [Proxy]"
	s := sweepGroup("smart-group")

	foreign := newSweepTracker("foreign", target, "node-b", "another-group")
	statistic.DefaultManager.Join(foreign)
	defer statistic.DefaultManager.Leave(foreign)

	dialer := &C.Metadata{UUID: "dialer", SmartTarget: target}
	s.closeSameConnection(dialer, "node-a", target, "", true)

	if foreign.closed {
		t.Fatal("the sweep closed a connection that is not in this group's chain")
	}
	if foreign.block() != "normal" {
		t.Fatalf("the sweep marked a connection outside the group as %q", foreign.block())
	}
}

// The flood suppressor is the only bound on a disconnect storm. Counting a
// connection the group closed itself as a healthy close is what let the storm
// hold its own damper open: a swept victim reports no read or write error, so
// it arrived on the err == nil path and zeroed the counter.
func TestFloodSuppressorIsNotClearedByTheConnectionsItClosed(t *testing.T) {
	s := sweepGroup("smart-group")
	failure := errors.New("connection reset")
	var now int64 = 1000

	for range floodThreshold {
		s.admitConnectionStats(&C.Metadata{SmartBlock: "normal"}, failure, now)
	}
	if proceed, _ := s.admitConnectionStats(&C.Metadata{SmartBlock: "normal"}, failure, now); proceed {
		t.Fatal("the suppressor never engaged after a full flood")
	}

	victim := &C.Metadata{SmartBlock: "degraded"}
	if proceed, _ := s.admitConnectionStats(victim, nil, now); !proceed {
		t.Fatal("a self-closed connection was dropped instead of recorded")
	}
	if proceed, _ := s.admitConnectionStats(&C.Metadata{SmartBlock: "normal"}, failure, now); proceed {
		t.Fatal("a connection the group closed itself cleared the suppressor")
	}

	// Real traffic getting through is the signal that the flood is over.
	s.admitConnectionStats(&C.Metadata{SmartBlock: "normal"}, nil, now)
	if proceed, _ := s.admitConnectionStats(&C.Metadata{SmartBlock: "normal"}, failure, now); !proceed {
		t.Fatal("a genuine success did not clear the suppressor")
	}
}

// Arming the suppressor is what tells the caller to drop the group's queued
// flood records, and it must happen exactly once per engagement.
func TestFloodSuppressorArmsOnce(t *testing.T) {
	s := sweepGroup("smart-group")
	failure := errors.New("connection reset")
	var now int64 = 1000

	var arms int
	for range floodThreshold * 2 {
		if _, tripped := s.admitConnectionStats(&C.Metadata{SmartBlock: "normal"}, failure, now); tripped {
			arms++
		}
	}
	if arms != 1 {
		t.Fatalf("suppressor armed %d times across one flood, want 1", arms)
	}
}
