//go:build with_ebpf && (linux || android)

package ebpf

import (
 "net/netip"
 "testing"
)

// Verify that a network-generation change rejects stale IPv6 assignments,
// including when the interface index and five-tuple are reused.
func TestLookupAssignmentIPv6GenerationAndInterfaceReuse(t *testing.T) {
 backend := newTestAssignmentBackend(t)
 source := netip.MustParseAddrPort("[2001:db8:1::2]:41004")
 destination := netip.MustParseAddrPort("[2001:db8:1::1]:443")
 const ifindex uint32 = 12
 writeTestAssignment(t, backend, ProtocolTCP, source, destination, ifindex, TCAssignment{
  NetworkGeneration: 1, SocketCookie: 0xaaaa, InterfaceIndex: ifindex, Path: TCPathShared,
 })
 backend.control.NetworkGeneration = 2
 if _, err := backend.LookupAssignment(ProtocolTCP, source, destination, ifindex, false); err == nil {
  t.Fatal("stale IPv6 assignment survived a network-generation change")
 }
 writeTestAssignment(t, backend, ProtocolTCP, source, destination, ifindex, TCAssignment{
  NetworkGeneration: 2, SocketCookie: 0xbbbb, InterfaceIndex: ifindex, Path: TCPathShared,
 })
 if _, err := backend.LookupAssignment(ProtocolTCP, source, destination, ifindex+1, false); err == nil {
  t.Fatal("IPv6 assignment leaked across interface indexes")
 }
 got, err := backend.LookupAssignment(ProtocolTCP, source, destination, ifindex, true)
 if err != nil { t.Fatal(err) }
 if got.SocketCookie != 0xbbbb || got.NetworkGeneration != 2 {
  t.Fatalf("fresh IPv6 assignment corrupted: %+v", got)
 }
 if _, err := backend.LookupAssignment(ProtocolTCP, source, destination, ifindex, false); err == nil {
  t.Fatal("consumed IPv6 TCP assignment remained in the map")
 }
}
