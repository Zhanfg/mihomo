//go:build smart_extreme

package smart

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/metacubex/bbolt"
)

func TestSmartExtremeExpertTwoMillionDistillBounded(t *testing.T) {
	bank := NewExpertBank()
	for i := 0; i < 2_000_000; i++ {
		input := expertTestInput(i, i&1 == 1)
		prior := 0.52 + float64(i%13)*0.025
		teacher := syntheticTeacher(prior, OnlineBanditFeatures(input))
		bank.Distill(input, prior, teacher)
		if i%250_000 == 0 {
			data, err := bank.MarshalBounded()
			if err != nil {
				t.Fatalf("snapshot at %d: %v", i, err)
			}
			if len(data) > ExpertMaxPersistBytes {
				t.Fatalf("snapshot grew beyond cap at %d: %d", i, len(data))
			}
		}
	}

	data, err := bank.MarshalBounded()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > ExpertMaxPersistBytes {
		t.Fatalf("final expert snapshot=%d > cap=%d", len(data), ExpertMaxPersistBytes)
	}
	for i, e := range bank.Snapshot().Experts {
		for j, theta := range e.Theta {
			if math.IsNaN(theta) || math.IsInf(theta, 0) {
				t.Fatalf("expert %d theta %d became non-finite", i, j)
			}
		}
		for j, p := range e.Precision {
			if p < 1 || p > 65536 || math.IsNaN(p) || math.IsInf(p, 0) {
				t.Fatalf("expert %d precision %d invalid: %v", i, j, p)
			}
		}
	}
}

func TestSmartExtremeConcurrentExpertDistillAndSnapshot(t *testing.T) {
	bank := NewExpertBank()
	const workers = 16
	const updatesPerWorker = 20_000

	var wg sync.WaitGroup
	wg.Add(workers + 1)
	for w := 0; w < workers; w++ {
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < updatesPerWorker; i++ {
				n := worker*updatesPerWorker + i
				input := expertTestInput(n, worker&1 == 1)
				prior := 0.6 + float64(n%11)*0.02
				bank.Distill(input, prior, syntheticTeacher(prior, OnlineBanditFeatures(input)))
			}
		}(w)
	}
	go func() {
		defer wg.Done()
		for i := 0; i < 10_000; i++ {
			data, _, err := bank.marshalForPersist()
			if err != nil {
				t.Errorf("concurrent marshal: %v", err)
				return
			}
			if len(data) > ExpertMaxPersistBytes {
				t.Errorf("concurrent snapshot exceeded cap: %d", len(data))
				return
			}
		}
	}()
	wg.Wait()

	total := uint64(0)
	for _, e := range bank.Snapshot().Experts {
		total += e.Distilled
	}
	want := uint64(workers * updatesPerWorker)
	if total != want {
		t.Fatalf("lost concurrent distillation updates: got=%d want=%d", total, want)
	}
}

func TestSmartExtremeStudentRegimeDrift(t *testing.T) {
	input := expertTestInput(0, false)
	x := OnlineBanditFeatures(input)
	state := defaultOnlineBanditState()
	state.Generation = processBanditGeneration
	state.Epoch = 1

	const cycles = 40
	const perCycle = 5_000
	prior := 0.7
	for cycle := 0; cycle < cycles; cycle++ {
		state.ObserveEpoch(uint64(cycle + 1))
		high := cycle%2 == 0
		reward := 1.0
		if !high {
			reward = 0.22
		}
		for i := 0; i < perCycle; i++ {
			state.Update(prior, reward, x, 1)
		}
		got, uncertainty := state.Predict(prior, x)
		if high && got <= prior {
			t.Fatalf("cycle %d failed to adapt upward: %v", cycle, got)
		}
		if !high && got >= prior {
			t.Fatalf("cycle %d failed to adapt downward: %v", cycle, got)
		}
		if uncertainty < 0 || uncertainty > 1 || math.IsNaN(uncertainty) {
			t.Fatalf("cycle %d invalid uncertainty=%v", cycle, uncertainty)
		}
	}
}

func TestSmartExtremeRank100kCandidatesBoundedTopK(t *testing.T) {
	const candidates = 100_000
	const limit = 10
	now := time.Now().Unix()
	stats := make(map[string][]byte, candidates)
	for i := 0; i < candidates; i++ {
		weight := 0.45 + float64((i*7919)%50_000)/100_000.0
		record := StatsRecord{
			LastUsed: now,
			Weights: map[string]float64{WeightTypeTCP: weight},
			BanditTCP: &OnlineBanditState{Updates: 32, Uncertainty: float64(i%10) / 10},
		}
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		stats[fmt.Sprintf("node-%06d", i)] = data
	}

	start := time.Now()
	got := NewStore(nil).rankTargetStatsWithExploration("g", "c", "t", stats, false, limit, now, 0.06)
	elapsed := time.Since(start)
	if len(got) != limit {
		t.Fatalf("top-k length=%d want=%d", len(got), limit)
	}
	for i := 1; i < len(got); i++ {
		if got[i].Weight > got[i-1].Weight {
			t.Fatalf("ranking not descending at %d: %+v", i, got)
		}
	}
	if elapsed > 20*time.Second {
		t.Fatalf("100k candidate rank exceeded extreme budget: %s", elapsed)
	}
}

func TestSmartExtremeTeacherRateAfterDistillation(t *testing.T) {
	bank := NewExpertBank()
	bank.ObserveTeacherRevision("teacher-v1")
	input := expertTestInput(0, false)
	prior := 0.7
	for i := 0; i < 2_000; i++ {
		input.Success = int64(64 + i)
		bank.Distill(input, prior, syntheticTeacher(prior, OnlineBanditFeatures(input)))
	}

	probe := expertTestInput(1, false)
	_, confidence, ready := bank.Predict(probe, prior)
	if !ready || confidence < 0.65 {
		t.Fatalf("expert never matured: ready=%v confidence=%v", ready, confidence)
	}

	calls := 0
	const decisions = 1_000_000
	for i := 1; i <= decisions; i++ {
		probe.Success = int64(i)
		probe.Failure = 0
		probe.ConnectionFailed = false
		probe.LossRate = 0
		if bank.NeedsTeacher(probe, confidence, ready) {
			calls++
		}
	}
	rate := float64(calls) / decisions
	if rate > 0.011 {
		t.Fatalf("mature expert teacher rate too high: calls=%d rate=%.4f", calls, rate)
	}
	if rate < 0.009 {
		t.Fatalf("teacher refresh unexpectedly vanished: calls=%d rate=%.4f", calls, rate)
	}
}

func TestSmartExtremeExpertSnapshotSizeDoesNotScaleWithExperience(t *testing.T) {
	bank := NewExpertBank()
	sizes := make([]int, 0, 4)
	checkpoints := []int{100, 10_000, 250_000, 1_000_000}
	done := 0
	for _, target := range checkpoints {
		for i := done; i < target; i++ {
			input := expertTestInput(i, i&1 == 1)
			prior := 0.65
			bank.Distill(input, prior, syntheticTeacher(prior, OnlineBanditFeatures(input)))
		}
		done = target
		data, err := bank.MarshalBounded()
		if err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, len(data))
	}
	for i, size := range sizes {
		if size > ExpertMaxPersistBytes {
			t.Fatalf("checkpoint %d exceeded cap: %d", checkpoints[i], size)
		}
	}
	// Numeric digit growth is allowed; structural growth is not.
	if sizes[len(sizes)-1]-sizes[0] > 2048 {
		t.Fatalf("snapshot structurally grew with experience: %v", sizes)
	}
}


func TestSmartExtremeBoltPhysicalGrowthPlateaus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "smart-plateau.db")
	testDB, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	previousDB := db
	db = testDB
	defer func() {
		db = previousDB
		_ = testDB.Close()
	}()

	store := &Store{}
	const keys = 512
	const rounds = 240
	const warmupRound = 60
	payload := make([]byte, 2048)
	for i := range payload {
		payload[i] = byte((i*31 + 17) & 0xff)
	}

	var warmSize int64
	for round := 0; round < rounds; round++ {
		ops := make([]StoreOperation, 0, keys)
		for i := 0; i < keys; i++ {
			// Keep the live key set fixed while continuously changing values,
			// which models long-running Smart stats/model rewrites.
			data := append([]byte(nil), payload...)
			data[0] = byte(round)
			data[1] = byte(i)
			ops = append(ops, StoreOperation{
				Type:   OpSaveStats,
				Group:  "plateau",
				Config: "cfg",
				Target: fmt.Sprintf("target-%03d", i),
				Node:   "node",
				Data:   data,
			})
		}
		if err := store.BatchSave(ops); err != nil {
			t.Fatalf("round %d batch save: %v", round, err)
		}

		if round%12 == 11 {
			// Delete and recreate a subset to force freelist churn rather than
			// testing only same-size in-place replacements.
			prefixes := make([]string, 0, 64)
			for i := 0; i < 64; i++ {
				prefixes = append(prefixes, FormatDBKey(KeyTypeStats, "cfg", "plateau", fmt.Sprintf("target-%03d", i), "node"))
			}
			if err := store.DBBatchDeletePrefix(prefixes, true); err != nil {
				t.Fatalf("round %d delete: %v", round, err)
			}
		}

		if round == warmupRound {
			if err := testDB.Sync(); err != nil {
				t.Fatal(err)
			}
			st, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			warmSize = st.Size()
		}
	}

	if err := testDB.Sync(); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	finalSize := st.Size()
	if warmSize <= 0 {
		t.Fatal("failed to capture bbolt warm high-water mark")
	}

	// bbolt does not shrink its file on delete, but with a bounded working set
	// it must recycle free pages and converge. Allow a generous 50% + 4 MiB
	// allocator margin while rejecting write-amplification that grows forever.
	limit := warmSize + warmSize/2 + 4*1024*1024
	if finalSize > limit {
		t.Fatalf("bbolt Smart file kept growing after warmup: warm=%d final=%d limit=%d", warmSize, finalSize, limit)
	}
	if finalSize > 32*1024*1024 {
		t.Fatalf("bounded 512-record Smart workload produced oversized DB: %d", finalSize)
	}
}
