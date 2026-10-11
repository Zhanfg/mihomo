package cfir

// SecurityEvidenceProvider is an optional connection-level interface.
// Evidence is bound to one realized connection/association rather than to a
// mutable adapter, so concurrent handshakes and reconnects cannot share state.
type SecurityEvidenceProvider interface {
	CFIRSecurityEvidence() (SecurityContext, bool)
}

func SecurityEvidenceFrom(value any) (SecurityContext, bool) {
	provider, ok := value.(SecurityEvidenceProvider)
	if !ok {
		return SecurityContext{}, false
	}
	evidence, ok := provider.CFIRSecurityEvidence()
	if !ok || !evidence.AttestedByCore {
		return SecurityContext{}, false
	}
	if err := evidence.Validate(); err != nil {
		return SecurityContext{}, false
	}
	return evidence, true
}
