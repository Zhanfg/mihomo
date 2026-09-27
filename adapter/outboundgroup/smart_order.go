package outboundgroup

// stableDelayBand maps a measured delay into a deterministic tolerance band.
// The old pairwise "abs(a-b)<=tolerance" comparator was not transitive:
// A could tie B and B tie C while A still ranked before/after C. Any sorting or
// bounded top-K algorithm built on such a relation can become input-order
// dependent. Quantized bands form a total preorder; provider order breaks ties.
//
// Bands are rounded to the nearest width so ordinary jitter around a nominal
// delay tends to stay in one band. Winner hysteresis is handled separately by
// stabilizeSmartOrder and the live tunnel assessment.
func stableDelayBand(delay, tolerance uint16) uint32 {
	if tolerance == 0 {
		return uint32(delay)
	}
	width := uint32(tolerance) + 1
	return (uint32(delay) + width/2) / width
}

func stableDelayLess(aDelay uint16, aIndex int, bDelay uint16, bIndex int, tolerance uint16) bool {
	aBand := stableDelayBand(aDelay, tolerance)
	bBand := stableDelayBand(bDelay, tolerance)
	if aBand != bBand {
		return aBand < bBand
	}
	return aIndex < bIndex
}
