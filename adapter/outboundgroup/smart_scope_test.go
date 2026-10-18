package outboundgroup

import (
	"strings"
	"testing"
)

func TestScopedSmartLearningIsolatesIdenticallyNamedProfiles(t *testing.T) {
	const base = "config"
	a, err := scopedSmartConfigName(base, "bettbox-1234")
	if err != nil { t.Fatal(err) }
	b, err := scopedSmartConfigName(base, "bettbox-5678")
	if err != nil { t.Fatal(err) }
	if a == b || a == base || b == base {
		t.Fatalf("independent profiles have overlapping storage namespace: %q, %q",a,b)
	}
	repeated, err := scopedSmartConfigName(base,"bettbox-1234")
	if err != nil || repeated != a {
		t.Fatalf("scope is not stable: %q, %v",repeated,err)
	}
}

func TestScopedSmartLearningPreservesExistingUpstreamConfig(t *testing.T) {
	legacy, err := scopedSmartConfigName("config","")
	if err != nil || legacy != "config" {
		t.Fatalf("upstream Smart namespace must be unchanged: %q, %v",legacy,err)
	}
}

func TestScopedSmartLearningRejectsTraversalAndExcessiveIDs(t *testing.T) {
	for _, bad := range []string{"../../cache", "config:bad", "group/name", "bad space", strings.Repeat("x",65)} {
		if _, err := scopedSmartConfigName("config",bad); err == nil {
			t.Fatalf("invalid learning scope was accepted: %q",bad)
		}
	}
}
