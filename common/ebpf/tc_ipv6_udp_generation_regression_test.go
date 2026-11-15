//go:build with_ebpf && (linux || android)

package ebpf

import (
 "net/netip"
 "testing"
)

// A reused UDP five-tuple must not recover an assignment from an earlier
// network generation, even if its interface index is unchanged.
func TestLookupAssignmentUDPIPv6GenerationReuse(t *testing.T) {
 backend := newTestAssignmentBackend(t)
 source := netip.MustParseAddrPort("[2001:db8:2::2]:53000")
 destination := netip.MustParseAddrPort("[2001:db8:2::1]:53")
 const ifindex uint32 = 19
 writeTestAssignment(t, backend, ProtocolUDP, source, destination, ifindex, TCAssignment{
  NetworkGeneration: 5, SocketCookie: 0x1111, InterfaceIndex: ifindex, Path: TCPathShared,
 })
 backend.control.NetworkGeneration = 6
 if _, err := backend.LookupAssignment(ProtocolUDP, source, destination, ifindex, false); err == nil {
  t.Fatal("stale IPv6 UDP assignment survived generation change")
 }
 writeTestAssignment(t, backend, ProtocolUDP, source, destination, ifindex, TCAssignment{
  NetworkGeneration: 6, SocketCookie: 0x2222, InterfaceIndex: ifindex, Path: TCPathShared,
 })
 got, err := backend.LookupAssignment(ProtocolUDP, source, destination, ifindex, false)
 if err != nil { t.Fatal(err) }
 if got.NetworkGeneration != 6 || got.SocketCookie != 0x2222 {
  t.Fatalf("unexpected current-generation UDP assignment: %+v", got)
 }
}
