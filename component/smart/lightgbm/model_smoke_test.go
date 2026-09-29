package lightgbm

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vernesong/leaves"
	"github.com/metacubex/mihomo/component/smart"
)

func externalModelInput() *smart.ModelInput {
	return &smart.ModelInput{
		Success:                   128,
		Failure:                   3,
		ConnectTime:               95,
		Latency:                   120,
		UploadTotal:               1.5,
		HistoryUploadTotal:        32,
		MaxuploadRate:             850,
		HistoryMaxUploadRate:      1200,
		DownloadTotal:             12,
		HistoryDownloadTotal:      240,
		MaxdownloadRate:           4200,
		HistoryMaxDownloadRate:    5200,
		ConnectionDuration:        3.5,
		HistoryConnectionDuration: 12,
		LastUsed:                  1,
		IsTCP:                     true,
		LossRate:                  0.002,
		CumulLossRate:             0.005,
		EmaLossRate:               0.003,
		DestIPASN:                 "AS13335 Cloudflare",
		Host:                      "example.com",
		DestIP:                    "1.1.1.1",
		DestPort:                  443,
		DestGeoIP:                 []string{"US"},
		GroupName:                 "smoke",
		NodeName:                  "node",
	}
}

func loadExternalModel(tb testing.TB) *WeightModel {
	tb.Helper()
	path := os.Getenv("MIHOMO_SMART_MODEL")
	if path == "" {
		tb.Skip("MIHOMO_SMART_MODEL is not set")
	}
	m := &WeightModel{}
	if err := m.loadModel(path); err != nil {
		tb.Fatalf("load external model: %v", err)
	}
	return m
}

func TestCurrentModelSmoke(t *testing.T) {
	m := loadExternalModel(t)
	weight, predicted := m.PredictWeight(externalModelInput(), 1)
	if !predicted {
		t.Fatal("loaded current model fell back to heuristic scorer")
	}
	if weight <= 0 || math.IsNaN(weight) || math.IsInf(weight, 0) {
		t.Fatalf("invalid model prediction: %v", weight)
	}
}

func BenchmarkCurrentModelPredict(b *testing.B) {
	m := loadExternalModel(b)
	input := externalModelInput()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		weight, predicted := m.PredictWeight(input, 1)
		if !predicted || weight <= 0 {
			b.Fatalf("prediction failed: predicted=%v weight=%v", predicted, weight)
		}
	}
}


func TestReloadModelKeepsUnusedModelLazy(t *testing.T) {
	previous := smartModel
	smartModel = &WeightModel{}
	t.Cleanup(func() { smartModel = previous })

	ReloadModel()

	smartModel.mutex.RLock()
	loaded := smartModel.model != nil
	smartModel.mutex.RUnlock()
	if loaded {
		t.Fatal("reload made an unused model resident")
	}
}


func TestIdleModelReleaseKeepsOnlyResidualState(t *testing.T) {
	now := time.Now()
	m := &WeightModel{
		model:              &leaves.Ensemble{},
		transforms:         &FeatureTransforms{},
		featuresCompatible: true,
	}
	m.lastUse.Store(now.Add(-modelIdleTTL - time.Second).UnixNano())

	m.mutex.Lock()
	released, next := m.releaseIfIdleLocked(now)
	modelNil := m.model == nil
	transformsNil := m.transforms == nil
	compatible := m.featuresCompatible
	m.mutex.Unlock()

	if !released || next != 0 {
		t.Fatalf("idle model release = (%v,%v), want (true,0)", released, next)
	}
	if !modelNil || !transformsNil || compatible {
		t.Fatal("idle release retained parsed ensemble state")
	}
}

func TestActiveModelLeaseStaysResident(t *testing.T) {
	now := time.Now()
	m := &WeightModel{model: &leaves.Ensemble{}}
	m.lastUse.Store(now.Add(-time.Minute).UnixNano())

	m.mutex.Lock()
	released, next := m.releaseIfIdleLocked(now)
	stillLoaded := m.model != nil
	m.mutex.Unlock()

	if released || !stillLoaded {
		t.Fatal("active model was released before idle TTL")
	}
	if next <= 0 || next > modelIdleTTL {
		t.Fatalf("unexpected next idle check: %v", next)
	}
}


func TestLoadModelRejectsOversizeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversize-model.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxModelFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	m := &WeightModel{}
	if err := m.loadModel(path); err == nil {
		t.Fatal("oversize LightGBM model was accepted")
	}
}
