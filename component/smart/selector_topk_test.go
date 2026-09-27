package smart

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func encodedRankRecord(t *testing.T, weight float64, lastUsed int64) []byte {
	t.Helper()
	data, err := json.Marshal(StatsRecord{
		LastUsed: lastUsed,
		Weights: map[string]float64{WeightTypeTCP: weight},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRankTargetStatsKeepsOnlyTopK(t *testing.T) {
	now := time.Now().Unix()
	stats := make(map[string][]byte, 100)
	for i := 0; i < 100; i++ {
		stats[fmt.Sprintf("node-%03d", i)] = encodedRankRecord(t, float64(i+1), now)
	}

	got := rankTargetStats(stats, false, 10, now)
	if len(got) != 10 {
		t.Fatalf("len=%d, want 10", len(got))
	}
	if got[0].Node != "node-099" || got[9].Node != "node-090" {
		t.Fatalf("topK=%+v", got)
	}
}

func TestRankTargetStatsTieBreakIsStable(t *testing.T) {
	now := time.Now().Unix()
	stats := map[string][]byte{
		"z": encodedRankRecord(t, 1, now),
		"a": encodedRankRecord(t, 1, now),
		"m": encodedRankRecord(t, 1, now),
	}
	got := rankTargetStats(stats, false, 2, now)
	if len(got) != 2 || got[0].Node != "a" || got[1].Node != "m" {
		t.Fatalf("got=%+v", got)
	}
}

func BenchmarkRankTargetStatsTop10(b *testing.B) {
	now := time.Now().Unix()
	stats := make(map[string][]byte, 1000)
	for i := 0; i < 1000; i++ {
		data, _ := json.Marshal(StatsRecord{
			LastUsed: now,
			Weights: map[string]float64{WeightTypeTCP: float64((i%97)+1) / 97},
		})
		stats[fmt.Sprintf("node-%04d", i)] = data
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rankTargetStats(stats, false, 10, now)
	}
}
