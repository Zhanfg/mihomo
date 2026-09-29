package lightgbm

import (
	"math"
	"os"
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


func BenchmarkDistilledCurrentModelDecision(b *testing.B) {
	m := loadExternalModel(b)
	input := externalModelInput()
	heuristic, ok := smart.CalculateWeight(input, 1)
	if !ok && heuristic <= 0 {
		b.Fatal("heuristic prior unavailable")
	}
	x := smart.OnlineBanditFeatures(input)
	state := smart.OnlineBanditState{
		Updates:        128,
		Uncertainty:    0.10,
		ErrorEWMA:      0.03,
		TeacherRatio:   1.02,
		TeacherAt:      128,
		TeacherProbeAt: 128,
	}
	for i := range state.Precision {
		state.Precision[i] = 32
	}

	teacherCalls := 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prior := state.ApplyTeacherAnchor(heuristic)
		if smart.ShouldRefreshTeacher(input, 0.03, state) {
			state.NoteTeacherAttempt()
			modelWeight, predicted := m.PredictWeight(input, 1)
			if predicted {
				state.ObserveTeacher(modelWeight, heuristic)
				prior = state.ApplyTeacherAnchor(heuristic)
				teacherCalls++
			}
		}
		weight, _ := state.Predict(prior, x)
		if weight <= 0 {
			b.Fatalf("distilled prediction failed: %v", weight)
		}
		state.Updates++
	}
	b.StopTimer()
	if b.N > 0 {
		b.ReportMetric(float64(teacherCalls)/float64(b.N), "teacher/decision")
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
