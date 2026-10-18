package updater

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/metacubex/mihomo/common/atomic"
	"github.com/metacubex/mihomo/common/utils"
	"github.com/metacubex/mihomo/component/resource"
	"github.com/metacubex/mihomo/component/smart/lightgbm"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/log"

	"github.com/vernesong/leaves"
)

var (
	lgbmAutoUpdate     bool
	lgbmUpdateInterval int

	updatingLgbm atomic.Bool
)

func LgbmAutoUpdate() bool {
	return lgbmAutoUpdate
}

func LgbmUpdateInterval() int {
	return lgbmUpdateInterval
}


func SetLgbmAutoUpdate(newAutoUpdate bool) {
	lgbmAutoUpdate = newAutoUpdate
}

func SetLgbmUpdateInterval(newUpdateInterval int) {
	lgbmUpdateInterval = newUpdateInterval
}


func UpdateLgbmModel() (err error) {
	modelUrl := lightgbm.LgbmUrl()
	if modelUrl == "" {
		modelUrl = lightgbm.GetModelDownloadURL()
	}

	vehicle := resource.NewHTTPVehicle(modelUrl, C.Path.SmartModel(), "", nil, defaultHttpTimeout, 0)
	var oldHash utils.HashType
	if buf, err := os.ReadFile(vehicle.Path()); err == nil {
		oldHash = utils.MakeHash(buf)
	}
	data, hash, err := vehicle.Read(context.Background(), oldHash)
	if err != nil {
		return fmt.Errorf("can't download LightGBM model file: %w", err)
	}
	if oldHash.Equal(hash) {
		log.Infoln("[Smart] LightGBM model is up to date")
		return nil
	}
	if len(data) == 0 {
		return fmt.Errorf("can't download LightGBM model file: no data")
	}

	tmpFile, err := os.CreateTemp("", "lgbm_model_*.bin")
	if err != nil {
		return fmt.Errorf("failed to create temp file for validation: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	_, err = leaves.LGEnsembleFromFile(tmpPath, false)
	if err != nil {
		return fmt.Errorf("invalid LightGBM model file: %s", err)
	}

	if err = vehicle.Write(data); err != nil {
		return fmt.Errorf("can't save LightGBM model file: %w", err)
	}

	defer func() {
		log.Infoln("[Smart] LightGBM model update completed")
		lightgbm.ReloadModel()
	}()

	return nil
}

func updateLgbmModel() error {
	// Do not force a stop-the-world GC after a model refresh. The temporary
	// download/validation buffers become ordinary garbage and Go can reclaim
	// them with normal heap pacing instead of creating a predictable CPU/power
	// spike immediately after network I/O.
	return UpdateLgbmModel()
}

var ErrGetLgbmModelUpdateSkip = errors.New("LightGBM model is updating, skip")

func UpdateLgbmModelDatabase() error {
	if !updatingLgbm.CompareAndSwap(false, true) {
		return ErrGetLgbmModelUpdateSkip
	}
	defer updatingLgbm.Store(false)

	log.Infoln("[Smart] Start updating LightGBM model")

	if err := updateLgbmModel(); err != nil {
		log.Errorln("[Smart] update LightGBM model error: %s", err.Error())
		return err
	}

	return nil
}

func getLgbmModelUpdateTime() (time time.Time, err error) {
	fileInfo, err := os.Stat(C.Path.SmartModel())
	if err == nil {
		return fileInfo.ModTime(), nil
	}
	return
}

func RegisterLgbmUpdater() {
	if lgbmUpdateInterval <= 0 {
		log.Errorln("[Smart] Invalid update interval: %d", lgbmUpdateInterval)
		return
	}

	go func() {
		ticker := time.NewTicker(time.Duration(lgbmUpdateInterval) * time.Hour)
		defer ticker.Stop()

		lastUpdate, err := getLgbmModelUpdateTime()
		if err != nil {
			log.Errorln("[Smart] Get LightGBM model update time error: %s", err.Error())
			return
		}

		log.Infoln("[Smart] last update time %s", lastUpdate)
		if lastUpdate.Add(time.Duration(lgbmUpdateInterval) * time.Hour).Before(time.Now()) {
			log.Infoln("[Smart] Model has not been updated for %v, update now", time.Duration(lgbmUpdateInterval)*time.Hour)
			if err := UpdateLgbmModelDatabase(); err != nil {
				log.Errorln("[Smart] Failed to update LightGBM model: %s", err.Error())
				return
			}
		}

		for range ticker.C {
			log.Infoln("[Smart] updating model every %d hours", lgbmUpdateInterval)
			if err := UpdateLgbmModelDatabase(); err != nil {
				log.Errorln("[Smart] Failed to update LightGBM model: %s", err.Error())
			}
		}
	}()
}

// RestoreBundledLiteModel activates the immutable, SHA-256-verified Lite
// baseline without touching its fallback copy. The recovery is crash-safe:
// the existing active model is kept until the new model is validated.
func RestoreBundledLiteModel() error {
	source := lightgbm.BundledLitePath()
	if err := lightgbm.VerifyBundledLite(source); err != nil {
		return fmt.Errorf("offline Lite fallback unavailable: %w", err)
	}
	if _, err := leaves.LGEnsembleFromFile(source, false); err != nil {
		return fmt.Errorf("Lite fallback cannot be parsed: %w", err)
	}

	dest := C.Path.SmartModel()
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	contents, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".lite-restore-*.bin")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(contents); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := lightgbm.VerifyBundledLite(tmpName); err != nil {
		return err
	}

	backup := dest + ".before-lite-restore"
	// Renaming an occupied destination is not portable on Windows. Move the
	// previous active model aside first, and roll back if activation fails.
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	hadOld := false
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, backup); err != nil {
			return fmt.Errorf("cannot safeguard previous active model: %w", err)
		}
		hadOld = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		if hadOld {
			if restoreErr := os.Rename(backup, dest); restoreErr != nil {
				return fmt.Errorf("Lite activation failed: %v; rollback failed: %w", err, restoreErr)
			}
		}
		return fmt.Errorf("Lite activation failed: %w", err)
	}
	if hadOld {
		_ = os.Remove(backup)
	}
	lightgbm.ReloadModel()
	return nil
}
