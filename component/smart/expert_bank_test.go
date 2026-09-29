package smart

import (
	"math"
	"testing"
)

func expertTestInput(i int, udp bool) *ModelInput {
	latency := int64(40 + (i%17)*18)
	connect := int64(25 + (i%11)*12)
	return &ModelInput{
		Success: 64 + int64(i%31), Failure: int64(i % 3),
		ConnectTime: connect, Latency: latency,
		UploadTotal: 1 + float64(i%7),
		DownloadTotal: 3 + float64(i%13),
		MaxuploadRate: 200 + float64((i*37)%1800),
		MaxdownloadRate: 500 + float64((i*83)%6000),
		HistoryMaxUploadRate: 700,
		HistoryMaxDownloadRate: 2600,
		ConnectionDuration: 0.5 + float64(i%19)/3,
		IsUDP: udp, IsTCP: !udp,
		LossRate: float64(i%5) * 0.001,
		EmaLossRate: float64(i%3) * 0.001,
	}
}

func syntheticTeacher(prior float64, x [OnlineBanditDimension]float64) float64 {
	residual := 0.12*x[0] - 0.06*x[2] + 0.09*x[3] + 0.05*x[4]
	return prior * math.Exp(residual)
}

func TestDistilledExpertConvergesTowardTeacher(t *testing.T) {
	bank := NewExpertBank()
	for i := 0; i < 600; i++ {
		input := expertTestInput(i, false)
		prior := 0.55 + float64(i%9)*0.025
		teacher := syntheticTeacher(prior, OnlineBanditFeatures(input))
		bank.Distill(input, prior, teacher)
	}

	var errSum float64
	var readyCount int
	for i := 700; i < 820; i++ {
		input := expertTestInput(i, false)
		prior := 0.61 + float64(i%7)*0.02
		want := syntheticTeacher(prior, OnlineBanditFeatures(input))
		got, confidence, ready := bank.Predict(input, prior)
		if ready {
			readyCount++
			errSum += math.Abs(math.Log(got / want))
			if confidence <= 0 {
				t.Fatal("ready expert has no confidence")
			}
		}
	}
	if readyCount < 80 {
		t.Fatalf("too few mature expert predictions: %d", readyCount)
	}
	if mean := errSum / float64(readyCount); mean > 0.08 {
		t.Fatalf("distilled expert error too high: mean log error=%v", mean)
	}
}

func TestExpertBankPersistenceIsConstantSize(t *testing.T) {
	bank := NewExpertBank()
	for i := 0; i < 20_000; i++ {
		input := expertTestInput(i, i&1 == 1)
		prior := 0.7
		teacher := syntheticTeacher(prior, OnlineBanditFeatures(input))
		bank.Distill(input, prior, teacher)
	}
	data, err := bank.MarshalBounded()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > ExpertMaxPersistBytes {
		t.Fatalf("expert bank grew beyond hard cap: %d", len(data))
	}
	if len(data) > 12*1024 {
		t.Fatalf("expert bank unexpectedly large: %d", len(data))
	}
	loaded := LoadExpertBank(data)
	if loaded == nil {
		t.Fatal("failed to reload bounded expert bank")
	}
}

func TestExpertPersistenceGenerationDoesNotLoseConcurrentUpdate(t *testing.T) {
	bank := NewExpertBank()
	input := expertTestInput(1, false)
	bank.Distill(input, 0.7, 0.8)
	_, generation, err := bank.marshalForPersist()
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an update that lands while durable I/O is in progress.
	bank.Distill(input, 0.7, 0.82)
	bank.markPersisted(generation)
	if !bank.TakeDirty(1) {
		t.Fatal("concurrent post-snapshot update was incorrectly marked persisted")
	}
}

func TestExpertTeacherCadenceTransitions(t *testing.T) {
	bank := NewExpertBank()
	bank.ObserveTeacherRevision("teacher-v1")
	input := expertTestInput(0, false)
	input.Success = 12
	if !bank.NeedsTeacher(input, 0, false) {
		t.Fatal("cold expert should consult teacher")
	}
	input.Success = 30
	if bank.NeedsTeacher(input, 0, false) != (30%3 == 0) {
		t.Fatal("cold expert bootstrap cadence changed")
	}
	input.Success = 194
	if bank.NeedsTeacher(input, 0.9, true) {
		t.Fatal("mature expert should not refresh before prime cadence")
	}
	input.Success = 194
	input.Failure = 0
	if !bank.NeedsTeacher(&ModelInput{Success: 194, Failure: 0, ConnectionFailed: true}, 0.9, true) {
		t.Fatal("failure must force teacher escalation")
	}
	input = &ModelInput{Success: 97, IsTCP: true}
	if !bank.NeedsTeacher(input, 0.9, true) {
		t.Fatal("mature expert did not receive periodic teacher refresh")
	}
}

func BenchmarkDistilledExpertPredict(b *testing.B) {
	bank := NewExpertBank()
	input := expertTestInput(0, false)
	x := OnlineBanditFeatures(input)
	for i := 0; i < 200; i++ {
		bank.Distill(input, 0.7, syntheticTeacher(0.7, x))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = bank.Predict(input, 0.7)
	}
}

func BenchmarkDistilledExpertUpdate(b *testing.B) {
	bank := NewExpertBank()
	input := expertTestInput(0, false)
	x := OnlineBanditFeatures(input)
	teacher := syntheticTeacher(0.7, x)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bank.Distill(input, 0.7, teacher)
	}
}


func TestExpertBankTeacherRevisionReopensConfidence(t *testing.T) {
	bank := NewExpertBank()
	bank.ObserveTeacherRevision("teacher-v1")
	input := expertTestInput(0, false)
	prior := 0.7
	for i := 0; i < 400; i++ {
		bank.Distill(input, prior, syntheticTeacher(prior, OnlineBanditFeatures(input)))
	}
	_, confidenceBefore, readyBefore := bank.Predict(input, prior)
	if !readyBefore || confidenceBefore < 0.45 {
		t.Fatalf("expert did not mature before revision change: ready=%v conf=%v", readyBefore, confidenceBefore)
	}

	before := bank.Snapshot()
	bank.ObserveTeacherRevision("teacher-v2")
	after := bank.Snapshot()
	if after.TeacherRevision != "teacher-v2" {
		t.Fatalf("teacher revision not updated: %q", after.TeacherRevision)
	}
	idx := expertIndex(input)
	if after.Experts[idx].Distilled != 0 {
		t.Fatalf("expert did not reopen distillation after teacher change: %d", after.Experts[idx].Distilled)
	}
	for i := 0; i < OnlineBanditDimension; i++ {
		if math.Abs(after.Experts[idx].Theta[i]) > math.Abs(before.Experts[idx].Theta[i])*0.21+1e-12 {
			t.Fatalf("expert theta was not softened at dim %d", i)
		}
	}
	if !bank.NeedsTeacher(input, 0, false) {
		t.Fatal("teacher revision change did not force re-distillation")
	}
}

func TestExpertSnapshotRoundTripKeepsTeacherRevisionUnverified(t *testing.T) {
	bank := NewExpertBank()
	bank.ObserveTeacherRevision("teacher-v1")
	input := expertTestInput(0, false)
	for i := 0; i < 100; i++ {
		bank.Distill(input, 0.7, 0.8)
	}
	data, err := bank.MarshalBounded()
	if err != nil {
		t.Fatal(err)
	}
	loaded := LoadExpertBank(data)
	if loaded.Snapshot().TeacherRevision != "teacher-v1" {
		t.Fatal("teacher revision lost across snapshot")
	}
	// Restarted process must verify the current on-disk teacher at least once.
	if !loaded.NeedsTeacher(input, 0.9, true) {
		t.Fatal("reloaded expert bank suppressed teacher before revision verification")
	}
}
