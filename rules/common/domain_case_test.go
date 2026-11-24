package common

import (
	"testing"

	C "github.com/metacubex/mihomo/constant"
)

func TestDomainRulesMatchMixedCaseHost(t *testing.T) {
	const adapter = "test"
	regex, err := NewDomainRegex("^www[.]example[.]com$", adapter)
	if err != nil {
		t.Fatal(err)
	}
	wildcard, err := NewDomainWildcard("*.example.com", adapter)
	if err != nil {
		t.Fatal(err)
	}
	rules := []struct {
		name string
		rule C.Rule
	}{
		{"exact", NewDomain("www.example.com", adapter)},
		{"keyword", NewDomainKeyword("example", adapter)},
		{"suffix", NewDomainSuffix("example.com", adapter)},
		{"regex", regex},
		{"wildcard", wildcard},
	}
	cases := []struct {
		name     string
		metadata C.Metadata
		want     bool
	}{
		{"lowercase", C.Metadata{Host: "www.example.com"}, true},
		{"mixed-case", C.Metadata{Host: "WWW.Example.COM"}, true},
		{"sniff-host", C.Metadata{Host: "unrelated.test", SniffHost: "WWW.Example.COM"}, true},
		{"sniff-priority", C.Metadata{Host: "www.example.com", SniffHost: "unrelated.test"}, false},
		{"nonmatch", C.Metadata{Host: "unrelated.test"}, false},
		{"empty", C.Metadata{}, false},
	}
	for _, rule := range rules {
		t.Run(rule.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					m := tc.metadata
					got, target := rule.rule.Match(&m, C.RuleMatchHelper{})
					if got != tc.want || target != adapter {
						t.Fatalf("Match(%q) = %v, %q; want %v, %q", m.RuleHost(), got, target, tc.want, adapter)
					}
					if m.Host != tc.metadata.Host || m.SniffHost != tc.metadata.SniffHost {
						t.Fatal("matching mutated connection metadata")
					}
				})
			}
		})
	}
}
