package netstate

import "testing"

func TestEpochAdvancesAndInvalidatesCachedProfile(t *testing.T) {
	before := CurrentEpoch()
	after := Advance()
	if after <= before {
		t.Fatalf("epoch did not advance: before=%d after=%d", before, after)
	}
	if got := Local().Epoch; got != after {
		t.Fatalf("local profile epoch=%d want=%d", got, after)
	}
}
