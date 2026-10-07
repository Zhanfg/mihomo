package cfir

import (
	"errors"
	"fmt"
	"slices"
)

// BuildProfile describes the logical modules a statically linked distribution
// wants to expose. It is intentionally independent from Go build tags; a
// generator can turn the resolved closure into imports/build metadata later.
type BuildProfile struct {
	Name      string
	Platform  Platform
	Protocols []ProtocolID
	Backends  []string
}

type ResolvedBuildProfile struct {
	Name      string
	Platform  Platform
	Protocols []ProtocolID
	Layers    []LayerID
	Backends  []string
}

func (r *Registry) ResolveBuildProfile(profile BuildProfile) (ResolvedBuildProfile, error) {
	if r == nil {
		return ResolvedBuildProfile{}, errors.New("cfir: nil registry")
	}
	if profile.Name == "" {
		return ResolvedBuildProfile{}, errors.New("cfir: build profile name is empty")
	}
	if profile.Platform <= PlatformInvalid || profile.Platform > PlatformIOS {
		return ResolvedBuildProfile{}, errors.New("cfir: build profile platform is invalid")
	}

	resolved := ResolvedBuildProfile{Name: profile.Name, Platform: profile.Platform}
	protocolSeen := make(map[ProtocolID]struct{}, len(profile.Protocols))
	layerSeen := make(map[LayerID]struct{})
	backendSeen := make(map[string]struct{}, len(profile.Backends))

	for _, id := range profile.Protocols {
		if _, exists := protocolSeen[id]; exists {
			continue
		}
		adapter, ok := r.Protocol(id)
		if !ok {
			return ResolvedBuildProfile{}, fmt.Errorf("cfir: build profile references unknown protocol %s", id)
		}
		descriptor := adapter.Descriptor()
		if err := r.ResolveComposition(descriptor); err != nil {
			return ResolvedBuildProfile{}, err
		}
		protocolSeen[id] = struct{}{}
		resolved.Protocols = append(resolved.Protocols, id)
		for _, ref := range descriptor.Composition {
			if _, exists := layerSeen[ref.ID]; exists {
				continue
			}
			layerSeen[ref.ID] = struct{}{}
			resolved.Layers = append(resolved.Layers, ref.ID)
		}
	}

	for _, id := range profile.Backends {
		if _, exists := backendSeen[id]; exists {
			continue
		}
		backend, ok := r.Backend(id)
		if !ok {
			return ResolvedBuildProfile{}, fmt.Errorf("cfir: build profile references unknown backend %s", id)
		}
		descriptor := backend.Descriptor()
		if !descriptor.Platforms.Supports(profile.Platform) {
			return ResolvedBuildProfile{}, fmt.Errorf(
				"cfir: backend %s does not support target platform %d", id, profile.Platform,
			)
		}
		backendSeen[id] = struct{}{}
		resolved.Backends = append(resolved.Backends, id)
	}

	slices.Sort(resolved.Protocols)
	slices.Sort(resolved.Layers)
	slices.Sort(resolved.Backends)
	return resolved, nil
}
