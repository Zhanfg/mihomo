package cfir

import "testing"

func TestCandidateBackendsArePlatformAndCapabilityScoped(t *testing.T) {
	registry := NewRegistry()
	for _, descriptor := range []BackendDescriptor{
		{
			ID: "ebpf-fast", Kind: BackendKernelAccelerator,
			Platforms: Platforms(PlatformLinux, PlatformAndroid),
			MinCFIR: CurrentVersion,
			Capabilities: NewCapabilitySet(CapabilityZeroCopy),
		},
		{
			ID: "wintun", Kind: BackendTUN,
			Platforms: Platforms(PlatformWindows),
			MinCFIR: CurrentVersion,
		},
		{
			ID: "networkextension", Kind: BackendTUN,
			Platforms: Platforms(PlatformMacOS, PlatformIOS),
			MinCFIR: CurrentVersion,
		},
	} {
		if err := registry.RegisterBackend(testBackend{descriptor: descriptor}); err != nil {
			t.Fatal(err)
		}
	}

	intent := ExecutionIntent{
		Action: RouteActionForward,
		Platform: PlatformAndroid,
		Primitive: PrimitiveStream,
		BackendRequirements: CapabilityRequirement{Standard: []StandardCapability{
			CapabilityZeroCopy,
		}},
	}
	got, err := registry.CandidateBackends(intent, BackendKernelAccelerator)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "ebpf-fast" {
		t.Fatalf("android candidates=%#v", got)
	}

	intent.Platform = PlatformWindows
	got, err = registry.CandidateBackends(intent, BackendKernelAccelerator)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("windows unexpectedly inherited eBPF backend: %#v", got)
	}
}

func TestPlatformFromGOOSKeepsIOSDistinctFromMacOS(t *testing.T) {
	ios, err := PlatformFromGOOS("ios")
	if err != nil { t.Fatal(err) }
	mac, err := PlatformFromGOOS("darwin")
	if err != nil { t.Fatal(err) }
	if ios != PlatformIOS || mac != PlatformMacOS || ios == mac {
		t.Fatalf("ios=%v mac=%v", ios, mac)
	}
}
