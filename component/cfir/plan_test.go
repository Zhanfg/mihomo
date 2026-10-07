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
		Action:       RouteActionForward,
		Protocol:     "future-quic",
		Instance:     "node-a",
		Capabilities: caps,
		Security:     SecurityContext{Profile: security, AttestedByCore: true},
		Primitive:    PrimitiveSession,
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


func TestExecutionPlanKeepsProtocolAndBackendCapabilitiesScoped(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterProtocol(testProtocol{descriptor: ProtocolDescriptor{
		ID:         "stream-only",
		MinCFIR:    CurrentVersion,
		Primitives: PrimitiveSet(PrimitiveStream),
		Capabilities: NewCapabilitySet(
			CapabilityHalfClose,
		),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterBackend(testBackend{descriptor: BackendDescriptor{
		ID:        "linux-fast",
		Kind:      BackendKernelAccelerator,
		Platforms: Platforms(PlatformLinux),
		MinCFIR:   CurrentVersion,
		Capabilities: NewCapabilitySet(
			CapabilityZeroCopy,
			CapabilityReliableDatagram,
		),
	}}); err != nil {
		t.Fatal(err)
	}

	valid := ExecutionPlan{
		Action:       RouteActionForward,
		Protocol:     "stream-only",
		Instance:     "node-a",
		Capabilities: NewCapabilitySet(CapabilityHalfClose),
		Backend:      "linux-fast",
		Platform:  PlatformLinux,
		Primitive: PrimitiveStream,
		Requirements: CapabilityRequirement{Standard: []StandardCapability{
			CapabilityHalfClose,
		}},
		BackendRequirements: CapabilityRequirement{Standard: []StandardCapability{
			CapabilityZeroCopy,
		}},
	}
	if err := valid.Validate(registry); err != nil {
		t.Fatal(err)
	}

	wrongScope := valid
	wrongScope.Requirements.Standard = []StandardCapability{CapabilityReliableDatagram}
	if err := wrongScope.Validate(registry); err == nil {
		t.Fatal("backend capability must not satisfy a protocol requirement")
	}

	wrongPlatform := valid
	wrongPlatform.Platform = PlatformWindows
	if err := wrongPlatform.Validate(registry); err == nil {
		t.Fatal("platform-incompatible backend must be rejected")
	}
}


func TestCandidateProtocolsFilterBeforeRanking(t *testing.T) {
	registry := NewRegistry()
	strong := SecurityProfile{
		Authentication: AuthenticationServer,
		Confidentiality: ConfidentialityTransport,
		Integrity: IntegrityAuthenticated,
		ForwardSecrecy: true,
	}
	for _, descriptor := range []ProtocolDescriptor{
		{
			ID: "fast-quic", MinCFIR: CurrentVersion,
			Primitives: PrimitiveSet(PrimitiveStream, PrimitiveDatagram),
			Capabilities: NewCapabilitySet(CapabilityPathMigration, CapabilityMultiplex),
			Security: strong,
		},
		{
			ID: "tcp-only", MinCFIR: CurrentVersion,
			Primitives: PrimitiveSet(PrimitiveStream),
			Capabilities: NewCapabilitySet(CapabilityMultiplex),
			Security: strong,
		},
		{
			ID: "weak-quic", MinCFIR: CurrentVersion,
			Primitives: PrimitiveSet(PrimitiveStream, PrimitiveDatagram),
			Capabilities: NewCapabilitySet(CapabilityPathMigration, CapabilityMultiplex),
			Security: SecurityProfile{},
		},
	} {
		if err := registry.RegisterProtocol(testProtocol{descriptor: descriptor}); err != nil {
			t.Fatal(err)
		}
	}

	intent := ExecutionIntent{
		Action: RouteActionForward,
		Primitive: PrimitiveDatagram,
		Requirements: CapabilityRequirement{Standard: []StandardCapability{
			CapabilityPathMigration,
		}},
		SecurityFloor: SecurityProfile{
			Authentication: AuthenticationServer,
			Confidentiality: ConfidentialityTransport,
			Integrity: IntegrityAuthenticated,
		},
	}
	got, err := registry.CandidateProtocols(intent)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "fast-quic" || got[1].ID != "weak-quic" {
		t.Fatalf("family candidates=%#v want structurally capable QUIC families", got)
	}

	if _, err := registry.ValidateProtocolCandidate(intent, ProtocolCandidate{
		Protocol: "weak-quic",
		Instance: "plain-node",
		Capabilities: NewCapabilitySet(CapabilityPathMigration, CapabilityMultiplex),
		Security: SecurityContext{},
	}); err == nil {
		t.Fatal("unattested weak instance must be rejected by security floor")
	}

	if _, err := registry.ValidateProtocolCandidate(intent, ProtocolCandidate{
		Protocol: "fast-quic",
		Instance: "secure-node",
		Capabilities: NewCapabilitySet(CapabilityPathMigration, CapabilityMultiplex),
		Security: SecurityContext{Profile: strong, AttestedByCore: true},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestNonForwardPlanDoesNotRequireProtocol(t *testing.T) {
	registry := NewRegistry()
	plan := ExecutionPlan{
		Action: RouteActionDirect,
		Primitive: PrimitiveStream,
	}
	if err := plan.Validate(registry); err != nil {
		t.Fatal(err)
	}
	plan.Protocol = "vless"
	if err := plan.Validate(registry); err == nil {
		t.Fatal("direct plan must not carry a wire protocol")
	}
}


func TestProtocolProjectionCannotSelfApproveImpossibleClaims(t *testing.T) {
	family := ProtocolDescriptor{
		ID: "future",
		MinCFIR: CurrentVersion,
		Primitives: PrimitiveSet(PrimitiveStream),
		Capabilities: NewCapabilitySet(CapabilityHalfClose),
		Security: SecurityProfile{
			Authentication: AuthenticationServer,
			Confidentiality: ConfidentialityTransport,
			Integrity: IntegrityAuthenticated,
		},
	}
	projection := ProtocolProjection{
		Family: family,
		Instance: ProtocolCandidate{
			Protocol: "future",
			Instance: "node",
			Capabilities: NewCapabilitySet(CapabilityHalfClose, CapabilityPathMigration),
			Security: SecurityContext{
				Profile: family.Security,
				AttestedByCore: true,
			},
		},
	}
	if err := projection.Validate(); err == nil {
		t.Fatal("instance capability outside family declaration must fail")
	}

	projection.Instance.Capabilities = NewCapabilitySet(CapabilityHalfClose)
	projection.Instance.Security.AttestedByCore = false
	if err := projection.Validate(); err == nil {
		t.Fatal("family minimum security must require core attestation")
	}

	projection.Instance.Security.AttestedByCore = true
	if err := projection.Validate(); err != nil {
		t.Fatal(err)
	}
}
