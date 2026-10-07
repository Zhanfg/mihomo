package cfir

import "slices"

// CandidateProtocols returns every registered protocol that can legally satisfy
// an unresolved forward intent. Smart/Neural ranking consumes this already
// filtered set instead of scoring incapable or security-ineligible protocols.
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
