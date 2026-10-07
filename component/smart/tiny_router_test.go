package smart

import (
	"math"
	"testing"
)

func healthyTinyInput() *ModelInput {
	return &ModelInput{
		Success:                40,
		Failure:                1,
		ConnectTime:            55,
		Latency:                70,
		MaxdownloadRate:        4096,
		HistoryMaxDownloadRate: 3000,
		LossRate:               0.001,
		EmaLossRate:            0.002,
		ConnectionDuration:     1.5,
	}
}

func TestTinyRouterColdStateIsNeutral(t *testing.T) {
	state := defaultTinyRouterState()
	x := TinyRouterFeatures(healthyTinyInput(), TinyRouterTransport{
		RTTVarMs: 8, Unacked: 2, Cwnd: 64,
	})
	const prior = 0.82
	got, uncertainty := state.Predict(prior, x)
	if math.Abs(got-prior) > 1e-12 {
		t.Fatalf("cold router changed prior: got=%v want=%v", got, prior)
	}
	if uncertainty != 1 {
		t.Fatalf("cold uncertainty=%v want=1", uncertainty)
	}
}

func TestTinyRouterLearnsNonlinearGoodPathResidual(t *testing.T) {
	state := defaultTinyRouterState()
	x := TinyRouterFeatures(healthyTinyInput(), TinyRouterTransport{
		RTTVarMs: 6, Unacked: 1, Cwnd: 80,
	})
	const prior = 0.70

	before, _ := state.Predict(prior, x)
	for i := 0; i < 32; i++ {
		state.Update(prior, 0.98, x, 1)
	}
	after, uncertainty := state.Predict(prior, x)

	if after <= before {
		t.Fatalf("good completed flows did not raise route score: before=%v after=%v", before, after)
	}
	if after > prior*math.Exp(0.241) {
		t.Fatalf("router escaped residual bound: prior=%v after=%v", prior, after)
	}
	if uncertainty >= 0.7 {
		t.Fatalf("training did not reduce uncertainty enough: %v", uncertainty)
	}
}

func TestTinyRouterLearnsFailureResidualDown(t *testing.T) {
	state := defaultTinyRouterState()
	input := healthyTinyInput()
	input.ConnectionFailed = true
	input.LossRate = 0.20
	input.EmaLossRate = 0.10
	x := TinyRouterFeatures(input, TinyRouterTransport{
		RTTVarMs: 220, Unacked: 48, Lost: 8, Cwnd: 32,
	})
	const prior = 0.80

	for i := 0; i < 24; i++ {
		state.Update(prior, 0.05, x, 1)
	}
	after, _ := state.Predict(prior, x)
	if after >= prior {
		t.Fatalf("failed/congested path was not penalized: prior=%v after=%v", prior, after)
	}
}

func TestTinyRouterEpochChangeSoftensLearnedBias(t *testing.T) {
	state := defaultTinyRouterState()
	state.Generation = processTinyRouterGeneration
	state.Epoch = 7
	state.Updates = 64
	state.Bias = 0.20
	state.Output = [TinyRouterHiddenDimension]float64{0.30, -0.20, 0.15, 0.10}
	state.Uncertainty = 0.1

	beforeMagnitude := math.Abs(state.Bias)
	for _, v := range state.Output {
		beforeMagnitude += math.Abs(v)
	}
	state.ObserveEpoch(8)
	afterMagnitude := math.Abs(state.Bias)
	for _, v := range state.Output {
		afterMagnitude += math.Abs(v)
	}

	if afterMagnitude >= beforeMagnitude*0.20 {
		t.Fatalf("epoch change kept too much stale nonlinear bias: before=%v after=%v", beforeMagnitude, afterMagnitude)
	}
	if state.Uncertainty != 1 {
		t.Fatalf("epoch change uncertainty=%v want=1", state.Uncertainty)
	}
	if state.Updates > 4 {
		t.Fatalf("epoch change kept stale sample age: updates=%v", state.Updates)
	}
	if uint64(state.Epoch) != 8 {
		t.Fatalf("epoch=%v want=8", state.Epoch)
	}
}

func TestTinyRouterTransportFeaturesAreBounded(t *testing.T) {
	input := healthyTinyInput()
	x := TinyRouterFeatures(input, TinyRouterTransport{
		RTTVarMs: 5000,
		Unacked:  1_000_000,
		Lost:     1_000_000,
		Cwnd:     1,
	})
	for i, value := range x {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			t.Fatalf("feature[%d]=%v outside 0..1", i, value)
		}
	}
}


func BenchmarkTinyRouterPredict(b *testing.B) {
	state := defaultTinyRouterState()
	state.Updates = 64
	state.Output = [TinyRouterHiddenDimension]float64{0.05, -0.04, 0.03, 0.02}
	x := TinyRouterFeatures(healthyTinyInput(), TinyRouterTransport{
		RTTVarMs: 8, Unacked: 2, Cwnd: 64,
	})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = state.Predict(0.82, x)
	}
}

func BenchmarkTinyRouterUpdate(b *testing.B) {
	x := TinyRouterFeatures(healthyTinyInput(), TinyRouterTransport{
		RTTVarMs: 8, Unacked: 2, Cwnd: 64,
	})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		state := defaultTinyRouterState()
		_, _ = state.Update(0.82, 0.90, x, 1)
	}
}
