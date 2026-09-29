package smart

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/metacubex/mihomo/common/lru"
)

func requireSmartExtreme(t *testing.T) {
	t.Helper()
	if os.Getenv("SMART_EXTREME") != "1" {
		t.Skip("set SMART_EXTREME=1 to run destructive-scale Smart tests")
	}
}

type extremePRNG uint64

func (r *extremePRNG) next() uint64 {
	x := uint64(*r)
	x ^= x << 13
	x ^= x >> 7
	x ^= x << 17
	*r = extremePRNG(x)
	return x
}

func (r *extremePRNG) unit() float64 {
	return float64(r.next()>>11) / float64(uint64(1)<<53)
}

func extremeModelInput(r *extremePRNG, i int) ModelInput {
	success := int64(r.next() % 100000)
	failure := int64(r.next() % 5000)
	duration := 0.01 + r.unit()*120
	input := ModelInput{
		Success:                   success,
		Failure:                   failure,
		ConnectTime:               int64(1 + r.next()%8000),
		Latency:                   int64(1 + r.next()%12000),
		UploadTotal:               r.unit() * 500,
		HistoryUploadTotal:        r.unit() * 5000,
		MaxuploadRate:             r.unit() * 32768,
		HistoryMaxUploadRate:      r.unit() * 32768,
		DownloadTotal:             r.unit() * 2000,
		HistoryDownloadTotal:      r.unit() * 20000,
		MaxdownloadRate:           r.unit() * 65536,
		HistoryMaxDownloadRate:    r.unit() * 65536,
		ConnectionDuration:        duration,
		HistoryConnectionDuration: r.unit() * 180,
		IsUDP:                     r.next()&1 == 1,
		LossRate:                  r.unit() * 0.35,
		CumulLossRate:             r.unit() * 0.25,
		EmaLossRate:               r.unit() * 0.20,
	}
	input.IsTCP = !input.IsUDP
	if i%8192 == 0 {
		input.Success = math.MaxInt64
		input.Failure = math.MaxInt64
	}
	if i%16384 == 0 {
		input.MaxdownloadRate = math.Inf(1)
		input.UploadTotal = math.NaN()
	}
	return input
}

func TestSmartExtremeMillionCaseDistillationParity(t *testing.T) {
	requireSmartExtreme(t)
	rng := extremePRNG(0x9e3779b97f4a7c15)
	maxDisagreement := 0.0
	for i := 0; i < 1_000_000; i++ {
		input := extremeModelInput(&rng, i)
		full := ExpertTeacherPrior(&input, 1)
		distilled := DistilledExpertPrior(&input, 1)
		if math.IsNaN(full) || math.IsInf(full, 0) || math.IsNaN(distilled) || math.IsInf(distilled, 0) {
			t.Fatalf("non-finite expert output at case %d: full=%v distilled=%v", i, full, distilled)
		}
		disagreement := ExpertDisagreement(full, distilled)
		if disagreement > maxDisagreement {
			maxDisagreement = disagreement
		}
	}
	if maxDisagreement > 2e-7 {
		t.Fatalf("million-case distillation drift=%g", maxDisagreement)
	}
	t.Logf("million_case_max_relative_error=%.12g", maxDisagreement)
}

func TestSmartExtremeAdversarialNumerics(t *testing.T) {
	requireSmartExtreme(t)
	cases := []ModelInput{
		{Success: math.MaxInt64, Failure: math.MaxInt64, ConnectTime: math.MaxInt64, Latency: math.MaxInt64},
		{Success: -1, Failure: -1, ConnectTime: -1, Latency: -1, LossRate: math.NaN(), EmaLossRate: math.Inf(1)},
		{Success: math.MaxInt64, Failure: 1, MaxuploadRate: math.Inf(1), MaxdownloadRate: math.Inf(-1), ConnectionDuration: math.NaN()},
	}
	for i := 0; i < 250_000; i++ {
		in := cases[i%len(cases)]
		in.IsUDP = i&1 == 1
		in.IsTCP = !in.IsUDP
		full := ExpertTeacherPrior(&in, 1)
		distilled := DistilledExpertPrior(&in, 1)
		if full < 0.03 || full > 1.20 || math.IsNaN(full) || math.IsInf(full, 0) {
			t.Fatalf("case=%d invalid full=%v", i, full)
		}
		if distilled < 0.03 || distilled > 1.20 || math.IsNaN(distilled) || math.IsInf(distilled, 0) {
			t.Fatalf("case=%d invalid distilled=%v", i, distilled)
		}
		x := OnlineBanditFeatures(&in)
		for j, value := range x {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				t.Fatalf("case=%d feature=%d non-finite=%v", i, j, value)
			}
		}
	}
}

func TestSmartExtremeHundredHandoversReactionBound(t *testing.T) {
	requireSmartExtreme(t)
	var a, b OnlineBanditState
	a = defaultOnlineBanditState()
	b = defaultOnlineBanditState()
	a.Generation, b.Generation = processBanditGeneration, processBanditGeneration
	a.Epoch, b.Epoch = 1, 1

	x := [OnlineBanditDimension]float64{1, 0.95, 0.9, 0.15, 0.98, 1}
	const prior = 0.70
	const good = 0.95
	const bad = 0.20

	totalReaction := 0
	maxReaction := 0
	totalWrong := 0
	epoch := uint64(1)

	for cycle := 0; cycle < 100; cycle++ {
		epoch++
		a.ObserveEpoch(epoch)
		b.ObserveEpoch(epoch)
		goodA := cycle%2 == 0
		reaction := -1

		for step := 0; step < 50; step++ {
			wa, ua := a.Predict(prior, x)
			wb, ub := b.Predict(prior, x)
			scoreA := ExplorationBonus(wa, ua, 0.06)
			scoreB := ExplorationBonus(wb, ub, 0.06)
			chooseA := scoreA >= scoreB

			selectedGood := chooseA == goodA
			if selectedGood && reaction < 0 {
				reaction = step
			}
			if !selectedGood {
				totalWrong++
			}

			if chooseA {
				reward := bad
				if goodA {
					reward = good
				}
				a.Update(prior, reward, x, 1)
			} else {
				reward := bad
				if !goodA {
					reward = good
				}
				b.Update(prior, reward, x, 1)
			}
		}
		if reaction < 0 {
			t.Fatalf("cycle=%d never found new good arm", cycle)
		}
		totalReaction += reaction
		if reaction > maxReaction {
			maxReaction = reaction
		}
	}
	avgReaction := float64(totalReaction) / 100
	if maxReaction > 12 {
		t.Fatalf("handover max reaction=%d steps", maxReaction)
	}
	if avgReaction > 4 {
		t.Fatalf("handover average reaction=%.2f steps", avgReaction)
	}
	if totalWrong > 550 {
		t.Fatalf("cumulative wrong selections=%d exceeded regret budget", totalWrong)
	}
	t.Logf("handover_max_steps=%d average_steps=%.2f wrong_selections=%d", maxReaction, avgReaction, totalWrong)
}

func TestSmartExtremeStudentPersistenceDoesNotGrow(t *testing.T) {
	requireSmartExtreme(t)
	record := &AtomicStatsRecord{weights: lru.New[string, float64](lru.WithSize[string, float64](100))}
	state := defaultOnlineBanditState()
	state.Generation = processBanditGeneration
	state.Epoch = 1
	x := [OnlineBanditDimension]float64{1, 0.9, 0.8, 0.2, 0.95, 1}

	var baseline int
	for i := 0; i < 250_000; i++ {
		reward := 0.82
		if i%997 == 0 {
			reward = 0.18
		}
		_, uncertainty := state.Update(0.75, reward, x, 1)
		SaveOnlineBanditState(record, false, state, uncertainty)
		if i == 1000 {
			data, err := json.Marshal(record.CreateStatsSnapshot("extreme"))
			if err != nil {
				t.Fatal(err)
			}
			baseline = len(data)
		}
	}
	data, err := json.Marshal(record.CreateStatsSnapshot("extreme"))
	if err != nil {
		t.Fatal(err)
	}
	if baseline == 0 {
		t.Fatal("baseline snapshot was not captured")
	}
	if len(data) > baseline+128 {
		t.Fatalf("student persistence grew with update count: baseline=%d final=%d", baseline, len(data))
	}
	if len(data) > 4096 {
		t.Fatalf("student snapshot unexpectedly large: %d bytes", len(data))
	}
	t.Logf("student_snapshot_bytes=%d", len(data))
}

func TestSmartExtremeRankFiftyThousandCandidates(t *testing.T) {
	requireSmartExtreme(t)
	const count = 50_000
	now := time.Now().Unix()
	stats := make(map[string][]byte, count)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("node-%05d", i)
		record := StatsRecord{
			LastUsed: now,
			Weights: map[string]float64{
				WeightTypeTCP: float64(i+1) / count,
			},
			BanditTCP: &OnlineBanditState{
				Updates:     32,
				Uncertainty: float64((i*17)%100) / 100,
			},
		}
		data, err := json.Marshal(&record)
		if err != nil {
			t.Fatal(err)
		}
		stats[name] = data
	}

	store := NewStore(nil)
	start := time.Now()
	top := store.rankTargetStatsWithExploration("g", "c", "target", stats, false, 10, now, 0.06)
	elapsed := time.Since(start)
	if len(top) != 10 {
		t.Fatalf("top len=%d", len(top))
	}
	if top[0].Weight <= top[len(top)-1].Weight {
		t.Fatalf("top-K order invalid: first=%v last=%v", top[0], top[len(top)-1])
	}
	if elapsed > 15*time.Second {
		t.Fatalf("50k candidate ranking too slow: %v", elapsed)
	}
	t.Logf("rank_50k_elapsed=%s top=%s weight=%.6f", elapsed, top[0].Node, top[0].Weight)
}


func TestSmartExtremePoisonedTeacherIsBounded(t *testing.T) {
	requireSmartExtreme(t)
	const prior = 0.72
	calibration := 1.0
	modelError := 0.0
	for i := 0; i < 100_000; i++ {
		model := 50.0
		if i&1 == 1 {
			model = 1e-12
		}
		weight, nextCalibration, nextError := AdaptModelPredictionWithReliability(
			model, prior, calibration, modelError, 512,
		)
		if weight < prior*0.55-1e-12 || weight > prior*1.45+1e-12 {
			t.Fatalf("poisoned teacher escaped bound at %d: weight=%v", i, weight)
		}
		if math.IsNaN(weight) || math.IsInf(weight, 0) {
			t.Fatalf("poisoned teacher produced non-finite weight at %d", i)
		}
		calibration, modelError = nextCalibration, nextError
	}
}

func TestSmartExtremeExpertConflictEscalatesTeacher(t *testing.T) {
	requireSmartExtreme(t)
	input := extremeModelInput(new(extremePRNG), 1)
	input.Success = 512
	input.Failure = 3
	input.ConnectionFailed = false
	input.LossRate = 0

	original := distilledExpertCalibration
	defer func() { distilledExpertCalibration = original }()

	bucket := DistilledExpertBucket(&input)
	distilledExpertCalibration[bucket] = 1.15
	distilled := DistilledExpertPrior(&input, 1)
	full := ExpertTeacherPrior(&input, 1)
	disagreement := ExpertDisagreement(distilled, full)
	if disagreement < 0.10 {
		t.Fatalf("test did not create material expert disagreement: %v", disagreement)
	}
	if !ShouldInvokeTeacherWithExperts(&input, 0.02, 0.05, 0.03, 1000, disagreement) {
		t.Fatal("material expert disagreement did not escalate heavy teacher")
	}
}
