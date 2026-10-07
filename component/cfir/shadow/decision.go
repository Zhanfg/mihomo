package shadow

import (
	"sync/atomic"

	"github.com/metacubex/mihomo/component/cfir"
	"github.com/metacubex/mihomo/component/cfir/legacybridge"
	C "github.com/metacubex/mihomo/constant"
)

type DecisionMismatch uint64

const (
	DecisionMismatchNone DecisionMismatch = 0
	DecisionMismatchUnresolvedForward DecisionMismatch = 1 << iota
	DecisionMismatchPrimitive
	DecisionMismatchCapability
	DecisionMismatchSecurity
	DecisionMismatchSelectedOutsideEligibleSet
)

type DecisionResult struct {
	Intent           cfir.ExecutionIntent
	Legacy           legacybridge.LegacyDecision
	EligibleCount    int
	SelectedEligible bool
	Mismatch         DecisionMismatch
}

func (r DecisionResult) Equal() bool { return r.Mismatch == DecisionMismatchNone }

func decisionEligible(intent cfir.ExecutionIntent, candidate legacybridge.LegacyDecision) bool {
	if candidate.Action != cfir.RouteActionForward || !candidate.Resolved {
		return false
	}
	if !intent.AcceptsProtocol(candidate.Family) {
		return false
	}
	if !intent.Requirements.SatisfiedBy(candidate.Instance.Capabilities) {
		return false
	}
	return candidate.Instance.Security.Meets(intent.SecurityFloor)
}

func ObserveDecision(metadata *C.Metadata, proxy C.ProxyAdapter, generation cfir.Generation) (DecisionResult, error) {
	legacy, err := legacybridge.ProjectProxyDecision(proxy, metadata)
	if err != nil {
		return DecisionResult{}, err
	}
	intent, err := legacybridge.IntentForLegacyDecision(metadata, generation, legacy.Action)
	if err != nil {
		return DecisionResult{Legacy: legacy}, err
	}

	result := DecisionResult{Intent: intent, Legacy: legacy}
	if legacy.Action != cfir.RouteActionForward {
		return result, nil
	}
	if !legacy.Resolved {
		result.Mismatch |= DecisionMismatchUnresolvedForward
		return result, nil
	}

	descriptor := legacy.Family
	if !descriptor.Primitives.Supports(intent.Primitive) {
		result.Mismatch |= DecisionMismatchPrimitive
	}
	if !intent.Requirements.SatisfiedBy(descriptor.Capabilities) ||
		!intent.Requirements.SatisfiedBy(legacy.Instance.Capabilities) {
		result.Mismatch |= DecisionMismatchCapability
	}
	if !legacy.Instance.Security.Meets(intent.SecurityFloor) {
		result.Mismatch |= DecisionMismatchSecurity
	}

	candidates, err := legacybridge.ProjectProxyCandidateSet(proxy, metadata)
	if err != nil {
		return result, err
	}
	for _, candidate := range candidates {
		if !decisionEligible(intent, candidate) {
			continue
		}
		result.EligibleCount++
		if candidate.LeafName == legacy.LeafName && candidate.Protocol == legacy.Protocol {
			result.SelectedEligible = true
		}
	}
	if !result.SelectedEligible {
		result.Mismatch |= DecisionMismatchSelectedOutsideEligibleSet
	}
	return result, nil
}

type DecisionCounters struct {
	observed   atomic.Uint64
	matched    atomic.Uint64
	mismatch   atomic.Uint64
	unresolved atomic.Uint64
	errors     atomic.Uint64
}

type DecisionSnapshot struct {
	Observed   uint64
	Matched    uint64
	Mismatch   uint64
	Unresolved uint64
	Errors     uint64
}

func (c *DecisionCounters) Record(result DecisionResult, err error) {
	c.observed.Add(1)
	if err != nil {
		c.errors.Add(1)
		return
	}
	if result.Mismatch&DecisionMismatchUnresolvedForward != 0 {
		c.unresolved.Add(1)
	}
	if result.Equal() {
		c.matched.Add(1)
	} else {
		c.mismatch.Add(1)
	}
}

func (c *DecisionCounters) Snapshot() DecisionSnapshot {
	return DecisionSnapshot{
		Observed: c.observed.Load(),
		Matched: c.matched.Load(),
		Mismatch: c.mismatch.Load(),
		Unresolved: c.unresolved.Load(),
		Errors: c.errors.Load(),
	}
}
