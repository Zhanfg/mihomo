package smart

import "testing"

func TestLinkQualityFactorHealthyIsNeutral(t *testing.T) {
	got := LinkQualityFactor(80, 5, 0, 2, 0, 20)
	if got != 1 {
		t.Fatalf("healthy link factor=%v, want 1", got)
	}
}

func TestLinkQualityFactorWeakPathLowersConfidence(t *testing.T) {
	got := LinkQualityFactor(800, 400, 0.05, 40, 5, 10)
	if got >= 0.85 {
		t.Fatalf("weak link factor=%v, want < 0.85", got)
	}
}

func TestLinkQualityFactorIsBounded(t *testing.T) {
	got := LinkQualityFactor(5000, 5000, 1, 1000, 1000, 1)
	if got < 0.60 || got > 1.0 {
		t.Fatalf("factor=%v outside [0.60,1.0]", got)
	}
}
