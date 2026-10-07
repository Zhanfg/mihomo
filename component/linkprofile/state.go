package linkprofile

import (
	"math"
	"time"
)

type Condition uint8

const (
	ConditionUnknown Condition = iota
	ConditionHealthy
	ConditionConstrained
	ConditionWeak
	ConditionUnstable
)

func (c Condition) String() string {
	switch c {
	case ConditionHealthy:
		return "healthy"
	case ConditionConstrained:
		return "constrained"
	case ConditionWeak:
		return "weak"
	case ConditionUnstable:
		return "unstable"
	default:
		return "unknown"
	}
}

type Assessment struct {
	Condition    Condition
	Stress       float64
	HedgeDelay   time.Duration
	SwitchMargin float64
}

func clamp01(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}

// Assess converts passive tunnel evidence into a continuous path-stress score.
// RTT alone has the smallest influence because server/route distance can be
// naturally high; jitter and loss are stronger evidence of a path that is
// actively unstable.
func Assess(m TunnelMetrics, samples uint32) Assessment {
	if samples == 0 || (m.RTTMs <= 0 && m.RTTVarMs <= 0 && m.LossRate <= 0 && m.Cwnd == 0 && m.Unacked == 0 && m.Lost == 0) {
		return Assessment{Condition: ConditionUnknown, HedgeDelay: 250 * time.Millisecond, SwitchMargin: 0.12}
	}

	rttStress := clamp01((m.RTTMs - 80) / 720)
	jitterStress := 0.0
	if m.RTTMs > 0 && m.RTTVarMs > 0 {
		jitterRatio := m.RTTVarMs / math.Max(m.RTTMs, 1)
		jitterStress = clamp01((jitterRatio - 0.05) / 0.45)
	}
	lossStress := clamp01(m.LossRate / 0.05)

	pressureStress := 0.0
	if m.Cwnd > 0 {
		if m.Unacked > m.Cwnd {
			pressureStress = clamp01(float64(m.Unacked-m.Cwnd) / (2 * float64(m.Cwnd)))
		}
		if m.Lost > 0 {
			pressureStress = math.Max(pressureStress, clamp01(float64(m.Lost)/math.Max(float64(m.Cwnd), 1)/2))
		}
	}

	stress := clamp01(
		0.25*rttStress +
			0.30*jitterStress +
			0.30*lossStress +
			0.15*pressureStress,
	)

	// A single sample is useful for fast failure detection but insufficient to
	// call a path healthy. Bias sparse evidence slightly toward caution.
	if samples < 3 {
		stress = math.Min(1, stress+0.05)
	}

	switch {
	case stress < 0.15:
		return Assessment{Condition: ConditionHealthy, Stress: stress, HedgeDelay: 320 * time.Millisecond, SwitchMargin: 0.14}
	case stress < 0.35:
		return Assessment{Condition: ConditionConstrained, Stress: stress, HedgeDelay: 240 * time.Millisecond, SwitchMargin: 0.12}
	case stress < 0.60:
		return Assessment{Condition: ConditionWeak, Stress: stress, HedgeDelay: 150 * time.Millisecond, SwitchMargin: 0.08}
	default:
		return Assessment{Condition: ConditionUnstable, Stress: stress, HedgeDelay: 100 * time.Millisecond, SwitchMargin: 0.05}
	}
}
