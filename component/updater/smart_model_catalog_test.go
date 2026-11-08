package updater

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSmartModelRejectsUnknownPresets(t *testing.T) {
	for _, name := range []string{"", "../../evil", "gigantic", "GGUF"} {
		if err := InstallSmartModelPreset(name); err == nil {
			t.Fatalf("invalid preset %q accepted", name)
		}
	}
}

func TestSmartModelRejectsInvalidFile(t *testing.T) {
	if err := verifySmartModel(filepath.Join(t.TempDir(), "missing.bin")); err == nil {
		t.Fatal("missing model accepted")
	}
	path := filepath.Join(t.TempDir(), "fake.gguf")
	if err := os.WriteFile(path, []byte("GGUF"), 0600); err != nil { t.Fatal(err) }
	if err := verifySmartModel(path); err == nil {
		t.Fatal("non-LightGBM model accepted")
	}
}

func TestSmartPresetsHaveDistinctValidDigests(t *testing.T) {
	mid := smartModelPresets["middle"]
	large := smartModelPresets["large"]
	if mid.Size >= large.Size || mid.URL == large.URL ||
		mid.SHA256 == large.SHA256 || len(mid.SHA256) != 64 || len(large.SHA256) != 64 {
		t.Fatal("Smart model catalog has inconsistent sizes or pinned digests")
	}
}
