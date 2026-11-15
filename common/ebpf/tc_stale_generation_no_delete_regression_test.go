//go:build with_ebpf && (linux || android)

package ebpf

import (
 "errors"
 "net/netip"
 "testing"

 "golang.org/x/sys/unix"
)

// A stale assignment must be rejected without deleting the map entry.
// Deleting on generation mismatch could race a fresh kernel-side replacement.
func TestLookupAssignmentStaleGenerationPreservesMapEntry(t *testing.T) {
 backend := newTestAssignmentBackend(t)
 source := netip.MustParseAddrPort("192.0.2.10:42000")
 destination := netip.MustParseAddrPort("198.51.100.20:443")
 const ifindex uint32 = 23
 writeTestAssignment(t, backend, ProtocolTCP, source, destination, ifindex, TCAssignment{
  NetworkGeneration: 10, SocketCookie: 0x1234, InterfaceIndex: ifindex, Path: TCPathShared,
 })
 backend.control.NetworkGeneration = 11
 if _, err := backend.LookupAssignment(ProtocolTCP, source, destination, ifindex, true); !errors.Is(err, unix.ENOENT) {
  t.Fatalf("stale assignment lookup: got %v, want ENOENT", err)
 }
 // Restoring the generation simulates observing the unchanged map entry.
 // This deliberately tests the no-delete contract, not live concurrent replacement.
 backend.control.NetworkGeneration = 10
 got, err := backend.LookupAssignment(ProtocolTCP, source, destination, ifindex, false)
 if err != nil { t.Fatalf("stale entry was deleted on mismatch: %v", err) }
 if got.SocketCookie != 0x1234 || got.NetworkGeneration != 10 {
  t.Fatalf("unexpected preserved assignment: %+v", got)
 }
}
