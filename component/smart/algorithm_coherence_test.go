package smart

import (
	"math"
	"testing"
)

func TestBanditMalformedTelemetryNeverPoisonsStudent(t *testing.T) {
	input := testBanditInput()
	input.MaxdownloadRate = math.NaN()
	input.HistoryMaxDownloadRate = math.Inf(1)
	input.LossRate = math.NaN()
	x := OnlineBanditFeatures(input)
	for i, v := range x {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < -1 || v > 1 {
			t.Fatalf("invalid feature %d: %v", i, v)
		}
	}
	state := defaultOnlineBanditState()
	state.Theta[2] = math.NaN()
	state.Precision[4] = math.Inf(1)
	for i := 0; i < 32; i++ {
		weight, u := state.Update(0.8, 0.75, x, 4)
		if math.IsNaN(weight) || math.IsInf(weight, 0) || weight <= 0 ||
			math.IsNaN(u) || math.IsInf(u, 0) {
			t.Fatalf("student became invalid: weight=%v uncertainty=%v", weight, u)
		}
	}
	for i := range state.Theta {
		if math.IsNaN(state.Theta[i]) || math.IsInf(state.Theta[i], 0) ||
			math.IsNaN(state.Precision[i]) || math.IsInf(state.Precision[i], 0) {
			t.Fatalf("state poisoned at feature %d", i)
		}
	}
}

func TestBanditSampleWeightAgesConfidenceByRepresentedObservations(t *testing.T) {
	state := defaultOnlineBanditState()
	state.Precision[0] = 100
	x := [OnlineBanditDimension]float64{1}
	state.Update(0.8, 0.8, x, 4)
	want := 100*math.Pow(0.995, 4) + 4
	if math.Abs(state.Precision[0]-want) > 1e-8 {
		t.Fatalf("confidence after weighted samples=%v want=%v", state.Precision[0], want)
	}
}

func TestDistillHoldoutDoesNotLeakRepeatedFeatureVectors(t *testing.T) {
	input := *expertTestInput(sceneStreaming, false)
	samples := make([]DistillTrainingSample, 50)
	for i := range samples {
		samples[i] = DistillTrainingSample{Input: input, TargetWeight: 0.8}
	}
	stats, holdout := BuildDistillSufficientStats(samples, 8, "test")
	bucket := DistilledExpertBucket(&input)
	if !((stats.BucketTrain[bucket] == 50 && len(holdout) == 0) ||
		(stats.BucketTrain[bucket] == 0 && len(holdout) == 50)) {
		t.Fatalf("identical features leaked across splits: train=%d holdout=%d",
			stats.BucketTrain[bucket], len(holdout))
	}
}

func TestDistillHoldoutIndependentOfLoggingOrder(t *testing.T) {
	samples, _ := syntheticDistillSamples()
	a, ha := BuildDistillSufficientStats(samples, 2, "same")
	for i, j := 0, len(samples)-1; i < j; i, j = i+1, j-1 {
		samples[i], samples[j] = samples[j], samples[i]
	}
	b, hb := BuildDistillSufficientStats(samples, 2, "same")
	if len(ha) != len(hb) {
		t.Fatalf("holdout changed after reordering: %d vs %d", len(ha), len(hb))
	}
	for bucket := 0; bucket < 8; bucket++ {
		if a.BucketTrain[bucket] != b.BucketTrain[bucket] ||
			a.BucketHoldout[bucket] != b.BucketHoldout[bucket] {
			t.Fatalf("bucket %d split changed with logging order", bucket)
		}
		for i := 0; i < ExpertFeatureDimension; i++ {
			if math.Abs(a.XTY[bucket][i]-b.XTY[bucket][i]) > 1e-8 {
				t.Fatalf("bucket %d coefficient summary shifted with order", bucket)
			}
		}
	}
	if len(ha) < 100 {
		t.Fatalf("synthetic holdout unexpectedly small: %d", len(ha))
	}
}

func TestDistillQualityGateRejectsNonfiniteMetrics(t *testing.T) {
	good := DistillMetrics{
		HoldoutSamples: 100, MeanAbsoluteError: 0.01, RMSE: 0.02,
		P95AbsoluteError: 0.05, MeanRelativeError: 0.02, RankingAgreement: 0.95,
	}
	if err := ValidateDistillQuality(good, DefaultDistillQualityPolicy()); err != nil {
		t.Fatalf("valid metrics rejected: %v", err)
	}
	bad := good
	bad.RankingAgreement = math.NaN()
	if ValidateDistillQuality(bad, DefaultDistillQualityPolicy()) == nil {
		t.Fatal("NaN ranking agreement escaped gate")
	}
	bad = good
	bad.RMSE = math.Inf(1)
	if ValidateDistillQuality(bad, DefaultDistillQualityPolicy()) == nil {
		t.Fatal("infinite RMSE escaped gate")
	}
}
