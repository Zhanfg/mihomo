package smart

import (
	"encoding/binary"
	"math"
	"time"
)

const (
	VectorDimensions    = 12
	VectorTagDimensions = 8

	vectorMemoryVersionV1 byte = 1
	vectorMemoryVersion   byte = 2

	vectorMemoryHeader = 1 + 4 + 4 + 8
	vectorMemoryV1Size = vectorMemoryHeader + VectorDimensions*4*2
	vectorMemoryV2Size = vectorMemoryV1Size + VectorTagDimensions*12*2
)

type vectorTagMemory struct {
	Hash uint64
	Mass float32
}

type VectorMemory struct {
	Success     [VectorDimensions]float32
	Failure     [VectorDimensions]float32
	SuccessMass float32
	FailureMass float32

	// Fixed-size categorical heavy hitters. Each slot has a defined semantic
	// position in adapter.NodeVectorProfile (protocol/provider/country/ASN/
	// vendor), so we need no strings or heap maps in target memory.
	SuccessTags [VectorTagDimensions]vectorTagMemory
	FailureTags [VectorTagDimensions]vectorTagMemory

	Updated int64
}

func vectorMemoryKey(group, config, target string) string {
	return FormatDBKey(KeyTypeVector, config, group, target)
}

func encodeVectorMemory(v VectorMemory) []byte {
	buf := make([]byte, vectorMemoryV2Size)
	buf[0] = vectorMemoryVersion
	binary.LittleEndian.PutUint32(buf[1:5], math.Float32bits(v.SuccessMass))
	binary.LittleEndian.PutUint32(buf[5:9], math.Float32bits(v.FailureMass))
	binary.LittleEndian.PutUint64(buf[9:17], uint64(v.Updated))
	off := vectorMemoryHeader
	for _, x := range v.Success {
		binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(x))
		off += 4
	}
	for _, x := range v.Failure {
		binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(x))
		off += 4
	}
	writeTags := func(tags [VectorTagDimensions]vectorTagMemory) {
		for _, tag := range tags {
			binary.LittleEndian.PutUint64(buf[off:off+8], tag.Hash)
			off += 8
			binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(tag.Mass))
			off += 4
		}
	}
	writeTags(v.SuccessTags)
	writeTags(v.FailureTags)
	return buf
}

func decodeVectorMemory(buf []byte) (VectorMemory, bool) {
	var v VectorMemory
	if len(buf) != vectorMemoryV1Size && len(buf) != vectorMemoryV2Size {
		return v, false
	}
	if buf[0] != vectorMemoryVersionV1 && buf[0] != vectorMemoryVersion {
		return v, false
	}
	if buf[0] == vectorMemoryVersionV1 && len(buf) != vectorMemoryV1Size {
		return v, false
	}
	if buf[0] == vectorMemoryVersion && len(buf) != vectorMemoryV2Size {
		return v, false
	}

	v.SuccessMass = math.Float32frombits(binary.LittleEndian.Uint32(buf[1:5]))
	v.FailureMass = math.Float32frombits(binary.LittleEndian.Uint32(buf[5:9]))
	v.Updated = int64(binary.LittleEndian.Uint64(buf[9:17]))
	off := vectorMemoryHeader
	for i := range v.Success {
		v.Success[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[off : off+4]))
		off += 4
	}
	for i := range v.Failure {
		v.Failure[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[off : off+4]))
		off += 4
	}

	// Version 1 is intentionally accepted: old learned continuous centroids are
	// immediately useful, while sparse categorical memory warms up lazily.
	if buf[0] == vectorMemoryVersionV1 {
		return v, true
	}

	readTags := func(tags *[VectorTagDimensions]vectorTagMemory) {
		for i := range tags {
			tags[i].Hash = binary.LittleEndian.Uint64(buf[off : off+8])
			off += 8
			tags[i].Mass = math.Float32frombits(binary.LittleEndian.Uint32(buf[off : off+4]))
			off += 4
		}
	}
	readTags(&v.SuccessTags)
	readTags(&v.FailureTags)
	return v, true
}

func loadVectorMemory(s *Store, key string) (VectorMemory, bool) {
	if vectorMemoryCache != nil {
		if cached, _, ok := vectorMemoryCache.GetWithExpire(key); ok {
			if cached.Updated == 0 || time.Since(time.Unix(cached.Updated, 0)) <= RecordExpiredTime {
				return cached, true
			}
			vectorMemoryCache.Delete(key)
		}
	}
	raw, err := s.DBViewGetItem(key)
	if err != nil {
		return VectorMemory{}, false
	}
	v, ok := decodeVectorMemory(raw)
	if !ok {
		return VectorMemory{}, false
	}
	if v.Updated > 0 && time.Since(time.Unix(v.Updated, 0)) > RecordExpiredTime {
		// Ignore stale behavior immediately. Physical cleanup follows the
		// existing config/group lifecycle instead of creating a write just
		// because a cold target was read once.
		return VectorMemory{}, false
	}
	if vectorMemoryCache != nil {
		vectorMemoryCache.Set(key, v)
	}
	return v, true
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

// updateTagHeavyHitters is a weighted Boyer-Moore majority tracker per semantic
// slot. It learns one dominant categorical fingerprint without a map/list. A
// changing provider/ASN can replace an old dominant value after enough fresh
// evidence, while stable values quickly build confidence.
func updateTagHeavyHitters(dst *[VectorTagDimensions]vectorTagMemory, tags []uint64, strength float32) {
	if len(tags) < VectorTagDimensions {
		return
	}
	if strength < 0.25 {
		strength = 0.25
	}
	if strength > 2 {
		strength = 2
	}
	for i := 0; i < VectorTagDimensions; i++ {
		tag := tags[i]
		if tag == 0 {
			continue
		}
		slot := &dst[i]
		switch {
		case slot.Hash == 0:
			slot.Hash, slot.Mass = tag, strength
		case slot.Hash == tag:
			slot.Mass += strength
			if slot.Mass > 64 {
				slot.Mass = 64
			}
		default:
			slot.Mass -= strength
			if slot.Mass <= 0 {
				slot.Hash, slot.Mass = tag, -slot.Mass+0.25
			}
		}
	}
}

// UpdateVectorMemory learns positive/negative continuous centroids and sparse
// categorical heavy hitters for a Smart target. It reuses the existing bbolt
// database, write queue, and target lock shards.
func (s *Store) UpdateVectorMemory(group, config, target string, vector []float32, tags []uint64, success bool, strength float32) {
	if s == nil || target == "" || len(vector) < VectorDimensions {
		return
	}
	key := vectorMemoryKey(group, config, target)

	lock := GetTargetNodeLock(target, group, "__vector__")
	lock.Lock()
	v, _ := loadVectorMemory(s, key)
	if success {
		updateVectorCentroid(&v.Success, &v.SuccessMass, vector, strength)
		updateTagHeavyHitters(&v.SuccessTags, tags, strength)
	} else {
		updateVectorCentroid(&v.Failure, &v.FailureMass, vector, strength)
		updateTagHeavyHitters(&v.FailureTags, tags, strength)
	}
	v.Updated = time.Now().Unix()
	if vectorMemoryCache != nil {
		vectorMemoryCache.Set(key, v)
	}
	data := encodeVectorMemory(v)
	lock.Unlock()

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

func categoricalAffinity(success, failure [VectorTagDimensions]vectorTagMemory, tags []uint64) (float64, bool) {
	if len(tags) < VectorTagDimensions {
		return 0.5, false
	}
	var weighted, total float64
	for i := 0; i < VectorTagDimensions; i++ {
		tag := tags[i]
		if tag == 0 {
			continue
		}
		if slot := success[i]; slot.Hash != 0 && slot.Mass > 0 {
			conf := math.Min(1, float64(slot.Mass)/6)
			if slot.Hash == tag {
				weighted += conf
			}
			total += conf
		}
		if slot := failure[i]; slot.Hash != 0 && slot.Mass > 0 {
			conf := math.Min(1, float64(slot.Mass)/6)
			if slot.Hash == tag {
				weighted -= 0.55 * conf
			}
			total += 0.55 * conf
		}
	}
	if total == 0 {
		return 0.5, false
	}
	score := 0.5 + 0.5*(weighted/total)
	if score < 0 {
		score = 0
	} else if score > 1 {
		score = 1
	}
	return score, true
}

// VectorAffinity compares a candidate profile with what this Smart target has
// learned from successful and failed nodes. Continuous similarity is blended
// with fixed categorical fingerprints (protocol/provider/country/ASN/vendor).
func (s *Store) VectorAffinity(group, config, target string, vector []float32, tags []uint64) (float64, bool) {
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
	continuous := 0.5 + 0.5*raw*confidence
	if continuous < 0 {
		continuous = 0
	} else if continuous > 1 {
		continuous = 1
	}

	if categorical, hasCategorical := categoricalAffinity(v.SuccessTags, v.FailureTags, tags); hasCategorical {
		// Continuous quality remains dominant; categorical affinity can move a
		// candidate meaningfully but cannot override bad health by itself.
		return 0.75*continuous + 0.25*categorical, true
	}
	return continuous, true
}
