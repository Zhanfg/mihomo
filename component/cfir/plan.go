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
	Protocol            ProtocolID
	Backend             string
	Platform            Platform
	Primitive           Primitive
	Requirements        CapabilityRequirement
	BackendRequirements CapabilityRequirement
	SecurityFloor       SecurityProfile
	Transport           TransportPolicy
	Generation          Generation
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
	if err := p.BackendRequirements.Validate(); err != nil {
		return err
	}
	descriptor, err := registry.EffectiveProtocolDescriptor(p.Protocol)
	if err != nil {
		return err
	}
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
	if p.Backend != "" {
		backend, ok := registry.Backend(p.Backend)
		if !ok {
			return fmt.Errorf("cfir: execution plan references unknown backend %s", p.Backend)
		}
		backendDescriptor := backend.Descriptor()
		if p.Platform != PlatformInvalid && !backendDescriptor.Platforms.Supports(p.Platform) {
			return fmt.Errorf("cfir: backend %s does not support platform %d", p.Backend, p.Platform)
		}
		if !p.BackendRequirements.SatisfiedBy(backendDescriptor.Capabilities) {
			missing := p.BackendRequirements.MissingFrom(backendDescriptor.Capabilities)
			return fmt.Errorf("cfir: backend %s misses capabilities: standard=%v extensions=%v", p.Backend, missing.Standard, missing.Extensions)
		}
	} else if len(p.BackendRequirements.Standard) > 0 || len(p.BackendRequirements.Extensions) > 0 {
		return errors.New("cfir: backend capabilities requested without selecting a backend")
	}
	if p.Transport.Hedge.MaxAttempts > 8 {
		return errors.New("cfir: hedge attempt budget exceeds hard safety bound")
	}
	if p.Transport.Hedge.Delay < 0 {
		return errors.New("cfir: negative hedge delay")
	}
	return nil
}
