// Package linkprofile contains transport-agnostic path evidence and scoring.
// It has no proxy, database or scheduler dependencies so new evidence sources
// (Android NetworkCapabilities, eBPF, QUIC stats, server cooperation) can be
// plugged in without growing Smart's selection code.
package linkprofile

import "math"

type TunnelMetrics struct {
	RTTMs    float64
	RTTVarMs float64
	LossRate float64
	Unacked  uint32
	Lost     uint32
	Cwnd     uint32
}

func QualityFactor(m TunnelMetrics) float64 {
	factor := 1.0

	if m.RTTMs > 180 {
		factor *= 1.0 - math.Min(0.12, (m.RTTMs-180)/1500.0*0.12)
	}
	if m.RTTMs > 0 && m.RTTVarMs > 0 {
		jitterRatio := m.RTTVarMs / math.Max(m.RTTMs, 1)
		if jitterRatio > 0.10 {
			factor *= 1.0 - math.Min(0.18, (jitterRatio-0.10)*0.35)
		}
	}
	if m.LossRate > 0 {
		factor *= 1.0 - math.Min(0.10, m.LossRate*1.5)
	}
	if m.Cwnd > 0 && m.Unacked > m.Cwnd {
		pressure := float64(m.Unacked-m.Cwnd) / float64(m.Cwnd)
		factor *= 1.0 - math.Min(0.12, pressure*0.06)
	}
	if m.Lost > 0 {
		denom := math.Max(float64(m.Cwnd), 1)
		factor *= 1.0 - math.Min(0.08, float64(m.Lost)/denom*0.04)
	}

	return math.Max(0.60, math.Min(1.0, factor))
}

func EWMA(oldValue, newValue, alpha float64) float64 {
	if newValue <= 0 {
		return oldValue
	}
	if oldValue <= 0 {
		return newValue
	}
	if alpha <= 0 {
		return oldValue
	}
	if alpha >= 1 {
		return newValue
	}
	return oldValue*(1-alpha) + newValue*alpha
}
