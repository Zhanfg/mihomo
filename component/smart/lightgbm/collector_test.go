package lightgbm

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/metacubex/mihomo/component/smart"
	C "github.com/metacubex/mihomo/constant"
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


func TestCollectorSchemaIncludesTeacherWeight(t *testing.T) {
	path := filepath.Join(t.TempDir(), "samples.csv")
	collector := &DataCollector{dataPath: path, smartCollectorSize: defaultSmartCollectorSize}
	if err := collector.initializeWriter(); err != nil {
		t.Fatalf("initialize collector: %v", err)
	}
	if err := collector.Close(); err != nil {
		t.Fatalf("close collector: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	header, err := csv.NewReader(f).Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(header) != expectedColumns {
		t.Fatalf("columns=%d want=%d", len(header), expectedColumns)
	}
	found := false
	for _, h := range header {
		if h == "teacher_weight" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("collector schema lost teacher_weight")
	}
}

func TestCollectorUpgradesPreDistillationSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "samples.csv")
	legacy := "success,failure,cumul_loss_rate,weight,weight_source,timestamp\n1,0,0,0.8,Traditional,now\n"
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	collector := &DataCollector{dataPath: path, smartCollectorSize: defaultSmartCollectorSize}
	if err := collector.initializeWriter(); err != nil {
		t.Fatalf("upgrade collector: %v", err)
	}
	if err := collector.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("single bounded legacy backup missing: %v", err)
	}
	matches, err := filepath.Glob(path + ".bak.*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("timestamped legacy backups leaked: %v", matches)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	header, err := csv.NewReader(f).Read()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range header {
		if h == "teacher_weight" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("upgraded schema has no teacher_weight")
	}
}


func TestNormalizeCollectorSizeHardCaps(t *testing.T) {
	if got := normalizeCollectorSize(1<<40, false); got != maxSmartCollectorSize {
		t.Fatalf("desktop hard cap=%d want=%d", got, maxSmartCollectorSize)
	}
	if got := normalizeCollectorSize(1<<40, true); got != maxSmartCollectorSizeAndroid {
		t.Fatalf("android hard cap=%d want=%d", got, maxSmartCollectorSizeAndroid)
	}
	if got := normalizeCollectorSize(2*1024*1024, true); got != 2*1024*1024 {
		t.Fatalf("small explicit budget changed: %d", got)
	}
	if got := normalizeCollectorSize(0, true); got != defaultSmartCollectorSize {
		t.Fatalf("default=%d want=%d", got, defaultSmartCollectorSize)
	}
}

func TestCollectorUpgradeKeepsOneBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "samples.csv")
	for i := 0; i < 3; i++ {
		name := path + ".bak." + string(rune('a'+i))
		if err := os.WriteFile(name, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	legacy := "success,failure,cumul_loss_rate,weight,weight_source,timestamp\n1,0,0,0.8,Traditional,now\n"
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	collector := &DataCollector{dataPath: path, smartCollectorSize: 1024 * 1024}
	if err := collector.initializeWriter(); err != nil {
		t.Fatal(err)
	}
	if err := collector.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("bounded backup missing: %v", err)
	}
	matches, _ := filepath.Glob(path + ".bak.*")
	if len(matches) != 0 {
		t.Fatalf("old timestamped backups survived: %v", matches)
	}
}


func TestCollectorNeverExceedsLogicalByteBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bounded.csv")
	collector := &DataCollector{dataPath: path, smartCollectorSize: defaultSmartCollectorSize}
	if err := collector.initializeWriter(); err != nil {
		t.Fatal(err)
	}
	headerSize := collector.currentSize
	collector.smartCollectorSize = headerSize + 2048

	input := &smart.ModelInput{
		Success: 10, Failure: 1, ConnectTime: 80, Latency: 90,
		IsTCP: true, Host: "example.com", DestPort: 443,
		GroupName: "g", NodeName: "n",
	}
	meta := &C.Metadata{Host: "example.com", DstPort: 443}
	for i := 0; i < 1000; i++ {
		collector.AddSample(input, meta, 0.8, 0.82, "Distilled")
	}
	if err := collector.Flush(); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if stat.Size() > collector.smartCollectorSize {
		t.Fatalf("collector exceeded hard budget: size=%d budget=%d", stat.Size(), collector.smartCollectorSize)
	}
	if collector.currentSize > collector.smartCollectorSize {
		t.Fatalf("logical size exceeded budget: size=%d budget=%d", collector.currentSize, collector.smartCollectorSize)
	}
	if err := collector.Close(); err != nil {
		t.Fatal(err)
	}
}
