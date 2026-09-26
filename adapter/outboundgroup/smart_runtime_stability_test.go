package outboundgroup

import (
	"sync/atomic"
	"testing"
	"time"
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
