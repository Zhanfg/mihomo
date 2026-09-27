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

type smartStatsSampler struct {
	counters [statsSampleStripes]atomic.Uint32
}

func sampleStripe(target, node string, udp bool) uint32 {
	// FNV-1a over existing strings: no allocation and a fixed-size state table.
	h := uint32(2166136261)
	mix := func(s string) {
		for i := 0; i < len(s); i++ {
			h ^= uint32(s[i])
			h *= 16777619
		}
	}
	mix(target)
	h ^= 0xff
	h *= 16777619
	mix(node)
	if udp {
		h ^= 0xa5
		h *= 16777619
	}
	return h & (statsSampleStripes - 1)
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
	every := androidStableTCPEvery
	if udp {
		every = androidStableUDPEvery
	}
	idx := sampleStripe(metadata.SmartTarget, proxy.Name(), udp)
	count := s.statsSampler.counters[idx].Add(1)

	// Keep the first observation so a new target/node pair becomes useful
	// immediately; after warm-up, retain one in every N stable successes.
	if count == 1 {
		return 1
	}
	if count%every == 0 {
		return int64(every)
	}
	return 0
}
