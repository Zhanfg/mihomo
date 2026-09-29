package smart

import (
	"math"
	"testing"

	"github.com/metacubex/mihomo/common/lru"
)

func testBanditInput() *ModelInput {
	return &ModelInput{
		Success: 40, Failure: 2,
		ConnectTime: 80, Latency: 120,
		MaxuploadRate: 512, MaxdownloadRate: 4096,
		HistoryMaxUploadRate: 384, HistoryMaxDownloadRate: 3000,
		LossRate: 0.002, EmaLossRate: 0.004,
	}
}

func TestOnlineBanditLearnsPositiveResidual(t *testing.T) {
	input := testBanditInput()
	x := OnlineBanditFeatures(input)
	state := OnlineBanditState{}
	for i := range state.Precision {
		state.Precision[i] = 1
	}

	prior := 0.65
	before, beforeU := state.Predict(prior, x)
	for i := 0; i < 80; i++ {
		state.Update(prior, 0.95, x, 1)
	}
	after, afterU := state.Predict(prior, x)
	if after <= before {
		t.Fatalf("student did not learn positive residual: before=%v after=%v", before, after)
	}
	if afterU >= beforeU {
		t.Fatalf("uncertainty did not fall with evidence: before=%v after=%v", beforeU, afterU)
	}
	if after > prior*1.43 {
		t.Fatalf("student escaped residual safety bound: %v", after)
	}
}

func TestOnlineBanditLearnsNegativeResidual(t *testing.T) {
	input := testBanditInput()
	input.ConnectionFailed = true
	input.Failure += 8
	x := OnlineBanditFeatures(input)
	state := OnlineBanditState{}
	for i := range state.Precision {
		state.Precision[i] = 1
	}

	prior := 0.85
	for i := 0; i < 80; i++ {
		state.Update(prior, 0.15, x, 1)
	}
	after, _ := state.Predict(prior, x)
	if after >= prior {
		t.Fatalf("student did not suppress persistently bad path: prior=%v after=%v", prior, after)
	}
	if after < prior*0.70 {
		t.Fatalf("student escaped negative residual safety bound: %v", after)
	}
}

func TestObservedRewardDoesNotDependOnPrediction(t *testing.T) {
	input := testBanditInput()
	a := ObserveConnectionReward(input, 1)
	b := ObserveConnectionReward(input, 1)
	if a != b || a <= 0 {
		t.Fatalf("reward must be deterministic connection truth: a=%v b=%v", a, b)
	}

	input.ConnectionFailed = true
	failed := ObserveConnectionReward(input, 1)
	if failed >= a {
		t.Fatalf("failed connection reward=%v should be below healthy=%v", failed, a)
	}
}

func TestOnlineBanditForgetsConfidenceUnderPersistentError(t *testing.T) {
	input := testBanditInput()
	x := OnlineBanditFeatures(input)
	state := OnlineBanditState{}
	for i := range state.Precision {
		state.Precision[i] = 1
	}

	for i := 0; i < 120; i++ {
		state.Update(0.7, 0.72, x, 1)
	}
	_, stableU := state.Predict(0.7, x)

	// Abrupt environment change: persistent large residual increases model
	// error and activates stronger confidence forgetting.
	for i := 0; i < 24; i++ {
		state.Update(0.7, 0.20, x, 1)
	}
	_, driftU := state.Predict(0.7, x)
	if state.ErrorEWMA < 0.15 {
		t.Fatalf("drift was not detected: error=%v", state.ErrorEWMA)
	}
	if driftU < stableU {
		t.Fatalf("confidence should not keep tightening through drift: stable=%v drift=%v", stableU, driftU)
	}
}

func TestExplorationBonusBounded(t *testing.T) {
	if got := ExplorationBonus(1, 1, 1); got > 1.0800001 {
		t.Fatalf("exploration exceeded 8%% cap: %v", got)
	}
	if got := ExplorationBonus(1, 1, 0); got != 1 {
		t.Fatalf("zero exploration changed score: %v", got)
	}
}

func BenchmarkOnlineBanditPredict(b *testing.B) {
	x := OnlineBanditFeatures(testBanditInput())
	state := OnlineBanditState{}
	for i := range state.Precision {
		state.Precision[i] = 8
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = state.Predict(0.8, x)
	}
}

func BenchmarkOnlineBanditUpdate(b *testing.B) {
	x := OnlineBanditFeatures(testBanditInput())
	state := OnlineBanditState{}
	for i := range state.Precision {
		state.Precision[i] = 8
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		state.Update(0.8, 0.82, x, 1)
	}
}


func TestOnlineBanditStatePersistsInStatsRecord(t *testing.T) {
	record := &AtomicStatsRecord{weights: lru.New[string, float64](lru.WithSize[string, float64](100))}
	state := OnlineBanditState{Updates: 17, ErrorEWMA: 0.12}
	for i := 0; i < OnlineBanditDimension; i++ {
		state.Theta[i] = float64(i+1) * 0.01
		state.Precision[i] = float64(i + 2)
	}
	SaveOnlineBanditState(record, false, state, 0.42)
	got := LoadOnlineBanditState(record, false)

	if math.Abs(got.Updates-state.Updates) > 1e-12 || math.Abs(got.ErrorEWMA-state.ErrorEWMA) > 1e-12 {
		t.Fatalf("scalar state mismatch: got=%+v want=%+v", got, state)
	}
	for i := 0; i < OnlineBanditDimension; i++ {
		if math.Abs(got.Theta[i]-state.Theta[i]) > 1e-12 || math.Abs(got.Precision[i]-state.Precision[i]) > 1e-12 {
			t.Fatalf("dimension %d mismatch: got=%+v want=%+v", i, got, state)
		}
	}
	if gotU := record.GetWeight(BanditUncertaintyWeightType(false)); math.Abs(gotU-0.42) > 1e-12 {
		t.Fatalf("uncertainty=%v want=0.42", gotU)
	}
}
