package cfir

import (
	"errors"
	"fmt"
	"slices"
)

// CandidateProtocols returns protocol families that are structurally capable
// of an unresolved forward intent. Runtime security and per-node optional
// capabilities are validated on ProtocolCandidate before Smart/Neural ranking.
func (r *Registry) CandidateProtocols(intent ExecutionIntent) ([]ProtocolDescriptor, error) {
	if err := intent.Validate(); err != nil {
		return nil, err
	}
	if intent.Action != RouteActionForward {
		return nil, nil
	}

	r.mu.RLock()
	ids := make([]ProtocolID, 0, len(r.protocols))
	for id := range r.protocols {
		ids = append(ids, id)
	}
	r.mu.RUnlock()
	slices.Sort(ids)

	result := make([]ProtocolDescriptor, 0, len(ids))
	for _, id := range ids {
		descriptor, err := r.EffectiveProtocolDescriptor(id)
		if err != nil {
			return nil, err
		}
		if intent.AcceptsProtocol(descriptor) {
			result = append(result, descriptor)
		}
	}
	return result, nil
}


type ProtocolCandidate struct {
	Protocol     ProtocolID
	Instance     string
	Capabilities CapabilitySet
	Security     SecurityContext
}

func (r *Registry) ValidateProtocolCandidate(intent ExecutionIntent, candidate ProtocolCandidate) (ProtocolDescriptor, error) {
	if err := intent.Validate(); err != nil {
		return ProtocolDescriptor{}, err
	}
	if intent.Action != RouteActionForward {
		return ProtocolDescriptor{}, errors.New("cfir: protocol candidate is only valid for forward intents")
	}
	if candidate.Instance == "" {
		return ProtocolDescriptor{}, errors.New("cfir: protocol candidate instance is empty")
	}
	descriptor, err := r.EffectiveProtocolDescriptor(candidate.Protocol)
	if err != nil {
		return ProtocolDescriptor{}, err
	}
	if !intent.AcceptsProtocol(descriptor) {
		return ProtocolDescriptor{}, fmt.Errorf("cfir: protocol family %s cannot satisfy structural intent", candidate.Protocol)
	}
	if !descriptor.Capabilities.ContainsAll(candidate.Capabilities) {
		return ProtocolDescriptor{}, fmt.Errorf(
			"cfir: protocol instance %s/%s claims capabilities outside family declaration",
			candidate.Protocol, candidate.Instance,
		)
	}
	if !intent.Requirements.SatisfiedBy(candidate.Capabilities) {
		missing := intent.Requirements.MissingFrom(candidate.Capabilities)
		return ProtocolDescriptor{}, fmt.Errorf(
			"cfir: protocol instance %s/%s misses capabilities: standard=%v extensions=%v",
			candidate.Protocol, candidate.Instance, missing.Standard, missing.Extensions,
		)
	}
	if !candidate.Security.Meets(intent.SecurityFloor) {
		return ProtocolDescriptor{}, fmt.Errorf(
			"cfir: protocol instance %s/%s violates or cannot attest security floor",
			candidate.Protocol, candidate.Instance,
		)
	}
	return descriptor, nil
}


type ProtocolProjection struct {
	Family   ProtocolDescriptor
	Instance ProtocolCandidate
}

func (p ProtocolProjection) Validate() error {
	if err := p.Family.Validate(); err != nil {
		return err
	}
	if p.Instance.Protocol == "" {
		return errors.New("cfir: protocol projection instance has no protocol id")
	}
	if p.Instance.Protocol != p.Family.ID {
		return fmt.Errorf(
			"cfir: protocol projection instance %s does not match family %s",
			p.Instance.Protocol, p.Family.ID,
		)
	}
	if !p.Family.Capabilities.ContainsAll(p.Instance.Capabilities) {
		return fmt.Errorf("cfir: protocol instance %s claims capabilities outside family declaration", p.Instance.Instance)
	}
	if err := p.Instance.Security.Profile.Validate(); err != nil {
		return err
	}
	if p.Family.Security != (SecurityProfile{}) && !p.Instance.Security.Meets(p.Family.Security) {
		return fmt.Errorf("cfir: protocol instance %s does not attest family minimum security", p.Instance.Instance)
	}
	return nil
}

// ProtocolProjectionProvider lets a new protocol self-describe without adding
// a protocol-name switch to CFIR or the legacy migration bridge.
type ProtocolProjectionProvider interface {
	CFIRProtocolProjection() ProtocolProjection
}
