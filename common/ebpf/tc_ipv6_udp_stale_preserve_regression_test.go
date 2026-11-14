//go:build with_ebpf && (linux || android)

package ebpf

import (
    "errors"
    "net/netip"
    "testing"

    "golang.org/x/sys/unix"
)

// UDP lookups must reject a stale IPv6 assignment without deleting it.
// A concurrent kernel writer may replace the entry immediately afterward.
func TestLookupAssignmentUDPIPv6StaleGenerationPreservesEntry(t *testing.T) {
    backend := newTestAssignmentBackend(t)
    source := netip.MustParseAddrPort("[2001:db8:20::2]:53535")
    destination := netip.MustParseAddrPort("[2001:db8:20::1]:53")
    const ifindex uint32 = 25

    writeTestAssignment(t, backend, ProtocolUDP, source, destination, ifindex, TCAssignment{
        NetworkGeneration: 30, SocketCookie: 0xabcdef, InterfaceIndex: ifindex, Path: TCPathShared,
    })
    backend.control.NetworkGeneration = 31
    if _, err := backend.LookupAssignment(ProtocolUDP, source, destination, ifindex, true); !errors.Is(err, unix.ENOENT) {
        t.Fatalf("stale UDP IPv6 assignment: got %v, want ENOENT", err)
    }

    // Revert only for inspecting the same map entry. This does not simulate
    // concurrent kernel replacement or prove privileged attach behavior.
    backend.control.NetworkGeneration = 30
    got, err := backend.LookupAssignment(ProtocolUDP, source, destination, ifindex, false)
    if err != nil {
        t.Fatalf("stale UDP IPv6 entry was deleted: %v", err)
    }
    if got.NetworkGeneration != 30 || got.SocketCookie != 0xabcdef {
        t.Fatalf("preserved UDP IPv6 entry changed: %+v", got)
    }
}
