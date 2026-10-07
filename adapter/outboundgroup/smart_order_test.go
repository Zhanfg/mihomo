package outboundgroup

import "testing"

func TestStableDelayBandPreservesExactOrderWithoutTolerance(t *testing.T) {
	for delay := uint16(0); delay < 1000; delay++ {
		if got := stableDelayBand(delay, 0); got != uint32(delay) {
			t.Fatalf("delay=%d band=%d", delay, got)
		}
	}
}

func TestStableDelayLessIsAntisymmetric(t *testing.T) {
	for tolerance := uint16(0); tolerance <= 64; tolerance += 8 {
		for a := uint16(0); a < 128; a += 7 {
			for b := uint16(0); b < 128; b += 9 {
				ab := stableDelayLess(a, 1, b, 2, tolerance)
				ba := stableDelayLess(b, 2, a, 1, tolerance)
				if ab && ba {
					t.Fatalf("both directions true: a=%d b=%d tolerance=%d", a, b, tolerance)
				}
			}
		}
	}
}

func TestStableDelayLessIsTransitive(t *testing.T) {
	for tolerance := uint16(0); tolerance <= 64; tolerance += 8 {
		for a := uint16(0); a < 96; a += 8 {
			for b := uint16(0); b < 96; b += 8 {
				for c := uint16(0); c < 96; c += 8 {
					if stableDelayLess(a, 0, b, 1, tolerance) &&
						stableDelayLess(b, 1, c, 2, tolerance) &&
						!stableDelayLess(a, 0, c, 2, tolerance) {
						t.Fatalf("non-transitive: a=%d b=%d c=%d tolerance=%d", a, b, c, tolerance)
					}
				}
			}
		}
	}
}

func TestStableDelayBandKeepsProviderOrderInsideBand(t *testing.T) {
	const tolerance = uint16(100)
	// Both values land in the same rounded 101 ms band. Provider order should
	// decide rather than tiny health-check jitter.
	a, b := uint16(100), uint16(140)
	if stableDelayBand(a, tolerance) != stableDelayBand(b, tolerance) {
		t.Fatalf("expected same band: %d vs %d", stableDelayBand(a, tolerance), stableDelayBand(b, tolerance))
	}
	if !stableDelayLess(b, 0, a, 1, tolerance) {
		t.Fatal("provider index should win inside one tolerance band")
	}
}
