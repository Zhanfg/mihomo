package cfir

import (
	"errors"
	"fmt"
)

// UpstreamSource records provenance without making the runtime depend on an
// upstream repository's package layout. A future Mihomo Next, sing-box major
// rewrite or Xray backend enters CFIR as a provider bundle instead of becoming
// the owner of the core architecture.
type UpstreamSource struct {
	Project  string
	Revision string
}

func (s UpstreamSource) Validate() error {
	if s.Project == "" {
		return errors.New("cfir: upstream project is empty")
	}
	if s.Revision == "" {
		return errors.New("cfir: upstream revision is empty")
	}
	return nil
}

type ProviderBundle struct {
	Source    UpstreamSource
	Protocols []ProtocolAdapter
	Backends  []Backend
}

// RegisterProvider validates the whole upstream bundle before publishing any
// part of it. A broken or conflicting upstream update therefore cannot leave
// half of a new runtime registered.
func (r *Registry) RegisterProvider(bundle ProviderBundle) error {
	if err := bundle.Source.Validate(); err != nil {
		return err
	}

	protocols := make(map[ProtocolID]ProtocolAdapter, len(bundle.Protocols))
	for _, adapter := range bundle.Protocols {
		if adapter == nil {
			return fmt.Errorf("cfir: %s provider contains nil protocol", bundle.Source.Project)
		}
		descriptor := adapter.Descriptor()
		if err := descriptor.Validate(); err != nil {
			return err
		}
		if _, exists := protocols[descriptor.ID]; exists {
			return fmt.Errorf("cfir: provider %s duplicates protocol %s", bundle.Source.Project, descriptor.ID)
		}
		protocols[descriptor.ID] = adapter
	}

	backends := make(map[string]Backend, len(bundle.Backends))
	for _, backend := range bundle.Backends {
		if backend == nil {
			return fmt.Errorf("cfir: %s provider contains nil backend", bundle.Source.Project)
		}
		descriptor := backend.Descriptor()
		if err := descriptor.Validate(); err != nil {
			return err
		}
		if _, exists := backends[descriptor.ID]; exists {
			return fmt.Errorf("cfir: provider %s duplicates backend %s", bundle.Source.Project, descriptor.ID)
		}
		backends[descriptor.ID] = backend
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for id := range protocols {
		if _, exists := r.protocols[id]; exists {
			return fmt.Errorf("cfir: provider %s conflicts with protocol %s", bundle.Source.Project, id)
		}
	}
	for id := range backends {
		if _, exists := r.backends[id]; exists {
			return fmt.Errorf("cfir: provider %s conflicts with backend %s", bundle.Source.Project, id)
		}
	}

	for id, adapter := range protocols {
		r.protocols[id] = adapter
	}
	for id, backend := range backends {
		r.backends[id] = backend
	}
	return nil
}
