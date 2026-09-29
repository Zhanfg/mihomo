package smart

import (
	"math"
	"testing"
)

func expertTestInput(scene sceneKind, udp bool) *ModelInput {
	in := &ModelInput{
		Success: 64, Failure: 3,
		ConnectTime: 90, Latency: 120,
		MaxuploadRate: 900, MaxdownloadRate: 4200,
		HistoryMaxUploadRate: 700, HistoryMaxDownloadRate: 3500,
		LossRate: 0.003, EmaLossRate: 0.004,
		IsUDP: udp, IsTCP: !udp,
		ConnectionDuration: 2,
		UploadTotal: 1, DownloadTotal: 3,
	}
	switch scene {
	case sceneInteractive:
		in.ConnectionDuration = 5
		in.MaxuploadRate = 500
		in.MaxdownloadRate = 700
		in.UploadTotal = 1
		in.DownloadTotal = 1
		in.Latency = 60
	case sceneStreaming:
		in.ConnectionDuration = 12
		in.DownloadTotal = 80
		in.MaxdownloadRate = 6000
	case sceneTransfer:
		in.ConnectionDuration = 4
		in.UploadTotal = 60
		in.DownloadTotal = 10
		in.MaxuploadRate = 7000
	}
	return in
}

func TestDistilledExpertMatchesFullEnsemble(t *testing.T) {
	if err := DistillationCoefficientError(); err > 1e-7 {
		t.Fatalf("distilled coefficient drift=%g", err)
	}
	for scene := sceneWeb; scene <= sceneTransfer; scene++ {
		for _, udp := range []bool{false, true} {
			input := expertTestInput(scene, udp)
			full := ExpertTeacherPrior(input, 1)
			distilled := DistilledExpertPrior(input, 1)
			if rel := ExpertDisagreement(full, distilled); rel > 1e-7 {
				t.Fatalf("scene=%v udp=%v full=%v distilled=%v disagreement=%g", scene, udp, full, distilled, rel)
			}
		}
	}
}

func TestExpertRoutingEmphasizesSceneObjective(t *testing.T) {
	interactive := expertTestInput(sceneInteractive, true)
	transfer := expertTestInput(sceneTransfer, false)

	ix := ExpertFeatures(interactive)
	tx := ExpertFeatures(transfer)
	interactiveGate := expertGate(sceneInteractive, true)
	transferGate := expertGate(sceneTransfer, false)

	if interactiveGate[ExpertLatency] <= interactiveGate[ExpertThroughput] {
		t.Fatal("interactive route does not prioritize latency expert")
	}
	if transferGate[ExpertThroughput] <= transferGate[ExpertLatency] {
		t.Fatal("transfer route does not prioritize throughput expert")
	}
	if dotExpert(expertCoefficients[ExpertLatency], ix) <= 0 || dotExpert(expertCoefficients[ExpertThroughput], tx) <= 0 {
		t.Fatal("expert produced invalid score")
	}
}

func TestProductionDistillationCalibrationIsBoundedAndAnonymous(t *testing.T) {
	input := *expertTestInput(sceneStreaming, false)
	samples := make([]DistillProductionSample, 64)
	for i := range samples {
		samples[i] = DistillProductionSample{Input: input, ActualWeight: 10}
	}
	cal := FitProductionDistillationCalibration(samples)
	bucket := DistilledExpertBucket(&input)
	if math.Abs(cal[bucket]-1.15) > 1e-12 {
		t.Fatalf("production scale=%v want cap=1.15", cal[bucket])
	}
	for i, v := range cal {
		if i != bucket && v != 1 {
			t.Fatalf("unobserved bucket %d changed to %v", i, v)
		}
	}
}

func BenchmarkExpertTeacherPrior(b *testing.B) {
	input := expertTestInput(sceneInteractive, true)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ExpertTeacherPrior(input, 1)
	}
}

func BenchmarkDistilledExpertPrior(b *testing.B) {
	input := expertTestInput(sceneInteractive, true)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = DistilledExpertPrior(input, 1)
	}
}
