package adapter

import "testing"

func TestURLTestHookLifecycle(t *testing.T) {
	defer SetURLTestHook(nil)
	var name, url string
	var delay uint16
	SetURLTestHook(func(u, n string, d uint16) {
		url, name, delay = u, n, d
	})
	notifyURLTestHook("https://example.com/test", "US-1", 37)
	if url != "https://example.com/test" || name != "US-1" || delay != 37 {
		t.Fatalf("hook received invalid probe: %q %q %d", url, name, delay)
	}
	SetURLTestHook(nil)
	notifyURLTestHook("https://example.org", "ignored", 0)
	if name != "US-1" {
		t.Fatal("disabled hook was invoked")
	}
}
