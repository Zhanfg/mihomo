package smart

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func syntheticDistillSamples() ([]DistillTrainingSample, [8][ExpertFeatureDimension]float64) {
	teacher := CompileExpertDistillation()
	for bucket := range teacher {
		teacher[bucket][1] += 0.025
		teacher[bucket][3] -= 0.015
		teacher[bucket][5] += 0.010
	}

	samples := make([]DistillTrainingSample, 0, 960)
	for scene := sceneWeb; scene <= sceneTransfer; scene++ {
		for _, udp := range []bool{false, true} {
			for i := 0; i < 120; i++ {
				input := *expertTestInput(scene, udp)
				input.Success += int64(i % 17)
				input.Failure += int64(i % 3)
				input.ConnectTime += int64((i % 11) - 5)
				input.Latency += int64((i % 13) - 6)
				input.MaxdownloadRate *= 0.85 + float64(i%9)*0.04
				input.MaxuploadRate *= 0.90 + float64(i%7)*0.03
				input.LossRate += float64(i%5) * 0.0004
				input.EmaLossRate = input.LossRate * 0.9
				input.Host = "private-production.example"
				input.DestIP = "203.0.113.77"
				input.NodeName = "private-node"
				target := predictDistilledCoefficients(teacher, &input)
				samples = append(samples, DistillTrainingSample{Input: input, TargetWeight: target})
			}
		}
	}
	return samples, teacher
}

func TestDistillSnapshotRebuildsIdenticalModel(t *testing.T) {
	samples, _ := syntheticDistillSamples()
	stats, holdout := BuildDistillSufficientStats(samples, 2, "production:test")
	fromStats := FitDistilledExpertFromStats(stats)
	fromMemory := FitDistilledExpertModel(samples, 2)

	for bucket := 0; bucket < 8; bucket++ {
		for feature := 0; feature < ExpertFeatureDimension; feature++ {
			a := fromStats.Coefficients[bucket][feature]
			b := fromMemory.Coefficients[bucket][feature]
			if math.Abs(a-b) > 1e-12 {
				t.Fatalf("bucket=%d feature=%d snapshot=%v memory=%v", bucket, feature, a, b)
			}
		}
	}

	metrics := EvaluateDistilledExpertModel(fromStats.Coefficients, holdout)
	if metrics.HoldoutSamples < 100 {
		t.Fatalf("holdout too small: %d", metrics.HoldoutSamples)
	}
}

func TestDistillSnapshotContainsNoRawIdentity(t *testing.T) {
	samples, _ := syntheticDistillSamples()
	stats, _ := BuildDistillSufficientStats(samples, 8, "production:test")
	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, secret := range []string{"private-production.example", "203.0.113.77", "private-node"} {
		if strings.Contains(text, secret) {
			t.Fatalf("anonymous snapshot leaked %q", secret)
		}
	}
}

func TestDistillQualityGateAcceptsGoodSyntheticTeacher(t *testing.T) {
	samples, _ := syntheticDistillSamples()
	result := FitDistilledExpertModel(samples, 2)
	policy := DefaultDistillQualityPolicy()
	if err := ValidateDistillQuality(result.Metrics, policy); err != nil {
		t.Fatalf("good synthetic distillation rejected: %v metrics=%+v", err, result.Metrics)
	}
}

func TestDistillQualityGateRejectsBadModel(t *testing.T) {
	metrics := DistillMetrics{
		HoldoutSamples:    1000,
		MeanAbsoluteError: 0.40,
		RMSE:              0.50,
		P95AbsoluteError:  0.80,
		MeanRelativeError: 0.60,
		RankingAgreement:  0.40,
	}
	if err := ValidateDistillQuality(metrics, DefaultDistillQualityPolicy()); err == nil {
		t.Fatal("bad distillation unexpectedly passed quality gate")
	}
}

func TestInvalidDistilledArtifactFallsBackToAnalyticExperts(t *testing.T) {
	oldCoeff := distilledExpertCoefficients
	oldCalibration := distilledExpertCalibration
	oldManifest := distilledExpertManifest
	oldValid := distilledArtifactValid
	t.Cleanup(func() {
		distilledExpertCoefficients = oldCoeff
		distilledExpertCalibration = oldCalibration
		distilledExpertManifest = oldManifest
		distilledArtifactValid = oldValid
	})

	distilledExpertCoefficients[0][0] = math.NaN()
	distilledArtifactValid = ValidateDistilledArtifactValues(
		distilledExpertCoefficients,
		distilledExpertCalibration,
		distilledExpertManifest,
	) == nil
	if distilledArtifactValid {
		t.Fatal("corrupted artifact was marked valid")
	}

	input := expertTestInput(sceneWeb, false)
	got := DistilledExpertPrior(input, 1)
	want := math.Max(0.03, math.Min(1.20, dotExpert(analyticDistilledFallback[DistilledExpertBucket(input)], ExpertFeatures(input))))
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("fallback=%v want=%v", got, want)
	}
}

func TestDistilledArtifactManifestIsValid(t *testing.T) {
	if err := ValidateDistilledArtifactValues(
		distilledExpertCoefficients,
		distilledExpertCalibration,
		distilledExpertManifest,
	); err != nil {
		t.Fatalf("committed distilled artifact invalid: %v", err)
	}
	if !DistilledArtifactValid() {
		t.Fatal("runtime did not accept committed distilled artifact")
	}
}
