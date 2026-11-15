package tunnel

import (
	"testing"

	C "github.com/metacubex/mihomo/constant"
	P "github.com/metacubex/mihomo/constant/provider"
)

// The upstream swap API must return the previous configuration while publishing
// the replacement. The original global state is restored for other tunnel tests.
func TestUpdateRulesReturnsPreviousState(t *testing.T) {
	priorRules, priorSubRules, priorProviders := rules, subRules, ruleProviders
	t.Cleanup(func() { UpdateRules(priorRules, priorSubRules, priorProviders) })

	firstRules := []C.Rule{nil}
	firstSubRules := map[string][]C.Rule{"previous": nil}
	firstProviders := map[string]P.RuleProvider{"previous": nil}
	UpdateRules(firstRules, firstSubRules, firstProviders)

	nextRules := []C.Rule{nil, nil}
	nextSubRules := map[string][]C.Rule{"next": nil}
	nextProviders := map[string]P.RuleProvider{"next": nil}
	oldRules, oldSubRules, oldProviders := UpdateRules(nextRules, nextSubRules, nextProviders)
	if len(oldRules) != 1 || len(rules) != 2 {
		t.Fatalf("rule swap failed: prior=%d current=%d", len(oldRules), len(rules))
	}
	if _, ok := oldSubRules["previous"]; !ok {
		t.Fatal("previous sub-rule map not returned")
	}
	if _, ok := oldProviders["previous"]; !ok {
		t.Fatal("previous rule provider map not returned")
	}
	if _, ok := subRules["next"]; !ok {
		t.Fatal("new sub-rule map not installed")
	}
	if _, ok := ruleProviders["next"]; !ok {
		t.Fatal("new rule provider map not installed")
	}
}

func TestUpdateProxiesReturnsPreviousState(t *testing.T) {
	priorProxies, priorProviders := proxies, providers
	t.Cleanup(func() { UpdateProxies(priorProxies, priorProviders) })

	firstProxies := map[string]C.Proxy{"previous": nil}
	firstProviders := map[string]P.ProxyProvider{"previous": nil}
	UpdateProxies(firstProxies, firstProviders)

	nextProxies := map[string]C.Proxy{"next": nil}
	nextProviders := map[string]P.ProxyProvider{"next": nil}
	oldProxies, oldProviders := UpdateProxies(nextProxies, nextProviders)
	if _, ok := oldProxies["previous"]; !ok {
		t.Fatal("previous proxy map not returned")
	}
	if _, ok := oldProviders["previous"]; !ok {
		t.Fatal("previous provider map not returned")
	}
	if _, ok := proxies["next"]; !ok {
		t.Fatal("new proxy map not installed")
	}
	if _, ok := providers["next"]; !ok {
		t.Fatal("new provider map not installed")
	}
}
