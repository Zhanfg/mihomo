package sing_tun

import (
	"testing"

	tun "github.com/metacubex/sing-tun"
)

func TestNormalizeDNSMode(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "default", input: "", want: ""},
		{name: "disabled", input: tun.DNSModeDisabled, want: tun.DNSModeDisabled},
		{name: "native", input: tun.DNSModeNative, want: tun.DNSModeNative},
		{name: "hijack", input: tun.DNSModeHijack, want: tun.DNSModeHijack},
		{name: "unknown", input: "unsupported-dns-mode", want: ""},
		{name: "case-sensitive", input: "NATIVE", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeDNSMode(tt.input); got != tt.want {
				t.Fatalf("normalizeDNSMode(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
