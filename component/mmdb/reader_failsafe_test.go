package mmdb

import (
	"net"
	"testing"
)

func TestZeroIPReaderFailsSoft(t *testing.T) {
	var r IPReader
	if got := r.LookupCode(net.ParseIP("1.1.1.1")); len(got) != 0 {
		t.Fatalf("zero IPReader returned %v, want empty", got)
	}
}

func TestZeroASNReaderFailsSoft(t *testing.T) {
	var r ASNReader
	asn, org := r.LookupASN(net.ParseIP("1.1.1.1"))
	if asn != "" || org != "" {
		t.Fatalf("zero ASNReader returned %q %q, want empty", asn, org)
	}
}
