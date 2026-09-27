package outboundgroup

import (
	"errors"
	"testing"

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

func TestSampleSlotScaleKeepsFirstThenOneInFour(t *testing.T) {
	var slot smartSampleSlot
	const fingerprint = uint64(0x101)
	want := []int64{1, 0, 0, 4, 0, 0, 0, 4}
	for i, expected := range want {
		if got := sampleSlotScale(&slot, fingerprint, 4); got != expected {
			t.Fatalf("sample %d scale=%d want=%d", i+1, got, expected)
		}
	}
}

func TestSampleSlotCollisionRestartsColdStart(t *testing.T) {
	var slot smartSampleSlot
	first := uint64(0x101)
	collision := uint64(0x201)
	if sampleStripe(first) != sampleStripe(collision) {
		t.Fatal("test fingerprints must collide into one slot")
	}

	if got := sampleSlotScale(&slot, first, 4); got != 1 {
		t.Fatalf("first key initial scale=%d", got)
	}
	if got := sampleSlotScale(&slot, first, 4); got != 0 {
		t.Fatalf("first key second scale=%d", got)
	}
	if got := sampleSlotScale(&slot, collision, 4); got != 1 {
		t.Fatalf("colliding new key must retain cold-start sample, got=%d", got)
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

func TestSampleFingerprintIsStableAndSeparatesTransport(t *testing.T) {
	a := sampleFingerprint("target", "node", false)
	b := sampleFingerprint("target", "node", false)
	u := sampleFingerprint("target", "node", true)
	if a != b {
		t.Fatalf("fingerprint unstable: %d != %d", a, b)
	}
	if a == u {
		t.Fatalf("TCP and UDP must have distinct fingerprints: %d", a)
	}
	if sampleStripe(a) >= statsSampleStripes {
		t.Fatalf("stripe out of range: %d", sampleStripe(a))
	}
}
