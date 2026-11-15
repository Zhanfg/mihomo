//go:build with_ebpf && (linux || android)

package ebpf

import "testing"

func TestTCNetworkGenerationNormalization(t *testing.T) {
	for _, tc := range []struct{ in, want uint32 }{
		{0, 1}, {1, 1}, {42, 42}, {0x80000001, 1},
		{0x80000000, 1}, {0xffffffff, 0x7fffffff},
	} {
		if got := normalizeTCNetworkGeneration(tc.in); got != tc.want {
			t.Fatalf("normalizeTCNetworkGeneration(%#x) = %#x, want %#x", tc.in, got, tc.want)
		}
	}
}

func TestTCBypassMarkRoutingCollision(t *testing.T) {
	for _, tc := range []struct{ routing, mark, mask, want uint32 }{
		{1 << 29, 0xa7, 0xff, 0xff},
		{0xa7, 0xa7, 0xff, 0},
		{1 << 29, 0, 0xff, 0},
		{1 << 29, 0xa7, 0, 0},
		{1 << 29, 0x1a7, 0xff, 0},
	} {
		if got := checkedTCBypassMask(tc.routing, tc.mark, tc.mask); got != tc.want {
			t.Fatalf("checkedTCBypassMask(%#x,%#x,%#x) = %#x, want %#x", tc.routing, tc.mark, tc.mask, got, tc.want)
		}
	}
}
