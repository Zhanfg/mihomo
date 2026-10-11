package cfir

import "testing"

type testProtocol struct{ descriptor ProtocolDescriptor }

func (p testProtocol) Descriptor() ProtocolDescriptor { return p.descriptor }

type testBackend struct{ descriptor BackendDescriptor }

func (b testBackend) Descriptor() BackendDescriptor { return b.descriptor }

type testExtension struct{ id ExtensionID }

func (e testExtension) CFIRExtensionID() ExtensionID { return e.id }

func TestVersionCompatibilityIsMinorAdditive(t *testing.T) {
	if !(Version{Major: 1, Minor: 4}).Supports(Version{Major: 1, Minor: 2}) {
		t.Fatal("newer minor must support an older minor")
	}
	if (Version{Major: 1, Minor: 2}).Supports(Version{Major: 1, Minor: 4}) {
		t.Fatal("older minor must not claim newer semantics")
	}
	if (Version{Major: 2, Minor: 0}).Supports(Version{Major: 1, Minor: 9}) {
		t.Fatal("major versions are intentionally incompatible")
	}
}

func TestCapabilitiesKeepUnknownProtocolFeaturesOutOfCore(t *testing.T) {
	set := NewCapabilitySet(CapabilityHalfClose, CapabilityPathMigration)
	if !set.HasStandard(CapabilityHalfClose) || !set.HasStandard(CapabilityPathMigration) {
		t.Fatal("standard capabilities not retained")
	}

	id, err := ParseExtensionID("future.foo.parallel-path-stripe")
	if err != nil {
		t.Fatal(err)
	}
	if err := set.AddExtension(id); err != nil {
		t.Fatal(err)
	}
	if !set.HasExtension(id) {
		t.Fatal("extension capability not retained")
	}

	bag := ExtensionBag{testExtension{id: id}}
	if _, ok := bag.Find(id); !ok {
		t.Fatal("unknown typed extension could not round-trip through CFIR")
	}
}

func TestRegistryAcceptsNewProtocolWithoutCoreProtocolSwitch(t *testing.T) {
	registry := NewRegistry()
	descriptor := ProtocolDescriptor{
		ID:          "future-proto",
		DisplayName: "Future Protocol",
		WireVersion: "9",
		MinCFIR:     Version{Major: 1, Minor: 0},
		Primitives:  PrimitiveSet(PrimitiveStream, PrimitiveDatagram, PrimitiveSession),
		Capabilities: NewCapabilitySet(
			CapabilityMultiplex,
			CapabilityPathMigration,
			CapabilityNativeCongestionTelemetry,
		),
	}
	if err := registry.RegisterProtocol(testProtocol{descriptor: descriptor}); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Protocol("future-proto"); !ok {
		t.Fatal("new protocol is not discoverable")
	}
	if err := registry.RegisterProtocol(testProtocol{descriptor: descriptor}); err == nil {
		t.Fatal("duplicate protocol registration must fail")
	}
}

func TestBackendRegistrySeparatesPlatformImplementationFromCore(t *testing.T) {
	registry := NewRegistry()
	backends := []BackendDescriptor{
		{ID: "ebpf-linux", Kind: BackendKernelAccelerator, Platforms: Platforms(PlatformLinux, PlatformAndroid), MinCFIR: CurrentVersion},
		{ID: "wintun", Kind: BackendTUN, Platforms: Platforms(PlatformWindows), MinCFIR: CurrentVersion},
		{ID: "networkextension", Kind: BackendTUN, Platforms: Platforms(PlatformMacOS, PlatformIOS), MinCFIR: CurrentVersion},
	}
	for _, descriptor := range backends {
		if err := registry.RegisterBackend(testBackend{descriptor: descriptor}); err != nil {
			t.Fatal(err)
		}
	}
	if got := registry.Backends(BackendTUN, PlatformIOS); len(got) != 1 || got[0].ID != "networkextension" {
		t.Fatalf("unexpected iOS TUN backends: %#v", got)
	}
	if got := registry.Backends(BackendKernelAccelerator, PlatformWindows); len(got) != 0 {
		t.Fatalf("Windows must not inherit Linux eBPF semantics: %#v", got)
	}
}

func TestSecurityPolicyCanRejectProtocolDowngrade(t *testing.T) {
	strong := SecurityProfile{
		Authentication:   AuthenticationMutual,
		Confidentiality:  ConfidentialityEndToEnd,
		Integrity:        IntegrityAuthenticated,
		ForwardSecrecy:   true,
		ReplayProtection: true,
	}
	weak := SecurityProfile{
		Authentication:  AuthenticationServer,
		Confidentiality: ConfidentialityTransport,
		Integrity:       IntegrityAuthenticated,
	}
	if !strong.Meets(weak) {
		t.Fatal("strong profile should meet weaker policy")
	}
	if weak.Meets(strong) {
		t.Fatal("security downgrade must be detectable")
	}
}
