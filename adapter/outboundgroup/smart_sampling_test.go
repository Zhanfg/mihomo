package outboundgroup

import (
	"errors"
	"testing"

	"github.com/metacubex/mihomo/common/atomic"
	"github.com/metacubex/mihomo/component/smart/tcpstats"
)

func TestStableSampleEvery(t *testing.T) {
	if got := stableSampleEvery(false, false); got != 1 {
		t.Fatalf("desktop every=%d", got)
	}
	if got := stableSampleEvery(true, false); got != 4 {
		t.Fatalf("android TCP every=%d", got)
	}
	if got := stableSampleEvery(true, true); got != 2 {
		t.Fatalf("android UDP every=%d", got)
	}
}

func TestSampleCounterScaleKeepsFirstThenOneInFour(t *testing.T) {
	var counter atomic.Uint32
	want := []int64{1, 0, 0, 4, 0, 0, 0, 4}
	for i, expected := range want {
		if got := sampleCounterScale(&counter, 4); got != expected {
			t.Fatalf("sample %d scale=%d want=%d", i+1, got, expected)
		}
	}
}

func TestInformativeStatsAreNeverLowValue(t *testing.T) {
	if !informativeStatsSample(0, 0, 0, 0, 0, nil, errors.New("dial failed")) {
		t.Fatal("failure must be informative")
	}
	if !informativeStatsSample(900, 0, 0, 0, 0, nil, nil) {
		t.Fatal("slow connect must be informative")
	}
	if !informativeStatsSample(0, 0, 0, 0, 0, &tcpstats.Stats{RTTUsec: 500_000}, nil) {
		t.Fatal("high RTT must be informative")
	}
	if informativeStatsSample(80, 60, 16<<10, 32<<10, 1_000, &tcpstats.Stats{RTTUsec: 80_000, RTTVarUsec: 4_000}, nil) {
		t.Fatal("healthy short flow should be sampleable")
	}
}

func TestSampleStripeIsStableAndSeparatesTransport(t *testing.T) {
	a := sampleStripe("target", "node", false)
	b := sampleStripe("target", "node", false)
	u := sampleStripe("target", "node", true)
	if a != b {
		t.Fatalf("stripe unstable: %d != %d", a, b)
	}
	if a == u {
		t.Fatalf("TCP and UDP unexpectedly share stripe %d", a)
	}
}
