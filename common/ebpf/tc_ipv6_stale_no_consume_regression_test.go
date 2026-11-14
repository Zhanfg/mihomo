//go:build with_ebpf && (linux || android)

package ebpf

import (
    "errors"
    "net/netip"
    "testing"

    "golang.org/x/sys/unix"
)

// An IPv6 assignment from an older network generation must not be consumed
// by a TCP lookup. The old entry must remain untouched on mismatch: deleting
// it could race with a kernel-side replacement for the new generation.
func TestLookupAssignmentIPv6StaleGenerationDoesNotConsume(t *testing.T) {
    backend := newTestAssignmentBackend(t)
    source := netip.MustParseAddrPort("[2001:db8:10::2]:42001")
    destination := netip.MustParseAddrPort("[2001:db8:10::1]:443")
    const ifindex uint32 = 24
    writeTestAssignment(t, backend, ProtocolTCP, source, destination, ifindex, TCAssignment{
        NetworkGeneration: 20, SocketCookie: 0x12345678, InterfaceIndex: ifindex, Path: TCPathShared,
    })

    backend.control.NetworkGeneration = 21
    if _, err := backend.LookupAssignment(ProtocolTCP, source, destination, ifindex, true); !errors.Is(err, unix.ENOENT) {
        t.Fatalf("stale IPv6 assignment: got %v, want ENOENT", err)
    }

    // Restore the generation solely to inspect whether the original map
    // entry survived; this is not a live concurrent kernel replacement test.
    backend.control.NetworkGeneration = 20
    got, err := backend.LookupAssignment(ProtocolTCP, source, destination, ifindex, false)
    if err != nil {
        t.Fatalf("stale IPv6 assignment was consumed: %v", err)
    }
    if got.NetworkGeneration != 20 || got.SocketCookie != 0x12345678 {
        t.Fatalf("preserved IPv6 assignment changed: %+v", got)
    }
}
