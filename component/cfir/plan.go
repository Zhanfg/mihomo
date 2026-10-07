package cfir

import (
	"errors"
	"fmt"
	"time"
)

type IPFamilyPolicy uint8

const (
	IPFamilyAuto IPFamilyPolicy = iota
	IPFamilyV4Only
	IPFamilyV6Only
	IPFamilyPreferV4
	IPFamilyPreferV6
)

type ReusePolicy uint8

const (
	ReuseDefault ReusePolicy = iota
	ReuseDisable
	ReusePrefer
	ReuseRequire
)

type MultiplexPolicy uint8

const (
	MultiplexDefault MultiplexPolicy = iota
	MultiplexDisable
	MultiplexPrefer
	MultiplexRequire
)

type HedgePolicy struct {
	Delay       time.Duration
	MaxAttempts uint8
}

type TransportPolicy struct {
	IPFamily          IPFamilyPolicy
	CongestionControl string
	Reuse             ReusePolicy
	Multiplex         MultiplexPolicy
	Hedge             HedgePolicy
}

// ExecutionPlan is the stable handoff from Smart/Planner to protocol and
// platform backends. It names the selected protocol only at the final edge;
// all earlier decision stages can operate on capability requirements.
type ExecutionPlan struct {
	Protocol      ProtocolID
	Backend       string
	Primitive     Primitive
	Requirements  CapabilityRequirement
	SecurityFloor SecurityProfile
	Transport     TransportPolicy
	Generation    Generation
}

func (p ExecutionPlan) Validate(registry *Registry) error {
	if registry == nil {
		return errors.New("cfir: nil registry")
	}
	if !p.Primitive.Valid() {
		return errors.New("cfir: execution plan has invalid primitive")
	}
	if err := p.Requirements.Validate(); err != nil {
		return err
	}
	adapter, ok := registry.Protocol(p.Protocol)
	if !ok {
		return fmt.Errorf("cfir: execution plan references unknown protocol %s", p.Protocol)
	}
	descriptor := adapter.Descriptor()
	if !descriptor.Primitives.Supports(p.Primitive) {
		return fmt.Errorf("cfir: protocol %s does not support %s", p.Protocol, p.Primitive)
	}
	if !p.Requirements.SatisfiedBy(descriptor.Capabilities) {
		missing := p.Requirements.MissingFrom(descriptor.Capabilities)
		return fmt.Errorf("cfir: protocol %s misses capabilities: standard=%v extensions=%v", p.Protocol, missing.Standard, missing.Extensions)
	}
	if !descriptor.Security.Meets(p.SecurityFloor) {
		return fmt.Errorf("cfir: protocol %s violates execution plan security floor", p.Protocol)
	}
	if p.Transport.Hedge.MaxAttempts > 8 {
		return errors.New("cfir: hedge attempt budget exceeds hard safety bound")
	}
	if p.Transport.Hedge.Delay < 0 {
		return errors.New("cfir: negative hedge delay")
	}
	return nil
}
