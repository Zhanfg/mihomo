package cfir

import "fmt"

// ConformanceInvariant describes runtime behavior every protocol integration is
// expected to preserve. It is deliberately protocol-neutral: the core owns
// lifecycle semantics, while wire adapters prove they do not violate them.
type ConformanceInvariant uint64

const (
	ConformanceCancellation ConformanceInvariant = 1 << iota
	ConformanceHandshakeDeadline
	ConformanceIdleDeadline
	ConformanceAbsoluteLifetime
	ConformanceIdempotentClose
	ConformanceConcurrentClose
	ConformanceBackpressure
	ConformanceErrorClassification
	ConformanceNetworkEpoch
	ConformanceMemoryPressure
	ConformanceSecurityContext
	ConformanceNoGlobalRouteMutation

	ConformanceStreamHalfClose
	ConformanceDatagramExpiry
	ConformancePacketMTU
	ConformanceSessionDrain
)

const baseConformance = ConformanceCancellation |
	ConformanceHandshakeDeadline |
	ConformanceIdleDeadline |
	ConformanceAbsoluteLifetime |
	ConformanceIdempotentClose |
	ConformanceConcurrentClose |
	ConformanceBackpressure |
	ConformanceErrorClassification |
	ConformanceNetworkEpoch |
	ConformanceMemoryPressure |
	ConformanceSecurityContext |
	ConformanceNoGlobalRouteMutation

type ConformanceDeclaration struct {
	Invariants ConformanceInvariant
}

func RequiredConformance(descriptor ProtocolDescriptor) ConformanceInvariant {
	required := baseConformance
	if descriptor.Primitives.Supports(PrimitiveStream) &&
		descriptor.Capabilities.HasStandard(CapabilityHalfClose) {
		required |= ConformanceStreamHalfClose
	}
	if descriptor.Primitives.Supports(PrimitiveDatagram) {
		required |= ConformanceDatagramExpiry
	}
	if descriptor.Primitives.Supports(PrimitivePacket) {
		required |= ConformancePacketMTU
	}
	if descriptor.Primitives.Supports(PrimitiveSession) {
		required |= ConformanceSessionDrain
	}
	return required
}

func (c ConformanceDeclaration) ValidateFor(descriptor ProtocolDescriptor) error {
	required := RequiredConformance(descriptor)
	missing := required &^ c.Invariants
	if missing != 0 {
		return fmt.Errorf("cfir: protocol %s misses conformance invariants 0x%x", descriptor.ID, uint64(missing))
	}
	return nil
}

// ProtocolModule is the future-facing SDK registration unit. Registry still
// accepts ProtocolAdapter for the migration period, but new adapters should
// implement this stronger contract from day one.
type ProtocolModule interface {
	ProtocolAdapter
	Conformance() ConformanceDeclaration
}
