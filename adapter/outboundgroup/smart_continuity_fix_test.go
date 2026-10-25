package outboundgroup

import (
	"testing"

	"github.com/metacubex/mihomo/component/smart"
)

// Regression: no model output, one short-lived TLS flow, HTTP site response,
// or a loss sample is allowed to close another app's established TUN socket.
func TestSmartPassiveEvidenceCannotForceCloseSessions(t *testing.T) {
	soft := []smart.BlockCode{
		smart.BlockNone, smart.BlockLowWeight, smart.BlockNoResponse,
		smart.BlockPacketLoss, smart.BlockAbnormalStatus, smart.BlockDialFailure,
	}
	for _, code := range soft {
		if shouldForceCloseSmartSessions(code) {
			t.Fatalf("passive code %v is able to close active sessions", code)
		}
	}
	if !shouldForceCloseSmartSessions(smart.BlockManual) {
		t.Fatal("explicit manual closure must remain available")
	}
}

func TestSmartPinHysteresisOnHealthyAlternative(t *testing.T) {
	if !preserveSmartPin("A", "B", true) {
		t.Fatal("different usable host winner should not evict healthy pin")
	}
	if preserveSmartPin("A", "B", false) {
		t.Fatal("removed member must be replaceable")
	}
	if preserveSmartPin("", "B", true) {
		t.Fatal("missing previous pin must be replaceable")
	}
}
