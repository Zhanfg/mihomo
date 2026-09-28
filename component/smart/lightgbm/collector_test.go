package lightgbm

import (
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
