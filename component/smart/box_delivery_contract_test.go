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
		"BACKUP_SUFFIX=\".smart-prev\"",
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


func TestSmartBoxV8AppImportContract(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/boxproxy-native-ebpf-android.yml")
	if err != nil {
		t.Fatalf("read Android workflow: %v", err)
	}
	workflow := string(data)
	for _, required := range []string{
		"mihomo-android-arm64-v8-alpha-smart-$HEAD_SHORT",
		"BOXPROXY_IMPORT_INFO.txt",
		"tools/install-smart-v8-box.sh",
		"sh tools/test-smart-v8-installer.sh",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("Android delivery workflow missing %q", required)
		}
	}
}


func TestSmartBoxV8ExtremeRecoveryContract(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/mobile-core-stress.yml")
	if err != nil {
		t.Fatalf("read stress workflow: %v", err)
	}
	workflow := string(data)
	for _, required := range []string{
		"backlog=8192",
		"/tmp/mihomo-soak/listener.up",
		"/tmp/mihomo-soak/listener.down",
		"--count 30000 --concurrency 128",
		"--count 2000 --concurrency 128",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("extreme recovery workflow missing %q", required)
		}
	}
}
