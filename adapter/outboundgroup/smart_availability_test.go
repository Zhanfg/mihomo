package outboundgroup

import (
	"net/netip"
	"testing"

	C "github.com/metacubex/mihomo/constant"
)

// auto-ip-family is an availability-first hint. A failed/unknown telemetry
// probe must never remove the last usable proxy from a Smart group. Explicit
// require-* remains the fail-closed mechanism.
func TestAutoIPFamilyDoesNotHardFilterCandidates(t *testing.T) {
	s := &Smart{autoIPFamily: true}
	p := strategyTestProxy{name: "candidate", alive: true}
	meta := &C.Metadata{DstIP: netip.MustParseAddr("2001:4860:4860::8888")}

	if !s.ipFamilyEligible(meta, p) {
		t.Fatal("auto-ip-family must not hard-filter a candidate")
	}
}

func TestExplicitRequireIPFamilyStillFailsClosed(t *testing.T) {
	s := &Smart{requireIPv6: true}
	p := strategyTestProxy{name: "candidate", alive: true}
	meta := &C.Metadata{DstIP: netip.MustParseAddr("2001:4860:4860::8888")}

	if s.ipFamilyEligible(meta, p) {
		t.Fatal("require-ipv6 must reject an unproven candidate")
	}
}
