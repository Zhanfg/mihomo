package smart

import (
	"os"
	"strings"
	"testing"
)

func TestSmartBoxV8DeliveryContract(t *testing.T) {
	data, err := os.ReadFile("../../tools/install-smart-v8-box.sh")
	if err != nil {
		t.Fatalf("read installer: %v", err)
	}
	if len(data) > 32*1024 {
		t.Fatalf("installer grew beyond 32KiB mobile delivery budget: %d", len(data))
	}
	script := string(data)
	for _, required := range []string{
		"/data/adb/box",
		"database/box.db",
		"boxctl",
		"runtime_profile",
		"mihomo.smart-prev",
		"modern_restart",
		"legacy_restart",
		"BOX_DIR_OVERRIDE",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("installer missing Box integration contract %q", required)
		}
	}
	if strings.Contains(strings.ToUpper(script), "UPDATE RUNTIME_PROFILE") {
		t.Fatal("installer must not mutate BoxProxy runtime_profile directly")
	}
}
