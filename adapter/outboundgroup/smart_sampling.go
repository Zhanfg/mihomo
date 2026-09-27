package outboundgroup

import (
	"runtime"

	"github.com/metacubex/mihomo/common/atomic"
	"github.com/metacubex/mihomo/component/smart/tcpstats"
	C "github.com/metacubex/mihomo/constant"
)

const (
	statsSampleStripes    = 256
	androidStableTCPEvery = uint32(4)
	androidStableUDPEvery = uint32(2)
)

type smartSampleSlot struct {
	fingerprint atomic.Uint64
	count       atomic.Uint32
}

type smartStatsSampler struct {
	slots [statsSampleStripes]smartSampleSlot
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
func sampleSlotScale(slot *smartSampleSlot, fingerprint uint64, every uint32) int64 {
	if every <= 1 {
		return 1
	}

	for {
		current := slot.fingerprint.Load()
		if current != fingerprint {
			if slot.fingerprint.CompareAndSwap(current, fingerprint) {
				slot.count.Store(1)
				return 1
			}
			continue
		}

		count := slot.count.Add(1)
		if count == 1 {
			return 1
		}
		if count%every == 0 {
			return int64(every)
		}
		return 0
	}
}

func informativeStatsSample(connectTime, latency, uploadTotal, downloadTotal, connectionDuration int64, tcpStats *tcpstats.Stats, err error) bool {
	if err != nil {
		return true
	}
	if connectionDuration >= 30_000 || uploadTotal+downloadTotal >= 2<<20 {
		return true
	}
	if connectTime >= 800 || latency >= 500 {
		return true
	}
	if tcpStats != nil {
		loss := tcpStats.LossRate()
		rtt := float64(tcpStats.RTTUsec) / 1000.0
		rttVar := float64(tcpStats.RTTVarUsec) / 1000.0
		if loss >= 0.005 || tcpStats.Lost > 0 || rtt >= 400 {
			return true
		}
		if rtt > 0 && rttVar/rtt >= 0.15 {
			return true
		}
	}
	return false
}

// sampleScale returns 0 when a stable low-information success can be skipped,
// 1 for a full-fidelity sample, or N when one retained healthy sample
// represents N similar successes. Abnormal/high-information samples are never
// downsampled.
func (s *Smart) sampleScale(metadata *C.Metadata, proxy C.Proxy,
	connectTime, latency, uploadTotal, downloadTotal, connectionDuration int64,
	tcpStats *tcpstats.Stats, err error,
) int64 {
	if runtime.GOOS != "android" || informativeStatsSample(connectTime, latency, uploadTotal, downloadTotal, connectionDuration, tcpStats, err) {
		return 1
	}
	if metadata == nil || proxy == nil {
		return 1
	}

	udp := metadata.NetWork == C.UDP
	every := stableSampleEvery(true, udp)
	fingerprint := sampleFingerprint(metadata.SmartTarget, proxy.Name(), udp)
	idx := sampleStripe(fingerprint)
	return sampleSlotScale(&s.statsSampler.slots[idx], fingerprint, every)
}
