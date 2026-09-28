package lightgbm

import (
	"math"
	"os"
	"testing"

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
