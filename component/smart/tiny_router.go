package smart

import (
	"math"
	"time"
)

const (
	TinyRouterInputDimension  = 8
	TinyRouterHiddenDimension = 4
)

// TinyRouterTransport contains transport evidence that is cheap to collect
// from an already-open TCP socket. UDP and unsupported platforms leave these
// fields at zero; the router never performs an active probe.
type TinyRouterTransport struct {
	RTTVarMs float64
	Unacked  uint32
	Lost     uint32
	Cwnd     uint32
}

type TinyRouterFeatureVector [TinyRouterInputDimension]float64

// The first layer is fixed. Only the four-unit output head is learned online,
// making this an ELM-style nonlinear residual rather than a resident model
// runtime or a back-propagation graph.
var tinyRouterHiddenWeights = [TinyRouterHiddenDimension][TinyRouterInputDimension]float64{
	{0.20, 0.95, 0.55, 0.35, 1.10, 0.10, 0.45, 0.80},
	{-0.10, 0.25, 1.15, 1.25, 0.20, 0.05, 0.75, 0.35},
	{-0.15, 0.30, 0.15, 0.20, 0.35, 1.30, 0.20, 0.20},
	{0.05, 0.55, 0.65, 0.50, 0.90, 0.45, 1.00, 1.10},
}

var tinyRouterHiddenBias = [TinyRouterHiddenDimension]float64{-2.00, -1.65, -0.90, -2.10}

var processTinyRouterGeneration = float64((uint64(time.Now().UnixNano()) & ((1 << 53) - 1)) + 1)

type TinyRouterState struct {
	Output      [TinyRouterHiddenDimension]float64 `json:"output"`
	Bias        float64                           `json:"bias,omitempty"`
	Updates     float64                           `json:"updates,omitempty"`
	ErrorEWMA   float64                           `json:"error_ewma,omitempty"`
	Epoch       float64                           `json:"epoch,omitempty"`
	Generation  float64                           `json:"generation,omitempty"`
	Uncertainty float64                           `json:"uncertainty,omitempty"`
}

func defaultTinyRouterState() TinyRouterState {
	return TinyRouterState{Uncertainty: 1}
}

func normalizeTinyRouterState(state TinyRouterState) TinyRouterState {
	for i := range state.Output {
		if math.IsNaN(state.Output[i]) || math.IsInf(state.Output[i], 0) {
			state.Output[i] = 0
		}
		state.Output[i] = math.Max(-0.45, math.Min(0.45, state.Output[i]))
	}
	if math.IsNaN(state.Bias) || math.IsInf(state.Bias, 0) {
		state.Bias = 0
	}
	state.Bias = math.Max(-0.25, math.Min(0.25, state.Bias))
	if state.Updates < 0 || math.IsNaN(state.Updates) || math.IsInf(state.Updates, 0) {
		state.Updates = 0
	}
	if state.ErrorEWMA < 0 || math.IsNaN(state.ErrorEWMA) || math.IsInf(state.ErrorEWMA, 0) {
		state.ErrorEWMA = 0
	}
	if state.Epoch < 0 || math.IsNaN(state.Epoch) || math.IsInf(state.Epoch, 0) {
		state.Epoch = 0
	}
	if state.Generation < 0 || math.IsNaN(state.Generation) || math.IsInf(state.Generation, 0) {
		state.Generation = 0
	}
	if state.Updates <= 0 {
		state.Uncertainty = 1
	} else {
		state.Uncertainty = clamp01(state.Uncertainty)
	}
	return state
}

func (state *TinyRouterState) softenForEnvironmentChange(keep float64) {
	if state == nil {
		return
	}
	keep = clamp01(keep)
	for i := range state.Output {
		state.Output[i] *= keep
	}
	state.Bias *= keep
	// A handover must reopen learning rate as well as confidence. Keeping a
	// large old sample age would make the tiny head adapt too slowly.
	state.Updates = math.Min(4, state.Updates*keep)
	if state.ErrorEWMA < 0.18 {
		state.ErrorEWMA = 0.18
	}
	state.Uncertainty = 1
}

func (state *TinyRouterState) ObserveEpoch(epoch uint64) {
	if state == nil || epoch == 0 {
		return
	}
	if state.Generation != processTinyRouterGeneration {
		if state.Updates > 0 {
			state.softenForEnvironmentChange(0.20)
		}
		state.Generation = processTinyRouterGeneration
		state.Epoch = float64(epoch)
		return
	}
	if state.Epoch == 0 {
		state.Epoch = float64(epoch)
		return
	}
	if uint64(state.Epoch) == epoch {
		return
	}
	// Wi-Fi/5G/hotspot handover can change NAT, MTU and upstream path.
	state.softenForEnvironmentChange(0.12)
	state.Epoch = float64(epoch)
}

func TinyRouterFeatures(input *ModelInput, transport TinyRouterTransport) TinyRouterFeatureVector {
	var x TinyRouterFeatureVector
	x[0] = 1
	if input == nil {
		x[1], x[2], x[3], x[4], x[5], x[6], x[7] = 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5
		return x
	}

	total := SampleCount(input.Success, input.Failure)
	if total > 0 {
		x[1] = clamp01(float64(input.Success) / float64(total))
	} else {
		x[1] = 0.5
	}
	x[2] = banditQualityFromDelay(input.ConnectTime)
	x[3] = banditQualityFromDelay(input.Latency)

	loss := math.Max(input.LossRate, input.EmaLossRate)
	if input.ConnectionFailed {
		x[4] = 0
	} else {
		x[4] = clamp01(math.Exp(-math.Max(0, loss) * 14))
	}

	// Unknown throughput is neutral, not bad.
	x[5] = 0.5
	currentRate := math.Max(input.MaxuploadRate, input.MaxdownloadRate)
	historyRate := math.Max(input.HistoryMaxUploadRate, input.HistoryMaxDownloadRate)
	if currentRate > 0 || historyRate > 0 {
		x[5] = 0.5 + 0.5*math.Tanh(math.Log((currentRate+1)/(historyRate+1)))
	}

	if transport.RTTVarMs > 0 {
		x[6] = clamp01(math.Exp(-transport.RTTVarMs / 120.0))
	} else {
		x[6] = 0.5
	}

	if transport.Cwnd > 0 {
		pressure := float64(transport.Unacked+transport.Lost) / float64(transport.Cwnd)
		x[7] = clamp01(math.Exp(-math.Max(0, pressure)))
	} else {
		x[7] = 0.5
	}
	return x
}

func tinyRouterHidden(x TinyRouterFeatureVector) [TinyRouterHiddenDimension]float64 {
	var hidden [TinyRouterHiddenDimension]float64
	for unit := 0; unit < TinyRouterHiddenDimension; unit++ {
		sum := tinyRouterHiddenBias[unit]
		for i := 0; i < TinyRouterInputDimension; i++ {
			sum += tinyRouterHiddenWeights[unit][i] * x[i]
		}
		hidden[unit] = math.Tanh(sum)
	}
	return hidden
}

func (state *TinyRouterState) Predict(prior float64, x TinyRouterFeatureVector) (weight, uncertainty float64) {
	if state == nil || prior <= 0 || math.IsNaN(prior) || math.IsInf(prior, 0) {
		return prior, 1
	}
	hidden := tinyRouterHidden(x)
	residual := state.Bias
	for i := 0; i < TinyRouterHiddenDimension; i++ {
		residual += state.Output[i] * hidden[i]
	}

	confidence := 1 - math.Exp(-state.Updates/12.0)
	residual *= confidence
	residual = math.Max(-0.24, math.Min(0.24, residual))
	weight = prior * math.Exp(residual)

	evidenceUncertainty := math.Exp(-state.Updates / 18.0)
	errorUncertainty := clamp01(state.ErrorEWMA * 2.5)
	uncertainty = clamp01(math.Max(evidenceUncertainty, errorUncertainty))
	return weight, uncertainty
}

// Update trains only the four-unit output head on completed-flow reward.
func (state *TinyRouterState) Update(
	prior, reward float64,
	x TinyRouterFeatureVector,
	sampleScale int64,
) (weight, uncertainty float64) {
	if state == nil || prior <= 0 || reward <= 0 ||
		math.IsNaN(prior) || math.IsNaN(reward) ||
		math.IsInf(prior, 0) || math.IsInf(reward, 0) {
		return state.Predict(prior, x)
	}
	if sampleScale < 1 {
		sampleScale = 1
	}
	if sampleScale > 4 {
		sampleScale = 4
	}
	scale := float64(sampleScale)

	predicted, _ := state.Predict(prior, x)
	targetResidual := math.Log(reward / prior)
	targetResidual = math.Max(-0.50, math.Min(0.50, targetResidual))
	predictedResidual := math.Log(math.Max(1e-9, predicted) / prior)
	err := targetResidual - predictedResidual
	robustErr := math.Max(-0.28, math.Min(0.28, err))

	instantError := math.Min(1, math.Abs(err))
	if state.ErrorEWMA == 0 {
		state.ErrorEWMA = instantError
	} else {
		state.ErrorEWMA = state.ErrorEWMA*0.90 + instantError*0.10
	}

	hidden := tinyRouterHidden(x)
	lr := 0.045 / math.Sqrt(1+state.Updates/24.0)
	if state.ErrorEWMA >= 0.22 {
		lr *= 1.35
	}
	lr *= scale

	const decay = 0.999
	state.Bias = math.Max(-0.25, math.Min(0.25, state.Bias*decay+lr*robustErr*0.35))
	for i := 0; i < TinyRouterHiddenDimension; i++ {
		state.Output[i] = state.Output[i]*decay + lr*robustErr*hidden[i]
		state.Output[i] = math.Max(-0.45, math.Min(0.45, state.Output[i]))
	}
	state.Updates += scale

	weight, uncertainty = state.Predict(prior, x)
	state.Uncertainty = uncertainty
	return weight, uncertainty
}
