package smart

import (
	"math"
	"testing"

	"github.com/metacubex/mihomo/common/lru"
)

func testBanditInput() *ModelInput {
	return &ModelInput{
		Success: 40, Failure: 2,
		ConnectTime: 80, Latency: 120,
		MaxuploadRate: 512, MaxdownloadRate: 4096,
		HistoryMaxUploadRate: 384, HistoryMaxDownloadRate: 3000,
		LossRate: 0.002, EmaLossRate: 0.004,
	}
}

func TestOnlineBanditLearnsPositiveResidual(t *testing.T) {
	input := testBanditInput()
	x := OnlineBanditFeatures(input)
	state := OnlineBanditState{}
	for i := range state.Precision {
		state.Precision[i] = 1
	}

	prior := 0.65
	before, beforeU := state.Predict(prior, x)
	for i := 0; i < 80; i++ {
		state.Update(prior, 0.95, x, 1)
	}
	after, afterU := state.Predict(prior, x)
	if after <= before {
		t.Fatalf("student did not learn positive residual: before=%v after=%v", before, after)
	}
	if afterU >= beforeU {
		t.Fatalf("uncertainty did not fall with evidence: before=%v after=%v", beforeU, afterU)
	}
	if after > prior*1.43 {
		t.Fatalf("student escaped residual safety bound: %v", after)
	}
}

func TestOnlineBanditLearnsNegativeResidual(t *testing.T) {
	input := testBanditInput()
	input.ConnectionFailed = true
	input.Failure += 8
	x := OnlineBanditFeatures(input)
	state := OnlineBanditState{}
	for i := range state.Precision {
		state.Precision[i] = 1
	}

	prior := 0.85
	for i := 0; i < 80; i++ {
		state.Update(prior, 0.15, x, 1)
	}
	after, _ := state.Predict(prior, x)
	if after >= prior {
		t.Fatalf("student did not suppress persistently bad path: prior=%v after=%v", prior, after)
	}
	if after < prior*0.70 {
		t.Fatalf("student escaped negative residual safety bound: %v", after)
	}
}

func TestObservedRewardDoesNotDependOnPrediction(t *testing.T) {
	input := testBanditInput()
	a := ObserveConnectionReward(input, 1)
	b := ObserveConnectionReward(input, 1)
	if a != b || a <= 0 {
		t.Fatalf("reward must be deterministic connection truth: a=%v b=%v", a, b)
	}

	input.ConnectionFailed = true
	failed := ObserveConnectionReward(input, 1)
	if failed >= a {
		t.Fatalf("failed connection reward=%v should be below healthy=%v", failed, a)
	}
}

func TestOnlineBanditForgetsConfidenceUnderPersistentError(t *testing.T) {
	input := testBanditInput()
	x := OnlineBanditFeatures(input)
	state := OnlineBanditState{}
	for i := range state.Precision {
		state.Precision[i] = 1
	}

	for i := 0; i < 120; i++ {
		state.Update(0.7, 0.72, x, 1)
	}
	_, stableU := state.Predict(0.7, x)

	// Abrupt environment change: persistent large residual increases model
	// error and activates stronger confidence forgetting.
	for i := 0; i < 24; i++ {
		state.Update(0.7, 0.20, x, 1)
	}
	_, driftU := state.Predict(0.7, x)
	if state.ErrorEWMA < 0.15 {
		t.Fatalf("drift was not detected: error=%v", state.ErrorEWMA)
	}
	if driftU < stableU {
		t.Fatalf("confidence should not keep tightening through drift: stable=%v drift=%v", stableU, driftU)
	}
}

func TestExplorationBonusBounded(t *testing.T) {
	if got := ExplorationBonus(1, 1, 1); got > 1.0800001 {
		t.Fatalf("exploration exceeded 8%% cap: %v", got)
	}
	if got := ExplorationBonus(1, 1, 0); got != 1 {
		t.Fatalf("zero exploration changed score: %v", got)
	}
}

func BenchmarkOnlineBanditPredict(b *testing.B) {
	x := OnlineBanditFeatures(testBanditInput())
	state := OnlineBanditState{}
	for i := range state.Precision {
		state.Precision[i] = 8
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = state.Predict(0.8, x)
	}
}

func BenchmarkOnlineBanditUpdate(b *testing.B) {
	x := OnlineBanditFeatures(testBanditInput())
	state := OnlineBanditState{}
	for i := range state.Precision {
		state.Precision[i] = 8
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		state.Update(0.8, 0.82, x, 1)
	}
}


func TestOnlineBanditStatePersistsInStatsRecord(t *testing.T) {
	record := &AtomicStatsRecord{weights: lru.New[string, float64](lru.WithSize[string, float64](100))}
	state := OnlineBanditState{Updates: 17, ErrorEWMA: 0.12, Epoch: 7, Generation: 12345, TeacherRatio: 1.08, TeacherAt: 12, TeacherProbeAt: 12}
	for i := 0; i < OnlineBanditDimension; i++ {
		state.Theta[i] = float64(i+1) * 0.01
		state.Precision[i] = float64(i + 2)
	}
	SaveOnlineBanditState(record, false, state, 0.42)
	got := LoadOnlineBanditState(record, false)

	if math.Abs(got.Updates-state.Updates) > 1e-12 ||
		math.Abs(got.ErrorEWMA-state.ErrorEWMA) > 1e-12 ||
		math.Abs(got.Epoch-state.Epoch) > 1e-12 ||
		math.Abs(got.Generation-state.Generation) > 1e-12 ||
		math.Abs(got.TeacherRatio-state.TeacherRatio) > 1e-12 ||
		math.Abs(got.TeacherAt-state.TeacherAt) > 1e-12 ||
		math.Abs(got.TeacherProbeAt-state.TeacherProbeAt) > 1e-12 ||
		math.Abs(got.Uncertainty-0.42) > 1e-12 {
		t.Fatalf("scalar state mismatch: got=%+v want=%+v", got, state)
	}
	for i := 0; i < OnlineBanditDimension; i++ {
		if math.Abs(got.Theta[i]-state.Theta[i]) > 1e-12 || math.Abs(got.Precision[i]-state.Precision[i]) > 1e-12 {
			t.Fatalf("dimension %d mismatch: got=%+v want=%+v", i, got, state)
		}
	}
	if gotU := record.banditUncertaintyTCP.Load(); math.Abs(gotU-0.42) > 1e-12 {
		t.Fatalf("atomic uncertainty=%v want=0.42", gotU)
	}
	if legacy := record.GetWeight(BanditUncertaintyWeightType(false)); legacy != 0 {
		t.Fatalf("compact state leaked uncertainty into generic weights: %v", legacy)
	}
}


func TestOnlineBanditEpochShiftReopensConfidence(t *testing.T) {
	x := OnlineBanditFeatures(testBanditInput())
	state := OnlineBanditState{Epoch: 11, Generation: processBanditGeneration, ErrorEWMA: 0.02}
	for i := 0; i < OnlineBanditDimension; i++ {
		state.Theta[i] = 0.20
		state.Precision[i] = 100
	}
	beforeWeight, beforeU := state.Predict(0.8, x)
	state.ObserveEpoch(12)
	afterWeight, afterU := state.Predict(0.8, x)

	if state.Epoch != 12 {
		t.Fatalf("epoch=%v want=12", state.Epoch)
	}
	if afterU <= beforeU {
		t.Fatalf("handover did not reopen confidence: before=%v after=%v", beforeU, afterU)
	}
	if state.ErrorEWMA < 0.15 {
		t.Fatalf("handover did not raise adaptation pressure: %v", state.ErrorEWMA)
	}
	if afterWeight == 0 || afterWeight >= beforeWeight {
		t.Fatalf("handover should retain but soften learned residual: before=%v after=%v", beforeWeight, afterWeight)
	}
}


func TestOnlineBanditReopensConfidenceAcrossProcessRestart(t *testing.T) {
	oldGeneration := processBanditGeneration
	processBanditGeneration = 424242
	t.Cleanup(func() { processBanditGeneration = oldGeneration })

	state := OnlineBanditState{
		Updates:    200,
		ErrorEWMA: 0.02,
		Epoch:      1,
		Generation: 123456, // persisted by a previous core process
	}
	for i := 0; i < OnlineBanditDimension; i++ {
		state.Theta[i] = 0.30
		state.Precision[i] = 512
	}

	state.ObserveEpoch(1) // same numeric epoch, different process generation

	if state.Generation != processBanditGeneration {
		t.Fatalf("generation=%v want=%v", state.Generation, processBanditGeneration)
	}
	if state.Epoch != 1 {
		t.Fatalf("epoch=%v want=1", state.Epoch)
	}
	if state.Precision[0] >= 512 {
		t.Fatalf("restart did not reopen confidence: precision=%v", state.Precision[0])
	}
	if state.Theta[0] >= 0.30 {
		t.Fatalf("restart did not soften stale residual: theta=%v", state.Theta[0])
	}
	if state.ErrorEWMA < 0.15 {
		t.Fatalf("restart did not raise adaptation pressure: error=%v", state.ErrorEWMA)
	}
}


func TestLegacyBanditWeightsMigrateToCompactState(t *testing.T) {
	weights := map[string]float64{
		banditThetaTCP[0]:                 0.11,
		banditPrecisionTCP[0]:             9,
		banditUpdatesKey(false):           12,
		banditErrorKey(false):             0.08,
		banditEpochKey(false):             3,
		banditGenerationKey(false):        999,
		BanditUncertaintyWeightType(false): 0.35,
		"tcp":                             0.82,
	}
	state, ok := legacyBanditStateFromWeights(weights, false)
	if !ok {
		t.Fatal("legacy state was not detected")
	}
	if math.Abs(state.Theta[0]-0.11) > 1e-12 ||
		math.Abs(state.Precision[0]-9) > 1e-12 ||
		math.Abs(state.Updates-12) > 1e-12 ||
		math.Abs(state.Uncertainty-0.35) > 1e-12 {
		t.Fatalf("legacy migration mismatch: %+v", state)
	}
	if !isLegacyBanditWeightKey(banditThetaTCP[0]) || isLegacyBanditWeightKey("tcp") {
		t.Fatal("legacy key classifier is too broad or too narrow")
	}
}

func BenchmarkOnlineBanditCompactStateRoundTrip(b *testing.B) {
	record := &AtomicStatsRecord{weights: lru.New[string, float64](lru.WithSize[string, float64](100))}
	record.banditUncertaintyTCP.Store(1)
	state := defaultOnlineBanditState()
	state.Updates = 64
	state.Generation = processBanditGeneration
	for i := range state.Precision {
		state.Precision[i] = 8
	}
	// Allocate the one compact state object before measuring steady-state cost.
	SaveOnlineBanditState(record, false, state, 0.4)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got := LoadOnlineBanditState(record, false)
		SaveOnlineBanditState(record, false, got, 0.4)
	}
}


func TestTeacherAnchorIsBoundedAndPersistent(t *testing.T) {
	state := defaultOnlineBanditState()
	if got := state.ApplyTeacherAnchor(0.8); math.Abs(got-0.8) > 1e-12 {
		t.Fatalf("default teacher anchor changed heuristic: %v", got)
	}

	state.ObserveTeacher(2.0, 0.8) // ratio 2.5, must clamp to 1.25
	if math.Abs(state.TeacherRatio-1.25) > 1e-12 {
		t.Fatalf("teacher ratio=%v want=1.25", state.TeacherRatio)
	}
	if got := state.ApplyTeacherAnchor(0.8); math.Abs(got-1.0) > 1e-12 {
		t.Fatalf("anchored prior=%v want=1.0", got)
	}

	previous := state.TeacherRatio
	state.Updates = 20
	state.ObserveTeacher(0.60, 0.80) // ratio 0.75, EMA should move conservatively
	if !(state.TeacherRatio < previous && state.TeacherRatio > 0.75) {
		t.Fatalf("teacher refresh was not conservative: before=%v after=%v", previous, state.TeacherRatio)
	}
	if state.TeacherAt != 21 || state.TeacherProbeAt != 21 {
		t.Fatalf("teacher timestamps not updated: at=%v probe=%v", state.TeacherAt, state.TeacherProbeAt)
	}
}

func TestTeacherRefreshUsesStudentConfidenceAndAge(t *testing.T) {
	input := testBanditInput()
	state := defaultOnlineBanditState()
	state.Updates = 100
	state.TeacherAt = 90
	state.TeacherProbeAt = 90
	state.Uncertainty = 0.20
	state.ErrorEWMA = 0.05

	if ShouldRefreshTeacher(input, 0.05, state) {
		t.Fatal("fresh normal teacher anchor refreshed too early")
	}
	state.Updates = 114 // normal age=24
	if !ShouldRefreshTeacher(input, 0.05, state) {
		t.Fatal("normal teacher anchor did not refresh at age 24")
	}

	state.Updates = 200
	state.TeacherAt = 150
	state.Uncertainty = 0.10
	state.ErrorEWMA = 0.03
	if ShouldRefreshTeacher(input, 0.03, state) {
		t.Fatal("mature confident student refreshed before age 64")
	}
	state.Updates = 214
	if !ShouldRefreshTeacher(input, 0.03, state) {
		t.Fatal("mature confident student did not refresh at age 64")
	}

	state.Updates = 100
	state.TeacherAt = 95
	state.Uncertainty = 0.60
	if ShouldRefreshTeacher(input, 0.03, state) {
		t.Fatal("uncertain student refreshed before age 6")
	}
	state.Updates = 101
	if !ShouldRefreshTeacher(input, 0.03, state) {
		t.Fatal("uncertain student did not refresh at age 6")
	}

	input.ConnectionFailed = true
	state.Uncertainty = 0.20
	state.Updates = 99
	state.TeacherAt = 95
	if !ShouldRefreshTeacher(input, 0.03, state) {
		t.Fatal("failure path did not request teacher refresh at age 4")
	}
}

func TestUnavailableTeacherProbeIsThrottled(t *testing.T) {
	input := testBanditInput()
	state := defaultOnlineBanditState()
	state.Updates = 20
	if !ShouldRefreshTeacher(input, 0, state) {
		t.Fatal("cold teacher was not requested")
	}
	state.NoteTeacherAttempt()
	if ShouldRefreshTeacher(input, 0, state) {
		t.Fatal("teacher retry was not throttled immediately")
	}
	state.Updates = 24
	if !ShouldRefreshTeacher(input, 0, state) {
		t.Fatal("teacher retry did not reopen after four student updates")
	}
}

func TestEnvironmentChangeSoftensTeacherAnchor(t *testing.T) {
	state := defaultOnlineBanditState()
	state.Updates = 100
	state.Epoch = 7
	state.Generation = processBanditGeneration
	state.TeacherRatio = 1.20
	state.TeacherAt = 90
	state.TeacherProbeAt = 90
	for i := range state.Precision {
		state.Precision[i] = 64
	}
	state.ObserveEpoch(8)
	if math.Abs(state.TeacherRatio-1.05) > 1e-12 {
		t.Fatalf("handover teacher ratio=%v want=1.05", state.TeacherRatio)
	}
	if state.TeacherAt != 0 || state.TeacherProbeAt != 0 {
		t.Fatalf("handover did not force teacher refresh: at=%v probe=%v", state.TeacherAt, state.TeacherProbeAt)
	}
}

func BenchmarkShouldRefreshTeacher(b *testing.B) {
	input := testBanditInput()
	state := defaultOnlineBanditState()
	state.Updates = 256
	state.TeacherAt = 220
	state.Uncertainty = 0.10
	state.ErrorEWMA = 0.04
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ShouldRefreshTeacher(input, 0.04, state)
	}
}


func TestMatureStudentKeepsTeacherRefreshSparse(t *testing.T) {
	input := testBanditInput()
	state := defaultOnlineBanditState()
	state.Updates = 128
	state.TeacherRatio = 1.03
	state.TeacherAt = 128
	state.TeacherProbeAt = 128
	state.Uncertainty = 0.10
	state.ErrorEWMA = 0.03

	refreshes := 0
	for i := 0; i < 640; i++ {
		input.Success++
		if ShouldRefreshTeacher(input, 0.03, state) {
			refreshes++
			state.ObserveTeacher(0.82, 0.80)
		}
		state.Updates++
	}
	if refreshes < 8 || refreshes > 11 {
		t.Fatalf("mature teacher refreshes=%d want about 10/640", refreshes)
	}
}

func BenchmarkDistilledSmartDecision(b *testing.B) {
	input := testBanditInput()
	x := OnlineBanditFeatures(input)
	state := defaultOnlineBanditState()
	state.Updates = 256
	state.TeacherRatio = 1.04
	state.TeacherAt = 220
	state.TeacherProbeAt = 220
	state.Uncertainty = 0.10
	state.ErrorEWMA = 0.04
	for i := range state.Precision {
		state.Precision[i] = 32
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		prior := state.ApplyTeacherAnchor(0.80)
		_, _ = state.Predict(prior, x)
		_ = ShouldRefreshTeacher(input, 0.04, state)
	}
}


func TestV7StudentDefaultsToNeutralTeacherAnchor(t *testing.T) {
	// v7 compact records do not have TeacherRatio/TeacherAt fields. Loading
	// them into v8 must be neutral rather than suppressing the heuristic prior.
	state := normalizeOnlineBanditState(OnlineBanditState{
		Updates:     40,
		Uncertainty: 0.20,
	})
	if math.Abs(state.TeacherRatio-1) > 1e-12 {
		t.Fatalf("v7 migration teacher ratio=%v want=1", state.TeacherRatio)
	}
	if got := state.ApplyTeacherAnchor(0.80); math.Abs(got-0.80) > 1e-12 {
		t.Fatalf("neutral migrated anchor changed prior: %v", got)
	}
	if !ShouldRefreshTeacher(testBanditInput(), 0.05, state) {
		t.Fatal("migrated v7 student did not request its first Teacher refresh")
	}
}
