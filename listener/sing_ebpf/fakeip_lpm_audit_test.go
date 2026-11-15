//go:build with_ebpf && (linux || android)

package sing_ebpf

import (
    "net/netip"
    "testing"
)

func TestFakeIPLPMNormalizeAndDetectBypassOverlap(t *testing.T) {
    ipv4, ipv6, err := normalizeFakeIPPrefixPair(
        netip.MustParsePrefix("198.19.5.12/15"),
        netip.MustParsePrefix("2001:db8:abcd:1234::f/48"),
    )
    if err != nil { t.Fatal(err) }
    if want := netip.MustParsePrefix("198.18.0.0/15"); ipv4 != want {
        t.Fatalf("FakeIP IPv4 = %s, want %s", ipv4, want)
    }
    if want := netip.MustParsePrefix("2001:db8:abcd::/48"); ipv6 != want {
        t.Fatalf("FakeIP IPv6 = %s, want %s", ipv6, want)
    }
    inbound := &Inbound{fakeIPIPv4Prefix: ipv4, fakeIPIPv6Prefix: ipv6}
    overlaps := inbound.fakeIPBypassConflictCount([]netip.Prefix{
        netip.MustParsePrefix("198.18.0.0/16"),
        netip.MustParsePrefix("2001:db8::/32"),
        netip.MustParsePrefix("192.0.2.0/24"),
    })
    if overlaps != 2 {
        t.Fatalf("expected one IPv4 and one IPv6 Fake-IP/LPM bypass overlap, got %d", overlaps)
    }
}

func TestFakeIPLPMMandatorySafetyRanges(t *testing.T) {
    tests := []struct{
        name string
        prefix string
    }{
        {"loopback-v4", "127.0.0.1/32"},
        {"multicast-v4", "224.0.0.0/4"},
        {"unspecified-v4", "0.0.0.0/8"},
        {"loopback-v6", "::1/128"},
        {"multicast-v6", "ff02::/16"},
        {"mapped-v4", "::ffff:198.18.0.0/112"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            prefix := netip.MustParsePrefix(tt.prefix)
            var ipv4, ipv6 netip.Prefix
            if prefix.Addr().Is4() { ipv4 = prefix } else { ipv6 = prefix }
            if _, _, err := normalizeFakeIPPrefixPair(ipv4, ipv6); err == nil {
                t.Fatalf("unsafe Fake-IP range %s was accepted", prefix)
            }
        })
    }
}

func TestFakeIPLPMDisjointFamiliesDoNotOverlap(t *testing.T) {
    v4 := netip.MustParsePrefix("198.18.0.0/15")
    v6 := netip.MustParsePrefix("2001:db8::/32")
    if prefixesOverlap(v4, v6) {
        t.Fatal("IPv4 and IPv6 Fake-IP were treated as overlapping")
    }
}
