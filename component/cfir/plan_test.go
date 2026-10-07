package cfir

import (
	"testing"
	"time"
)

func TestExecutionPlanSelectsByCapabilityNotProtocolSpecialCase(t *testing.T) {
	registry := NewRegistry()
	caps := NewCapabilitySet(
		CapabilityMultiplex,
		CapabilityPathMigration,
		CapabilityNativeCongestionTelemetry,
	)
	security := SecurityProfile{
		Authentication:   AuthenticationServer,
		Confidentiality:  ConfidentialityTransport,
		Integrity:        IntegrityAuthenticated,
		ForwardSecrecy:   true,
		ReplayProtection: true,
	}
	adapter := testProtocol{descriptor: ProtocolDescriptor{
		ID:           "future-quic",
		MinCFIR:      CurrentVersion,
		Primitives:   PrimitiveSet(PrimitiveStream, PrimitiveDatagram, PrimitiveSession),
		Capabilities: caps,
		Security:     security,
	}}
	if err := registry.RegisterProtocol(adapter); err != nil {
		t.Fatal(err)
	}

	plan := ExecutionPlan{
		Protocol:  "future-quic",
		Primitive: PrimitiveSession,
		Requirements: CapabilityRequirement{Standard: []StandardCapability{
			CapabilityMultiplex,
			CapabilityPathMigration,
		}},
		SecurityFloor: SecurityProfile{
			Authentication:  AuthenticationServer,
			Confidentiality: ConfidentialityTransport,
			Integrity:       IntegrityAuthenticated,
		},
		Transport: TransportPolicy{
			IPFamily: IPFamilyPreferV6,
			Reuse:    ReusePrefer,
			Multiplex: MultiplexRequire,
			Hedge: HedgePolicy{Delay: 120 * time.Millisecond, MaxAttempts: 2},
		},
	}
	if err := plan.Validate(registry); err != nil {
		t.Fatal(err)
	}

	plan.Requirements.Standard = append(plan.Requirements.Standard, CapabilityMultiPath)
	if err := plan.Validate(registry); err == nil {
		t.Fatal("unsupported future capability must reject the plan without protocol-specific code")
	}
}

func TestCapabilityExtensionsRemainNegotiable(t *testing.T) {
	id, err := ParseExtensionID("future.foo.path-stripe")
	if err != nil {
		t.Fatal(err)
	}
	set := NewCapabilitySet()
	if err := set.AddExtension(id); err != nil {
		t.Fatal(err)
	}
	requirement := CapabilityRequirement{Extensions: []ExtensionID{id}}
	if !requirement.SatisfiedBy(set) {
		t.Fatal("extension capability did not participate in normal negotiation")
	}
}
