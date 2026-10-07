package sing_tun

import (
	"testing"

	"github.com/metacubex/mihomo/component/cfir"
	C "github.com/metacubex/mihomo/constant"
	LC "github.com/metacubex/mihomo/listener/config"
)

func TestCFIRTUNProjectionDoesNotClaimIOSBeforeDataPlaneExists(t *testing.T) {
	listener := &Listener{
		tunName: "utun7",
		options: LC.Tun{Stack: C.TunSystem},
	}
	projection := listener.CFIRBackendProjection()
	if err := projection.Validate(); err != nil {
		t.Fatal(err)
	}
	if projection.Family.Platforms.Supports(cfir.PlatformIOS) {
		t.Fatal("current sing_tun backend must not claim iOS support")
	}
	for _, platform := range []cfir.Platform{
		cfir.PlatformAndroid,
		cfir.PlatformLinux,
		cfir.PlatformWindows,
		cfir.PlatformMacOS,
	} {
		if !projection.Family.Platforms.Supports(platform) {
			t.Fatalf("missing current platform %v", platform)
		}
	}
}

func TestCFIRTUNRuntimeCapabilitiesFollowConfiguration(t *testing.T) {
	listener := &Listener{
		tunName: "tun0",
		options: LC.Tun{
			Stack: C.TunMips,
			AutoRoute: true,
			DNSHijack: []string{"any:53"},
			GSO: true,
			IncludeUID: []uint32{1000},
			RecvMsgX: true,
			SendMsgX: true,
		},
		cDialerInterfaceFinder: &cDialerInterfaceFinder{tunName: "tun0"},
	}

	linux := listener.cfirTUNCapabilities(cfir.PlatformLinux)
	if !linux.HasStandard(cfir.CapabilityInterfaceBind) ||
		!linux.HasStandard(cfir.CapabilityUIDPolicy) {
		t.Fatalf("linux runtime capabilities incomplete: %#v", linux.Standards())
	}
	if !linux.HasExtension("tun.auto-route") ||
		!linux.HasExtension("tun.dns-hijack") ||
		!linux.HasExtension("tun.gso") ||
		!linux.HasExtension("tun.stack.mips") {
		t.Fatalf("linux runtime extensions incomplete: %#v", linux.Extensions())
	}
	if linux.HasStandard(cfir.CapabilityBatchPacketIO) {
		t.Fatal("Linux must not inherit Darwin RecvMsgX/SendMsgX capability")
	}

	mac := listener.cfirTUNCapabilities(cfir.PlatformMacOS)
	if !mac.HasStandard(cfir.CapabilityBatchPacketIO) {
		t.Fatal("macOS RecvMsgX+SendMsgX should expose batch packet I/O")
	}
	if mac.HasStandard(cfir.CapabilityUIDPolicy) {
		t.Fatal("macOS projection must not infer Linux/Android UID policy")
	}
}

func TestCFIRTUNUIDPolicyIsPlatformScoped(t *testing.T) {
	listener := &Listener{
		options: LC.Tun{
			IncludePackage: []string{"com.example.app"},
		},
	}
	if !listener.cfirTUNCapabilities(cfir.PlatformAndroid).HasStandard(cfir.CapabilityUIDPolicy) {
		t.Fatal("Android package policy should project as UID policy")
	}
	if listener.cfirTUNCapabilities(cfir.PlatformLinux).HasStandard(cfir.CapabilityUIDPolicy) {
		t.Fatal("Linux must not claim Android package-name policy as UID capability")
	}
}
