package outboundgroup

import (
	"testing"

	"github.com/metacubex/mihomo/component/smart"
	C "github.com/metacubex/mihomo/constant"
)

// The rule is meant to catch a node that accepts a flow and then swallows it.
// Requiring zero upload inverted the attribution: if the client never sent a
// byte the node had nothing to answer, so every speculative TLS connection a
// browser opens and closes unused cost the node a 24-hour block. The fork's
// own stall detector draws the line the same way.
func TestNoResponseVerdictNeedsTheClientToHaveSentSomething(t *testing.T) {
	const (
		group          = "quality-group"
		config         = "config"
		wildcardTarget = "example.com"
		node           = "node-a"
	)
	smart.InitCache()
	smart.InitQueue()

	for _, testCase := range []struct {
		name         string
		uploadTotal  float64
		wantDegraded bool
		wantCode     int64
	}{
		{name: "client sent nothing", uploadTotal: 0, wantDegraded: false, wantCode: 0},
		{name: "client sent a request and got nothing back", uploadTotal: 1, wantDegraded: true, wantCode: 4},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			s := sweepGroup(group + testCase.name)
			s.configName = config
			s.store = &smart.Store{}
			s.maxFailedTimes = 3
			s.hostFailLimit.Store(1_000)

			// Host is empty so the abnormal-status branch below cannot run a
			// live StatusTest through a nil proxy.
			metadata := &C.Metadata{
				WildcardTarget: wildcardTarget, SmartBlock: "normal",
				NetWork: C.TCP, DstPort: 443,
			}
			_, isDegraded, _, blockCode := s.checkNodeQuality(
				nil, metadata, nil, wildcardTarget, "example.com:443", node,
				0.9, 0.9, 1_000, testCase.uploadTotal, 0, "tcp", "", false, 0, 0)

			if isDegraded != testCase.wantDegraded || blockCode != testCase.wantCode {
				t.Fatalf("degraded=%v code=%d, want %v and %d",
					isDegraded, blockCode, testCase.wantDegraded, testCase.wantCode)
			}
		})
	}
}
