package smart

import (
	"math"
)

type DistillProductionSample struct {
	Input        ModelInput
	ActualWeight float64
}

func DistilledExpertBucket(input *ModelInput) int {
	if input == nil {
		return 0
	}
	bucket := int(ExpertScene(input)) * 2
	if input.IsUDP {
		bucket++
	}
	if bucket < 0 || bucket >= 8 {
		return 0
	}
	return bucket
}

// FitProductionDistillationCalibration compresses production behavior into one
// bounded scalar per scene/transport bucket. Raw targets, IPs and node names do
// not enter the artifact; only the aggregate ratio survives.
func FitProductionDistillationCalibration(samples []DistillProductionSample) [8]float64 {
	var sumLog [8]float64
	var count [8]int
	for _, sample := range samples {
		if sample.ActualWeight <= 0 || math.IsNaN(sample.ActualWeight) || math.IsInf(sample.ActualWeight, 0) {
			continue
		}
		prior := DistilledExpertPrior(&sample.Input, 1)
		if prior <= 0 || math.IsNaN(prior) || math.IsInf(prior, 0) {
			continue
		}
		ratio := sample.ActualWeight / prior
		ratio = math.Max(0.70, math.Min(1.30, ratio))
		bucket := DistilledExpertBucket(&sample.Input)
		sumLog[bucket] += math.Log(ratio)
		count[bucket]++
	}

	var calibration [8]float64
	for i := range calibration {
		calibration[i] = 1
		if count[i] < 8 {
			continue
		}
		scale := math.Exp(sumLog[i] / float64(count[i]))
		calibration[i] = math.Max(0.85, math.Min(1.15, scale))
	}
	return calibration
}
