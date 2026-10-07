package cfir

import "testing"

type testLayer struct{ descriptor LayerDescriptor }

func (l testLayer) Descriptor() LayerDescriptor { return l.descriptor }

func TestCompositionAllowsProtocolReuseWithoutCoreChanges(t *testing.T) {
	registry := NewRegistry()
	for _, descriptor := range []LayerDescriptor{
		{ID: "tls13", Kind: LayerSecurity, MinCFIR: CurrentVersion},
		{ID: "quic", Kind: LayerTransport, MinCFIR: CurrentVersion, Capabilities: NewCapabilitySet(CapabilityPathMigration)},
		{ID: "future-frame", Kind: LayerFraming, MinCFIR: CurrentVersion},
		{ID: "h3-session", Kind: LayerSession, MinCFIR: CurrentVersion, Capabilities: NewCapabilitySet(CapabilityMultiplex)},
	} {
		if err := registry.RegisterLayer(testLayer{descriptor: descriptor}); err != nil {
			t.Fatal(err)
		}
	}

	protocol := ProtocolDescriptor{
		ID:         "future-composed",
		MinCFIR:    CurrentVersion,
		Primitives: PrimitiveSet(PrimitiveStream, PrimitiveDatagram, PrimitiveSession),
		Composition: []LayerRef{
			{ID: "tls13", Kind: LayerSecurity},
			{ID: "quic", Kind: LayerTransport},
			{ID: "future-frame", Kind: LayerFraming},
			{ID: "h3-session", Kind: LayerSession},
		},
	}
	if err := registry.ResolveComposition(protocol); err != nil {
		t.Fatal(err)
	}

	missing := protocol
	missing.Composition = append([]LayerRef(nil), protocol.Composition...)
	missing.Composition[2].ID = "missing-frame"
	if err := registry.ResolveComposition(missing); err == nil {
		t.Fatal("unregistered layer must fail composition resolution")
	}
}
