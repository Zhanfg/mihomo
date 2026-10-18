package smart

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/metacubex/bbolt"
)

// Regression: a Smart offline/bandit learner must actually survive closing the
// bbolt handle. Successful JSON marshaling in RAM is not enough.
func TestCompletedConnectionLearningPersistsAfterDatabaseReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "smart-learning.db")
	openDB := func() *bbolt.DB {
		t.Helper()
		database, err := bbolt.Open(path, 0o600, &bbolt.Options{})
		if err != nil { t.Fatal(err) }
		return database
	}
	firstDB := openDB()
	store := NewStore(firstDB)
	target := "test-network"
	node := "test-node"
	bandit := &OnlineBanditState{Updates: 31, ErrorEWMA: 0.08, Uncertainty: 0.35}
	bandit.Theta[0] = 0.19
	bandit.Precision[0] = 3.2
	tiny := &TinyRouterState{Updates: 29, ErrorEWMA: 0.06, Uncertainty: 0.22}
	tiny.Output[0] = 0.14
	record := StatsRecord{
		Success: 38, Failure: 4, Latency: 120,
		Weights: map[string]float64{WeightTypeTCP: 0.82},
		BanditTCP: bandit, TinyRouterTCP: tiny,
	}
	encoded, err := json.Marshal(record)
	if err != nil { t.Fatal(err) }
	store.AppendToGlobalQueue(StoreOperation{
		Type: OpSaveStats, Group: "Auto", Config: "config",
		Target: target, Node: node, Data: encoded,
	})
	store.FlushQueue(true)
	if err := firstDB.Close(); err != nil { t.Fatal(err) }
	defer func() { db = nil }()

	reopened := openDB()
	defer reopened.Close()
	var restored StatsRecord
	key := FormatDBKey(KeyTypeStats, "config", "Auto", target, node)
	err = reopened.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketSmartStats)
		if bucket == nil { t.Fatal("missing Smart learning bucket") }
		raw := bucket.Get([]byte(key))
		if raw == nil { t.Fatal("Smart learning record was not persisted") }
		return json.Unmarshal(raw, &restored)
	})
	if err != nil { t.Fatal(err) }
	if restored.Success != 38 || restored.Failure != 4 ||
		restored.BanditTCP == nil || restored.BanditTCP.Updates != 31 ||
		restored.BanditTCP.Theta[0] != 0.19 ||
		restored.TinyRouterTCP == nil || restored.TinyRouterTCP.Updates != 29 ||
		restored.TinyRouterTCP.Output[0] != 0.14 {
		t.Fatalf("long-term online learning changed after reload: %+v", restored)
	}
}
