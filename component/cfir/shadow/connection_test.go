package shadow

import (
	"testing"

	"github.com/metacubex/mihomo/component/cfir"
	C "github.com/metacubex/mihomo/constant"
)

type connectionEvidenceCarrier struct {
	evidence cfir.SecurityContext
	ready    bool
}

func (c connectionEvidenceCarrier) CFIRSecurityEvidence() (cfir.SecurityContext, bool) {
	return c.evidence, c.ready
}

func secureSelfDescribingProxy() *selfDescribingProxy {
	offer := cfir.SecurityProfile{
		Authentication: cfir.AuthenticationServer,
		Confidentiality: cfir.ConfidentialityTransport,
		Integrity: cfir.IntegrityAuthenticated,
	}
	return &selfDescribingProxy{
		fakeProxy: &fakeProxy{name: "secure-node", kind: C.AdapterType(999), udp: false},
		projection: cfir.ProtocolProjection{
			Family: cfir.ProtocolDescriptor{
				ID: "future-secure",
				DisplayName: "FutureSecure",
				MinCFIR: cfir.CurrentVersion,
				Primitives: cfir.PrimitiveSet(cfir.PrimitiveStream),
				Security: offer,
			},
			Instance: cfir.ProtocolCandidate{
				Protocol: "future-secure",
				Instance: "secure-node",
				Primitives: cfir.PrimitiveSet(cfir.PrimitiveStream),
				SecurityOffer: offer,
			},
		},
	}
}

func TestObserveSelectedConnectionSealsMatchingRuntimeEvidence(t *testing.T) {
	proxy := secureSelfDescribingProxy()
	offer := proxy.projection.Instance.SecurityOffer
	result, err := ObserveSelectedConnection(
		decisionMetadata(C.TCP),
		proxy,
		connectionEvidenceCarrier{
			evidence: cfir.SecurityContext{
				Profile: offer,
				PeerIdentity: "peer.example",
				AttestedByCore: true,
			},
			ready: true,
		},
		cfir.Generation{Network: 7},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.EvidenceRequired || !result.EvidencePresent || !result.Sealed {
		t.Fatalf("unexpected observation: %#v", result)
	}
	if !result.Plan.Security.AttestedByCore || result.Plan.Security.PeerIdentity != "peer.example" {
		t.Fatalf("plan was not sealed with runtime evidence: %#v", result.Plan.Security)
	}
}

func TestObserveSelectedConnectionReportsMissingEvidenceForSecureOffer(t *testing.T) {
	proxy := secureSelfDescribingProxy()
	result, err := ObserveSelectedConnection(
		decisionMetadata(C.TCP),
		proxy,
		struct{}{},
		cfir.Generation{},
	)
	if err == nil {
		t.Fatal("secure selected protocol without runtime evidence must be reported")
	}
	if !result.EvidenceRequired || result.EvidencePresent || result.Sealed {
		t.Fatalf("unexpected observation: %#v", result)
	}
}

func TestObserveSelectedConnectionRejectsWeakerAttestation(t *testing.T) {
	proxy := secureSelfDescribingProxy()
	result, err := ObserveSelectedConnection(
		decisionMetadata(C.TCP),
		proxy,
		connectionEvidenceCarrier{
			evidence: cfir.SecurityContext{
				Profile: cfir.SecurityProfile{
					Authentication: cfir.AuthenticationNone,
					Confidentiality: cfir.ConfidentialityTransport,
					Integrity: cfir.IntegrityAuthenticated,
				},
				AttestedByCore: true,
			},
			ready: true,
		},
		cfir.Generation{},
	)
	if err == nil {
		t.Fatal("runtime evidence weaker than the selected offer must fail closed")
	}
	if !result.EvidencePresent || result.Sealed {
		t.Fatalf("unexpected observation: %#v", result)
	}
}
