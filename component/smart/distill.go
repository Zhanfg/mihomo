package smart

import (
	"math"
)

type DistillProductionSample struct {
	Input         ModelInput
	ActualWeight  float64
	TeacherWeight float64
}

type DistillationBucketQuality struct {
	Count              int
	BaseLogRMSE        float64
	CalibratedLogRMSE  float64
	MaxRelativeError   float64
}

type DistillationQualityReport struct {
	Count              int
	BaseLogRMSE        float64
	CalibratedLogRMSE  float64
	MaxRelativeError   float64
	Buckets            [8]DistillationBucketQuality
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

func distillationTarget(sample DistillProductionSample) float64 {
	if sample.TeacherWeight > 0 && !math.IsNaN(sample.TeacherWeight) && !math.IsInf(sample.TeacherWeight, 0) {
		return sample.TeacherWeight
	}
	if sample.ActualWeight > 0 && !math.IsNaN(sample.ActualWeight) && !math.IsInf(sample.ActualWeight, 0) {
		return sample.ActualWeight
	}
	return 0
}

// FitProductionDistillationCalibration compresses production/teacher behavior
// into one bounded scalar per scene/transport bucket. Teacher soft targets are
// preferred when available; otherwise the final production weight is used.
// Raw targets, IPs and node names never enter the generated artifact.
func FitProductionDistillationCalibration(samples []DistillProductionSample) [8]float64 {
	var sumLog [8]float64
	var count [8]int
	for _, sample := range samples {
		target := distillationTarget(sample)
		if target <= 0 {
			continue
		}
		prior := DistilledExpertBasePrior(&sample.Input, 1)
		if prior <= 0 || math.IsNaN(prior) || math.IsInf(prior, 0) {
			continue
		}
		ratio := target / prior
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

func EvaluateProductionDistillation(samples []DistillProductionSample, calibration [8]float64) DistillationQualityReport {
	var report DistillationQualityReport
	var baseSq, calibratedSq float64
	var bucketBaseSq, bucketCalibratedSq [8]float64

	for _, sample := range samples {
		target := distillationTarget(sample)
		if target <= 0 {
			continue
		}
		base := DistilledExpertBasePrior(&sample.Input, 1)
		if base <= 0 || math.IsNaN(base) || math.IsInf(base, 0) {
			continue
		}
		bucket := DistilledExpertBucket(&sample.Input)
		predicted := base * calibration[bucket]
		if predicted <= 0 || math.IsNaN(predicted) || math.IsInf(predicted, 0) {
			continue
		}

		baseLogErr := math.Log(base / target)
		calibratedLogErr := math.Log(predicted / target)
		relErr := math.Abs(predicted-target) / math.Max(target, 1e-9)

		report.Count++
		report.Buckets[bucket].Count++
		baseSq += baseLogErr * baseLogErr
		calibratedSq += calibratedLogErr * calibratedLogErr
		bucketBaseSq[bucket] += baseLogErr * baseLogErr
		bucketCalibratedSq[bucket] += calibratedLogErr * calibratedLogErr
		if relErr > report.MaxRelativeError {
			report.MaxRelativeError = relErr
		}
		if relErr > report.Buckets[bucket].MaxRelativeError {
			report.Buckets[bucket].MaxRelativeError = relErr
		}
	}

	if report.Count > 0 {
		report.BaseLogRMSE = math.Sqrt(baseSq / float64(report.Count))
		report.CalibratedLogRMSE = math.Sqrt(calibratedSq / float64(report.Count))
	}
	for i := range report.Buckets {
		n := report.Buckets[i].Count
		if n == 0 {
			continue
		}
		report.Buckets[i].BaseLogRMSE = math.Sqrt(bucketBaseSq[i] / float64(n))
		report.Buckets[i].CalibratedLogRMSE = math.Sqrt(bucketCalibratedSq[i] / float64(n))
	}
	return report
}

// ProductionDistillationAcceptable rejects a calibration that makes the
// observed teacher/production distribution worse in log-RMSE. The absolute
// ceiling catches a dataset that the tiny deployed model cannot represent even
// if calibration technically improves it.
func ProductionDistillationAcceptable(report DistillationQualityReport, maxLogRMSE float64) bool {
	if report.Count == 0 {
		return true
	}
	if maxLogRMSE <= 0 {
		maxLogRMSE = 0.35
	}
	if report.CalibratedLogRMSE > report.BaseLogRMSE+1e-12 {
		return false
	}
	if report.CalibratedLogRMSE > maxLogRMSE {
		return false
	}
	for _, bucket := range report.Buckets {
		if bucket.Count >= 8 && bucket.CalibratedLogRMSE > bucket.BaseLogRMSE+1e-12 {
			return false
		}
	}
	return true
}
