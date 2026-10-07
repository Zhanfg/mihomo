package cfir

import (
	"errors"
	"fmt"
	"slices"
	"sync"
)

type ProtocolID string

func ValidateProtocolID(id ProtocolID) error {
	value := string(id)
	if value == "" || len(value) > 64 {
		return errors.New("cfir: invalid protocol id length")
	}
	for i, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case (r == '-' || r == '_' || r == '.') && i > 0:
		default:
			return fmt.Errorf("cfir: invalid protocol id character %q", r)
		}
	}
	return nil
}

type CostClass uint8

const (
	CostUnknown CostClass = iota
	CostLow
	CostBalanced
	CostHigh
)

type ResourceProfile struct {
	IdleCPU      CostClass
	ActiveCPU    CostClass
	Memory       CostClass
	ZeroCopyHint bool
}

type ProtocolDescriptor struct {
	ID           ProtocolID
	DisplayName  string
	WireVersion  string
	MinCFIR      Version
	Primitives   PrimitiveMask
	Capabilities CapabilitySet
	Security     SecurityProfile
	Resource     ResourceProfile
}

func (d ProtocolDescriptor) Validate() error {
	if err := ValidateProtocolID(d.ID); err != nil {
		return err
	}
	if !d.MinCFIR.Valid() {
		return errors.New("cfir: protocol descriptor has invalid minimum CFIR version")
	}
	if !CurrentVersion.Supports(d.MinCFIR) {
		return fmt.Errorf("cfir: protocol %s requires CFIR %s, runtime is %s", d.ID, d.MinCFIR, CurrentVersion)
	}
	if d.Primitives == 0 {
		return fmt.Errorf("cfir: protocol %s declares no primitives", d.ID)
	}
	return d.Security.Validate()
}

// ProtocolAdapter is deliberately tiny in CFIR v1. Wire I/O lives in the
// protocol SDK layer, which can evolve without changing the registry ABI.
type ProtocolAdapter interface {
	Descriptor() ProtocolDescriptor
}

type BackendKind uint8

const (
	BackendInvalid BackendKind = iota
	BackendTUN
	BackendTransport
	BackendRoute
	BackendResolver
	BackendFlowObserver
	BackendKernelAccelerator
	BackendCrypto
	BackendCongestionControl
)

type Platform uint8

const (
	PlatformInvalid Platform = iota
	PlatformAndroid
	PlatformLinux
	PlatformWindows
	PlatformMacOS
	PlatformIOS
)

type PlatformMask uint16

func Platforms(values ...Platform) PlatformMask {
	var mask PlatformMask
	for _, platform := range values {
		if platform > PlatformInvalid && platform <= PlatformIOS {
			mask |= 1 << (platform - 1)
		}
	}
	return mask
}

func (m PlatformMask) Supports(platform Platform) bool {
	return platform > PlatformInvalid && platform <= PlatformIOS &&
		m&(1<<(platform-1)) != 0
}

type BackendDescriptor struct {
	ID           string
	Kind         BackendKind
	Platforms    PlatformMask
	Capabilities CapabilitySet
	MinCFIR      Version
}

func (d BackendDescriptor) Validate() error {
	if d.ID == "" {
		return errors.New("cfir: backend id is empty")
	}
	if d.Kind <= BackendInvalid || d.Kind > BackendCongestionControl {
		return errors.New("cfir: backend kind is invalid")
	}
	if d.Platforms == 0 {
		return errors.New("cfir: backend has no platform")
	}
	if !CurrentVersion.Supports(d.MinCFIR) {
		return fmt.Errorf("cfir: backend %s requires CFIR %s, runtime is %s", d.ID, d.MinCFIR, CurrentVersion)
	}
	return nil
}

type Backend interface {
	Descriptor() BackendDescriptor
}

type Registry struct {
	mu        sync.RWMutex
	protocols map[ProtocolID]ProtocolAdapter
	backends  map[string]Backend
}

func NewRegistry() *Registry {
	return &Registry{
		protocols: make(map[ProtocolID]ProtocolAdapter),
		backends:  make(map[string]Backend),
	}
}

func (r *Registry) RegisterProtocol(adapter ProtocolAdapter) error {
	if adapter == nil {
		return errors.New("cfir: nil protocol adapter")
	}
	descriptor := adapter.Descriptor()
	if err := descriptor.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.protocols[descriptor.ID]; exists {
		return fmt.Errorf("cfir: duplicate protocol %s", descriptor.ID)
	}
	r.protocols[descriptor.ID] = adapter
	return nil
}

func (r *Registry) Protocol(id ProtocolID) (ProtocolAdapter, bool) {
	r.mu.RLock()
	adapter, ok := r.protocols[id]
	r.mu.RUnlock()
	return adapter, ok
}

func (r *Registry) Protocols() []ProtocolDescriptor {
	r.mu.RLock()
	result := make([]ProtocolDescriptor, 0, len(r.protocols))
	for _, adapter := range r.protocols {
		result = append(result, adapter.Descriptor())
	}
	r.mu.RUnlock()
	slices.SortFunc(result, func(a, b ProtocolDescriptor) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return result
}

func (r *Registry) RegisterBackend(backend Backend) error {
	if backend == nil {
		return errors.New("cfir: nil backend")
	}
	descriptor := backend.Descriptor()
	if err := descriptor.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.backends[descriptor.ID]; exists {
		return fmt.Errorf("cfir: duplicate backend %s", descriptor.ID)
	}
	r.backends[descriptor.ID] = backend
	return nil
}

func (r *Registry) Backends(kind BackendKind, platform Platform) []BackendDescriptor {
	r.mu.RLock()
	result := make([]BackendDescriptor, 0, len(r.backends))
	for _, backend := range r.backends {
		descriptor := backend.Descriptor()
		if (kind == BackendInvalid || descriptor.Kind == kind) &&
			(platform == PlatformInvalid || descriptor.Platforms.Supports(platform)) {
			result = append(result, descriptor)
		}
	}
	r.mu.RUnlock()
	slices.SortFunc(result, func(a, b BackendDescriptor) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return result
}
