package outboundgroup

import (
	"testing"
	"time"
)

func TestSmartDialQuarantineLifecycle(t *testing.T) {
	s := &Smart{}

	if s.nodeQuarantined("node-a") {
		t.Fatal("fresh Smart unexpectedly reports node quarantined")
	}

	s.quarantineNode("node-a")
	if !s.nodeQuarantined("node-a") {
		t.Fatal("failed node was not quarantined")
	}

	s.clearNodeQuarantine("node-a")
	if s.nodeQuarantined("node-a") {
		t.Fatal("successful node remained quarantined")
	}
}

func TestSmartDialQuarantineExpiresAndCleansUp(t *testing.T) {
	s := &Smart{}
	s.dialQuarantine.Store("expired-node", time.Now().Add(-time.Second).UnixNano())

	if s.nodeQuarantined("expired-node") {
		t.Fatal("expired quarantine remained active")
	}
	if _, ok := s.dialQuarantine.Load("expired-node"); ok {
		t.Fatal("expired quarantine entry was not removed")
	}
}

func TestSmartDialQuarantineIgnoresEmptyName(t *testing.T) {
	s := &Smart{}
	s.quarantineNode("")
	if _, ok := s.dialQuarantine.Load(""); ok {
		t.Fatal("empty node name should not create quarantine state")
	}
}
