package outboundgroup

import (
	"runtime"

	"github.com/metacubex/mihomo/common/atomic"
	"github.com/metacubex/mihomo/component/smart/tcpstats"
	C "github.com/metacubex/mihomo/constant"
)

const (
	statsSampleStripes        = 256
	androidStableTCPEvery     = uint32(4)
	androidStableTCPLinkEvery = uint32(2)
	androidStableUDPEvery     = uint32(2)
)

type smartSampleSlot struct {
	fingerprint atomic.Uint64
	count       atomic.Uint32
}

type smartStatsSampler struct {
	slots [statsSampleStripes]smartSampleSlot
}

type smartSamplePlan struct {
	statsScale  int64
	observeLink bool
}

func stableSampleEvery(android, udp bool) uint32 {
	if !android {
		return 1
	}
	if udp {
		return androidStableUDPEvery
	}
	return androidStableTCPEvery
}

func sampleFingerprint(target, node string, udp bool) uint64 {
	// 64-bit FNV-1a over existing strings: no allocation. A non-zero
	// fingerprint is reserved so zero can mean an unused slot.
	h := uint64(14695981039346656037)
	mix := func(s string) {
		for i := 0; i < len(s); i++ {
			h ^= uint64(s[i])
			h *= 1099511628211
		}
	}
	mix(target)
	h ^= 0xff
	h *= 1099511628211
	mix(node)
	if udp {
		h ^= 0xa5
		h *= 1099511628211
	}
	if h == 0 {
		return 1
	}
	return h
}

func sampleStripe(fingerprint uint64) uint32 {
	return uint32(fingerprint) & (statsSampleStripes - 1)
}

// sampleSlotScale keeps bounded memory without sharing sampling history across
// unrelated target/node keys. A collision replaces the slot fingerprint and
// restarts at a first sample. Under contention this may retain an extra sample,
// which is safe for learning and preferable to dropping a new key's cold-start
// observation.
func sampleSlotAdvance(slot *smartSampleSlot, fingerprint uint64, every uint32) (uint32, int64) {
	if every <= 1 {
		return 1, 1
	}

	for {
		current := slot.fingerprint.Load()
		if current != fingerprint {
			if slot.fingerprint.CompareAndSwap(current, fingerprint) {
				slot.count.Store(1)
				return 1, 1
			}
			continue
		}

		count := slot.count.Add(1)
		if count == 1 {
			return count, 1
		}
		if count%every == 0 {
			return count, int64(every)
		}
		return count, 0
	}
}

func sampleSlotScale(slot *smartSampleSlot, fingerprint uint64, every uint32) int64 {
	_, scale := sampleSlotAdvance(slot, fingerprint, every)
	return scale
}

func informativeCheapSample(connectTime, latency, uploadTotal, downloadTotal, connectionDuration int64, err error) bool {
	if err != nil {
		return true
	}
	if connectionDuration >= 30_000 || uploadTotal+downloadTotal >= 2<<20 {
		return true
	}
	return connectTime >= 800 || latency >= 500
}

func informativeTCPStats(tcpStats *tcpstats.Stats) bool {
	if tcpStats == nil {
		return false
	}
	loss := tcpStats.LossRate()
	rtt := float64(tcpStats.RTTUsec) / 1000.0
	rttVar := float64(tcpStats.RTTVarUsec) / 1000.0
	if loss >= 0.005 || tcpStats.Lost > 0 || rtt >= 400 {
		return true
	}
	return rtt > 0 && rttVar/rtt >= 0.15
}

func informativeStatsSample(connectTime, latency, uploadTotal, downloadTotal, connectionDuration int64, tcpStats *tcpstats.Stats, err error) bool {
	return informativeCheapSample(connectTime, latency, uploadTotal, downloadTotal, connectionDuration, err) ||
		informativeTCPStats(tcpStats)
}

// stableSamplePlan advances the fixed-size target/node sampler exactly once.
// Stable Android TCP keeps full Smart learning at 1/4 cadence while auditing
// passive kernel link truth at 1/2 cadence. UDP has no TCP_INFO audit path and
// retains its historical 1/2 Smart cadence.
func stableSamplePlan(slot *smartSampleSlot, fingerprint uint64, android, udp bool) smartSamplePlan {
	if !android {
		return smartSamplePlan{statsScale: 1, observeLink: true}
	}
	every := stableSampleEvery(true, udp)
	count, scale := sampleSlotAdvance(slot, fingerprint, every)
	if udp {
		return smartSamplePlan{statsScale: scale}
	}
	observe := count == 1 || count%androidStableTCPLinkEvery == 0 || scale > 0
	return smartSamplePlan{statsScale: scale, observeLink: observe}
}

func (s *Smart) closeSamplePlan(metadata *C.Metadata, proxy C.Proxy,
	connectTime, latency, uploadTotal, downloadTotal, connectionDuration int64, err error,
) smartSamplePlan {
	if runtime.GOOS != "android" || informativeCheapSample(connectTime, latency, uploadTotal, downloadTotal, connectionDuration, err) {
		return smartSamplePlan{statsScale: 1, observeLink: true}
	}
	if metadata == nil || proxy == nil {
		return smartSamplePlan{statsScale: 1, observeLink: true}
	}

	udp := metadata.NetWork == C.UDP
	fingerprint := sampleFingerprint(metadata.SmartTarget, proxy.Name(), udp)
	idx := sampleStripe(fingerprint)
	return stableSamplePlan(&s.statsSampler.slots[idx], fingerprint, true, udp)
}

// sampleScale is retained for dial failures, UDP and callers that already own
// TCP_INFO. TCP close callbacks use closeSamplePlan before issuing TCP_INFO so
// skipped stable samples avoid the syscall itself.
func (s *Smart) sampleScale(metadata *C.Metadata, proxy C.Proxy,
	connectTime, latency, uploadTotal, downloadTotal, connectionDuration int64,
	tcpStats *tcpstats.Stats, err error,
) int64 {
	plan := s.closeSamplePlan(metadata, proxy, connectTime, latency, uploadTotal, downloadTotal, connectionDuration, err)
	if informativeTCPStats(tcpStats) {
		return 1
	}
	return plan.statsScale
}
