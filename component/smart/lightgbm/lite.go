package lightgbm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	C "github.com/metacubex/mihomo/constant"
)

// Lite is vernesong/mihomo's smallest published LightGBM Model.bin.
// These values are pinned to the GitHub LightGBM-Model release asset. This is
// a genuine LightGBM ensemble, not the compiled analytic expert distillation.
const BundledLiteBytes = 9345218
const BundledLiteSHA256 = "23633022a815fb34e510befe1c176ba50af3ced48f08b172454f4cd6b8327788"

func BundledLitePath() string {
	return filepath.Join(filepath.Dir(C.Path.SmartModel()), "Model-Lite.bin")
}

// VerifyBundledLite prevents a tampered/corrupt offline fallback from becoming
// an active model. Updater also checks ensemble parseability before restoring.
func VerifyBundledLite(path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	if stat.Size() != BundledLiteBytes {
		return fmt.Errorf("Lite model size mismatch: got %d", stat.Size())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != BundledLiteSHA256 {
		return fmt.Errorf("Lite model SHA-256 mismatch")
	}
	return nil
}
