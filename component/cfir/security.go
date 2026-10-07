package cfir

import "fmt"

type AuthenticationLevel uint8

const (
	AuthenticationUnknown AuthenticationLevel = iota
	AuthenticationNone
	AuthenticationServer
	AuthenticationMutual
)

type ConfidentialityLevel uint8

const (
	ConfidentialityUnknown ConfidentialityLevel = iota
	ConfidentialityNone
	ConfidentialityTransport
	ConfidentialityEndToEnd
)

type IntegrityLevel uint8

const (
	IntegrityUnknown IntegrityLevel = iota
	IntegrityNone
	IntegrityAuthenticated
)

type SecurityProfile struct {
	Authentication   AuthenticationLevel
	Confidentiality  ConfidentialityLevel
	Integrity        IntegrityLevel
	ForwardSecrecy   bool
	ReplayProtection bool
}

// SecurityContext is runtime evidence, not a protocol marketing claim.
// AttestedByCore is intentionally explicit: adapters may describe what their
// wire protocol promises, but only the trusted runtime can mark negotiated
// properties as verified.
type SecurityContext struct {
	Profile        SecurityProfile
	PeerIdentity   string
	AttestedByCore bool
}

func (p SecurityProfile) Validate() error {
	if p.Authentication > AuthenticationMutual {
		return fmt.Errorf("cfir: invalid authentication level %d", p.Authentication)
	}
	if p.Confidentiality > ConfidentialityEndToEnd {
		return fmt.Errorf("cfir: invalid confidentiality level %d", p.Confidentiality)
	}
	if p.Integrity > IntegrityAuthenticated {
		return fmt.Errorf("cfir: invalid integrity level %d", p.Integrity)
	}
	return nil
}

func (p SecurityProfile) Meets(min SecurityProfile) bool {
	if p.Authentication < min.Authentication ||
		p.Confidentiality < min.Confidentiality ||
		p.Integrity < min.Integrity {
		return false
	}
	if min.ForwardSecrecy && !p.ForwardSecrecy {
		return false
	}
	if min.ReplayProtection && !p.ReplayProtection {
		return false
	}
	return true
}
