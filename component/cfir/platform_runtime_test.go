package cfir

import (
	"runtime"
	"testing"
)

func TestRuntimePlatformMatchesGOOSMapping(t *testing.T) {
	got, err := RuntimePlatform()
	if err != nil {
		t.Fatal(err)
	}
	want, err := PlatformFromGOOS(runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("runtime platform=%v want=%v", got, want)
	}
}
