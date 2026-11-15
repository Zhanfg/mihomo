//go:build with_ebpf && (linux || android)

package ebpf

import (
    "net/netip"
    "testing"
    "unsafe"
)

// Fake-IP prefixes use fixed control-record masks, while destination
// bypass/include/exclude maps use LPM tries. Do not conflate the two ABIs.
func TestFakeIPLPMDualStackEncodingAndABI(t *testing.T) {
    policy, err := CompilePolicy(PolicyConfig{
        EnableTCP: true,
        EnableUDP: true,
        FakeIPIPv4: netip.MustParsePrefix("198.19.200.3/15"),
        FakeIPIPv6: netip.MustParsePrefix("2001:db8:abcd:1234::1/48"),
    })
    if err != nil { t.Fatal(err) }
    if want := netip.MustParsePrefix("198.18.0.0/15"); policy.fakeIPIPv4 != want {
        t.Fatalf("IPv4 Fake-IP prefix = %s, want %s", policy.fakeIPIPv4, want)
    }
    if want := netip.MustParsePrefix("2001:db8:abcd::/48"); policy.fakeIPIPv6 != want {
        t.Fatalf("IPv6 Fake-IP prefix = %s, want %s", policy.fakeIPIPv6, want)
    }
    if got, want := prefixMask4(15), ([4]byte{255, 254, 0, 0}); got != want {
        t.Fatalf("IPv4 /15 mask = %v, want %v", got, want)
    }
    if got, want := prefixMask16(48), ([16]byte{255,255,255,255,255,255}); got != want {
        t.Fatalf("IPv6 /48 mask = %v, want %v", got, want)
    }
    flags := tcFlags(TCConfig{
        EnableIPv4: true, EnableLocalIPv6: true, EnableSharedIPv6: true,
        EnableTCP: true, EnableUDP: true,
    }, policy)
    if flags&(tcFlagFakeIPIPv4|tcFlagFakeIPIPv6) != tcFlagFakeIPIPv4|tcFlagFakeIPIPv6 {
        t.Fatalf("TC Fake-IP dual-stack flags missing: %#x", flags)
    }
    if got := unsafe.Sizeof(ipv4CIDRLPMKey{}); got != 8 {
        t.Fatalf("IPv4 bypass trie key ABI changed: %d bytes", got)
    }
    if got := unsafe.Sizeof(ipv6CIDRLPMKey{}); got != 20 {
        t.Fatalf("IPv6 bypass trie key ABI changed: %d bytes", got)
    }
}

// A bypass range overlapping Fake-IP must stay in the existing LPM trie,
// while the Fake-IP range remains an independent forced-interception prefix.
func TestFakeIPLPMBypassPolicyIndependentOfFakeIPPrefix(t *testing.T) {
    fake := netip.MustParsePrefix("100.64.10.99/24")
    ip4, ip6, err := compileBypassCIDRPolicy([]netip.Prefix{
        netip.MustParsePrefix("100.64.0.0/10"),
        netip.MustParsePrefix("2001:db8::/32"),
    })
    if err != nil { t.Fatal(err) }
    if len(ip4) != 1 || len(ip6) != 1 ||
       !ip4[0].Contains(fake.Addr()) ||
       !ip6[0].Contains(netip.MustParseAddr("2001:db8::1")) {
        t.Fatalf("expected LPM CIDRs preserved: v4=%v v6=%v", ip4, ip6)
    }
    policy, err := CompilePolicy(PolicyConfig{EnableTCP: true, FakeIPIPv4: fake})
    if err != nil { t.Fatal(err) }
    if want := netip.MustParsePrefix("100.64.10.0/24"); policy.fakeIPIPv4 != want {
        t.Fatalf("Fake-IP compiled prefix changed unexpectedly: got %s want %s", policy.fakeIPIPv4, want)
    }
}

func TestFakeIPLPMMaskBoundaryLengths(t *testing.T) {
    if got := prefixMask4(0); got != ([4]byte{}) {
        t.Fatalf("IPv4 /0 mask = %v, want all zero", got)
    }
    if got := prefixMask4(32); got != ([4]byte{255,255,255,255}) {
        t.Fatalf("IPv4 /32 mask = %v, want all ones", got)
    }
    if got := prefixMask16(0); got != ([16]byte{}) {
        t.Fatalf("IPv6 /0 mask = %v, want all zero", got)
    }
    if got := prefixMask16(128); got != ([16]byte{255,255,255,255,255,255,255,255,255,255,255,255,255,255,255,255}) {
        t.Fatalf("IPv6 /128 mask = %v, want all ones", got)
    }
}

func TestFakeIPLPMIPv4MappedBypassKeyCanonicalization(t *testing.T) {
    ipv4, ipv6, err := compileBypassCIDRPolicy([]netip.Prefix{
        netip.MustParsePrefix("::ffff:198.19.0.0/112"),
    })
    if err != nil { t.Fatal(err) }
    if len(ipv4) != 1 || ipv4[0] != netip.MustParsePrefix("198.19.0.0/16") || len(ipv6) != 0 {
        t.Fatalf("IPv4-mapped CIDR unexpectedly populated wrong trie: v4=%v v6=%v", ipv4, ipv6)
    }
}
