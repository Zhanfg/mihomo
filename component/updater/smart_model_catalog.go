package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	mihomoHttp "github.com/metacubex/mihomo/component/http"
	"github.com/metacubex/mihomo/component/smart/lightgbm"
	C "github.com/metacubex/mihomo/constant"
	"github.com/vernesong/leaves"
)

// Smart model presets are independently verified release assets. An installed
// preset is cached locally, so switching back does not re-download it.
type SmartModelPreset struct {
	Name string
	URL string
	SHA256 string
	Size int64
}

var smartModelPresets = map[string]SmartModelPreset{
	"middle": {
		Name: "middle",
		URL: "https://github.com/vernesong/mihomo/releases/download/LightGBM-Model/Model-middle.bin",
		SHA256: "6a17dffbb29862135c9e3706eabf60488862f578ef9edf60a5d325529873cfaa",
		Size: 18357364,
	},
	"large": {
		Name: "large",
		URL: "https://github.com/vernesong/mihomo/releases/download/LightGBM-Model/Model-large.bin",
		SHA256: "832920df1290454710dd04aa4c7d4b5324edfd7816a7c9ad2272708b5d5a13a8",
		Size: 26787747,
	},
}

const maxCustomSmartModelSize = 256 << 20

func smartModelCacheDir() string {
	return filepath.Join(filepath.Dir(C.Path.SmartModel()), "smart-models")
}

// verifySmartModel accepts compatible LightGBM ensembles, not LLM/GGUF
// weights or any random file with a .bin extension.
func verifySmartModel(path string) error {
	stat, err := os.Stat(path)
	if err != nil { return err }
	if stat.Size() < 1024 || stat.Size() > maxCustomSmartModelSize {
		return fmt.Errorf("Smart model size %d is outside accepted range", stat.Size())
	}
	f, err := os.Open(path)
	if err != nil { return err }
	defer f.Close()
	header := make([]byte, 4096)
	n, err := f.Read(header)
	if err != nil && err != io.EOF { return err }
	// The V11 Smart scorer expects a 30-feature LightGBM model. More trees
	// and bigger files are supported; changing feature schema is not.
	if !strings.Contains(string(header[:n]), "max_feature_idx=29") {
		return errors.New("model has an unsupported feature schema (expected 30 LightGBM features)")
	}
	if _, err := leaves.LGEnsembleFromFile(path, false); err != nil {
		return fmt.Errorf("incompatible or damaged LightGBM model: %w", err)
	}
	if err := lightgbm.ValidateModelFeatureSchema(path); err != nil {
		return err
	}
	return nil
}

func activateSmartModel(source string) error {
	if err := verifySmartModel(source); err != nil { return err }
	dst := C.Path.SmartModel()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil { return err }
	temp, err := os.CreateTemp(filepath.Dir(dst), ".smart-model-*.tmp")
	if err != nil { return err }
	tmpPath := temp.Name()
	defer os.Remove(tmpPath)
	in, err := os.Open(source)
	if err != nil { temp.Close(); return err }
	_, err = io.Copy(temp, in)
	closeSourceErr := in.Close()
	if err == nil { err = closeSourceErr }
	if err == nil { err = temp.Sync() }
	closeErr := temp.Close()
	if err != nil { return err }
	if closeErr != nil { return closeErr }
	if err := verifySmartModel(tmpPath); err != nil { return err }

	backup := dst + ".previous-model"
	// A failed or interrupted switch must retain the previously running
	// model. Keep the backup until the candidate is fully promoted.
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) { return err }
	hadActive := false
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, backup); err != nil { return err }
		hadActive = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		if hadActive {
			if rollbackErr := os.Rename(backup, dst); rollbackErr != nil {
				return fmt.Errorf("activation failed: %v; rollback failed: %w", err, rollbackErr)
			}
		}
		return err
	}
	if hadActive { _ = os.Remove(backup) }
	lightgbm.ReloadModel()
	return nil
}

func verifyPresetFile(path string, model SmartModelPreset) error {
	st, err := os.Stat(path)
	if err != nil { return err }
	if st.Size() != model.Size { return fmt.Errorf("unexpected %s size: %d", model.Name, st.Size()) }
	f, err := os.Open(path)
	if err != nil { return err }
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil { return err }
	if hex.EncodeToString(hash.Sum(nil)) != model.SHA256 {
		return fmt.Errorf("unexpected %s SHA256", model.Name)
	}
	return verifySmartModel(path)
}

func fetchSmartPreset(ctx context.Context, model SmartModelPreset, destination string) error {
	response, err := mihomoHttp.HttpRequest(ctx, model.URL, http.MethodGet, nil, nil)
	if err != nil { return err }
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK { return fmt.Errorf("model HTTP %d", response.StatusCode) }
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil { return err }
	f, err := os.CreateTemp(filepath.Dir(destination), ".smart-preset-*.bin")
	if err != nil { return err }
	tempPath := f.Name()
	defer os.Remove(tempPath)
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(response.Body, model.Size+1))
	if copyErr == nil { copyErr = f.Sync() }
	closeErr := f.Close()
	if copyErr != nil { return copyErr }
	if closeErr != nil { return closeErr }
	if n != model.Size || hex.EncodeToString(hash.Sum(nil)) != model.SHA256 {
		return errors.New("model download failed pinned size/SHA256 validation")
	}
	if err := verifySmartModel(tempPath); err != nil { return err }
	// The cached file is separate from the active model and the Lite fallback.
	if err := os.Remove(destination); err != nil && !os.IsNotExist(err) { return err }
	return os.Rename(tempPath, destination)
}

// InstallSmartModelPreset switches between independently validated model
// sizes. The one bundled Lite copy always remains available offline.
func InstallSmartModelPreset(name string) error {
	if name == "lite" { return RestoreBundledLiteModel() }
	preset, ok := smartModelPresets[name]
	if !ok { return fmt.Errorf("unknown Smart model preset: %q", name) }
	if !updatingLgbm.CompareAndSwap(false, true) { return ErrGetLgbmModelUpdateSkip }
	defer updatingLgbm.Store(false)
	cache := filepath.Join(smartModelCacheDir(), "Model-"+preset.Name+".bin")
	if verifyPresetFile(cache, preset) != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		if err := fetchSmartPreset(ctx, preset, cache); err != nil { return err }
	}
	return activateSmartModel(cache)
}

// ImportSmartModelFile accepts user-selected local LightGBM models with the
// V11 30-feature schema. Third-party files are validated before activation;
// they are not silently treated as executable plugins or LLM weights.
func ImportSmartModelFile(path string) error {
	if path == "" { return errors.New("no model selected") }
	if !updatingLgbm.CompareAndSwap(false, true) { return ErrGetLgbmModelUpdateSkip }
	defer updatingLgbm.Store(false)
	return activateSmartModel(path)
}
