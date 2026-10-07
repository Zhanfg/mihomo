package cfir

import "slices"

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
