package smart

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestVectorMemoryBinaryRoundTrip(t *testing.T) {
	var original VectorMemory
	for i := 0; i < VectorDimensions; i++ {
		original.Success[i] = float32(i) / VectorDimensions
		original.Failure[i] = 1 - original.Success[i]
	}
	original.SuccessMass = 7.5
	original.FailureMass = 2.25
	original.Updated = 123456789

	encoded := encodeVectorMemory(original)
	if len(encoded) != vectorMemoryV2Size {
		t.Fatalf("encoded vector record = %d bytes, want %d", len(encoded), vectorMemoryV2Size)
	}
	decoded, ok := decodeVectorMemory(encoded)
	if !ok {
		t.Fatal("failed to decode vector memory")
	}
	if decoded != original {
		t.Fatalf("round trip mismatch:\n got  %+v\n want %+v", decoded, original)
	}
}

func TestVectorCentroidLearnsWithoutUnboundedMass(t *testing.T) {
	var centroid [VectorDimensions]float32
	var mass float32
	sample := make([]float32, VectorDimensions)
	for i := range sample {
		sample[i] = 1
	}
	for i := 0; i < 200; i++ {
		updateVectorCentroid(&centroid, &mass, sample, 1)
	}
	if mass != 64 {
		t.Fatalf("mass should saturate at 64, got %f", mass)
	}
	for i, v := range centroid {
		if v < 0.95 || v > 1 {
			t.Fatalf("centroid[%d]=%f, want close to 1", i, v)
		}
	}
}

func TestCenteredCosineSeparatesOppositeProfiles(t *testing.T) {
	var learned [VectorDimensions]float32
	same := make([]float32, VectorDimensions)
	opposite := make([]float32, VectorDimensions)
	for i := 0; i < VectorDimensions; i++ {
		if i%2 == 0 {
			learned[i], same[i], opposite[i] = 0.9, 0.9, 0.1
		} else {
			learned[i], same[i], opposite[i] = 0.1, 0.1, 0.9
		}
	}
	if got := centeredCosine(learned, same); got < 0.99 {
		t.Fatalf("same profile cosine=%f, want ~1", got)
	}
	if got := centeredCosine(learned, opposite); got > -0.99 {
		t.Fatalf("opposite profile cosine=%f, want ~-1", got)
	}
}

func TestVectorAffinityFormulaRewardsSuccessAndAvoidsFailure(t *testing.T) {
	var memory VectorMemory
	good := make([]float32, VectorDimensions)
	bad := make([]float32, VectorDimensions)
	for i := range good {
		if i%2 == 0 {
			memory.Success[i], good[i] = 0.9, 0.9
			memory.Failure[i], bad[i] = 0.1, 0.1
		} else {
			memory.Success[i], good[i] = 0.1, 0.1
			memory.Failure[i], bad[i] = 0.9, 0.9
		}
	}
	memory.SuccessMass = 8
	memory.FailureMass = 8

	score := func(vector []float32) float64 {
		raw := centeredCosine(memory.Success, vector) - 0.55*centeredCosine(memory.Failure, vector)
		return math.Max(0, math.Min(1, 0.5+0.5*raw))
	}
	if score(good) <= score(bad) {
		t.Fatalf("success-like vector must beat failure-like vector: good=%f bad=%f", score(good), score(bad))
	}
}


func TestVectorMemoryV1DecodeCompatibility(t *testing.T) {
	var legacy VectorMemory
	legacy.Success[0] = 0.75
	legacy.Failure[1] = 0.25
	legacy.SuccessMass = 3
	legacy.FailureMass = 1
	legacy.Updated = 42

	buf := make([]byte, vectorMemoryV1Size)
	buf[0] = vectorMemoryVersionV1
	binary.LittleEndian.PutUint32(buf[1:5], math.Float32bits(legacy.SuccessMass))
	binary.LittleEndian.PutUint32(buf[5:9], math.Float32bits(legacy.FailureMass))
	binary.LittleEndian.PutUint64(buf[9:17], uint64(legacy.Updated))
	off := vectorMemoryHeader
	for _, x := range legacy.Success {
		binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(x))
		off += 4
	}
	for _, x := range legacy.Failure {
		binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(x))
		off += 4
	}

	got, ok := decodeVectorMemory(buf)
	if !ok {
		t.Fatal("v1 target vector must remain readable")
	}
	if got.SuccessMass != legacy.SuccessMass || got.FailureMass != legacy.FailureMass ||
		got.Success[0] != legacy.Success[0] || got.Failure[1] != legacy.Failure[1] {
		t.Fatalf("legacy decode mismatch: %+v", got)
	}
	for _, tag := range got.SuccessTags {
		if tag.Hash != 0 || tag.Mass != 0 {
			t.Fatalf("legacy records must start with empty sparse tags: %+v", tag)
		}
	}
}

func TestTagHeavyHitterLearnsDominantCategory(t *testing.T) {
	var tags [VectorTagDimensions]vectorTagMemory
	a := make([]uint64, VectorTagDimensions)
	b := make([]uint64, VectorTagDimensions)
	a[0], b[0] = 100, 200

	for i := 0; i < 8; i++ {
		updateTagHeavyHitters(&tags, a, 1)
	}
	for i := 0; i < 3; i++ {
		updateTagHeavyHitters(&tags, b, 1)
	}
	if tags[0].Hash != 100 || tags[0].Mass <= 0 {
		t.Fatalf("dominant categorical tag should survive minority noise: %+v", tags[0])
	}

	for i := 0; i < 12; i++ {
		updateTagHeavyHitters(&tags, b, 1)
	}
	if tags[0].Hash != 200 || tags[0].Mass <= 0 {
		t.Fatalf("fresh dominant tag should eventually replace stale one: %+v", tags[0])
	}
}

func TestCategoricalAffinityRewardsLearnedProtocolVendor(t *testing.T) {
	var success, failure [VectorTagDimensions]vectorTagMemory
	good := make([]uint64, VectorTagDimensions)
	bad := make([]uint64, VectorTagDimensions)

	// protocol/provider/vendor slots
	for _, i := range []int{0, 1, 6, 7} {
		good[i] = uint64(1000 + i)
		bad[i] = uint64(2000 + i)
		success[i] = vectorTagMemory{Hash: good[i], Mass: 8}
		failure[i] = vectorTagMemory{Hash: bad[i], Mass: 8}
	}

	goodScore, goodOK := categoricalAffinity(success, failure, good)
	badScore, badOK := categoricalAffinity(success, failure, bad)
	if !goodOK || !badOK {
		t.Fatal("categorical affinity should have evidence")
	}
	if goodScore <= badScore {
		t.Fatalf("success-like categorical profile must beat failure-like: good=%f bad=%f", goodScore, badScore)
	}
}
