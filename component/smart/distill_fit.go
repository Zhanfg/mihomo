package smart

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

type DistillTrainingSample struct {
	Input        ModelInput
	TargetWeight float64
}

type DistillMetrics struct {
	TrainingSamples   int
	HoldoutSamples    int
	MeanAbsoluteError float64
	RMSE              float64
	P95AbsoluteError  float64
	MaxAbsoluteError  float64
	MeanRelativeError float64
	RankingAgreement  float64
}

type DistillFitResult struct {
	Coefficients [8][ExpertFeatureDimension]float64
	Metrics      DistillMetrics
	BucketTrain  [8]int
	BucketHoldout [8]int
}

type DistillSufficientStats struct {
	SchemaVersion int                                                   `json:"schema_version"`
	Ridge         float64                                               `json:"ridge"`
	Source        string                                                `json:"source"`
	BucketTrain   [8]int                                                `json:"bucket_train"`
	BucketHoldout [8]int                                                `json:"bucket_holdout"`
	XTX           [8][ExpertFeatureDimension][ExpertFeatureDimension]float64 `json:"xtx"`
	XTY           [8][ExpertFeatureDimension]float64                   `json:"xty"`
	Metrics       DistillMetrics                                        `json:"metrics"`
}

type DistillQualityPolicy struct {
	MinHoldout          int
	MaxMAE              float64
	MaxRMSE             float64
	MaxP95AbsoluteError float64
	MaxMeanRelative     float64
	MinRankingAgreement float64
}

func DefaultDistillQualityPolicy() DistillQualityPolicy {
	return DistillQualityPolicy{
		MinHoldout:          64,
		MaxMAE:              0.10,
		MaxRMSE:             0.14,
		MaxP95AbsoluteError: 0.24,
		MaxMeanRelative:     0.22,
		MinRankingAgreement: 0.76,
	}
}

func validDistillTarget(v float64) bool {
	return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0)
}

func solveDistillSystem(a [ExpertFeatureDimension][ExpertFeatureDimension]float64, b [ExpertFeatureDimension]float64) ([ExpertFeatureDimension]float64, bool) {
	var aug [ExpertFeatureDimension][ExpertFeatureDimension + 1]float64
	for i := 0; i < ExpertFeatureDimension; i++ {
		for j := 0; j < ExpertFeatureDimension; j++ {
			aug[i][j] = a[i][j]
		}
		aug[i][ExpertFeatureDimension] = b[i]
	}

	for col := 0; col < ExpertFeatureDimension; col++ {
		pivot := col
		best := math.Abs(aug[col][col])
		for row := col + 1; row < ExpertFeatureDimension; row++ {
			if v := math.Abs(aug[row][col]); v > best {
				best = v
				pivot = row
			}
		}
		if best < 1e-10 || math.IsNaN(best) || math.IsInf(best, 0) {
			return [ExpertFeatureDimension]float64{}, false
		}
		if pivot != col {
			aug[col], aug[pivot] = aug[pivot], aug[col]
		}
		div := aug[col][col]
		for j := col; j <= ExpertFeatureDimension; j++ {
			aug[col][j] /= div
		}
		for row := 0; row < ExpertFeatureDimension; row++ {
			if row == col {
				continue
			}
			factor := aug[row][col]
			if factor == 0 {
				continue
			}
			for j := col; j <= ExpertFeatureDimension; j++ {
				aug[row][j] -= factor * aug[col][j]
			}
		}
	}

	var out [ExpertFeatureDimension]float64
	for i := 0; i < ExpertFeatureDimension; i++ {
		out[i] = aug[i][ExpertFeatureDimension]
		if math.IsNaN(out[i]) || math.IsInf(out[i], 0) {
			return [ExpertFeatureDimension]float64{}, false
		}
	}
	return out, true
}

func clampDistilledCoefficients(coeff [ExpertFeatureDimension]float64) [ExpertFeatureDimension]float64 {
	coeff[0] = math.Max(-0.25, math.Min(0.55, coeff[0]))
	for i := 1; i < ExpertFeatureDimension; i++ {
		coeff[i] = math.Max(-1.20, math.Min(1.20, coeff[i]))
	}
	return coeff
}

type distillEvalPoint struct {
	bucket int
	target float64
	pred   float64
}

func predictDistilledCoefficients(coeff [8][ExpertFeatureDimension]float64, input *ModelInput) float64 {
	if input == nil {
		return 0
	}
	bucket := DistilledExpertBucket(input)
	score := dotExpert(coeff[bucket], ExpertFeatures(input))
	return math.Max(0.03, math.Min(1.20, score))
}

// FitDistilledExpertModel trains one tiny ridge-regression student per
// {scene,transport} bucket. The analytic expert fold is the regularization
// center, so sparse production data can refine a model but cannot erase the
// hand-designed prior.
func normalizeDistillRidge(ridge float64) float64 {
	if ridge <= 0 || math.IsNaN(ridge) || math.IsInf(ridge, 0) {
		return 8
	}
	return ridge
}

func BuildDistillSufficientStats(samples []DistillTrainingSample, ridge float64, source string) (DistillSufficientStats, []DistillTrainingSample) {
	stats := DistillSufficientStats{
		SchemaVersion: 1,
		Ridge:         normalizeDistillRidge(ridge),
		Source:        source,
	}
	var holdout []DistillTrainingSample
	for i, sample := range samples {
		if !validDistillTarget(sample.TargetWeight) {
			continue
		}
		bucket := DistilledExpertBucket(&sample.Input)
		if i%5 == 0 {
			stats.BucketHoldout[bucket]++
			holdout = append(holdout, sample)
			continue
		}
		stats.BucketTrain[bucket]++
		x := ExpertFeatures(&sample.Input)
		y := math.Max(0.03, math.Min(1.20, sample.TargetWeight))
		for row := 0; row < ExpertFeatureDimension; row++ {
			stats.XTY[bucket][row] += x[row] * y
			for col := 0; col < ExpertFeatureDimension; col++ {
				stats.XTX[bucket][row][col] += x[row] * x[col]
			}
		}
	}
	return stats, holdout
}

func FitDistilledExpertFromStats(stats DistillSufficientStats) DistillFitResult {
	base := CompileExpertDistillation()
	result := DistillFitResult{
		Coefficients: base,
		BucketTrain:  stats.BucketTrain,
		BucketHoldout: stats.BucketHoldout,
	}
	ridge := normalizeDistillRidge(stats.Ridge)
	for bucket := 0; bucket < 8; bucket++ {
		if result.BucketTrain[bucket] < ExpertFeatureDimension*4 {
			continue
		}
		a := stats.XTX[bucket]
		b := stats.XTY[bucket]
		for i := 0; i < ExpertFeatureDimension; i++ {
			a[i][i] += ridge
			b[i] += ridge * base[bucket][i]
		}
		if coeff, ok := solveDistillSystem(a, b); ok {
			result.Coefficients[bucket] = clampDistilledCoefficients(coeff)
		}
	}
	result.Metrics = stats.Metrics
	result.Metrics.TrainingSamples = 0
	result.Metrics.HoldoutSamples = 0
	for _, n := range result.BucketTrain {
		result.Metrics.TrainingSamples += n
	}
	for _, n := range result.BucketHoldout {
		result.Metrics.HoldoutSamples += n
	}
	return result
}

// FitDistilledExpertModel trains one tiny ridge-regression student per
// {scene,transport} bucket. It returns a model that can later be reconstructed
// from anonymized sufficient statistics without retaining raw production rows.
func FitDistilledExpertModel(samples []DistillTrainingSample, ridge float64) DistillFitResult {
	stats, holdout := BuildDistillSufficientStats(samples, ridge, "memory")
	result := FitDistilledExpertFromStats(stats)
	result.Metrics = EvaluateDistilledExpertModel(result.Coefficients, holdout)
	result.Metrics.TrainingSamples = 0
	for _, n := range result.BucketTrain {
		result.Metrics.TrainingSamples += n
	}
	stats.Metrics = result.Metrics
	return result
}

func EvaluateDistilledExpertModel(coeff [8][ExpertFeatureDimension]float64, samples []DistillTrainingSample) DistillMetrics {
	var metrics DistillMetrics
	var absErrors []float64
	var points [8][]distillEvalPoint
	var sumSq, sumRel float64

	for _, sample := range samples {
		if !validDistillTarget(sample.TargetWeight) {
			continue
		}
		target := math.Max(0.03, math.Min(1.20, sample.TargetWeight))
		pred := predictDistilledCoefficients(coeff, &sample.Input)
		err := math.Abs(pred - target)
		absErrors = append(absErrors, err)
		metrics.MeanAbsoluteError += err
		sumSq += err * err
		sumRel += err / math.Max(0.05, target)
		if err > metrics.MaxAbsoluteError {
			metrics.MaxAbsoluteError = err
		}
		bucket := DistilledExpertBucket(&sample.Input)
		points[bucket] = append(points[bucket], distillEvalPoint{bucket: bucket, target: target, pred: pred})
	}
	metrics.HoldoutSamples = len(absErrors)
	if metrics.HoldoutSamples == 0 {
		return metrics
	}
	n := float64(metrics.HoldoutSamples)
	metrics.MeanAbsoluteError /= n
	metrics.RMSE = math.Sqrt(sumSq / n)
	metrics.MeanRelativeError = sumRel / n
	sort.Float64s(absErrors)
	p95 := int(math.Ceil(0.95*float64(len(absErrors)))) - 1
	if p95 < 0 {
		p95 = 0
	}
	if p95 >= len(absErrors) {
		p95 = len(absErrors) - 1
	}
	metrics.P95AbsoluteError = absErrors[p95]

	// Approximate pairwise rank agreement with deterministic long-stride pairs.
	// This catches a low-MAE model that nonetheless reverses candidate order.
	pairs, agree := 0, 0
	for bucket := range points {
		p := points[bucket]
		if len(p) < 2 {
			continue
		}
		stride := len(p)/2 + 1
		limit := len(p)
		if limit > 4096 {
			limit = 4096
		}
		for i := 0; i < limit; i++ {
			j := (i + stride) % len(p)
			if i == j {
				continue
			}
			td := p[i].target - p[j].target
			if math.Abs(td) < 0.02 {
				continue
			}
			pd := p[i].pred - p[j].pred
			pairs++
			if (td > 0 && pd > 0) || (td < 0 && pd < 0) {
				agree++
			}
		}
	}
	if pairs > 0 {
		metrics.RankingAgreement = float64(agree) / float64(pairs)
	} else {
		metrics.RankingAgreement = 1
	}
	return metrics
}

func ValidateDistillQuality(metrics DistillMetrics, policy DistillQualityPolicy) error {
	if policy.MinHoldout <= 0 {
		policy = DefaultDistillQualityPolicy()
	}
	if metrics.HoldoutSamples < policy.MinHoldout {
		return fmt.Errorf("distillation holdout too small: %d < %d", metrics.HoldoutSamples, policy.MinHoldout)
	}
	var problems []error
	if metrics.MeanAbsoluteError > policy.MaxMAE {
		problems = append(problems, fmt.Errorf("MAE %.6f > %.6f", metrics.MeanAbsoluteError, policy.MaxMAE))
	}
	if metrics.RMSE > policy.MaxRMSE {
		problems = append(problems, fmt.Errorf("RMSE %.6f > %.6f", metrics.RMSE, policy.MaxRMSE))
	}
	if metrics.P95AbsoluteError > policy.MaxP95AbsoluteError {
		problems = append(problems, fmt.Errorf("P95 abs error %.6f > %.6f", metrics.P95AbsoluteError, policy.MaxP95AbsoluteError))
	}
	if metrics.MeanRelativeError > policy.MaxMeanRelative {
		problems = append(problems, fmt.Errorf("mean relative error %.6f > %.6f", metrics.MeanRelativeError, policy.MaxMeanRelative))
	}
	if metrics.RankingAgreement < policy.MinRankingAgreement {
		problems = append(problems, fmt.Errorf("ranking agreement %.6f < %.6f", metrics.RankingAgreement, policy.MinRankingAgreement))
	}
	return errors.Join(problems...)
}
