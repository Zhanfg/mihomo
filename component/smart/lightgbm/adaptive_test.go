package lightgbm

import (
	"testing"
	"time"

	"github.com/metacubex/mihomo/component/smart"
)

func TestAdaptiveModelConfidenceGrowsWithLocalEvidence(t *testing.T) {
	now := time.Now().Unix()
	sparse := &smart.ModelInput{Success: 2, LastUsed: now}
	mature := &smart.ModelInput{Success: 64, LastUsed: now}

	sparseConfidence := adaptiveModelConfidence(sparse)
	matureConfidence := adaptiveModelConfidence(mature)
	if !(matureConfidence > sparseConfidence) {
		t.Fatalf("mature confidence %.4f must exceed sparse confidence %.4f", matureConfidence, sparseConfidence)
	}
	if matureConfidence > 0.90 {
		t.Fatalf("confidence must keep a local-evidence floor, got %.4f", matureConfidence)
	}
}

func TestAdaptiveModelConfidenceRespondsToCurrentFailureAndLoss(t *testing.T) {
	now := time.Now().Unix()
	healthy := &smart.ModelInput{Success: 32, Failure: 2, LastUsed: now}
	failed := *healthy
	failed.ConnectionFailed = true
	lossy := *healthy
	lossy.EmaLossRate = 0.10

	base := adaptiveModelConfidence(healthy)
	if got := adaptiveModelConfidence(&failed); got >= base {
		t.Fatalf("current failure must reduce model confidence: base %.4f failed %.4f", base, got)
	}
	if got := adaptiveModelConfidence(&lossy); got >= base {
		t.Fatalf("recent loss must reduce model confidence: base %.4f lossy %.4f", base, got)
	}
}

func TestAdaptiveModelConfidenceDownweightsStaleHistory(t *testing.T) {
	recent := &smart.ModelInput{Success: 48, Failure: 2, LastUsed: time.Now().Unix()}
	stale := *recent
	stale.LastUsed = time.Now().Add(-7 * 24 * time.Hour).Unix()

	if got, wantLessThan := adaptiveModelConfidence(&stale), adaptiveModelConfidence(recent); got >= wantLessThan {
		t.Fatalf("stale confidence %.4f must be below recent %.4f", got, wantLessThan)
	}
}
