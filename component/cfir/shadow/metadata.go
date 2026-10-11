package shadow

import (
	"net"
	"slices"
	"sync/atomic"

	"github.com/metacubex/mihomo/component/cfir"
	"github.com/metacubex/mihomo/component/cfir/legacybridge"
	C "github.com/metacubex/mihomo/constant"
)

type Mismatch uint64

const (
	MismatchNone Mismatch = 0
	MismatchNetwork Mismatch = 1 << iota
	MismatchType
	MismatchSource
	MismatchDestination
	MismatchInbound
	MismatchIdentity
	MismatchRouting
	MismatchDNS
	MismatchSmart
	MismatchGeoASN
	MismatchRawAddress
)

type Result struct {
	Flow      cfir.Flow
	RoundTrip *C.Metadata
	Mismatch  Mismatch
}

func (r Result) Equal() bool { return r.Mismatch == MismatchNone }

func addrEqual(a, b net.Addr) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Network() == b.Network() && a.String() == b.String()
}

func stringSliceSemanticEqual(a, b []string) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	return slices.Equal(a, b)
}

func CompareMetadata(a, b *C.Metadata) Mismatch {
	if a == nil || b == nil {
		if a == nil && b == nil {
			return MismatchNone
		}
		return ^MismatchNone
	}

	var mismatch Mismatch
	if a.NetWork != b.NetWork {
		mismatch |= MismatchNetwork
	}
	if a.Type != b.Type {
		mismatch |= MismatchType
	}
	if a.SrcIP != b.SrcIP || a.SrcPort != b.SrcPort {
		mismatch |= MismatchSource
	}
	if a.DstIP != b.DstIP || a.DstPort != b.DstPort || a.Host != b.Host {
		mismatch |= MismatchDestination
	}
	if a.InIP != b.InIP || a.InPort != b.InPort || a.InName != b.InName || a.InUser != b.InUser {
		mismatch |= MismatchInbound
	}
	if a.Uid != b.Uid || a.Process != b.Process || a.ProcessPath != b.ProcessPath || a.UUID != b.UUID {
		mismatch |= MismatchIdentity
	}
	if a.RematchName != b.RematchName ||
		a.SpecialProxy != b.SpecialProxy ||
		a.SpecialRules != b.SpecialRules ||
		a.RemoteDst != b.RemoteDst ||
		a.DSCP != b.DSCP {
		mismatch |= MismatchRouting
	}
	if a.DNSMode != b.DNSMode || a.SniffHost != b.SniffHost {
		mismatch |= MismatchDNS
	}
	if a.SmartBlock != b.SmartBlock ||
		a.SmartTarget != b.SmartTarget ||
		a.WildcardTarget != b.WildcardTarget {
		mismatch |= MismatchSmart
	}
	if a.SrcIPASN != b.SrcIPASN ||
		a.DstIPASN != b.DstIPASN ||
		!stringSliceSemanticEqual(a.SrcGeoIP, b.SrcGeoIP) ||
		!stringSliceSemanticEqual(a.DstGeoIP, b.DstGeoIP) {
		mismatch |= MismatchGeoASN
	}
	if !addrEqual(a.RawSrcAddr, b.RawSrcAddr) || !addrEqual(a.RawDstAddr, b.RawDstAddr) {
		mismatch |= MismatchRawAddress
	}
	return mismatch
}

func ObserveMetadata(metadata *C.Metadata, generation cfir.Generation) (Result, error) {
	flow, err := legacybridge.FromMetadata(metadata, generation)
	if err != nil {
		return Result{}, err
	}
	roundTrip, err := legacybridge.ToMetadata(flow)
	if err != nil {
		return Result{Flow: flow}, err
	}
	return Result{
		Flow:      flow,
		RoundTrip: roundTrip,
		Mismatch:  CompareMetadata(metadata, roundTrip),
	}, nil
}

type Counters struct {
	observed atomic.Uint64
	matched  atomic.Uint64
	mismatch atomic.Uint64
	errors   atomic.Uint64
}

type Snapshot struct {
	Observed uint64
	Matched  uint64
	Mismatch uint64
	Errors   uint64
}

func (c *Counters) Record(result Result, err error) {
	c.observed.Add(1)
	if err != nil {
		c.errors.Add(1)
		return
	}
	if result.Equal() {
		c.matched.Add(1)
	} else {
		c.mismatch.Add(1)
	}
}

func (c *Counters) Snapshot() Snapshot {
	return Snapshot{
		Observed: c.observed.Load(),
		Matched:  c.matched.Load(),
		Mismatch: c.mismatch.Load(),
		Errors:   c.errors.Load(),
	}
}
