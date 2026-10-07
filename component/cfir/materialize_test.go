package cfir

import "testing"

func TestMaterializeExecutionPlanSealsRankedInstanceEvidence(t *testing.T) {
	registry := NewRegistry()
	security := SecurityProfile{
		Authentication: AuthenticationServer,
		Confidentiality: ConfidentialityTransport,
		Integrity: IntegrityAuthenticated,
	}
	caps := NewCapabilitySet(CapabilityMultiplex)
	if err := registry.RegisterProtocol(testProtocol{descriptor: ProtocolDescriptor{
		ID: "future", MinCFIR: CurrentVersion,
		Primitives: PrimitiveSet(PrimitiveStream),
		Capabilities: caps,
	}}); err != nil {
		t.Fatal(err)
	}

	intent := ExecutionIntent{
		Action: RouteActionForward,
		Platform: PlatformLinux,
		Primitive: PrimitiveStream,
		Requirements: CapabilityRequirement{Standard: []StandardCapability{CapabilityMultiplex}},
		SecurityFloor: security,
		Generation: Generation{Network: 7, Path: 2, Socket: 1},
	}
	scored := ScoredCandidate{
		Candidate: PlanCandidate{
			Protocol: ProtocolCandidate{
				Protocol: "future",
				Instance: "node-a",
				Capabilities: caps,
				Security: SecurityContext{Profile: security, AttestedByCore: true},
			},
			Transport: TransportPolicy{Reuse: ReusePrefer},
		},
		Score: CandidateScore{Utility: 1, Confidence: 0.9, Uncertainty: 0.1},
	}

	plan, err := registry.MaterializeExecutionPlan(intent, scored)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Protocol != "future" || plan.Instance != "node-a" ||
		plan.Generation != intent.Generation || plan.Transport.Reuse != ReusePrefer {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func TestMaterializeLocalPlanCannotCarryProtocol(t *testing.T) {
	intent := ExecutionIntent{
		Action: RouteActionDirect,
		Primitive: PrimitiveStream,
		Generation: Generation{Network: 3},
	}
	plan, err := MaterializeLocalExecutionPlan(intent)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != RouteActionDirect || plan.Protocol != "" || plan.Generation.Network != 3 {
		t.Fatalf("unexpected local plan: %#v", plan)
	}
}
