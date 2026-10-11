package cfir

import "testing"

type testSecurityEvidenceCarrier struct {
	evidence SecurityContext
	ready    bool
}

func (c testSecurityEvidenceCarrier) CFIRSecurityEvidence() (SecurityContext, bool) {
	return c.evidence, c.ready
}

func TestSecurityEvidenceFromRejectsMissingOrUnattestedEvidence(t *testing.T) {
	if _, ok := SecurityEvidenceFrom(struct{}{}); ok {
		t.Fatal("plain value must not expose security evidence")
	}
	carrier := testSecurityEvidenceCarrier{
		evidence: SecurityContext{
			Profile: SecurityProfile{
				Authentication: AuthenticationServer,
				Confidentiality: ConfidentialityTransport,
				Integrity: IntegrityAuthenticated,
			},
		},
		ready: true,
	}
	if _, ok := SecurityEvidenceFrom(carrier); ok {
		t.Fatal("unattested evidence must not be accepted")
	}
}

func TestExecutionPlanConsumesConnectionSecurityEvidence(t *testing.T) {
	offer := SecurityProfile{
		Authentication: AuthenticationServer,
		Confidentiality: ConfidentialityTransport,
		Integrity: IntegrityAuthenticated,
	}
	plan := ExecutionPlan{
		Action: RouteActionForward,
		Protocol: "future",
		Instance: "node",
		InstancePrimitives: PrimitiveSet(PrimitiveStream),
		SecurityOffer: offer,
		Primitive: PrimitiveStream,
		SecurityFloor: offer,
	}
	carrier := testSecurityEvidenceCarrier{
		evidence: SecurityContext{
			Profile: offer,
			PeerIdentity: "peer.example",
			AttestedByCore: true,
		},
		ready: true,
	}
	sealed, err := plan.WithSecurityEvidence(carrier)
	if err != nil {
		t.Fatal(err)
	}
	if !sealed.Security.AttestedByCore || sealed.Security.PeerIdentity != "peer.example" {
		t.Fatalf("unexpected sealed evidence: %#v", sealed.Security)
	}
}
