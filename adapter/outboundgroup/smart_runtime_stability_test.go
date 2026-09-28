package outboundgroup

import (
	"errors"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	C "github.com/metacubex/mihomo/constant"
)

func TestSmartStatsQueueSaturationDoesNotBlock(t *testing.T) {
	queue := make(chan func(), 1)
	if !tryEnqueueSmartStats(queue, func() {}) {
		t.Fatal("first enqueue should fit")
	}

	start := time.Now()
	if tryEnqueueSmartStats(queue, func() {}) {
		t.Fatal("full queue must drop telemetry instead of blocking")
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("saturated enqueue blocked data path for %s", elapsed)
	}
}

func TestSmartStatsWorkerRecoversTelemetryPanic(t *testing.T) {
	var ran atomic.Bool
	runSmartStatsJob(func() { panic("telemetry boom") })
	runSmartStatsJob(func() { ran.Store(true) })
	if !ran.Load() {
		t.Fatal("worker must continue after recovering a telemetry panic")
	}
}


func TestSmartStatsMetadataCapturesOnlyRequiredFields(t *testing.T) {
	original := &C.Metadata{
		NetWork:        C.UDP,
		DstIP:          netip.MustParseAddr("2001:db8::1"),
		DstGeoIP:       []string{"US"},
		DstIPASN:       "AS64500 example",
		DstPort:        443,
		Host:           "example.com",
		UUID:           "uuid-1",
		SmartBlock:     "degraded",
		SmartTarget:    "target",
		WildcardTarget: "*.example.com",
		Process:        "must-not-retain",
		ProcessPath:    "/very/large/path",
		RemoteDst:      "unused",
	}
	captured := captureSmartStatsMetadata(original)
	got := captured.materialize()

	if got.NetWork != original.NetWork || got.DstIP != original.DstIP ||
		got.DstIPASN != original.DstIPASN || got.DstPort != original.DstPort ||
		got.Host != original.Host || got.UUID != original.UUID ||
		got.SmartBlock != original.SmartBlock || got.SmartTarget != original.SmartTarget ||
		got.WildcardTarget != original.WildcardTarget {
		t.Fatalf("compact metadata lost required fields: %#v", got)
	}
	if len(got.DstGeoIP) != 1 || got.DstGeoIP[0] != "US" {
		t.Fatalf("compact metadata lost GeoIP: %#v", got.DstGeoIP)
	}
	if got.Process != "" || got.ProcessPath != "" || got.RemoteDst != "" {
		t.Fatal("stats event retained unrelated process/remote metadata")
	}
}

func TestSmartStatsEventQueueSaturationDoesNotBlock(t *testing.T) {
	queue := make(chan smartStatsEvent, 1)
	event := smartStatsEvent{err: errors.New("sample")}
	if !tryEnqueueSmartStatsEvent(queue, event) {
		t.Fatal("first event enqueue should fit")
	}
	start := time.Now()
	if tryEnqueueSmartStatsEvent(queue, event) {
		t.Fatal("full event queue must drop telemetry")
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("typed event queue blocked data path for %s", elapsed)
	}
}

func BenchmarkSmartStatsClosureQueue(b *testing.B) {
	queue := make(chan func(), 1)
	var sink int64
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		value := int64(i)
		job := func() { sink = value }
		if !tryEnqueueSmartStats(queue, job) {
			b.Fatal("enqueue failed")
		}
		(<-queue)()
	}
	_ = sink
}

func BenchmarkSmartStatsEventQueue(b *testing.B) {
	queue := make(chan smartStatsEvent, 1)
	event := smartStatsEvent{
		meta: smartStatsMetadata{
			network: C.TCP,
			dstIP:   netip.MustParseAddr("1.1.1.1"),
			host:    "example.com",
		},
		connectTime: 100,
		latency:     120,
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		event.sampleScale = int64(i) + 1
		if !tryEnqueueSmartStatsEvent(queue, event) {
			b.Fatal("enqueue failed")
		}
		_ = <-queue
	}
}


func BenchmarkCaptureSmartStatsMetadata(b *testing.B) {
	metadata := &C.Metadata{
		NetWork:        C.TCP,
		DstIP:          netip.MustParseAddr("1.1.1.1"),
		DstGeoIP:       []string{"US"},
		DstIPASN:       "AS13335 Cloudflare",
		DstPort:        443,
		Host:           "example.com",
		UUID:           "bench",
		SmartTarget:    "example.com",
		WildcardTarget: "example.com",
		Process:        "large-process-name-that-must-not-be-retained",
		ProcessPath:    "/data/user/0/example/files/something",
	}
	b.ReportAllocs()
	var sink smartStatsMetadata
	for i := 0; i < b.N; i++ {
		sink = captureSmartStatsMetadata(metadata)
	}
	_ = sink
}
