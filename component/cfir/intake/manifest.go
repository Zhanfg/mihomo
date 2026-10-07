package intake

import (
	"errors"
	"fmt"
	"slices"

	"github.com/metacubex/mihomo/component/cfir"
)

type Manifest struct {
	Source    cfir.UpstreamSource
	Protocols map[cfir.ProtocolID]cfir.ProtocolDescriptor
	Backends  map[string]cfir.BackendDescriptor
	Layers    map[cfir.LayerID]cfir.LayerDescriptor
}

func Snapshot(bundle cfir.ProviderBundle) (Manifest, error) {
	if err := bundle.Source.Validate(); err != nil {
		return Manifest{}, err
	}
	result := Manifest{
		Source:    bundle.Source,
		Protocols: make(map[cfir.ProtocolID]cfir.ProtocolDescriptor, len(bundle.Protocols)),
		Backends:  make(map[string]cfir.BackendDescriptor, len(bundle.Backends)),
		Layers:    make(map[cfir.LayerID]cfir.LayerDescriptor, len(bundle.Layers)),
	}
	for _, protocol := range bundle.Protocols {
		if protocol == nil {
			return Manifest{}, errors.New("cfir intake: nil protocol")
		}
		descriptor := protocol.Descriptor()
		if err := descriptor.Validate(); err != nil {
			return Manifest{}, err
		}
		if _, exists := result.Protocols[descriptor.ID]; exists {
			return Manifest{}, fmt.Errorf("cfir intake: duplicate protocol %s", descriptor.ID)
		}
		descriptor.Composition = slices.Clone(descriptor.Composition)
		result.Protocols[descriptor.ID] = descriptor
	}
	for _, backend := range bundle.Backends {
		if backend == nil {
			return Manifest{}, errors.New("cfir intake: nil backend")
		}
		descriptor := backend.Descriptor()
		if err := descriptor.Validate(); err != nil {
			return Manifest{}, err
		}
		if _, exists := result.Backends[descriptor.ID]; exists {
			return Manifest{}, fmt.Errorf("cfir intake: duplicate backend %s", descriptor.ID)
		}
		result.Backends[descriptor.ID] = descriptor
	}
	for _, layer := range bundle.Layers {
		if layer == nil {
			return Manifest{}, errors.New("cfir intake: nil layer")
		}
		descriptor := layer.Descriptor()
		if err := descriptor.Validate(); err != nil {
			return Manifest{}, err
		}
		if _, exists := result.Layers[descriptor.ID]; exists {
			return Manifest{}, fmt.Errorf("cfir intake: duplicate layer %s", descriptor.ID)
		}
		result.Layers[descriptor.ID] = descriptor
	}
	return result, nil
}

type ChangeKind uint8

const (
	ChangeAdded ChangeKind = iota + 1
	ChangeRemoved
	ChangeChanged
)

type Change struct {
	Kind ChangeKind
	Type string
	ID   string
}

type Diff struct {
	From    cfir.UpstreamSource
	To      cfir.UpstreamSource
	Changes []Change
}

func (d Diff) Empty() bool { return len(d.Changes) == 0 }

func Compare(before, after Manifest) Diff {
	diff := Diff{From: before.Source, To: after.Source}
	compareProtocols(&diff, before.Protocols, after.Protocols)
	compareBackends(&diff, before.Backends, after.Backends)
	compareLayers(&diff, before.Layers, after.Layers)
	slices.SortFunc(diff.Changes, func(a, b Change) int {
		if a.Type != b.Type {
			if a.Type < b.Type { return -1 }
			return 1
		}
		if a.ID != b.ID {
			if a.ID < b.ID { return -1 }
			return 1
		}
		return int(a.Kind) - int(b.Kind)
	})
	return diff
}

func compareProtocols(diff *Diff, before, after map[cfir.ProtocolID]cfir.ProtocolDescriptor) {
	for id, old := range before {
		next, ok := after[id]
		if !ok {
			diff.Changes = append(diff.Changes, Change{Kind: ChangeRemoved, Type: "protocol", ID: string(id)})
			continue
		}
		if !protocolEqual(old, next) {
			diff.Changes = append(diff.Changes, Change{Kind: ChangeChanged, Type: "protocol", ID: string(id)})
		}
	}
	for id := range after {
		if _, ok := before[id]; !ok {
			diff.Changes = append(diff.Changes, Change{Kind: ChangeAdded, Type: "protocol", ID: string(id)})
		}
	}
}

func compareBackends(diff *Diff, before, after map[string]cfir.BackendDescriptor) {
	for id, old := range before {
		next, ok := after[id]
		if !ok {
			diff.Changes = append(diff.Changes, Change{Kind: ChangeRemoved, Type: "backend", ID: id})
			continue
		}
		if !backendEqual(old, next) {
			diff.Changes = append(diff.Changes, Change{Kind: ChangeChanged, Type: "backend", ID: id})
		}
	}
	for id := range after {
		if _, ok := before[id]; !ok {
			diff.Changes = append(diff.Changes, Change{Kind: ChangeAdded, Type: "backend", ID: id})
		}
	}
}

func compareLayers(diff *Diff, before, after map[cfir.LayerID]cfir.LayerDescriptor) {
	for id, old := range before {
		next, ok := after[id]
		if !ok {
			diff.Changes = append(diff.Changes, Change{Kind: ChangeRemoved, Type: "layer", ID: string(id)})
			continue
		}
		if !layerEqual(old, next) {
			diff.Changes = append(diff.Changes, Change{Kind: ChangeChanged, Type: "layer", ID: string(id)})
		}
	}
	for id := range after {
		if _, ok := before[id]; !ok {
			diff.Changes = append(diff.Changes, Change{Kind: ChangeAdded, Type: "layer", ID: string(id)})
		}
	}
}

func protocolEqual(a, b cfir.ProtocolDescriptor) bool {
	return a.ID == b.ID &&
		a.DisplayName == b.DisplayName &&
		a.WireVersion == b.WireVersion &&
		a.MinCFIR == b.MinCFIR &&
		a.Primitives == b.Primitives &&
		a.Capabilities.Equal(b.Capabilities) &&
		a.Security == b.Security &&
		a.Resource == b.Resource &&
		slices.Equal(a.Composition, b.Composition)
}

func backendEqual(a, b cfir.BackendDescriptor) bool {
	return a.ID == b.ID &&
		a.Kind == b.Kind &&
		a.Platforms == b.Platforms &&
		a.MinCFIR == b.MinCFIR &&
		a.Capabilities.Equal(b.Capabilities)
}

func layerEqual(a, b cfir.LayerDescriptor) bool {
	return a.ID == b.ID &&
		a.Kind == b.Kind &&
		a.MinCFIR == b.MinCFIR &&
		a.Capabilities.Equal(b.Capabilities)
}
