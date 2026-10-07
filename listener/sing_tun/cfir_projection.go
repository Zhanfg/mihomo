package sing_tun

import (
	"runtime"

	"github.com/metacubex/mihomo/component/cfir"
	C "github.com/metacubex/mihomo/constant"
)

const cfirTUNBackendID = "mihomo-tun"

var cfirTUNExtensions = []cfir.ExtensionID{
	"tun.auto-route",
	"tun.dns-hijack",
	"tun.gso",
	"tun.stack.gvisor",
	"tun.stack.system",
	"tun.stack.mixed",
	"tun.stack.mips",
}

func cfirTUNFamilyCapabilities() cfir.CapabilitySet {
	capabilities := cfir.NewCapabilitySet(
		cfir.CapabilityInterfaceBind,
		cfir.CapabilityRouteChangeMonitor,
		cfir.CapabilityUIDPolicy,
		cfir.CapabilityBatchPacketIO,
	)
	for _, extension := range cfirTUNExtensions {
		_ = capabilities.AddExtension(extension)
	}
	return capabilities
}

func cfirTunStackExtension(stack C.TUNStack) cfir.ExtensionID {
	switch stack {
	case C.TunGvisor:
		return "tun.stack.gvisor"
	case C.TunSystem:
		return "tun.stack.system"
	case C.TunMixed:
		return "tun.stack.mixed"
	case C.TunMips:
		return "tun.stack.mips"
	default:
		return ""
	}
}

func (l *Listener) cfirTUNCapabilities(platform cfir.Platform) cfir.CapabilitySet {
	capabilities := cfir.NewCapabilitySet()

	if l.networkUpdateMonitor != nil && l.defaultInterfaceMonitor != nil {
		capabilities.AddStandard(cfir.CapabilityRouteChangeMonitor)
	}
	if l.cDialerInterfaceFinder != nil {
		capabilities.AddStandard(cfir.CapabilityInterfaceBind)
	}

	switch platform {
	case cfir.PlatformAndroid:
		if len(l.options.IncludeUID) > 0 || len(l.options.IncludeUIDRange) > 0 ||
			len(l.options.ExcludeUID) > 0 || len(l.options.ExcludeUIDRange) > 0 ||
			len(l.options.IncludeAndroidUser) > 0 ||
			len(l.options.IncludePackage) > 0 || len(l.options.ExcludePackage) > 0 {
			capabilities.AddStandard(cfir.CapabilityUIDPolicy)
		}
	case cfir.PlatformLinux:
		if len(l.options.IncludeUID) > 0 || len(l.options.IncludeUIDRange) > 0 ||
			len(l.options.ExcludeUID) > 0 || len(l.options.ExcludeUIDRange) > 0 {
			capabilities.AddStandard(cfir.CapabilityUIDPolicy)
		}
	case cfir.PlatformMacOS:
		if l.options.RecvMsgX && l.options.SendMsgX {
			capabilities.AddStandard(cfir.CapabilityBatchPacketIO)
		}
	}

	if l.options.AutoRoute {
		_ = capabilities.AddExtension("tun.auto-route")
	}
	if len(l.options.DNSHijack) > 0 {
		_ = capabilities.AddExtension("tun.dns-hijack")
	}
	if l.options.GSO {
		_ = capabilities.AddExtension("tun.gso")
	}
	if extension := cfirTunStackExtension(l.options.Stack); extension != "" {
		_ = capabilities.AddExtension(extension)
	}
	return capabilities
}

func (l *Listener) CFIRBackendProjection() cfir.BackendProjection {
	platform, _ := cfir.PlatformFromGOOS(runtime.GOOS)
	return cfir.BackendProjection{
		Family: cfir.BackendDescriptor{
			ID:           cfirTUNBackendID,
			Kind:         cfir.BackendTUN,
			Platforms:    cfir.Platforms(cfir.PlatformAndroid, cfir.PlatformLinux, cfir.PlatformWindows, cfir.PlatformMacOS),
			Capabilities: cfirTUNFamilyCapabilities(),
			MinCFIR:      cfir.CurrentVersion,
		},
		Instance: cfir.BackendCandidate{
			Backend:      cfirTUNBackendID,
			Instance:     l.tunName + "/" + l.options.Stack.String(),
			Capabilities: l.cfirTUNCapabilities(platform),
		},
	}
}

var _ cfir.BackendProjectionProvider = (*Listener)(nil)
