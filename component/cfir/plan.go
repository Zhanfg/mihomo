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

type RouteAction uint8

const (
	RouteActionInvalid RouteAction = iota
	RouteActionForward
	RouteActionDirect
	RouteActionReject
	RouteActionDNS
	RouteActionInternal
)

type ExecutionIntent struct {
	Action              RouteAction
	Platform            Platform
	Primitive           Primitive
	Requirements        CapabilityRequirement
	BackendRequirements CapabilityRequirement
	SecurityFloor       SecurityProfile
	Transport           TransportPolicy
	Generation          Generation
}

func (i ExecutionIntent) Validate() error {
	if i.Action <= RouteActionInvalid || i.Action > RouteActionInternal {
		return errors.New("cfir: execution intent has invalid route action")
	}
	if !i.Primitive.Valid() {
		return errors.New("cfir: execution intent has invalid primitive")
	}
	if err := i.Requirements.Validate(); err != nil {
		return err
	}
	if err := i.BackendRequirements.Validate(); err != nil {
		return err
	}
	if i.Transport.Hedge.MaxAttempts > 8 {
		return errors.New("cfir: hedge attempt budget exceeds hard safety bound")
	}
	if i.Transport.Hedge.Delay < 0 {
		return errors.New("cfir: negative hedge delay")
	}
	return i.SecurityFloor.Validate()
}

func (i ExecutionIntent) AcceptsProtocol(descriptor ProtocolDescriptor) bool {
	if i.Action != RouteActionForward {
		return false
	}
	if !descriptor.Primitives.Supports(i.Primitive) {
		return false
	}
	return i.Requirements.SatisfiedBy(descriptor.Capabilities)
}

// ExecutionPlan is the stable handoff from Smart/Planner to protocol and
// platform backends. It names the selected protocol only at the final edge;
// all earlier decision stages can operate on capability requirements.
type ExecutionPlan struct {
	Action              RouteAction
	Protocol            ProtocolID
	Instance            string
	InstancePrimitives  PrimitiveMask
	Capabilities        CapabilitySet
	Security            SecurityContext
	Backend             string
	BackendInstance     string
	BackendCapabilities CapabilitySet
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
	intent := ExecutionIntent{
		Action: p.Action, Platform: p.Platform, Primitive: p.Primitive,
		Requirements: p.Requirements, BackendRequirements: p.BackendRequirements,
		SecurityFloor: p.SecurityFloor, Transport: p.Transport, Generation: p.Generation,
	}
	if err := intent.Validate(); err != nil {
		return err
	}

	if p.Action == RouteActionForward {
		if p.Protocol == "" {
			return errors.New("cfir: forward execution plan must name a wire protocol")
		}
		if p.Instance == "" {
			return errors.New("cfir: forward execution plan must name a protocol instance")
		}
		descriptor, err := registry.EffectiveProtocolDescriptor(p.Protocol)
		if err != nil {
			return err
		}
		if !intent.AcceptsProtocol(descriptor) {
			if !descriptor.Primitives.Supports(p.Primitive) {
				return fmt.Errorf("cfir: protocol %s does not support %s", p.Protocol, p.Primitive)
			}
			missing := p.Requirements.MissingFrom(descriptor.Capabilities)
			return fmt.Errorf("cfir: protocol %s misses capabilities: standard=%v extensions=%v", p.Protocol, missing.Standard, missing.Extensions)
		}
		if p.InstancePrimitives == 0 {
			return fmt.Errorf("cfir: protocol instance %s/%s declares no primitives", p.Protocol, p.Instance)
		}
		if descriptor.Primitives&p.InstancePrimitives != p.InstancePrimitives {
			return fmt.Errorf("cfir: protocol instance %s/%s claims primitives outside family declaration", p.Protocol, p.Instance)
		}
		if !p.InstancePrimitives.Supports(p.Primitive) {
			return fmt.Errorf("cfir: protocol instance %s/%s does not support %s", p.Protocol, p.Instance, p.Primitive)
		}
		if !descriptor.Capabilities.ContainsAll(p.Capabilities) {
			return fmt.Errorf("cfir: protocol instance %s/%s claims capabilities outside family declaration", p.Protocol, p.Instance)
		}
		if !p.Requirements.SatisfiedBy(p.Capabilities) {
			missing := p.Requirements.MissingFrom(p.Capabilities)
			return fmt.Errorf(
				"cfir: protocol instance %s/%s misses runtime capabilities: standard=%v extensions=%v",
				p.Protocol, p.Instance, missing.Standard, missing.Extensions,
			)
		}
		if !p.Security.Meets(p.SecurityFloor) {
			return fmt.Errorf("cfir: protocol instance %s/%s violates or cannot attest execution-plan security floor", p.Protocol, p.Instance)
		}
	} else if p.Protocol != "" {
		return errors.New("cfir: non-forward execution plan must not name a wire protocol")
	}

	if p.Backend != "" {
		backend, ok := registry.Backend(p.Backend)
		if !ok {
			return fmt.Errorf("cfir: execution plan references unknown backend %s", p.Backend)
		}
		backendDescriptor := backend.Descriptor()
		if p.BackendInstance == "" {
			return fmt.Errorf("cfir: backend %s has no concrete instance identity", p.Backend)
		}
		if p.Platform != PlatformInvalid && !backendDescriptor.Platforms.Supports(p.Platform) {
			return fmt.Errorf("cfir: backend %s does not support platform %d", p.Backend, p.Platform)
		}
		if !backendDescriptor.Capabilities.ContainsAll(p.BackendCapabilities) {
			return fmt.Errorf(
				"cfir: backend instance %s/%s claims capabilities outside family declaration",
				p.Backend, p.BackendInstance,
			)
		}
		if !p.BackendRequirements.SatisfiedBy(p.BackendCapabilities) {
			missing := p.BackendRequirements.MissingFrom(p.BackendCapabilities)
			return fmt.Errorf(
				"cfir: backend instance %s/%s misses capabilities: standard=%v extensions=%v",
				p.Backend, p.BackendInstance, missing.Standard, missing.Extensions,
			)
		}
	} else if len(p.BackendRequirements.Standard) > 0 || len(p.BackendRequirements.Extensions) > 0 {
		return errors.New("cfir: backend capabilities requested without selecting a backend")
	}
	return nil
}
