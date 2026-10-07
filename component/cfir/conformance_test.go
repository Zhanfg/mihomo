package cfir

import "testing"

func fullyConformant(descriptor ProtocolDescriptor) ConformanceDeclaration {
	return ConformanceDeclaration{Invariants: RequiredConformance(descriptor)}
}

func TestConformanceExpandsFromDeclaredSemantics(t *testing.T) {
	stream := ProtocolDescriptor{
		ID:         "stream-next",
		MinCFIR:    CurrentVersion,
		Primitives: PrimitiveSet(PrimitiveStream),
		Capabilities: NewCapabilitySet(
			CapabilityHalfClose,
		),
	}
	required := RequiredConformance(stream)
	if required&ConformanceStreamHalfClose == 0 {
		t.Fatal("half-close capable stream must prove half-close behavior")
	}
	if required&ConformanceDatagramExpiry != 0 {
		t.Fatal("stream-only protocol inherited datagram invariant")
	}

	declaration := fullyConformant(stream)
	if err := declaration.ValidateFor(stream); err != nil {
		t.Fatal(err)
	}
	declaration.Invariants &^= ConformanceConcurrentClose
	if err := declaration.ValidateFor(stream); err == nil {
		t.Fatal("missing lifecycle invariant must fail validation")
	}
}

func TestProviderBundleRegistrationIsAtomic(t *testing.T) {
	registry := NewRegistry()
	existing := testProtocol{descriptor: ProtocolDescriptor{
		ID:         "existing",
		MinCFIR:    CurrentVersion,
		Primitives: PrimitiveSet(PrimitiveStream),
	}}
	if err := registry.RegisterProtocol(existing); err != nil {
		t.Fatal(err)
	}

	future := testProtocol{descriptor: ProtocolDescriptor{
		ID:         "future",
		MinCFIR:    CurrentVersion,
		Primitives: PrimitiveSet(PrimitiveDatagram),
	}}
	conflict := testProtocol{descriptor: ProtocolDescriptor{
		ID:         "existing",
		MinCFIR:    CurrentVersion,
		Primitives: PrimitiveSet(PrimitiveStream),
	}}
	backend := testBackend{descriptor: BackendDescriptor{
		ID:        "minext-stack",
		Kind:      BackendTUN,
		Platforms: Platforms(PlatformAndroid, PlatformLinux),
		MinCFIR:   CurrentVersion,
	}}

	err := registry.RegisterProvider(ProviderBundle{
		Source:    UpstreamSource{Project: "mihomo-next", Revision: "next-1"},
		Protocols: []ProtocolAdapter{future, conflict},
		Backends:  []Backend{backend},
	})
	if err == nil {
		t.Fatal("conflicting upstream bundle must be rejected")
	}
	if _, ok := registry.Protocol("future"); ok {
		t.Fatal("failed provider registration leaked a partial protocol")
	}
	if got := registry.Backends(BackendTUN, PlatformAndroid); len(got) != 0 {
		t.Fatalf("failed provider registration leaked a partial backend: %#v", got)
	}

	if err := registry.RegisterProvider(ProviderBundle{
		Source:    UpstreamSource{Project: "mihomo-next", Revision: "next-2"},
		Protocols: []ProtocolAdapter{future},
		Backends:  []Backend{backend},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Protocol("future"); !ok {
		t.Fatal("valid upstream protocol not registered")
	}
	if got := registry.Backends(BackendTUN, PlatformLinux); len(got) != 1 || got[0].ID != "minext-stack" {
		t.Fatalf("valid upstream backend not registered: %#v", got)
	}
}
