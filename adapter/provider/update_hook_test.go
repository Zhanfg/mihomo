package provider

import "testing"

func TestProviderUpdateHookLifecycle(t *testing.T) {
	defer SetProviderUpdateHook(nil)
	var last string
	SetProviderUpdateHook(func(name string) { last = name })
	notifyProviderUpdate("sample")
	if last != "sample" {
		t.Fatalf("expected updated provider, got %q", last)
	}
	SetProviderUpdateHook(nil)
	notifyProviderUpdate("ignored")
	if last != "sample" {
		t.Fatal("disabled hook was invoked")
	}
}
