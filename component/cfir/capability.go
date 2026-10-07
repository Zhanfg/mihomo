package cfir

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// StandardCapability numbers are append-only ABI. Existing numeric values must
// never be reordered. There are 256 O(1) standard slots; unusual or
// protocol-specific features use ExtensionID and therefore do not consume ABI
// space or require a core release.
type StandardCapability uint16

const (
	CapabilityInvalid StandardCapability = iota
	CapabilityHalfClose
	CapabilityEarlyData
	CapabilityMultiplex
	CapabilityReliableDatagram
	CapabilityUnreliableDatagram
	CapabilityPacketAddress
	CapabilityPathMigration
	CapabilityRebinding
	CapabilityPMTUFeedback
	CapabilityCongestionControl
	CapabilityNativeCongestionTelemetry
	CapabilityZeroCopy
	CapabilityNativeDNS
	CapabilityNativeResolve
	CapabilitySourceAddressPreserve
	CapabilityMultiPath
	CapabilityFlowControlTelemetry
	CapabilityMemoryPressure
)

const standardCapabilityLimit = 256

type ExtensionID string

func ParseExtensionID(value string) (ExtensionID, error) {
	if len(value) < 3 || len(value) > 128 {
		return "", fmt.Errorf("cfir: invalid extension id length: %d", len(value))
	}
	if value[0] == '.' || value[len(value)-1] == '.' || !strings.ContainsRune(value, '.') {
		return "", errors.New("cfir: extension id must contain a non-edge namespace separator")
	}
	segmentStart := true
	for _, r := range value {
		switch {
		case r == '.':
			if segmentStart {
				return "", errors.New("cfir: extension id contains an empty namespace segment")
			}
			segmentStart = true
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			segmentStart = false
		case r == '-' && !segmentStart:
			// Hyphens are allowed inside a namespace segment, but not first.
		default:
			return "", fmt.Errorf("cfir: invalid extension id character %q", r)
		}
	}
	if segmentStart {
		return "", errors.New("cfir: extension id ends with an empty segment")
	}
	return ExtensionID(value), nil
}

type CapabilitySet struct {
	standard   [4]uint64
	extensions []ExtensionID
}

func NewCapabilitySet(standard ...StandardCapability) CapabilitySet {
	var set CapabilitySet
	for _, capability := range standard {
		set.AddStandard(capability)
	}
	return set
}

func (s *CapabilitySet) AddStandard(capability StandardCapability) bool {
	if capability == CapabilityInvalid || capability >= standardCapabilityLimit {
		return false
	}
	index := uint16(capability)
	word := index >> 6
	bit := index & 63
	s.standard[word] |= uint64(1) << bit
	return true
}

func (s CapabilitySet) HasStandard(capability StandardCapability) bool {
	if capability == CapabilityInvalid || capability >= standardCapabilityLimit {
		return false
	}
	index := uint16(capability)
	return s.standard[index>>6]&(uint64(1)<<(index&63)) != 0
}

func (s *CapabilitySet) AddExtension(extension ExtensionID) error {
	parsed, err := ParseExtensionID(string(extension))
	if err != nil {
		return err
	}
	if slices.Contains(s.extensions, parsed) {
		return nil
	}
	s.extensions = append(s.extensions, parsed)
	slices.Sort(s.extensions)
	return nil
}

func (s CapabilitySet) HasExtension(extension ExtensionID) bool {
	_, found := slices.BinarySearch(s.extensions, extension)
	return found
}

func (s CapabilitySet) Extensions() []ExtensionID {
	return slices.Clone(s.extensions)
}

// Extension carries a statically linked, protocol-specific semantic that the
// current core may not understand. Unknown extensions can be forwarded without
// teaching Smart or the routing core the protocol's name.
type Extension interface {
	CFIRExtensionID() ExtensionID
}

type ExtensionBag []Extension

func (b ExtensionBag) Find(id ExtensionID) (Extension, bool) {
	for _, extension := range b {
		if extension != nil && extension.CFIRExtensionID() == id {
			return extension, true
		}
	}
	return nil, false
}


func (s CapabilitySet) Union(other CapabilitySet) CapabilitySet {
	result := s
	for i := range result.standard {
		result.standard[i] |= other.standard[i]
	}
	for _, extension := range other.extensions {
		if !result.HasExtension(extension) {
			result.extensions = append(result.extensions, extension)
		}
	}
	slices.Sort(result.extensions)
	result.extensions = slices.Compact(result.extensions)
	return result
}

func (s CapabilitySet) ContainsAll(other CapabilitySet) bool {
	for i := range s.standard {
		if s.standard[i]&other.standard[i] != other.standard[i] {
			return false
		}
	}
	for _, extension := range other.extensions {
		if !s.HasExtension(extension) {
			return false
		}
	}
	return true
}


func (s CapabilitySet) Equal(other CapabilitySet) bool {
	for i := range s.standard {
		if s.standard[i] != other.standard[i] {
			return false
		}
	}
	if len(s.extensions) != len(other.extensions) {
		return false
	}
	for i := range s.extensions {
		if s.extensions[i] != other.extensions[i] {
			return false
		}
	}
	return true
}


func (s CapabilitySet) Standards() []StandardCapability {
	result := make([]StandardCapability, 0)
	for capability := StandardCapability(1); capability < standardCapabilityLimit; capability++ {
		if s.HasStandard(capability) {
			result = append(result, capability)
		}
	}
	return result
}
