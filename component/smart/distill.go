package smart

import (
	"errors"
	"fmt"
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
		prior := DistilledExpertBasePrior(&sample.Input, 1)
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


type DistilledArtifactManifest struct {
	Version          string
	Source           string
	TrainingSamples  int
	HoldoutSamples   int
	MAE              float64
	RMSE             float64
	P95AbsoluteError float64
	MeanRelative     float64
	RankingAgreement float64
	Fingerprint      string
}

func ValidateDistilledArtifactValues(coeff [8][ExpertFeatureDimension]float64, calibration [8]float64, manifest DistilledArtifactManifest) error {
	if manifest.Version == "" {
		return errors.New("empty distilled artifact version")
	}
	for bucket := 0; bucket < len(coeff); bucket++ {
		for feature := 0; feature < ExpertFeatureDimension; feature++ {
			value := coeff[bucket][feature]
			if math.IsNaN(value) || math.IsInf(value, 0) || value < -1.25 || value > 1.25 {
				return fmt.Errorf("invalid distilled coefficient bucket=%d feature=%d value=%v", bucket, feature, value)
			}
		}
		scale := calibration[bucket]
		if math.IsNaN(scale) || math.IsInf(scale, 0) || scale < 0.80 || scale > 1.20 {
			return fmt.Errorf("invalid distilled calibration bucket=%d value=%v", bucket, scale)
		}
	}
	if manifest.TrainingSamples < 0 || manifest.HoldoutSamples < 0 {
		return errors.New("negative distilled sample count")
	}
	for name, value := range map[string]float64{
		"mae": manifest.MAE,
		"rmse": manifest.RMSE,
		"p95": manifest.P95AbsoluteError,
		"mean_relative": manifest.MeanRelative,
		"ranking_agreement": manifest.RankingAgreement,
	} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return fmt.Errorf("invalid distilled manifest %s=%v", name, value)
		}
	}
	return nil
}
