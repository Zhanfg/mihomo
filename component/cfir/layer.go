package cfir

import (
	"errors"
	"fmt"
	"slices"
)

type LayerKind uint8

const (
	LayerInvalid LayerKind = iota
	LayerSecurity
	LayerTransport
	LayerFraming
	LayerSession
)

type LayerID string

func ValidateLayerID(id LayerID) error {
	value := string(id)
	if value == "" || len(value) > 96 {
		return errors.New("cfir: invalid layer id length")
	}
	for i, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case (r == '-' || r == '_' || r == '.') && i > 0:
		default:
			return fmt.Errorf("cfir: invalid layer id character %q", r)
		}
	}
	return nil
}

type LayerDescriptor struct {
	ID           LayerID
	Kind         LayerKind
	MinCFIR      Version
	Capabilities CapabilitySet
}

func (d LayerDescriptor) Validate() error {
	if err := ValidateLayerID(d.ID); err != nil {
		return err
	}
	if d.Kind <= LayerInvalid || d.Kind > LayerSession {
		return errors.New("cfir: invalid layer kind")
	}
	if !CurrentVersion.Supports(d.MinCFIR) {
		return fmt.Errorf("cfir: layer %s requires CFIR %s, runtime is %s", d.ID, d.MinCFIR, CurrentVersion)
	}
	return nil
}

type ProtocolLayer interface {
	Descriptor() LayerDescriptor
}

type LayerRef struct {
	ID   LayerID
	Kind LayerKind
}

func (r LayerRef) Validate() error {
	if err := ValidateLayerID(r.ID); err != nil {
		return err
	}
	if r.Kind <= LayerInvalid || r.Kind > LayerSession {
		return errors.New("cfir: invalid protocol layer reference kind")
	}
	return nil
}

func validateComposition(refs []LayerRef) error {
	seen := make(map[LayerID]struct{}, len(refs))
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return err
		}
		if _, exists := seen[ref.ID]; exists {
			return fmt.Errorf("cfir: duplicate layer reference %s", ref.ID)
		}
		seen[ref.ID] = struct{}{}
	}
	return nil
}

func sortLayerDescriptors(result []LayerDescriptor) {
	slices.SortFunc(result, func(a, b LayerDescriptor) int {
		if a.Kind != b.Kind {
			if a.Kind < b.Kind {
				return -1
			}
			return 1
		}
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
}
