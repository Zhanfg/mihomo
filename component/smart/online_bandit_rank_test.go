package smart

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRankTargetStatsExplorationIsExplicitAndBounded(t *testing.T) {
	now := time.Now().Unix()
	encode := func(weight, uncertainty float64) []byte {
		data, err := json.Marshal(StatsRecord{
			LastUsed: now,
			Weights: map[string]float64{
				WeightTypeTCP:                 weight,
				WeightTypeBanditUncertaintyTCP: uncertainty,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}

	stats := map[string][]byte{
		"known":     encode(0.80, 0),
		"uncertain": encode(0.77, 1),
	}
	store := NewStore(nil)

	exploit := store.rankTargetStatsWithExploration("g", "c", "t", stats, false, 2, now, 0)
	if len(exploit) != 2 || exploit[0].Node != "known" {
		t.Fatalf("pure exploitation order=%v", exploit)
	}

	explore := store.rankTargetStatsWithExploration("g", "c", "t", stats, false, 2, now, 0.06)
	if len(explore) != 2 || explore[0].Node != "uncertain" {
		t.Fatalf("bounded exploration did not surface uncertain close candidate: %v", explore)
	}

	// A materially worse node cannot jump the queue even at maximum alpha.
	stats["uncertain"] = encode(0.60, 1)
	explore = store.rankTargetStatsWithExploration("g", "c", "t", stats, false, 2, now, 0.08)
	if explore[0].Node != "known" {
		t.Fatalf("exploration overrode material quality gap: %v", explore)
	}
}

func BenchmarkExplorationBonus(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ExplorationBonus(0.82, 0.4, 0.06)
	}
}
