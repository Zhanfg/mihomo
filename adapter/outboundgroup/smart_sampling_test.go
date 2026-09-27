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


func TestStableTCPSamplePlanSplitsLinkAuditFromFullLearning(t *testing.T) {
	var slot smartSampleSlot
	const fp = uint64(0x123401)
	want := []smartSamplePlan{
		{statsScale: 1, observeLink: true},
		{statsScale: 0, observeLink: true},
		{statsScale: 0, observeLink: false},
		{statsScale: 4, observeLink: true},
		{statsScale: 0, observeLink: false},
		{statsScale: 0, observeLink: true},
		{statsScale: 0, observeLink: false},
		{statsScale: 4, observeLink: true},
	}
	for i, expected := range want {
		got := stableSamplePlan(&slot, fp, true, false)
		if got != expected {
			t.Fatalf("sample %d plan=%+v want=%+v", i+1, got, expected)
		}
	}
}

func TestStableUDPSamplePlanKeepsHalfRateWithoutTCPAudit(t *testing.T) {
	var slot smartSampleSlot
	const fp = uint64(0x223402)
	wantScale := []int64{1, 2, 0, 2, 0, 2}
	for i, expected := range wantScale {
		got := stableSamplePlan(&slot, fp, true, true)
		if got.statsScale != expected || got.observeLink {
			t.Fatalf("sample %d plan=%+v wantScale=%d and no TCP audit", i+1, got, expected)
		}
	}
}

func TestTCPAuditPromotesHiddenTransportAnomaly(t *testing.T) {
	base := smartSamplePlan{statsScale: 0, observeLink: true}
	weak := &tcpstats.Stats{RTTUsec: 500_000, RTTVarUsec: 120_000}
	got := promoteTCPAudit(base, weak)
	if got.statsScale != 1 || !got.observeLink {
		t.Fatalf("weak audit must become full sample: %+v", got)
	}

	healthy := &tcpstats.Stats{RTTUsec: 70_000, RTTVarUsec: 3_000}
	got = promoteTCPAudit(base, healthy)
	if got != base {
		t.Fatalf("healthy audit should remain link-only: got=%+v want=%+v", got, base)
	}
}

func TestCheapAbnormalSignalBypassesStableSampler(t *testing.T) {
	if !informativeCheapSample(900, 0, 0, 0, 1_000, nil) {
		t.Fatal("slow connect must remain full fidelity before TCP_INFO")
	}
	if !informativeCheapSample(50, 50, 3<<20, 0, 1_000, nil) {
		t.Fatal("large flow must remain full fidelity before TCP_INFO")
	}
	if informativeCheapSample(50, 50, 16<<10, 16<<10, 1_000, nil) {
		t.Fatal("healthy short flow should be eligible for staged sampling")
	}
}
