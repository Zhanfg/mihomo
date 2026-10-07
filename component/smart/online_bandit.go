package smart

import (
	"math"
	"time"
)

// OnlineBanditDimension is intentionally small: the global LightGBM teacher
// handles high-dimensional generalization, while this local student only needs
// enough capacity to learn device/network/target-specific residuals cheaply.
const OnlineBanditDimension = 6

const (
	WeightTypeBanditUncertaintyTCP = "bandit-u:tcp"
	WeightTypeBanditUncertaintyUDP = "bandit-u:udp"
)

// processBanditGeneration distinguishes one core lifetime from persisted state.
// netstate epochs are process-local and restart from 1, so epoch alone cannot
// tell whether confidence was learned on the current boot/network.
var processBanditGeneration = float64((uint64(time.Now().UnixNano()) & ((1 << 53) - 1)) + 1)

var (
	banditThetaTCP = [OnlineBanditDimension]string{
		"bandit-th:tcp:0", "bandit-th:tcp:1", "bandit-th:tcp:2",
		"bandit-th:tcp:3", "bandit-th:tcp:4", "bandit-th:tcp:5",
	}
	banditThetaUDP = [OnlineBanditDimension]string{
		"bandit-th:udp:0", "bandit-th:udp:1", "bandit-th:udp:2",
		"bandit-th:udp:3", "bandit-th:udp:4", "bandit-th:udp:5",
	}
	banditPrecisionTCP = [OnlineBanditDimension]string{
		"bandit-pr:tcp:0", "bandit-pr:tcp:1", "bandit-pr:tcp:2",
		"bandit-pr:tcp:3", "bandit-pr:tcp:4", "bandit-pr:tcp:5",
	}
	banditPrecisionUDP = [OnlineBanditDimension]string{
		"bandit-pr:udp:0", "bandit-pr:udp:1", "bandit-pr:udp:2",
		"bandit-pr:udp:3", "bandit-pr:udp:4", "bandit-pr:udp:5",
	}
)

func banditUpdatesKey(isUDP bool) string {
	if isUDP {
		return "bandit-n:udp"
	}
	return "bandit-n:tcp"
}

func banditErrorKey(isUDP bool) string {
	if isUDP {
		return "bandit-e:udp"
	}
	return "bandit-e:tcp"
}


func banditEpochKey(isUDP bool) string {
	if isUDP {
		return "bandit-epoch:udp"
	}
	return "bandit-epoch:tcp"
}

func banditGenerationKey(isUDP bool) string {
	if isUDP {
		return "bandit-gen:udp"
	}
	return "bandit-gen:tcp"
}

func BanditUncertaintyWeightType(isUDP bool) string {
	if isUDP {
		return WeightTypeBanditUncertaintyUDP
	}
	return WeightTypeBanditUncertaintyTCP
}

// OnlineBanditState is a diagonal online least-squares residual model. Keeping
// only the diagonal is deliberate: one update is O(d), persistence is tiny,
// and no matrix inversion or heap allocation is required on Android.
type OnlineBanditState struct {
	Theta       [OnlineBanditDimension]float64 `json:"theta"`
	Precision   [OnlineBanditDimension]float64 `json:"precision"`
	Updates     float64                        `json:"updates,omitempty"`
	ErrorEWMA   float64                        `json:"error_ewma,omitempty"`
	Epoch       float64                        `json:"epoch,omitempty"`
	Generation  float64                        `json:"generation,omitempty"`
	Uncertainty float64                        `json:"uncertainty,omitempty"`
}

func defaultOnlineBanditState() OnlineBanditState {
	var state OnlineBanditState
	for i := range state.Precision {
		state.Precision[i] = 1
	}
	state.Uncertainty = 1
	return state
}

func normalizeOnlineBanditState(state OnlineBanditState) OnlineBanditState {
	for i := 0; i < OnlineBanditDimension; i++ {
		if math.IsNaN(state.Theta[i]) || math.IsInf(state.Theta[i], 0) {
			state.Theta[i] = 0
		}
		if state.Precision[i] < 1 || math.IsNaN(state.Precision[i]) || math.IsInf(state.Precision[i], 0) {
			state.Precision[i] = 1
		}
	}
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

// legacyBanditStateFromWeights migrates the first v7 on-disk representation,
// where each student scalar occupied its own generic weight/LRU entry.
func legacyBanditStateFromWeights(weights map[string]float64, isUDP bool) (OnlineBanditState, bool) {
	state := defaultOnlineBanditState()
	if len(weights) == 0 {
		return state, false
	}
	thetaKeys, precisionKeys := &banditThetaTCP, &banditPrecisionTCP
	if isUDP {
		thetaKeys, precisionKeys = &banditThetaUDP, &banditPrecisionUDP
	}
	found := false
	for i := 0; i < OnlineBanditDimension; i++ {
		if value, ok := weights[(*thetaKeys)[i]]; ok {
			state.Theta[i] = value
			found = true
		}
		if value, ok := weights[(*precisionKeys)[i]]; ok {
			state.Precision[i] = value
			found = true
		}
	}
	if value, ok := weights[banditUpdatesKey(isUDP)]; ok {
		state.Updates = value
		found = true
	}
	if value, ok := weights[banditErrorKey(isUDP)]; ok {
		state.ErrorEWMA = value
		found = true
	}
	if value, ok := weights[banditEpochKey(isUDP)]; ok {
		state.Epoch = value
		found = true
	}
	if value, ok := weights[banditGenerationKey(isUDP)]; ok {
		state.Generation = value
		found = true
	}
	if value, ok := weights[BanditUncertaintyWeightType(isUDP)]; ok {
		state.Uncertainty = value
		found = true
	}
	return normalizeOnlineBanditState(state), found
}

func isLegacyBanditWeightKey(key string) bool {
	return len(key) >= 7 && key[:7] == "bandit-"
}

func LoadOnlineBanditState(record *AtomicStatsRecord, isUDP bool) OnlineBanditState {
	if record == nil {
		return defaultOnlineBanditState()
	}
	record.banditMu.RLock()
	var state *OnlineBanditState
	if isUDP {
		state = record.banditUDP
	} else {
		state = record.banditTCP
	}
	if state == nil {
		record.banditMu.RUnlock()
		return defaultOnlineBanditState()
	}
	out := *state
	record.banditMu.RUnlock()
	return normalizeOnlineBanditState(out)
}

func SaveOnlineBanditState(record *AtomicStatsRecord, isUDP bool, state OnlineBanditState, uncertainty float64) {
	if record == nil {
		return
	}
	state.Uncertainty = clamp01(uncertainty)
	state = normalizeOnlineBanditState(state)

	record.banditMu.Lock()
	if isUDP {
		if record.banditUDP == nil {
			record.banditUDP = new(OnlineBanditState)
		}
		*record.banditUDP = state
	} else {
		if record.banditTCP == nil {
			record.banditTCP = new(OnlineBanditState)
		}
		*record.banditTCP = state
	}
	record.banditMu.Unlock()

	if isUDP {
		record.banditUncertaintyUDP.Store(state.Uncertainty)
	} else {
		record.banditUncertaintyTCP.Store(state.Uncertainty)
	}
}

func (state *OnlineBanditState) softenForEnvironmentChange(thetaKeep, precisionKeep float64) {
	if state == nil {
		return
	}
	for i := 0; i < OnlineBanditDimension; i++ {
		state.Theta[i] *= thetaKeep
		precision := state.Precision[i]
		if precision < 1 {
			precision = 1
		}
		state.Precision[i] = 1 + (precision-1)*precisionKeep
	}
	if state.ErrorEWMA < 0.15 {
		state.ErrorEWMA = 0.15
	}
	// Recompute on the next prediction from the reopened precision rather than
	// carrying a stale "certain" scalar into the selector.
	state.Uncertainty = 1
}

func (state *OnlineBanditState) ObserveEpoch(epoch uint64) {
	if state == nil || epoch == 0 {
		return
	}

	// A persisted learner belongs to a previous core lifetime even when both
	// process-local network epochs happen to equal 1. Reopen confidence once on
	// first use after restart, while preserving a small directional prior.
	if state.Generation != processBanditGeneration {
		if state.Updates > 0 {
			state.softenForEnvironmentChange(0.20, 0.25)
		}
		state.Generation = processBanditGeneration
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

	// A same-process handover is stronger evidence of context drift.
	state.softenForEnvironmentChange(0.15, 0.20)
	state.Epoch = float64(epoch)
}

func clamp01(v float64) float64 {
	if v < 0 || math.IsNaN(v) {
		return 0
	}
	if v > 1 || math.IsInf(v, 0) {
		return 1
	}
	return v
}

func banditQualityFromDelay(ms int64) float64 {
	if ms <= 0 {
		return 0.5
	}
	return clamp01(math.Exp(-float64(ms) / 1200.0))
}

// OnlineBanditFeatures are deliberately bounded. The student is learning the
// residual the global/heuristic prior misses, not relearning the whole routing
// problem from raw strings.
func OnlineBanditFeatures(input *ModelInput) [OnlineBanditDimension]float64 {
	var x [OnlineBanditDimension]float64
	x[0] = 1
	if input == nil {
		return x
	}

	total := SampleCount(input.Success, input.Failure)
	if total > 0 {
		x[1] = clamp01(float64(input.Success) / float64(total))
	} else {
		x[1] = 0.5
	}
	x[2] = 0.5 * (banditQualityFromDelay(input.ConnectTime) + banditQualityFromDelay(input.Latency))

	currentRate := math.Max(input.MaxuploadRate, input.MaxdownloadRate)
	historyRate := math.Max(input.HistoryMaxUploadRate, input.HistoryMaxDownloadRate)
	if currentRate > 0 || historyRate > 0 {
		x[3] = math.Tanh(math.Log((currentRate + 1) / (historyRate + 1)))
	}

	loss := math.Max(input.LossRate, input.EmaLossRate)
	x[4] = clamp01(math.Exp(-loss * 12))
	if input.ConnectionFailed {
		x[5] = 0
	} else {
		x[5] = 1
	}
	return x
}

func (state *OnlineBanditState) Predict(prior float64, x [OnlineBanditDimension]float64) (weight, uncertainty float64) {
	if prior <= 0 || math.IsNaN(prior) || math.IsInf(prior, 0) {
		return prior, 1
	}
	residual := 0.0
	var variance float64
	for i := 0; i < OnlineBanditDimension; i++ {
		residual += state.Theta[i] * x[i]
		precision := state.Precision[i]
		if precision < 1 {
			precision = 1
		}
		variance += x[i] * x[i] / precision
	}
	// The student can correct a stale/global prior, but cannot erase it after a
	// handful of local observations.
	residual = math.Max(-0.35, math.Min(0.35, residual))
	weight = prior * math.Exp(residual)
	uncertainty = clamp01(math.Sqrt(variance / OnlineBanditDimension))
	return weight, uncertainty
}

// ObserveConnectionRewardMetrics is the bandit's ground truth. Every signal
// comes from this completed connection, never from historical EMA fields and
// never from the student's own previous prediction.
func ObserveConnectionRewardMetrics(connectTime, latency int64, maxUploadRateKB, maxDownloadRateKB, lossRate float64, failed bool, priorityFactor float64) float64 {
	if priorityFactor <= 0 {
		return 0
	}
	if failed {
		return 0.03 * priorityFactor
	}

	connectQ := banditQualityFromDelay(connectTime)
	latencyQ := banditQualityFromDelay(latency)
	lossQ := clamp01(math.Exp(-math.Max(0, lossRate) * 14))

	rate := math.Max(maxUploadRateKB, maxDownloadRateKB)
	rateQ := 0.0
	if rate > 0 {
		// 8 MiB/s in the existing KB/s feature scale is already "very good";
		// faster links should not dominate latency/reliability indefinitely.
		rateQ = clamp01(math.Log1p(rate) / math.Log1p(8192))
	}

	reward := 0.45 + 0.18*connectQ + 0.22*latencyQ + 0.12*lossQ + 0.03*rateQ
	return math.Max(0.03, math.Min(1.20, reward)) * priorityFactor
}

// ObserveConnectionReward remains a convenience wrapper for tests/tools whose
// ModelInput fields are known to represent one connection.
func ObserveConnectionReward(input *ModelInput, priorityFactor float64) float64 {
	if input == nil {
		return 0
	}
	return ObserveConnectionRewardMetrics(
		input.ConnectTime, input.Latency,
		input.MaxuploadRate, input.MaxdownloadRate,
		input.LossRate, input.ConnectionFailed, priorityFactor,
	)
}

// Update learns a bounded log-residual around prior using diagonal recursive
// least squares with gentle forgetting. Larger recent model error deliberately
// forgets confidence faster so network handovers recover quickly.
func (state *OnlineBanditState) Update(prior, reward float64, x [OnlineBanditDimension]float64, sampleScale int64) (weight, uncertainty float64) {
	if prior <= 0 || reward <= 0 || math.IsNaN(prior) || math.IsNaN(reward) ||
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
	targetResidual = math.Max(-0.65, math.Min(0.65, targetResidual))
	predictedResidual := math.Log(math.Max(1e-9, predicted) / prior)
	err := targetResidual - predictedResidual
	robustErr := math.Max(-0.30, math.Min(0.30, err))

	instantError := math.Min(1, math.Abs(err))
	if state.ErrorEWMA == 0 {
		state.ErrorEWMA = instantError
	} else {
		state.ErrorEWMA = state.ErrorEWMA*0.88 + instantError*0.12
	}

	forget := 0.995
	if state.ErrorEWMA >= 0.20 {
		forget = 0.975
	}
	for i := 0; i < OnlineBanditDimension; i++ {
		precision := state.Precision[i]
		if precision < 1 {
			precision = 1
		}
		precision = math.Max(1, precision*forget+scale*x[i]*x[i])
		gain := scale * x[i] / precision
		state.Theta[i] += gain * robustErr
		state.Theta[i] = math.Max(-0.30, math.Min(0.30, state.Theta[i]))
		state.Precision[i] = math.Min(4096, precision)
	}
	state.Updates += scale

	weight, uncertainty = state.Predict(prior, x)
	state.Uncertainty = uncertainty
	return weight, uncertainty
}

// ExplorationBonus applies a bounded UCB-style optimism term. Callers decide
// the alpha from live link conditions; setting alpha=0 makes ranking purely
// exploitative during handovers or weak-network recovery.
func ExplorationBonus(weight, uncertainty, alpha float64) float64 {
	if weight <= 0 || alpha <= 0 {
		return weight
	}
	alpha = math.Min(0.08, math.Max(0, alpha))
	return weight * (1 + alpha*clamp01(uncertainty))
}
