package main

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/metacubex/mihomo/component/smart"
	"github.com/metacubex/mihomo/component/smart/lightgbm"
)

const artifactVersion = "expert-v9-1"

type collectedRow struct {
	Input        smart.ModelInput
	Features     []float64
	ActualWeight float64
}

func parseFloat(row []string, index map[string]int, key string) float64 {
	i, ok := index[key]
	if !ok || i < 0 || i >= len(row) {
		return 0
	}
	v, _ := strconv.ParseFloat(row[i], 64)
	return v
}

func parseInt(row []string, index map[string]int, key string) int64 {
	i, ok := index[key]
	if !ok || i < 0 || i >= len(row) {
		return 0
	}
	v, _ := strconv.ParseInt(row[i], 10, 64)
	return v
}

func parseString(row []string, index map[string]int, key string) string {
	i, ok := index[key]
	if !ok || i < 0 || i >= len(row) {
		return ""
	}
	value := strings.TrimSpace(row[i])
	if value == "unknown" {
		return ""
	}
	return value
}

func parseBool(row []string, index map[string]int, key string) bool {
	i, ok := index[key]
	if !ok || i < 0 || i >= len(row) {
		return false
	}
	v, _ := strconv.ParseBool(strings.TrimSpace(row[i]))
	return v
}

func inverseLogFeature(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	raw := math.Expm1(v)
	if raw < 0 || math.IsNaN(raw) || math.IsInf(raw, 0) {
		return 0
	}
	return raw
}

func loadRows(path string) ([]collectedRow, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	index := make(map[string]int, len(header))
	for i, h := range header {
		index[h] = i
	}
	if len(header) < lightgbm.MaxFeatureSize {
		return nil, fmt.Errorf("collector schema has %d columns, need at least %d feature columns", len(header), lightgbm.MaxFeatureSize)
	}

	now := time.Now().Unix()
	var out []collectedRow
	for {
		row, err := r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		if len(row) < lightgbm.MaxFeatureSize {
			continue
		}
		features := make([]float64, lightgbm.MaxFeatureSize)
		valid := true
		for i := 0; i < lightgbm.MaxFeatureSize; i++ {
			v, err := strconv.ParseFloat(row[i], 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				valid = false
				break
			}
			features[i] = v
		}
		if !valid {
			continue
		}

		lastUsedSeconds := int64(math.Round(inverseLogFeature(features[14])))
		input := smart.ModelInput{
			Success:                   int64(math.Round(features[0])),
			Failure:                   int64(math.Round(features[1])),
			ConnectTime:               int64(math.Round(inverseLogFeature(features[2]))),
			Latency:                   int64(math.Round(inverseLogFeature(features[3]))),
			UploadTotal:               inverseLogFeature(features[4]),
			HistoryUploadTotal:        inverseLogFeature(features[5]),
			MaxuploadRate:             inverseLogFeature(features[6]),
			HistoryMaxUploadRate:      inverseLogFeature(features[7]),
			DownloadTotal:             inverseLogFeature(features[8]),
			HistoryDownloadTotal:      inverseLogFeature(features[9]),
			MaxdownloadRate:           inverseLogFeature(features[10]),
			HistoryMaxDownloadRate:    inverseLogFeature(features[11]),
			ConnectionDuration:        inverseLogFeature(features[12]),
			HistoryConnectionDuration: inverseLogFeature(features[13]),
			LastUsed:                  now - lastUsedSeconds,
			IsUDP:                     features[15] >= 0.5,
			IsTCP:                     features[16] >= 0.5,
			LossRate:                  features[17],
			CumulLossRate:             features[18],
			EmaLossRate:               parseFloat(row, index, "ema_loss_rate"),
			ConnectionFailed:          parseBool(row, index, "connection_failed"),
			DestIPASN:                 parseString(row, index, "asn_raw"),
			Host:                      parseString(row, index, "host_raw"),
			DestIP:                    parseString(row, index, "ip_raw"),
			GroupName:                 parseString(row, index, "group_name"),
			NodeName:                  parseString(row, index, "node_name"),
		}
		if input.EmaLossRate == 0 {
			input.EmaLossRate = input.CumulLossRate
		}
		if port := parseInt(row, index, "port_raw"); port > 0 && port <= 65535 {
			input.DestPort = uint16(port)
		}
		if geo := parseString(row, index, "geoip_raw"); geo != "" {
			for _, part := range strings.Split(geo, ",") {
				if part = strings.TrimSpace(part); part != "" {
					input.DestGeoIP = append(input.DestGeoIP, part)
				}
			}
		}
		out = append(out, collectedRow{
			Input:        input,
			Features:     features,
			ActualWeight: parseFloat(row, index, "weight"),
		})
	}
	return out, nil
}

func trainingSamples(rows []collectedRow, targetMode string, teacher *lightgbm.WeightModel, teacherShare float64) ([]smart.DistillTrainingSample, error) {
	targetMode = strings.ToLower(strings.TrimSpace(targetMode))
	if teacherShare < 0 {
		teacherShare = 0
	}
	if teacherShare > 1 {
		teacherShare = 1
	}
	if (targetMode == "teacher" || targetMode == "hybrid") && teacher == nil {
		return nil, fmt.Errorf("target=%s requires -teacher-model", targetMode)
	}

	out := make([]smart.DistillTrainingSample, 0, len(rows))
	for _, row := range rows {
		actual := row.ActualWeight
		var target float64
		switch targetMode {
		case "actual":
			target = actual
		case "expert":
			target = smart.ExpertTeacherPrior(&row.Input, 1)
		case "teacher":
			value, ok := teacher.PredictFeatureVector(row.Features, 1)
			if !ok {
				continue
			}
			target = value
		case "hybrid":
			value, ok := teacher.PredictFeatureVector(row.Features, 1)
			if !ok && !(actual > 0) {
				continue
			}
			if !ok {
				target = actual
			} else if !(actual > 0) {
				target = value
			} else {
				target = value*teacherShare + actual*(1-teacherShare)
			}
		default:
			return nil, fmt.Errorf("unsupported target mode %q (use actual, expert, teacher, or hybrid)", targetMode)
		}
		if target <= 0 || math.IsNaN(target) || math.IsInf(target, 0) {
			continue
		}
		out = append(out, smart.DistillTrainingSample{Input: row.Input, TargetWeight: target})
	}
	return out, nil
}

func onesCalibration() [8]float64 {
	var out [8]float64
	for i := range out {
		out[i] = 1
	}
	return out
}

func artifactFingerprint(coeff [8][smart.ExpertFeatureDimension]float64, calibration [8]float64, manifest smart.DistilledArtifactManifest) string {
	copyManifest := manifest
	copyManifest.Fingerprint = ""
	payload, _ := json.Marshal(struct {
		Coefficients [8][smart.ExpertFeatureDimension]float64 `json:"coefficients"`
		Calibration  [8]float64                               `json:"calibration"`
		Manifest     smart.DistilledArtifactManifest          `json:"manifest"`
	}{coeff, calibration, copyManifest})
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("%x", sum[:12])
}

func render(coeff [8][smart.ExpertFeatureDimension]float64, calibration [8]float64, manifest smart.DistilledArtifactManifest) string {
	manifest.Version = artifactVersion
	if manifest.Source == "analytic" && manifest.TrainingSamples == 0 && manifest.HoldoutSamples == 0 {
		manifest.Fingerprint = "analytic-" + artifactVersion
	} else {
		manifest.Fingerprint = artifactFingerprint(coeff, calibration, manifest)
	}
	var b strings.Builder
	b.WriteString("package smart\n\n")
	b.WriteString("// Code generated by cmd/smart-distill; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "const DistilledExpertVersion = %q\n\n", artifactVersion)
	b.WriteString("var distilledExpertCoefficients = [8][ExpertFeatureDimension]float64{\n")
	for _, row := range coeff {
		b.WriteString("\t{")
		for i, value := range row {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%.8f", value)
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n\n")
	b.WriteString("var distilledExpertCalibration = [8]float64{\n\t")
	for i, value := range calibration {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%.8f", value)
	}
	b.WriteString(",\n}\n\n")
	b.WriteString("var distilledExpertManifest = DistilledArtifactManifest{\n")
	fmt.Fprintf(&b, "\tVersion: %q,\n", artifactVersion)
	fmt.Fprintf(&b, "\tSource: %q,\n", manifest.Source)
	fmt.Fprintf(&b, "\tTrainingSamples: %d,\n", manifest.TrainingSamples)
	fmt.Fprintf(&b, "\tHoldoutSamples: %d,\n", manifest.HoldoutSamples)
	fmt.Fprintf(&b, "\tMAE: %.8f,\n", manifest.MAE)
	fmt.Fprintf(&b, "\tRMSE: %.8f,\n", manifest.RMSE)
	fmt.Fprintf(&b, "\tP95AbsoluteError: %.8f,\n", manifest.P95AbsoluteError)
	fmt.Fprintf(&b, "\tMeanRelative: %.8f,\n", manifest.MeanRelative)
	fmt.Fprintf(&b, "\tRankingAgreement: %.8f,\n", manifest.RankingAgreement)
	fmt.Fprintf(&b, "\tFingerprint: %q,\n", manifest.Fingerprint)
	b.WriteString("}\n")
	return b.String()
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	defer os.Remove(tmp)
	return os.Rename(tmp, path)
}

func manifestFromMetrics(source string, metrics smart.DistillMetrics) smart.DistilledArtifactManifest {
	return smart.DistilledArtifactManifest{
		Version:          artifactVersion,
		Source:           source,
		TrainingSamples:  metrics.TrainingSamples,
		HoldoutSamples:   metrics.HoldoutSamples,
		MAE:              metrics.MeanAbsoluteError,
		RMSE:             metrics.RMSE,
		P95AbsoluteError: metrics.P95AbsoluteError,
		MeanRelative:     metrics.MeanRelativeError,
		RankingAgreement: metrics.RankingAgreement,
	}
}

func main() {
	defaultPolicy := smart.DefaultDistillQualityPolicy()
	input := flag.String("input", "", "smart_weight_data.csv; raw rows are never written to the generated artifact")
	output := flag.String("output", "component/smart/distilled_expert_model_gen.go", "generated Go artifact")
	snapshotPath := flag.String("snapshot", "component/smart/distilled_expert_snapshot.json", "anonymous sufficient-statistics snapshot")
	target := flag.String("target", "actual", "distillation target: actual, expert, teacher, or hybrid")
	teacherPath := flag.String("teacher-model", "", "LightGBM Model.bin for teacher/hybrid distillation")
	teacherShare := flag.Float64("teacher-share", 0.6, "teacher share in hybrid targets")
	ridge := flag.Float64("ridge", 8, "ridge strength around analytic expert prior")
	verify := flag.Bool("verify", false, "verify committed artifact from analytic baseline or anonymous snapshot")
	minHoldout := flag.Int("min-holdout", defaultPolicy.MinHoldout, "minimum holdout samples")
	maxMAE := flag.Float64("max-mae", defaultPolicy.MaxMAE, "maximum holdout MAE")
	maxRMSE := flag.Float64("max-rmse", defaultPolicy.MaxRMSE, "maximum holdout RMSE")
	maxP95 := flag.Float64("max-p95", defaultPolicy.MaxP95AbsoluteError, "maximum holdout P95 absolute error")
	maxRelative := flag.Float64("max-relative", defaultPolicy.MaxMeanRelative, "maximum mean relative error")
	minRanking := flag.Float64("min-ranking", defaultPolicy.MinRankingAgreement, "minimum pairwise ranking agreement")
	flag.Parse()

	policy := smart.DistillQualityPolicy{
		MinHoldout:          *minHoldout,
		MaxMAE:              *maxMAE,
		MaxRMSE:             *maxRMSE,
		MaxP95AbsoluteError: *maxP95,
		MaxMeanRelative:     *maxRelative,
		MinRankingAgreement: *minRanking,
	}

	var coeff [8][smart.ExpertFeatureDimension]float64
	calibration := onesCalibration()
	var manifest smart.DistilledArtifactManifest
	var snapshot *smart.DistillSufficientStats

	if *verify {
		if data, err := os.ReadFile(*snapshotPath); err == nil {
			var stats smart.DistillSufficientStats
			if err := json.Unmarshal(data, &stats); err != nil {
				panic(err)
			}
			result := smart.FitDistilledExpertFromStats(stats)
			coeff = result.Coefficients
			manifest = manifestFromMetrics(stats.Source, stats.Metrics)
			snapshot = &stats
		} else if !os.IsNotExist(err) {
			panic(err)
		} else {
			coeff = smart.CompileExpertDistillation()
			manifest = smart.DistilledArtifactManifest{
				Version: artifactVersion, Source: "analytic", RankingAgreement: 1,
			}
		}
		content := render(coeff, calibration, manifest)
		existing, err := os.ReadFile(*output)
		if err != nil {
			panic(err)
		}
		if string(existing) != content {
			fmt.Fprintln(os.Stderr, "distilled expert artifact is stale or does not match its snapshot")
			os.Exit(1)
		}
		if snapshot != nil {
			if err := smart.ValidateDistillQuality(snapshot.Metrics, policy); err != nil {
				fmt.Fprintf(os.Stderr, "snapshot quality gate failed: %v\n", err)
				os.Exit(1)
			}
		}
		fmt.Printf("verified %s (%s)\n", *output, manifest.Source)
		return
	}

	if *input == "" {
		coeff = smart.CompileExpertDistillation()
		manifest = smart.DistilledArtifactManifest{
			Version: artifactVersion, Source: "analytic", RankingAgreement: 1,
		}
	} else {
		rows, err := loadRows(*input)
		if err != nil {
			panic(err)
		}

		var teacher *lightgbm.WeightModel
		if *target == "teacher" || *target == "hybrid" {
			if *teacherPath == "" {
				panic("-teacher-model is required for teacher/hybrid targets")
			}
			teacher, err = lightgbm.LoadWeightModelFromPath(*teacherPath)
			if err != nil {
				panic(err)
			}
		}

		training, err := trainingSamples(rows, *target, teacher, *teacherShare)
		if err != nil {
			panic(err)
		}
		stats, holdout := smart.BuildDistillSufficientStats(training, *ridge, "production:"+strings.ToLower(*target))
		result := smart.FitDistilledExpertFromStats(stats)
		result.Metrics = smart.EvaluateDistilledExpertModel(result.Coefficients, holdout)
		result.Metrics.TrainingSamples = 0
		for _, n := range result.BucketTrain {
			result.Metrics.TrainingSamples += n
		}
		stats.Metrics = result.Metrics

		if err := smart.ValidateDistillQuality(result.Metrics, policy); err != nil {
			fmt.Fprintf(os.Stderr, "refusing to publish distilled artifact: %v\n", err)
			os.Exit(2)
		}
		coeff = result.Coefficients
		manifest = manifestFromMetrics(stats.Source, result.Metrics)
		snapshot = &stats
	}

	if err := smart.ValidateDistilledArtifactValues(coeff, calibration, manifest); err != nil {
		panic(err)
	}
	content := render(coeff, calibration, manifest)

	// Write the anonymous snapshot first. The production artifact is replaced
	// only after fitting and all quality gates have passed.
	if snapshot != nil {
		data, err := json.MarshalIndent(snapshot, "", "  ")
		if err != nil {
			panic(err)
		}
		data = append(data, '\n')
		if err := writeAtomic(*snapshotPath, data, 0o644); err != nil {
			panic(err)
		}
	}
	if err := writeAtomic(*output, []byte(content), 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("wrote %s source=%s train=%d holdout=%d mae=%.5f rmse=%.5f rank=%.4f\n",
		*output, manifest.Source, manifest.TrainingSamples, manifest.HoldoutSamples,
		manifest.MAE, manifest.RMSE, manifest.RankingAgreement,
	)
}
