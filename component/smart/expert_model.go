package smart

import "math"

const (
	ExpertReliability = iota
	ExpertLatency
	ExpertThroughput
	ExpertBalanced
	expertCount
)

const ExpertFeatureDimension = 7

// ExpertFeatureVector is intentionally compact and allocation-free. Raw string
// identity features stay with the global LightGBM teacher; these experts model
// transport behavior that is portable across devices and targets.
type ExpertFeatureVector [ExpertFeatureDimension]float64

var expertCoefficients = [expertCount][ExpertFeatureDimension]float64{
	// bias, success, connect, latency, loss, throughput, duration
	ExpertReliability: {0.04, 0.50, 0.12, 0.08, 0.25, 0.00, 0.01},
	ExpertLatency:     {0.04, 0.18, 0.25, 0.42, 0.08, 0.00, 0.03},
	ExpertThroughput:  {0.04, 0.15, 0.08, 0.08, 0.10, 0.50, 0.05},
	ExpertBalanced:    {0.04, 0.30, 0.18, 0.22, 0.18, 0.05, 0.03},
}

var expertSceneGates = [4][expertCount]float64{
	sceneWeb:         {0.28, 0.24, 0.10, 0.38},
	sceneInteractive: {0.30, 0.47, 0.05, 0.18},
	sceneStreaming:   {0.28, 0.10, 0.46, 0.16},
	sceneTransfer:    {0.20, 0.05, 0.60, 0.15},
}

var expertUDPMultipliers = [expertCount]float64{1.15, 1.20, 0.80, 0.95}


var (
	analyticDistilledFallback = CompileExpertDistillation()
	distilledArtifactValid    = ValidateDistilledArtifactValues(
		distilledExpertCoefficients,
		distilledExpertCalibration,
		distilledExpertManifest,
	) == nil
)

func DistilledArtifactValid() bool {
	return distilledArtifactValid
}

func DistilledArtifactManifestInfo() DistilledArtifactManifest {
	return distilledExpertManifest
}

func expertRateQuality(input *ModelInput) float64 {
	if input == nil {
		return 0
	}
	rate := math.Max(input.MaxuploadRate, input.MaxdownloadRate)
	if rate <= 0 {
		return 0
	}
	return clamp01(math.Log1p(rate) / math.Log1p(8192))
}

func expertDurationQuality(input *ModelInput) float64 {
	if input == nil || input.ConnectionDuration <= 0 {
		return 0.15
	}
	// A few seconds of evidence is useful, while long-lived flows should not
	// dominate the expert output merely because they stayed open.
	return clamp01(math.Log1p(input.ConnectionDuration) / math.Log(16))
}

func ExpertFeatures(input *ModelInput) ExpertFeatureVector {
	var x ExpertFeatureVector
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
	x[2] = banditQualityFromDelay(input.ConnectTime)
	x[3] = banditQualityFromDelay(input.Latency)
	loss := math.Max(input.LossRate, input.EmaLossRate)
	x[4] = clamp01(math.Exp(-math.Max(0, loss) * 14))
	x[5] = expertRateQuality(input)
	x[6] = expertDurationQuality(input)
	return x
}

func dotExpert(coeff [ExpertFeatureDimension]float64, x ExpertFeatureVector) float64 {
	sum := 0.0
	for i := 0; i < ExpertFeatureDimension; i++ {
		sum += coeff[i] * x[i]
	}
	return sum
}

func expertGate(scene sceneKind, isUDP bool) [expertCount]float64 {
	if scene < sceneWeb || scene > sceneTransfer {
		scene = sceneWeb
	}
	gate := expertSceneGates[scene]
	if !isUDP {
		return gate
	}
	sum := 0.0
	for i := 0; i < expertCount; i++ {
		gate[i] *= expertUDPMultipliers[i]
		sum += gate[i]
	}
	if sum <= 0 {
		return expertSceneGates[scene]
	}
	for i := 0; i < expertCount; i++ {
		gate[i] /= sum
	}
	return gate
}

func ExpertScene(input *ModelInput) sceneKind {
	if input == nil {
		return sceneWeb
	}
	return identifyConnectionScene(
		input.IsUDP,
		input.Latency,
		input.UploadTotal,
		input.DownloadTotal,
		input.MaxuploadRate,
		input.MaxdownloadRate,
		input.ConnectionDuration,
	)
}

func ExpertTeacherPrior(input *ModelInput, priorityFactor float64) float64 {
	if input == nil || priorityFactor <= 0 {
		return 0
	}
	x := ExpertFeatures(input)
	gate := expertGate(ExpertScene(input), input.IsUDP)
	score := 0.0
	for expert := 0; expert < expertCount; expert++ {
		score += gate[expert] * dotExpert(expertCoefficients[expert], x)
	}
	score = math.Max(0.03, math.Min(1.20, score))
	return score * priorityFactor
}

func distilledExpertPrior(input *ModelInput, priorityFactor float64, applyProductionCalibration bool) float64 {
	if input == nil || priorityFactor <= 0 {
		return 0
	}
	scene := ExpertScene(input)
	bucket := int(scene) * 2
	if input.IsUDP {
		bucket++
	}
	if bucket < 0 || bucket >= len(distilledExpertCoefficients) {
		bucket = 0
	}
	x := ExpertFeatures(input)
	coeff := distilledExpertCoefficients[bucket]
	scale := distilledExpertCalibration[bucket]
	if !distilledArtifactValid {
		coeff = analyticDistilledFallback[bucket]
		scale = 1
	}
	score := dotExpert(coeff, x)
	if applyProductionCalibration {
		score *= scale
	}
	score = math.Max(0.03, math.Min(1.20, score))
	return score * priorityFactor
}

func DistilledExpertPrior(input *ModelInput, priorityFactor float64) float64 {
	return distilledExpertPrior(input, priorityFactor, true)
}

func DistilledExpertBasePrior(input *ModelInput, priorityFactor float64) float64 {
	return distilledExpertPrior(input, priorityFactor, false)
}

// CompileExpertDistillation analytically folds the scene router and four
// linear experts into one seven-term dot product per {scene,transport}. The
// generated artifact can therefore reproduce the full expert ensemble without
// evaluating four experts in the connection hot path.
func CompileExpertDistillation() [8][ExpertFeatureDimension]float64 {
	var out [8][ExpertFeatureDimension]float64
	for scene := sceneWeb; scene <= sceneTransfer; scene++ {
		for udpIndex := 0; udpIndex < 2; udpIndex++ {
			gate := expertGate(scene, udpIndex == 1)
			bucket := int(scene)*2 + udpIndex
			for expert := 0; expert < expertCount; expert++ {
				for feature := 0; feature < ExpertFeatureDimension; feature++ {
					out[bucket][feature] += gate[expert] * expertCoefficients[expert][feature]
				}
			}
		}
	}
	return out
}

func DistillationCoefficientError() float64 {
	compiled := CompileExpertDistillation()
	maxErr := 0.0
	for bucket := range compiled {
		for feature := 0; feature < ExpertFeatureDimension; feature++ {
			err := math.Abs(compiled[bucket][feature] - distilledExpertCoefficients[bucket][feature])
			if err > maxErr {
				maxErr = err
			}
		}
	}
	return maxErr
}

func BlendHeuristicAndDistilled(heuristic, distilled float64, samples int64) float64 {
	if heuristic <= 0 {
		return distilled
	}
	if distilled <= 0 {
		return heuristic
	}
	// Cold start benefits most from the globally distilled experts. As local
	// evidence matures, the historical heuristic regains part of the vote.
	distilledShare := 0.72
	switch {
	case samples >= 256:
		distilledShare = 0.42
	case samples >= 64:
		distilledShare = 0.50
	case samples >= 16:
		distilledShare = 0.60
	}
	return distilled*distilledShare + heuristic*(1-distilledShare)
}

func ExpertDisagreement(distilled, full float64) float64 {
	if distilled <= 0 || full <= 0 {
		return 1
	}
	denom := math.Max(distilled, full)
	if denom <= 0 {
		return 0
	}
	return clamp01(math.Abs(distilled-full) / denom)
}

// ShouldConsultExpert keeps the full MoE off the steady-state hot path. It is
// consulted densely while the local student is cold/uncertain or drifting,
// and sparsely thereafter as a shadow check on the distilled artifact.
func ShouldConsultExpert(input *ModelInput, student OnlineBanditState) bool {
	if input == nil {
		return false
	}
	if input.ConnectionFailed || input.LossRate >= 0.01 {
		return true
	}
	if student.Updates < 8 || student.Uncertainty >= 0.45 || student.ErrorEWMA >= 0.20 {
		return true
	}
	total := SampleCount(input.Success, input.Failure)
	if total < 32 {
		return total%5 == 0
	}
	return total%31 == 0
}
