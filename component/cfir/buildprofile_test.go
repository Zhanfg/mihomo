package cfir

import "testing"

func TestBuildProfileComputesStaticLayerClosure(t *testing.T) {
	registry := NewRegistry()
	for _, descriptor := range []LayerDescriptor{
		{ID: "tls13", Kind: LayerSecurity, MinCFIR: CurrentVersion},
		{ID: "quic", Kind: LayerTransport, MinCFIR: CurrentVersion},
		{ID: "hy2-frame", Kind: LayerFraming, MinCFIR: CurrentVersion},
	} {
		if err := registry.RegisterLayer(testLayer{descriptor: descriptor}); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.RegisterProtocol(testProtocol{descriptor: ProtocolDescriptor{
		ID: "hy2", MinCFIR: CurrentVersion,
		Primitives: PrimitiveSet(PrimitiveStream, PrimitiveDatagram, PrimitiveSession),
		Composition: []LayerRef{
			{ID: "tls13", Kind: LayerSecurity},
			{ID: "quic", Kind: LayerTransport},
			{ID: "hy2-frame", Kind: LayerFraming},
		},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterBackend(testBackend{descriptor: BackendDescriptor{
		ID: "networkextension",
		Kind: BackendTUN,
		Platforms: Platforms(PlatformMacOS, PlatformIOS),
		MinCFIR: CurrentVersion,
	}}); err != nil {
		t.Fatal(err)
	}

	resolved, err := registry.ResolveBuildProfile(BuildProfile{
		Name: "ios-lite", Platform: PlatformIOS,
		Protocols: []ProtocolID{"hy2"},
		Backends: []string{"networkextension"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Protocols) != 1 || len(resolved.Layers) != 3 || len(resolved.Backends) != 1 {
		t.Fatalf("unexpected resolved profile: %#v", resolved)
	}

	if _, err := registry.ResolveBuildProfile(BuildProfile{
		Name: "windows-invalid", Platform: PlatformWindows,
		Protocols: []ProtocolID{"hy2"},
		Backends: []string{"networkextension"},
	}); err == nil {
		t.Fatal("profile must reject a backend unsupported on the target platform")
	}
}
