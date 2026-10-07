package cfir

import (
	"fmt"
	"slices"
)

// CandidateBackends filters platform implementations before any performance
// ranking. An optimization layer must never select a backend that cannot exist
// on the current OS or lacks a required kernel/runtime capability.
func (r *Registry) CandidateBackends(intent ExecutionIntent, kind BackendKind) ([]BackendDescriptor, error) {
	if err := intent.Validate(); err != nil {
		return nil, err
	}

	r.mu.RLock()
	ids := make([]string, 0, len(r.backends))
	for id := range r.backends {
		ids = append(ids, id)
	}
	r.mu.RUnlock()
	slices.Sort(ids)

	result := make([]BackendDescriptor, 0, len(ids))
	for _, id := range ids {
		backend, ok := r.Backend(id)
		if !ok {
			continue
		}
		descriptor := backend.Descriptor()
		if kind != BackendInvalid && descriptor.Kind != kind {
			continue
		}
		if intent.Platform != PlatformInvalid && !descriptor.Platforms.Supports(intent.Platform) {
			continue
		}
		if !intent.BackendRequirements.SatisfiedBy(descriptor.Capabilities) {
			continue
		}
		result = append(result, descriptor)
	}
	return result, nil
}


type BackendCandidate struct {
	Backend      string
	Instance     string
	Capabilities CapabilitySet
}

func (r *Registry) ValidateBackendCandidate(intent ExecutionIntent, candidate BackendCandidate) (BackendDescriptor, error) {
	if err := intent.Validate(); err != nil {
		return BackendDescriptor{}, err
	}
	backend, ok := r.Backend(candidate.Backend)
	if !ok {
		return BackendDescriptor{}, fmt.Errorf("cfir: unknown backend %s", candidate.Backend)
	}
	descriptor := backend.Descriptor()
	if intent.Platform != PlatformInvalid && !descriptor.Platforms.Supports(intent.Platform) {
		return BackendDescriptor{}, fmt.Errorf(
			"cfir: backend %s does not support platform %d",
			candidate.Backend, intent.Platform,
		)
	}
	if !intent.BackendRequirements.SatisfiedBy(candidate.Capabilities) {
		missing := intent.BackendRequirements.MissingFrom(candidate.Capabilities)
		return BackendDescriptor{}, fmt.Errorf(
			"cfir: backend instance %s/%s misses capabilities: standard=%v extensions=%v",
			candidate.Backend, candidate.Instance, missing.Standard, missing.Extensions,
		)
	}
	return descriptor, nil
}


type BackendProjection struct {
	Family   BackendDescriptor
	Instance BackendCandidate
}

func (p BackendProjection) Validate() error {
	if err := p.Family.Validate(); err != nil {
		return err
	}
	if p.Instance.Backend == "" {
		return fmt.Errorf("cfir: backend projection instance has no backend id")
	}
	if p.Instance.Backend != p.Family.ID {
		return fmt.Errorf(
			"cfir: backend projection instance %s does not match family %s",
			p.Instance.Backend, p.Family.ID,
		)
	}
	if !p.Family.Capabilities.ContainsAll(p.Instance.Capabilities) {
		return fmt.Errorf(
			"cfir: backend instance %s claims capabilities outside family declaration",
			p.Instance.Instance,
		)
	}
	return nil
}

type BackendProjectionProvider interface {
	CFIRBackendProjection() BackendProjection
}
