package smart

import (
	"encoding/json"
	"math"
	"sync"
	"testing"

	"github.com/metacubex/mihomo/common/lru"
)

func TestTinyRouterStatePersistsInsideStatsRecord(t *testing.T) {
	record := &AtomicStatsRecord{
		weights: lru.New[string, float64](lru.WithSize[string, float64](8)),
	}
	state := TinyRouterState{
		Output:      [TinyRouterHiddenDimension]float64{0.11, -0.07, 0.03, 0.09},
		Bias:        0.04,
		Updates:     23,
		ErrorEWMA:   0.08,
		Epoch:       4,
		Generation:  processTinyRouterGeneration,
		Uncertainty: 0.27,
	}
	SaveTinyRouterState(record, false, state)

	snapshot := record.CreateStatsSnapshot("tiny-router-persistence-test")
	if snapshot.TinyRouterTCP == nil {
		t.Fatal("TCP TinyRouter state missing from stats snapshot")
	}
	if snapshot.TinyRouterUDP != nil {
		t.Fatal("unused UDP TinyRouter state should remain absent")
	}

	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded StatsRecord
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.TinyRouterTCP == nil {
		t.Fatal("TinyRouter state missing after JSON round trip")
	}

	restored := &AtomicStatsRecord{
		weights: lru.New[string, float64](lru.WithSize[string, float64](8)),
	}
	restored.restoreTinyRouterState(decoded.TinyRouterTCP, false)
	got := LoadTinyRouterState(restored, false)

	if got.Updates != state.Updates || math.Abs(got.Bias-state.Bias) > 1e-12 ||
		math.Abs(got.Uncertainty-state.Uncertainty) > 1e-12 {
		t.Fatalf("restored TinyRouter state mismatch: got=%#v want=%#v", got, state)
	}
	for i := range state.Output {
		if math.Abs(got.Output[i]-state.Output[i]) > 1e-12 {
			t.Fatalf("restored output[%d]=%v want=%v", i, got.Output[i], state.Output[i])
		}
	}
}

func TestLoadTinyRouterStateColdDefault(t *testing.T) {
	record := &AtomicStatsRecord{}
	got := LoadTinyRouterState(record, true)
	if got.Updates != 0 || got.Uncertainty != 1 {
		t.Fatalf("cold TinyRouter state=%#v want updates=0 uncertainty=1", got)
	}
}


func TestTinyRouterConcurrentStateAccess(t *testing.T) {
	record := &AtomicStatsRecord{
		weights: lru.New[string, float64](lru.WithSize[string, float64](8)),
	}
	SaveTinyRouterState(record, false, TinyRouterState{
		Updates: 1, Uncertainty: 1,
	})

	const workers = 8
	const iterations = 250
	var wg sync.WaitGroup
	wg.Add(workers)
	for worker := 0; worker < workers; worker++ {
		worker := worker
		go func() {
			defer wg.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				if worker%2 == 0 {
					state := LoadTinyRouterState(record, false)
					state.Updates += 1
					state.Uncertainty = float64((worker+iteration)%100) / 100
					state.Output[(worker+iteration)%TinyRouterHiddenDimension] += 0.001
					SaveTinyRouterState(record, false, state)
				} else {
					_ = LoadTinyRouterState(record, false)
					_ = record.snapshotTinyRouterState(false)
				}
			}
		}()
	}
	wg.Wait()

	got := LoadTinyRouterState(record, false)
	if got.Updates <= 0 {
		t.Fatalf("concurrent state access lost all updates: %#v", got)
	}
	for i, value := range got.Output {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < -0.45 || value > 0.45 {
			t.Fatalf("output[%d]=%v outside normalized bounds", i, value)
		}
	}
	if got.Uncertainty < 0 || got.Uncertainty > 1 {
		t.Fatalf("uncertainty=%v outside [0,1]", got.Uncertainty)
	}
}
