package outboundgroup

import (
	"net/netip"
	"testing"

	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/adapter/outbound"
	smartstore "github.com/metacubex/mihomo/component/smart"
	C "github.com/metacubex/mihomo/constant"
)

func countryTestProxy(name string) C.Proxy {
	return adapter.NewProxy(outbound.NewDirectWithOption(outbound.DirectOption{Name: name}))
}

func TestCountryAffinityIsScopedPerSmartTarget(t *testing.T) {
	smartstore.InitCache()
	pJP := countryTestProxy("jp")
	pUS := countryTestProxy("us")
	fallback := countryTestProxy("fallback")
	s := &Smart{
		GroupBase: NewGroupBase(GroupBaseOption{
			Name:          "SMART",
			Type:          C.Smart,
			EmptyFallback: fallback,
		}),
		store:           &smartstore.Store{},
		configName:      "cfg",
		countryAffinity: true,
	}

	s.store.StoreUnwrapResultWithCountry("SMART", "cfg", "service-a", []C.Proxy{pJP}, "JP")
	s.store.StoreUnwrapResultWithCountry("SMART", "cfg", "service-b", []C.Proxy{pUS}, "US")

	metaA := &C.Metadata{SmartTarget: "service-a", DstIP: netip.MustParseAddr("2001:db8::1")}
	metaB := &C.Metadata{SmartTarget: "service-b", DstIP: netip.MustParseAddr("198.51.100.1")}

	if got, strict := s.desiredCountry(metaA, []C.Proxy{pJP, pUS}); got != "JP" || strict {
		t.Fatalf("service-a affinity=(%q,%v), want (JP,false)", got, strict)
	}
	if got, strict := s.desiredCountry(metaB, []C.Proxy{pJP, pUS}); got != "US" || strict {
		t.Fatalf("service-b affinity=(%q,%v), want (US,false)", got, strict)
	}
}

func TestExplicitCountryStillOverridesTargetAffinity(t *testing.T) {
	smartstore.InitCache()
	p := countryTestProxy("node")
	fallback := countryTestProxy("fallback")
	s := &Smart{
		GroupBase: NewGroupBase(GroupBaseOption{
			Name:          "SMART",
			Type:          C.Smart,
			EmptyFallback: fallback,
		}),
		store:           &smartstore.Store{},
		configName:      "cfg",
		countryAffinity: true,
		country:         "DE",
	}
	s.store.StoreUnwrapResultWithCountry("SMART", "cfg", "service", []C.Proxy{p}, "JP")
	meta := &C.Metadata{SmartTarget: "service", DstIP: netip.MustParseAddr("2001:db8::1")}

	if got, strict := s.desiredCountry(meta, []C.Proxy{p}); got != "DE" || !strict {
		t.Fatalf("explicit country=(%q,%v), want (DE,true)", got, strict)
	}
}
