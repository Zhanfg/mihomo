//go:build with_ebpf && linux && ebpf_integration

package ebpf

import (
	"testing"
	"time"
	"unsafe"
)

// The point of SetUDPTimeout is that the kernel reads the new value, so the
// assertion is against the control map the programs actually consult -- not
// against the backend's own copy of it, which would pass even if the write
// never left userspace.
func TestCgroupSetUDPTimeoutReachesTheControlMapIntegration(t *testing.T) {
	requireEBPFIntegration(t, "test cgroup UDP timeout control")
	cgroupPath, err := DetectCgroup2Root()
	if err != nil {
		t.Skipf("cgroup v2 is unavailable: %v", err)
	}
	path, _ := createIntegrationCgroup(t, cgroupPath, 0)
	backend, err := prepareCgroupIntegrationBackend(path, true, true, false)
	if err != nil {
		if cgroupIntegrationUnavailable(err) {
			t.Skipf("cgroup eBPF is unavailable: %v", err)
		}
		t.Fatalf("prepare cgroup backend: %v", err)
	}
	defer backend.Close()

	if err = backend.LoadPrograms(1080); err != nil {
		t.Fatalf("load cgroup programs: %v", err)
	}
	readTimeout := func() uint32 {
		t.Helper()
		key := uint32(0)
		var control cgroupControl
		if err := lookupMap(backend.runtime.control_map_fd, unsafe.Pointer(&key), unsafe.Pointer(&control)); err != nil {
			t.Fatalf("read cgroup control: %v", err)
		}
		return control.UDPTimeoutSeconds
	}

	if got := readTimeout(); got == 0 {
		t.Fatal("the backend loaded with no UDP timeout at all")
	}
	if err = backend.SetUDPTimeout(600 * time.Second); err != nil {
		t.Fatalf("set UDP timeout: %v", err)
	}
	if got := readTimeout(); got != 600 {
		t.Fatalf("kernel UDP timeout = %d, want 600", got)
	}
	if got := backend.UDPTimeoutSeconds(); got != 600 {
		t.Fatalf("backend reports %d, kernel has 600", got)
	}

	// The rest of the control record has to survive: it is rewritten wholesale
	// from the backend's own state on every change, so a field this path forgets
	// would be silently zeroed rather than left alone.
	key := uint32(0)
	var control cgroupControl
	if err = lookupMap(backend.runtime.control_map_fd, unsafe.Pointer(&key), unsafe.Pointer(&control)); err != nil {
		t.Fatalf("read cgroup control: %v", err)
	}
	if control.ListenerPort != 1080 {
		t.Fatalf("listener port = %d after a timeout change, want 1080", control.ListenerPort)
	}
	if control.Flags == 0 {
		t.Fatal("policy flags were cleared by a timeout change")
	}
}
