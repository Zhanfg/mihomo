//go:build with_ebpf && (linux || android)

package sing_ebpf

import (
	"testing"

	ECommon "github.com/metacubex/mihomo/common/ebpf"
	"github.com/metacubex/mihomo/component/cfir"
)

func TestCFIREBPFProjectionIsConservativeBeforeProbe(t *testing.T) {
	inbound := &Inbound{
		localEnabled:   true,
		localDataPlane: localDataPlaneTC,
		enableTCP:      true,
		enableUDP:      true,
	}
	projection := inbound.CFIRBackendProjection()
	if err := projection.Validate(); err != nil {
		t.Fatal(err)
	}
	if projection.Instance.Capabilities.HasStandard(cfir.CapabilityKernelRedirect) {
		t.Fatal("probe-pending backend must not claim kernel redirect")
	}
	if !projection.Instance.Capabilities.HasExtension("ebpf.local.tc") {
		t.Fatal("configured data-plane identity should remain visible while probe is pending")
	}
}

func TestCFIREBPFProjectionPromotesOnlyProbedRuntimeCapabilities(t *testing.T) {
	inbound := &Inbound{
		localEnabled:   true,
		localDataPlane: localDataPlaneTC,
		enableTCP:      true,
		enableUDP:      true,
		localPolicy: ECommon.LocalPolicy{
			IncludeUIDConfigured: true,
		},
	}
	inbound.kernelProbeReport.Store(&ECommon.KernelProbeReport{
		KernelRelease:  "6.6-test",
		LocalDataPlane: ECommon.KernelProbeDataPlaneTC,
		Network:        []string{"tcp", "udp"},
	})

	projection := inbound.CFIRBackendProjection()
	if err := projection.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, capability := range []cfir.StandardCapability{
		cfir.CapabilityKernelRedirect,
		cfir.CapabilityTransparentProxy,
		cfir.CapabilitySocketAssignment,
		cfir.CapabilityUIDPolicy,
	} {
		if !projection.Instance.Capabilities.HasStandard(capability) {
			t.Fatalf("missing probed runtime capability %d", capability)
		}
	}
	if projection.Instance.Capabilities.HasStandard(cfir.CapabilityPacketRewrite) {
		t.Fatal("local TC projection must not claim shared packet rewrite")
	}
}

func TestCFIREBPFProjectionWithRequiredIssueFailsClosed(t *testing.T) {
	inbound := &Inbound{
		localEnabled:   true,
		localDataPlane: localDataPlaneTC,
	}
	inbound.kernelProbeReport.Store(&ECommon.KernelProbeReport{
		KernelRelease: "old-kernel",
		Findings: []ECommon.KernelProbeFinding{{
			Status: ECommon.KernelProbeFail,
			Importance: ECommon.KernelProbeRequired,
			Feature: "required-helper",
		}},
	})
	projection := inbound.CFIRBackendProjection()
	if projection.Instance.Capabilities.HasStandard(cfir.CapabilityKernelRedirect) {
		t.Fatal("required probe failure must fail closed")
	}
}
