//go:build with_ebpf && (linux || android)

package sing_ebpf

import (
	"fmt"

	"github.com/metacubex/mihomo/component/cfir"
)

const cfirEBPFBackendID = "mihomo-ebpf"

var cfirEBPFDataPlaneExtensions = []cfir.ExtensionID{
	"ebpf.local.tc",
	"ebpf.local.cgroup",
	"ebpf.shared.socket-assign",
	"ebpf.shared.packet-rewrite",
}

func cfirEBPFFamilyCapabilities() cfir.CapabilitySet {
	capabilities := cfir.NewCapabilitySet(
		cfir.CapabilityRouteChangeMonitor,
		cfir.CapabilityUIDPolicy,
		cfir.CapabilityProcessAttribution,
		cfir.CapabilityKernelRedirect,
		cfir.CapabilityKernelFlowTelemetry,
		cfir.CapabilityPacketRewrite,
		cfir.CapabilityTransparentProxy,
		cfir.CapabilitySocketAssignment,
	)
	for _, extension := range cfirEBPFDataPlaneExtensions {
		_ = capabilities.AddExtension(extension)
	}
	return capabilities
}

func (i *Inbound) cfirInterfaceMonitorActive() bool {
	state := &i.interfaceMonitor
	state.access.Lock()
	defer state.access.Unlock()
	return state.network != nil && state.defaultInterface != nil
}

func (i *Inbound) cfirDataPlaneExtension() cfir.ExtensionID {
	switch {
	case i.localTCEnabled():
		return "ebpf.local.tc"
	case i.localCgroupEnabled():
		return "ebpf.local.cgroup"
	case i.sharedSocketAssignEnabled():
		return "ebpf.shared.socket-assign"
	case i.sharedRewriteEnabled():
		return "ebpf.shared.packet-rewrite"
	default:
		return ""
	}
}

func (i *Inbound) CFIRBackendProjection() cfir.BackendProjection {
	family := cfir.BackendDescriptor{
		ID:           cfirEBPFBackendID,
		Kind:         cfir.BackendKernelAccelerator,
		Platforms:    cfir.Platforms(cfir.PlatformAndroid, cfir.PlatformLinux),
		Capabilities: cfirEBPFFamilyCapabilities(),
		MinCFIR:      cfir.CurrentVersion,
	}

	actual := cfir.NewCapabilitySet()
	report := i.kernelProbeReport.Load()
	probeReady := report != nil && report.RequiredIssues() == 0

	if probeReady && (i.localEnabled || i.sharedEnabled) {
		actual.AddStandard(cfir.CapabilityKernelRedirect)
		actual.AddStandard(cfir.CapabilityTransparentProxy)
	}
	if probeReady && (i.localTCEnabled() || i.sharedSocketAssignEnabled()) {
		actual.AddStandard(cfir.CapabilitySocketAssignment)
	}
	if probeReady && i.sharedRewriteEnabled() {
		actual.AddStandard(cfir.CapabilityPacketRewrite)
	}
	if i.cfirInterfaceMonitorActive() {
		actual.AddStandard(cfir.CapabilityRouteChangeMonitor)
	}
	if i.processTracker != nil {
		actual.AddStandard(cfir.CapabilityProcessAttribution)
	}
	if i.hasDatapathStatSource() {
		actual.AddStandard(cfir.CapabilityKernelFlowTelemetry)
	}
	if probeReady && i.localEnabled &&
		(i.localPolicy.IncludeUIDConfigured || len(i.localPolicy.IncludeUID) > 0 || len(i.localPolicy.ExcludeUID) > 0) {
		actual.AddStandard(cfir.CapabilityUIDPolicy)
	}
	if extension := i.cfirDataPlaneExtension(); extension != "" {
		_ = actual.AddExtension(extension)
	}

	instanceName := "probe-pending"
	if report != nil {
		instanceName = fmt.Sprintf("%s/%s/%s", report.KernelRelease, report.LocalDataPlane, report.SharedDataPlane)
	}
	return cfir.BackendProjection{
		Family: family,
		Instance: cfir.BackendCandidate{
			Backend:      cfirEBPFBackendID,
			Instance:     instanceName,
			Capabilities: actual,
		},
	}
}

var _ cfir.BackendProjectionProvider = (*Inbound)(nil)
