//go:build with_ebpf && (linux || android)

package ebpf

import "testing"

// The high bit is reserved by the TC assignment ABI. A zero generation
// (including a value that becomes zero after masking) must never be emitted.
func TestTCNetworkGenerationNormalizationBoundary(t *testing.T) {
 cases := []struct {
  name string
  input uint32
  want uint32
 }{
  {"zero", 0, 1},
  {"first", 1, 1},
  {"maximum", 0x7fffffff, 0x7fffffff},
  {"high_bit_only", 0x80000000, 1},
  {"all_bits", 0xffffffff, 0x7fffffff},
  {"high_bit_and_one", 0x80000001, 1},
 }
 for _, tc := range cases {
  t.Run(tc.name, func(t *testing.T) {
   if got := normalizeTCNetworkGeneration(tc.input); got != tc.want {
    t.Fatalf("normalizeTCNetworkGeneration(%#x) = %#x, want %#x", tc.input, got, tc.want)
   }
  })
 }
}
