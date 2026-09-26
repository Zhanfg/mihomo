package outboundgroup

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/component/health"

	"github.com/metacubex/mihomo/common/callback"
	N "github.com/metacubex/mihomo/common/net"
	"github.com/metacubex/mihomo/common/singledo"
	"github.com/metacubex/mihomo/common/utils"
	C "github.com/metacubex/mihomo/constant"
	P "github.com/metacubex/mihomo/constant/provider"
)

type URLTestOption struct {
	Tolerance   uint16 `group:"tolerance,omitempty"`
	PreferIPv4 bool   `group:"prefer-ipv4,omitempty"`
	RequireIPv4 bool  `group:"require-ipv4,omitempty"`
	RequireIPv6 bool  `group:"require-ipv6,omitempty"`
}

type URLTest struct {
	*GroupBase
	selected       string
	testUrl        string
	expectedStatus string
	tolerance      uint16
	disableUDP     bool
	preferIPv4     bool
	requireIPv4    bool
	requireIPv6    bool
	fastNode       C.Proxy
	fastSingle     *singledo.Single[C.Proxy]
}

func (u *URLTest) Now() string {
	return u.fast(false).Name()
}

func (u *URLTest) Set(name string) error {
	var p C.Proxy
	for _, proxy := range u.GetProxies(false) {
		if proxy.Name() == name {
			p = proxy
			break
		}
	}
	if p == nil {
		return errors.New("proxy not exist")
	}
	u.ForceSet(name)
	return nil
}

func (u *URLTest) ForceSet(name string) {
	u.selected = name
	u.fastSingle.Reset()
}

// DialContext implements C.ProxyAdapter
func (u *URLTest) DialContext(ctx context.Context, metadata *C.Metadata) (c C.Conn, err error) {
	proxy := u.fast(true)
	c, err = proxy.DialContext(ctx, metadata)
	if err == nil {
		c.AppendToChains(u)
	} else {
		u.onDialFailed(proxy.Type(), err, u.healthCheck)
	}

	if N.NeedHandshake(c) {
		c = callback.NewFirstWriteCallBackConn(c, func(err error) {
			if err == nil {
				u.onDialSuccess()
			} else {
				u.onDialFailed(proxy.Type(), err, u.healthCheck)
			}
		})
	}

	return c, err
}

// ListenPacketContext implements C.ProxyAdapter
func (u *URLTest) ListenPacketContext(ctx context.Context, metadata *C.Metadata) (C.PacketConn, error) {
	proxy := u.fast(true)
	pc, err := proxy.ListenPacketContext(ctx, metadata)
	if err == nil {
		pc.AppendToChains(u)
	} else {
		u.onDialFailed(proxy.Type(), err, u.healthCheck)
	}

	return pc, err
}

// Unwrap implements C.ProxyAdapter
func (u *URLTest) Unwrap(metadata *C.Metadata, touch bool) C.Proxy {
	return u.fast(touch)
}

func (u *URLTest) healthCheck() {
	u.fastSingle.Reset()
	u.GroupBase.healthCheck()
	u.fastSingle.Reset()
}

// rankDelay is the latency used for ranking: the measured delay, plus a
// penalty for capability preferences the node does not meet, plus — when
// penalize-unstable is on — a penalty derived from recently stalled
// connections through that proxy. A node that passes latency probes but
// black-holes real traffic therefore stops winning the comparison, and a node
// missing a preferred capability sinks in the ordering while staying
// selectable as a last resort.
func (u *URLTest) rankDelay(proxy C.Proxy) uint16 {
	delay := adapter.AddCapabilityPenaltyExtended(
		proxy.LastDelayForTestUrl(u.testUrl), proxy, u.preferUDP, u.preferIPv4, u.preferIPv6)
	if !u.penalizeUnstable {
		return delay
	}
	p := health.Penalty(health.ProxyKey(proxy.Name(), proxy.ProxyInfo().ProviderName))
	if p == 0 {
		return delay
	}
	if uint32(delay)+uint32(p) > 0xFFFF {
		return 0xFFFF
	}
	return delay + p
}

func (u *URLTest) fast(touch bool) C.Proxy {
	elm, _, shared := u.fastSingle.Do(func() (C.Proxy, error) {
		proxies := u.GetProxies(touch)
		eligible := func(proxy C.Proxy) bool {
			return proxy != nil &&
				proxy.AliveForTestUrl(u.testUrl) &&
				adapter.IPFamilyRequirementsMet(proxy, u.requireIPv4, u.requireIPv6, false)
		}

		if u.selected != "" {
			for _, proxy := range proxies {
				if proxy.Name() == u.selected && eligible(proxy) {
					u.fastNode = proxy
					return proxy, nil
				}
			}
		}

		var fast C.Proxy
		var minDelay uint16
		fastNotExist := true

		for _, proxy := range proxies {
			if u.fastNode != nil && proxy.Name() == u.fastNode.Name() {
				fastNotExist = false
			}
			if !eligible(proxy) {
				continue
			}
			delay := u.rankDelay(proxy)
			if fast == nil || delay < minDelay {
				fast = proxy
				minDelay = delay
			}
		}

		// Hard requirements are intentionally fail-closed. A group that says
		// require-ipv4/require-ipv6 must never silently fall back to a proxy
		// whose family support is unknown or known-bad.
		if fast == nil {
			u.fastNode = u.EmptyFallback()
			return u.fastNode, nil
		}

		if u.fastNode == nil || fastNotExist || !eligible(u.fastNode) ||
			delayExceedsTolerance(u.rankDelay(u.fastNode), u.rankDelay(fast), u.tolerance) {
			u.fastNode = fast
		}
		return u.fastNode, nil
	})
	if shared && touch { // a shared fastSingle.Do() may cause providers untouched, so we touch them again
		u.Touch()
	}

	return elm
}

func delayExceedsTolerance(current, candidate, tolerance uint16) bool {
	return uint32(current) > uint32(candidate)+uint32(tolerance)
}

// SupportUDP implements C.ProxyAdapter
func (u *URLTest) SupportUDP() bool {
	if u.disableUDP {
		return false
	}
	return u.fast(false).SupportUDP()
}

// IsL3Protocol implements C.ProxyAdapter
func (u *URLTest) IsL3Protocol(metadata *C.Metadata) bool {
	return u.fast(false).IsL3Protocol(metadata)
}

// MarshalJSON implements C.ProxyAdapter
func (u *URLTest) MarshalJSON() ([]byte, error) {
	all := []string{}
	for _, proxy := range u.GetProxies(false) {
		all = append(all, proxy.Name())
	}
	return json.Marshal(map[string]any{
		"type":           u.Type().String(),
		"now":            u.Now(),
		"all":            all,
		"testUrl":        u.testUrl,
		"expectedStatus": u.expectedStatus,
		"fixed":          u.selected,
		"hidden":         u.Hidden(),
		"icon":           u.Icon(),
		"emptyFallback":  u.EmptyFallback().Name(),
		"preferIPv4":     u.preferIPv4,
		"preferIPv6":     u.preferIPv6,
		"requireIPv4":    u.requireIPv4,
		"requireIPv6":    u.requireIPv6,
	})
}

func (u *URLTest) Providers() []P.ProxyProvider {
	return u.providers
}

func (u *URLTest) Proxies() []C.Proxy {
	return u.GetProxies(false)
}

func (u *URLTest) URLTest(ctx context.Context, url string, expectedStatus utils.IntRanges[uint16]) (map[string]uint16, error) {
	return u.GroupBase.URLTest(ctx, u.testUrl, expectedStatus)
}

func NewURLTest(option GroupCommonOption, urlTestOption URLTestOption, emptyFallback C.Proxy, providers []P.ProxyProvider) (*URLTest, error) {
	if emptyFallback == nil {
		return nil, errors.New("empty fallback proxy not exist")
	}
	urlTest := &URLTest{
		GroupBase: NewGroupBase(GroupBaseOption{
			Name:             option.Name,
			Type:             C.URLTest,
			Hidden:           option.Hidden,
			Icon:             option.Icon,
			Filter:           option.Filter,
			ExcludeFilter:    option.ExcludeFilter,
			ExcludeType:      option.ExcludeType,
			TestTimeout:      option.TestTimeout,
			MaxFailedTimes:   option.MaxFailedTimes,
			EmptyFallback:    emptyFallback,
			PreferUDP:        option.PreferUDP,
			PenalizeUnstable: option.PenalizeUnstable,
			PreferIPv6:       option.PreferIPv6,
			Providers:        providers,
		}),
		fastSingle:     singledo.NewSingle[C.Proxy](time.Second * 10),
		disableUDP:     option.DisableUDP,
		testUrl:        option.URL,
		expectedStatus: option.ExpectedStatus,
		tolerance:      urlTestOption.Tolerance,
		preferIPv4:     urlTestOption.PreferIPv4,
		requireIPv4:    urlTestOption.RequireIPv4,
		requireIPv6:    urlTestOption.RequireIPv6,
	}

	return urlTest, nil
}
