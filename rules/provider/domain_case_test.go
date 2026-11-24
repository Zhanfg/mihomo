package provider_test

import (
	"testing"

	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/rules/provider"
)

func TestDomainStrategyMatchMixedCaseHost(t *testing.T) {
	strategy := provider.NewDomainStrategy()
	strategy.Insert("exact.example.com")
	strategy.Insert("+.suffix.example.com")
	strategy.FinishInsert()

	cases := []struct {
		name     string
		metadata C.Metadata
		want     bool
	}{
		{"exact", C.Metadata{Host: "EXACT.Example.COM"}, true},
		{"suffix", C.Metadata{Host: "SUFFIX.Example.COM"}, true},
		{"subdomain", C.Metadata{Host: "WWW.Suffix.EXAMPLE.COM"}, true},
		{"sniff-host", C.Metadata{Host: "unrelated.test", SniffHost: "EXACT.Example.COM"}, true},
		{"sniff-priority", C.Metadata{Host: "exact.example.com", SniffHost: "unrelated.test"}, false},
		{"nonmatch", C.Metadata{Host: "exact.example.com.invalid"}, false},
		{"empty", C.Metadata{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.metadata
			if got := strategy.Match(&m, C.RuleMatchHelper{}); got != tc.want {
				t.Fatalf("Match(%q) = %v; want %v", m.RuleHost(), got, tc.want)
			}
			if m.Host != tc.metadata.Host || m.SniffHost != tc.metadata.SniffHost {
				t.Fatal("matching mutated connection metadata")
			}
		})
	}
}
