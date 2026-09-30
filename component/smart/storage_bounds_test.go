package smart

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/metacubex/bbolt"
)

func TestCleanupOldRecordsEnforcesTargetCapImmediately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "smart.db")
	testDB, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer testDB.Close()

	previousDB := db
	db = testDB
	defer func() { db = previousDB }()

	globalCacheParams.mutex.Lock()
	oldMax := globalCacheParams.MaxTargets
	globalCacheParams.MaxTargets = 10
	globalCacheParams.mutex.Unlock()
	defer func() {
		globalCacheParams.mutex.Lock()
		globalCacheParams.MaxTargets = oldMax
		globalCacheParams.mutex.Unlock()
	}()

	store := &Store{}
	now := time.Now().Unix()
	for i := 0; i < 25; i++ {
		record := StatsRecord{
			Success:  int64(i + 1),
			LastUsed: now - int64(25-i),
			Weights:  map[string]float64{WeightTypeTCP: 0.5 + float64(i)/100},
		}
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		key := FormatDBKey(KeyTypeStats, "cfg", "group", fmt.Sprintf("target-%02d", i), "node")
		if err := store.DBBatchPutItem(key, data); err != nil {
			t.Fatal(err)
		}
	}

	store.CleanupOldRecords("group", "cfg")
	remaining, err := store.DBViewPrefixScan(FormatDBKey(KeyTypeStats, "cfg", "group"), -1, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) > 10 {
		t.Fatalf("cleanup retained %d stats records above hard cap 10", len(remaining))
	}
}

func TestSmartStatsRecordSerializedFootprintBounded(t *testing.T) {
	weights := make(map[string]float64, 100)
	for i := 0; i < 96; i++ {
		weights[fmt.Sprintf("asn:%d", 10000+i)] = float64(i)
	}
	weights[WeightTypeTCP] = 0.8
	weights[WeightTypeUDP] = 0.7
	weights[WeightTypeModelCalibrationTCP] = 1.1
	weights[WeightTypeModelErrorTCP] = 0.05

	fullState := defaultOnlineBanditState()
	fullState.Updates = 1_000_000
	fullState.ErrorEWMA = 0.123456
	fullState.Uncertainty = 0.234567
	for i := 0; i < OnlineBanditDimension; i++ {
		fullState.Theta[i] = 0.123456
		fullState.Precision[i] = 4096
	}

	record := StatsRecord{
		Success: 1_000_000, Failure: 10_000,
		ConnectTime: 88, Latency: 123, LastUsed: time.Now().Unix(),
		Weights: weights,
		UploadTotal: 1e9, DownloadTotal: 5e9,
		MaxUploadRate: 1e6, MaxDownloadRate: 2e6,
		ConnectionDuration: 9999,
		LossRate: 0.01, CumulSent: 1e9, CumulRetrans: 1e6,
		BanditTCP: &fullState,
		BanditUDP: &fullState,
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	const maxRecordBytes = 16 * 1024
	if len(data) > maxRecordBytes {
		t.Fatalf("maximal Smart stats record=%d bytes exceeds %d-byte budget", len(data), maxRecordBytes)
	}
}


func TestSmartV7FinalResourceContract(t *testing.T) {
	if ExpertMaxPersistBytes != 16*1024 {
		t.Fatalf("expert snapshot cap drifted: %d", ExpertMaxPersistBytes)
	}
	if ExpertCount != 8 {
		t.Fatalf("expert count drifted: %d", ExpertCount)
	}
	if OnlineBanditDimension != 6 {
		t.Fatalf("student dimension drifted: %d", OnlineBanditDimension)
	}
	if MaxTargetsLimit > 5000 {
		t.Fatalf("target cap exceeds mobile envelope: %d", MaxTargetsLimit)
	}
}

// Install-head validation marker: runtime-neutral change used to force all Smart
// CI gates to execute against the exact packaged BoxProxy installer revision.
