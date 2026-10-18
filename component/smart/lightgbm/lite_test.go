package lightgbm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundledLiteRejectsMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Model-Lite.bin")
	if err := os.WriteFile(path, []byte("not a LightGBM model"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyBundledLite(path); err == nil {
		t.Fatal("malformed Lite model was accepted")
	}
}

func TestBundledLiteRejectsWrongDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Model-Lite.bin")
	if err := os.WriteFile(path, []byte(strings.Repeat("X", BundledLiteBytes)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyBundledLite(path); err == nil {
		t.Fatal("untrusted Lite model of correct length was accepted")
	}
}
