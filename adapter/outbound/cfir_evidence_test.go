package outbound

import (
	"net"
	"testing"

	"github.com/metacubex/mihomo/component/cfir"
	C "github.com/metacubex/mihomo/constant"
)

func TestCFIRSecurityEvidenceIsConnectionScoped(t *testing.T) {
	adapter := NewBase(BaseOption{
		Name: "evidence-test",
		Addr: "127.0.0.1:443",
		Type: C.Vless,
	})
	profile := cfir.SecurityProfile{
		Authentication: cfir.AuthenticationServer,
		Confidentiality: cfir.ConfidentialityTransport,
		Integrity: cfir.IntegrityAuthenticated,
	}
	evidence := cfir.SecurityContext{
		Profile: profile,
		PeerIdentity: "peer.example",
		AttestedByCore: true,
	}

	leftEvidence, rightEvidence := net.Pipe()
	defer leftEvidence.Close()
	defer rightEvidence.Close()
	withEvidence := NewConnWithCFIRSecurityEvidence(leftEvidence, adapter, evidence)

	got, ok := cfir.SecurityEvidenceFrom(withEvidence)
	if !ok {
		t.Fatal("expected connection-scoped evidence")
	}
	if got != evidence {
		t.Fatalf("evidence=%#v want %#v", got, evidence)
	}

	leftPlain, rightPlain := net.Pipe()
	defer leftPlain.Close()
	defer rightPlain.Close()
	plain := NewConn(leftPlain, adapter)
	if _, ok := cfir.SecurityEvidenceFrom(plain); ok {
		t.Fatal("plain connection inherited evidence from another connection")
	}
}

func TestCFIRSecurityEvidenceWrapperRejectsUnattestedState(t *testing.T) {
	adapter := NewBase(BaseOption{Name: "plain", Addr: "127.0.0.1:443", Type: C.Vless})
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()

	conn := NewConnWithCFIRSecurityEvidence(left, adapter, cfir.SecurityContext{
		Profile: cfir.SecurityProfile{
			Confidentiality: cfir.ConfidentialityTransport,
			Integrity: cfir.IntegrityAuthenticated,
		},
		AttestedByCore: false,
	})
	if _, ok := cfir.SecurityEvidenceFrom(conn); ok {
		t.Fatal("unattested wrapper state must remain invisible")
	}
}
