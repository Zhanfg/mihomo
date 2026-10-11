package shadow

import (
	"testing"

	"github.com/metacubex/mihomo/component/cfir"
	"github.com/metacubex/mihomo/component/cfir/legacybridge"
)

func TestDecisionEligibleUsesOfferBeforeHandshakeAndEvidenceAfter(t *testing.T) {
	floor := cfir.SecurityProfile{
		Authentication: cfir.AuthenticationServer,
		Confidentiality: cfir.ConfidentialityTransport,
		Integrity: cfir.IntegrityAuthenticated,
	}
	intent := cfir.ExecutionIntent{
		Action: cfir.RouteActionForward,
		Primitive: cfir.PrimitiveStream,
		SecurityFloor: floor,
	}
	candidate := legacybridge.LegacyDecision{
		Action: cfir.RouteActionForward,
		Resolved: true,
		Protocol: "future-secure",
		Family: cfir.ProtocolDescriptor{
			ID: "future-secure",
			MinCFIR: cfir.CurrentVersion,
			Primitives: cfir.PrimitiveSet(cfir.PrimitiveStream),
		},
		Instance: cfir.ProtocolCandidate{
			Protocol: "future-secure",
			Instance: "node-a",
			Primitives: cfir.PrimitiveSet(cfir.PrimitiveStream),
			SecurityOffer: floor,
		},
	}
	if !decisionEligible(intent, candidate) {
		t.Fatal("pre-handshake candidate with a sufficient security offer should remain eligible")
	}

	candidate.Instance.Security = cfir.SecurityContext{
		Profile: cfir.SecurityProfile{
			Authentication: cfir.AuthenticationNone,
			Confidentiality: cfir.ConfidentialityTransport,
			Integrity: cfir.IntegrityAuthenticated,
		},
		AttestedByCore: true,
	}
	if decisionEligible(intent, candidate) {
		t.Fatal("attested evidence weaker than offer/floor must fail closed")
	}
}
