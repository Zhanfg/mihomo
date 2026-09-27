package outboundgroup

import (
	"math"
	"strings"

	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/component/linkprofile"
	C "github.com/metacubex/mihomo/constant"
)

// smartCountryFit is deliberately small and ordered by confidence, not by
// desirability. Explicit country remains a hard constraint; affinity only
// contributes a bounded greedy cost.
type smartCountryFit uint8

const (
	smartCountryUnknown smartCountryFit = iota
	smartCountryMatch
	smartCountryMismatch
)

const (
	// A known IPv4/IPv6 country mismatch should lose to a similarly-performing
	// aligned node, but it must still be able to win when the aligned path is
	// materially worse. This is the elastic part of the greedy selector.
	countryMismatchWeightFactor = 0.88
	countryUnknownWeightFactor  = 0.98

	countryMismatchDelayPenalty = uint16(120)
	countryUnknownDelayPenalty  = uint16(15)
)

func adjustedCountryWeight(weight float64, fit smartCountryFit) float64 {
	switch fit {
	case smartCountryMismatch:
		return weight * countryMismatchWeightFactor
	case smartCountryUnknown:
		return weight * countryUnknownWeightFactor
	default:
		return weight
	}
}

func saturatingDelayAdd(delay, penalty uint16) uint16 {
	if penalty == 0 {
		return delay
	}
	if uint32(delay)+uint32(penalty) > math.MaxUint16 {
		return math.MaxUint16
	}
	return delay + penalty
}

func adjustedCountryDelay(delay uint16, fit smartCountryFit) uint16 {
	var penalty uint16
	switch fit {
	case smartCountryMismatch:
		penalty = countryMismatchDelayPenalty
	case smartCountryUnknown:
		penalty = countryUnknownDelayPenalty
	}
	return saturatingDelayAdd(delay, penalty)
}

// adjustedGreedyWeight combines slow historical evidence with bounded current
// path stress. The live path can correct stale history, but at most by 14% so a
// single transient sample cannot erase a node's accumulated record.
func adjustedGreedyWeight(weight float64, fit smartCountryFit, assessment linkprofile.Assessment) float64 {
	weight = adjustedCountryWeight(weight, fit)
	if assessment.Condition == linkprofile.ConditionUnknown {
		return weight
	}
	pathFactor := 1.0 - math.Min(0.14, math.Max(0, assessment.Stress)*0.14)
	return weight * pathFactor
}

// adjustedGreedyDelay applies the same idea to delay-ranked fallback nodes.
// Country and current path condition are additive bounded costs, so every
// candidate remains reachable when it is materially faster or more reliable.
func adjustedGreedyDelay(delay uint16, fit smartCountryFit, assessment linkprofile.Assessment) uint16 {
	delay = adjustedCountryDelay(delay, fit)
	if assessment.Condition == linkprofile.ConditionUnknown {
		return delay
	}
	pathPenalty := uint16(math.Round(math.Min(1, math.Max(0, assessment.Stress)) * 100))
	return saturatingDelayAdd(delay, pathPenalty)
}

// countryGreedyFit returns hard eligibility plus the soft affinity fit.
//
// Strict country is user intent and therefore probes when needed and fails
// closed if it still cannot be verified. country-affinity never actively probes
// during ranking: cached measurements are enough to add a bounded greedy cost.
func (s *Smart) countryGreedyFit(metadata *C.Metadata, p C.Proxy, desired string, strict bool) (bool, smartCountryFit) {
	if desired == "" || p == nil {
		return true, smartCountryMatch
	}

	countryFor := adapter.CachedExitCountryForProxy
	if strict {
		countryFor = adapter.ExitCountryForProxy
	}

	if knownFamily, ipv6 := metadataIPFamily(metadata); knownFamily {
		known, country := countryFor(p, ipv6)
		if !known || country == "" {
			if strict {
				return false, smartCountryUnknown
			}
			return true, smartCountryUnknown
		}
		if strings.EqualFold(country, desired) {
			return true, smartCountryMatch
		}
		if strict {
			return false, smartCountryMismatch
		}
		return true, smartCountryMismatch
	}

	known4, country4 := countryFor(p, false)
	known6, country6 := countryFor(p, true)
	match4 := known4 && country4 != "" && strings.EqualFold(country4, desired)
	match6 := known6 && country6 != "" && strings.EqualFold(country6, desired)
	if match4 || match6 {
		return true, smartCountryMatch
	}

	if strict {
		return false, smartCountryMismatch
	}
	if known4 && known6 && country4 != "" && country6 != "" {
		return true, smartCountryMismatch
	}
	return true, smartCountryUnknown
}

func (s *Smart) countryEligible(metadata *C.Metadata, p C.Proxy, desired string, strict bool) bool {
	eligible, _ := s.countryGreedyFit(metadata, p, desired, strict)
	return eligible
}
