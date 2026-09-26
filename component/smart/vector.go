package smart

import (
	"encoding/binary"
	"math"
	"sync"
	"time"
)

const VectorDimensions = 12

const vectorMemoryVersion byte = 1

type VectorMemory struct {
	Success     [VectorDimensions]float32
	Failure     [VectorDimensions]float32
	SuccessMass float32
	FailureMass float32
	Updated     int64
}

var vectorMemoryMu sync.Mutex

func vectorMemoryKey(group, config, target string) string {
	return FormatDBKey(KeyTypeVector, config, group, target)
}

func encodeVectorMemory(v VectorMemory) []byte {
	const header = 1 + 4 + 4 + 8
	buf := make([]byte, header+VectorDimensions*4*2)
	buf[0] = vectorMemoryVersion
	binary.LittleEndian.PutUint32(buf[1:5], math.Float32bits(v.SuccessMass))
	binary.LittleEndian.PutUint32(buf[5:9], math.Float32bits(v.FailureMass))
	binary.LittleEndian.PutUint64(buf[9:17], uint64(v.Updated))
	off := header
	for _, x := range v.Success {
		binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(x))
		off += 4
	}
	for _, x := range v.Failure {
		binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(x))
		off += 4
	}
	return buf
}

func decodeVectorMemory(buf []byte) (VectorMemory, bool) {
	const header = 1 + 4 + 4 + 8
	var v VectorMemory
	if len(buf) != header+VectorDimensions*4*2 || buf[0] != vectorMemoryVersion {
		return v, false
	}
	v.SuccessMass = math.Float32frombits(binary.LittleEndian.Uint32(buf[1:5]))
	v.FailureMass = math.Float32frombits(binary.LittleEndian.Uint32(buf[5:9]))
	v.Updated = int64(binary.LittleEndian.Uint64(buf[9:17]))
	off := header
	for i := range v.Success {
		v.Success[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[off : off+4]))
		off += 4
	}
	for i := range v.Failure {
		v.Failure[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[off : off+4]))
		off += 4
	}
	return v, true
}

func loadVectorMemory(s *Store, key string) (VectorMemory, bool) {
	if vectorMemoryCache != nil {
		if cached, _, ok := vectorMemoryCache.GetWithExpire(key); ok {
			return cached, true
		}
	}
	raw, err := s.DBViewGetItem(key)
	if err != nil {
		return VectorMemory{}, false
	}
	v, ok := decodeVectorMemory(raw)
	if ok && vectorMemoryCache != nil {
		vectorMemoryCache.Set(key, v)
	}
	return v, ok
}

func updateVectorCentroid(dst *[VectorDimensions]float32, mass *float32, sample []float32, strength float32) {
	if len(sample) < VectorDimensions {
		return
	}
	if strength < 0.25 {
		strength = 0.25
	}
	if strength > 2 {
		strength = 2
	}

	// Exponential learning with bounded inertia. Old experience matters, but a
	// node/service relationship can still recover after a provider reroute.
	alpha := float32(0.08) * strength
	if *mass < 4 {
		alpha = 1 / (*mass + 1)
	}
	if alpha < 0.04 {
		alpha = 0.04
	}
	if alpha > 0.35 {
		alpha = 0.35
	}
	for i := 0; i < VectorDimensions; i++ {
		x := sample[i]
		if x < 0 {
			x = 0
		} else if x > 1 {
			x = 1
		}
		dst[i] += alpha * (x - dst[i])
	}
	*mass += strength
	if *mass > 64 {
		*mass = 64
	}
}

// UpdateVectorMemory learns a positive or negative node-profile centroid for a
// Smart target. It reuses the existing bbolt database and deduplicating queue;
// repeated closes for the same target collapse into one pending write.
func (s *Store) UpdateVectorMemory(group, config, target string, vector []float32, success bool, strength float32) {
	if s == nil || target == "" || len(vector) < VectorDimensions {
		return
	}
	key := vectorMemoryKey(group, config, target)

	vectorMemoryMu.Lock()
	v, _ := loadVectorMemory(s, key)
	if success {
		updateVectorCentroid(&v.Success, &v.SuccessMass, vector, strength)
	} else {
		updateVectorCentroid(&v.Failure, &v.FailureMass, vector, strength)
	}
	v.Updated = time.Now().Unix()
	if vectorMemoryCache != nil {
		vectorMemoryCache.Set(key, v)
	}
	data := encodeVectorMemory(v)
	vectorMemoryMu.Unlock()

	s.AppendToGlobalQueue(StoreOperation{
		Type:   OpSaveVector,
		Group:  group,
		Config: config,
		Target: target,
		Data:   data,
	})
}

func centeredCosine(a [VectorDimensions]float32, b []float32) float64 {
	if len(b) < VectorDimensions {
		return 0
	}
	var dot, na, nb float64
	for i := 0; i < VectorDimensions; i++ {
		x := float64(a[i]) - 0.5
		y := float64(b[i]) - 0.5
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}

// VectorAffinity compares a candidate vector with what this Smart target has
// learned from successful and failed nodes. The result is normalized to [0,1].
//
// No ANN index is needed: Smart calls this only for its bounded front candidate
// window, while the persistent memory is one 113-byte record per active target.
func (s *Store) VectorAffinity(group, config, target string, vector []float32) (float64, bool) {
	if s == nil || target == "" || len(vector) < VectorDimensions {
		return 0.5, false
	}
	v, ok := loadVectorMemory(s, vectorMemoryKey(group, config, target))
	if !ok || (v.SuccessMass <= 0 && v.FailureMass <= 0) {
		return 0.5, false
	}

	raw := 0.0
	if v.SuccessMass > 0 {
		raw += centeredCosine(v.Success, vector)
	}
	if v.FailureMass > 0 {
		raw -= 0.55 * centeredCosine(v.Failure, vector)
	}
	confidence := math.Min(1, float64(v.SuccessMass+v.FailureMass)/8)
	score := 0.5 + 0.5*raw*confidence
	if score < 0 {
		score = 0
	} else if score > 1 {
		score = 1
	}
	return score, true
}
