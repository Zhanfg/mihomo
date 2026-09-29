package lightgbm

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/metacubex/mihomo/component/smart"
)

func TestDataCollectorCloseAllowsReopen(t *testing.T) {
	collector := &DataCollector{
		dataPath:           filepath.Join(t.TempDir(), "samples.csv"),
		smartCollectorSize: defaultSmartCollectorSize,
	}
	if err := collector.initializeWriter(); err != nil {
		t.Fatalf("initialize collector: %v", err)
	}
	if err := collector.Close(); err != nil {
		t.Fatalf("close collector: %v", err)
	}
	if collector.configured || collector.file != nil || collector.writer != nil {
		t.Fatal("close retained state that points at the closed file")
	}
	if err := collector.initializeWriter(); err != nil {
		t.Fatalf("reopen collector: %v", err)
	}
	if err := collector.Close(); err != nil {
		t.Fatalf("close reopened collector: %v", err)
	}
}

func TestInitCollectorKeepsExistingHandlesValid(t *testing.T) {
	collectMutex.Lock()
	previous := smartCollector
	smartCollector = nil
	collectMutex.Unlock()
	t.Cleanup(func() {
		collectMutex.Lock()
		smartCollector = previous
		collectMutex.Unlock()
	})

	InitCollector(1)
	first := GetCollector()
	InitCollector(2)
	second := GetCollector()
	if first != second {
		t.Fatal("collector reconfiguration replaced the shared object")
	}
	if got, want := second.smartCollectorSize, int64(2*1024*1024); got != want {
		t.Fatalf("collector size=%d, want %d", got, want)
	}
}


func TestPrepareFeaturesIntoMatchesAllocatedPath(t *testing.T) {
	input := &smart.ModelInput{
		Success: 20, Failure: 2, ConnectTime: 120, Latency: 80,
		UploadTotal: 1.5, DownloadTotal: 7.5,
		HistoryUploadTotal: 20, HistoryDownloadTotal: 80,
		MaxuploadRate: 500, MaxdownloadRate: 1800,
		HistoryMaxUploadRate: 700, HistoryMaxDownloadRate: 2200,
		ConnectionDuration: 2.5, HistoryConnectionDuration: 8,
		IsTCP: true, LossRate: 0.01, CumulLossRate: 0.02,
		Host: "example.com", DestPort: 443, DestIPASN: "AS13335 Cloudflare",
		DestGeoIP: []string{"US"},
	}
	want := prepareFeatures(input)
	buf := make([]float64, MaxFeatureSize)
	got := prepareFeaturesInto(input, buf[:0])
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pooled feature path differs\n got=%v\nwant=%v", got, want)
	}
}

func TestApplyTransformsInPlaceMatchesCopyPath(t *testing.T) {
	ft := &FeatureTransforms{
		TransformsEnabled: true,
		Transforms: []TransformParams{{
			Type: StandardScalerTransform,
			FeatureIndices: []int{0, 2},
			Parameters: map[string][]float64{
				"mean":  {2, 10},
				"scale": {2, 5},
			},
		}},
	}
	input := []float64{4, 7, 20}
	want := ft.ApplyTransforms(input)
	got := append([]float64(nil), input...)
	if !ft.ApplyTransformsInPlace(got) {
		t.Fatal("in-place transform rejected valid input")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("in-place transform differs: got=%v want=%v", got, want)
	}
}


func TestBoundedCollectorSize(t *testing.T) {
	tests := []struct {
		mb   float64
		want int64
	}{
		{0, defaultSmartCollectorSize},
		{0.1, 1 * 1024 * 1024},
		{32, 32 * 1024 * 1024},
		{64, 64 * 1024 * 1024},
		{1024, maxSmartCollectorSize},
	}
	for _, tc := range tests {
		if got := boundedCollectorSize(tc.mb); got != tc.want {
			t.Fatalf("boundedCollectorSize(%v)=%d want=%d", tc.mb, got, tc.want)
		}
	}
}

func TestCollectorBackupCleanupIsBounded(t *testing.T) {
	dir := t.TempDir()
	dataPath := filepath.Join(dir, "smart_weight_data.csv")
	legacy := []string{
		dataPath + ".bak.20250101010101",
		dataPath + ".bak.20260101010101",
	}
	for _, path := range legacy {
		if err := os.WriteFile(path, []byte("legacy"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fixed := dataPath + ".bak"
	if err := os.WriteFile(fixed, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	cleanupCollectorBackups(dataPath)

	for _, path := range legacy {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("legacy timestamped backup still exists: %s", path)
		}
	}
	if _, err := os.Stat(fixed); err != nil {
		t.Fatalf("bounded fixed backup was removed: %v", err)
	}

	oversized := dataPath + ".bak"
	f, err := os.OpenFile(oversized, os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxSmartCollectorSize + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	cleanupCollectorBackups(dataPath)
	if _, err := os.Stat(oversized); !os.IsNotExist(err) {
		t.Fatal("oversized fixed backup was not removed")
	}
}
