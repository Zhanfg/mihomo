package smart

import (
	"encoding/json"
	"errors"
	"math"
	"sync"
)

const (
	ExpertCount           = 8
	ExpertMinDistill      = 24
	ExpertMaxPersistBytes = 16 * 1024
)

type DistilledExpert struct {
	Theta      [OnlineBanditDimension]float64 `json:"theta"`
	Precision  [OnlineBanditDimension]float64 `json:"precision"`
	Distilled  uint64                         `json:"distilled"`
	ErrorEWMA  float64                        `json:"error_ewma,omitempty"`
}

type ExpertBankSnapshot struct {
	Version uint8                   `json:"version"`
	Experts [ExpertCount]DistilledExpert `json:"experts"`
}

type ExpertBank struct {
	mu        sync.RWMutex
	experts   [ExpertCount]DistilledExpert
	generation uint64
	persisted  uint64
}

func NewExpertBank() *ExpertBank {
	b := &ExpertBank{}
	for i := range b.experts {
		for j := range b.experts[i].Precision {
			b.experts[i].Precision[j] = 1
		}
	}
	return b
}

func expertIndex(input *ModelInput) int {
	if input == nil {
		return 0
	}
	scene := identifyConnectionScene(
		input.IsUDP, input.Latency,
		input.UploadTotal, input.DownloadTotal,
		input.MaxuploadRate, input.MaxdownloadRate,
		input.ConnectionDuration,
	)
	idx := int(scene)
	if input.IsUDP {
		idx += 4
	}
	if idx < 0 || idx >= ExpertCount {
		return 0
	}
	return idx
}

func normalizeExpert(e DistilledExpert) DistilledExpert {
	for i := 0; i < OnlineBanditDimension; i++ {
		if math.IsNaN(e.Theta[i]) || math.IsInf(e.Theta[i], 0) {
			e.Theta[i] = 0
		}
		if e.Precision[i] < 1 || math.IsNaN(e.Precision[i]) || math.IsInf(e.Precision[i], 0) {
			e.Precision[i] = 1
		}
		if e.Precision[i] > 65536 {
			e.Precision[i] = 65536
		}
	}
	if e.ErrorEWMA < 0 || math.IsNaN(e.ErrorEWMA) || math.IsInf(e.ErrorEWMA, 0) {
		e.ErrorEWMA = 0
	}
	if e.ErrorEWMA > 1 {
		e.ErrorEWMA = 1
	}
	return e
}

func LoadExpertBank(data []byte) *ExpertBank {
	b := NewExpertBank()
	if len(data) == 0 || len(data) > ExpertMaxPersistBytes {
		return b
	}
	var snapshot ExpertBankSnapshot
	if json.Unmarshal(data, &snapshot) != nil || snapshot.Version != 1 {
		return b
	}
	for i := range snapshot.Experts {
		b.experts[i] = normalizeExpert(snapshot.Experts[i])
	}
	return b
}

func (b *ExpertBank) MarshalBounded() ([]byte, error) {
	if b == nil {
		return nil, nil
	}
	b.mu.RLock()
	snapshot := ExpertBankSnapshot{Version: 1, Experts: b.experts}
	b.mu.RUnlock()
	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	if len(data) > ExpertMaxPersistBytes {
		return nil, errors.New("Smart expert bank exceeded hard persistence budget")
	}
	return data, nil
}

func expertPredict(e DistilledExpert, prior float64, x [OnlineBanditDimension]float64) (float64, float64, bool) {
	if prior <= 0 || e.Distilled < ExpertMinDistill {
		return prior, 0, false
	}
	residual := 0.0
	var variance float64
	for i := 0; i < OnlineBanditDimension; i++ {
		residual += e.Theta[i] * x[i]
		p := e.Precision[i]
		if p < 1 {
			p = 1
		}
		variance += x[i] * x[i] / p
	}
	residual = math.Max(-0.30, math.Min(0.30, residual))
	uncertainty := clamp01(math.Sqrt(variance / OnlineBanditDimension))
	confidence := clamp01((1-uncertainty) * math.Exp(-2.5*e.ErrorEWMA))
	if confidence < 0.45 {
		return prior, confidence, false
	}
	return prior * math.Exp(residual), confidence, true
}

func (b *ExpertBank) Predict(input *ModelInput, heuristicPrior float64) (weight, confidence float64, ready bool) {
	if b == nil {
		return heuristicPrior, 0, false
	}
	idx := expertIndex(input)
	x := OnlineBanditFeatures(input)
	b.mu.RLock()
	e := b.experts[idx]
	b.mu.RUnlock()
	return expertPredict(e, heuristicPrior, x)
}

// Distill learns only the teacher's residual over the cheap heuristic prior.
// No raw samples are retained; the sufficient statistics stay fixed-size.
func (b *ExpertBank) Distill(input *ModelInput, heuristicPrior, teacherWeight float64) {
	if b == nil || input == nil || heuristicPrior <= 0 || teacherWeight <= 0 {
		return
	}
	if math.IsNaN(heuristicPrior) || math.IsInf(heuristicPrior, 0) ||
		math.IsNaN(teacherWeight) || math.IsInf(teacherWeight, 0) {
		return
	}
	idx := expertIndex(input)
	x := OnlineBanditFeatures(input)
	target := math.Log(teacherWeight / heuristicPrior)
	target = math.Max(-0.55, math.Min(0.55, target))

	b.mu.Lock()
	e := normalizeExpert(b.experts[idx])
	pred := 0.0
	for i := 0; i < OnlineBanditDimension; i++ {
		pred += e.Theta[i] * x[i]
	}
	err := target - pred
	robustErr := math.Max(-0.25, math.Min(0.25, err))
	instant := math.Min(1, math.Abs(err))
	if e.Distilled == 0 {
		e.ErrorEWMA = instant
	} else {
		e.ErrorEWMA = e.ErrorEWMA*0.92 + instant*0.08
	}
	for i := 0; i < OnlineBanditDimension; i++ {
		p := math.Max(1, e.Precision[i]*0.998+x[i]*x[i])
		gain := x[i] / p
		e.Theta[i] += gain * robustErr
		e.Theta[i] = math.Max(-0.28, math.Min(0.28, e.Theta[i]))
		e.Precision[i] = math.Min(65536, p)
	}
	e.Distilled++
	b.experts[idx] = e
	b.generation++
	b.mu.Unlock()
}

func (b *ExpertBank) NeedsTeacher(input *ModelInput, confidence float64, ready bool) bool {
	if input == nil {
		return false
	}
	if input.ConnectionFailed || input.LossRate >= 0.01 {
		return true
	}
	total := input.Success + input.Failure
	if !ready {
		if total < 16 {
			return true
		}
		return total%3 == 0
	}
	if confidence < 0.65 {
		return total%3 == 0
	}
	// Mature experts are periodically refreshed against the teacher so a
	// distribution shift cannot become permanently fossilized.
	return total > 0 && total%97 == 0
}

func (b *ExpertBank) TakeDirty(threshold uint32) bool {
	if b == nil {
		return false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.generation-b.persisted >= uint64(threshold)
}

func (b *ExpertBank) marshalForPersist() ([]byte, uint64, error) {
	if b == nil {
		return nil, 0, nil
	}
	b.mu.RLock()
	snapshot := ExpertBankSnapshot{Version: 1, Experts: b.experts}
	generation := b.generation
	b.mu.RUnlock()
	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil, generation, err
	}
	if len(data) > ExpertMaxPersistBytes {
		return nil, generation, errors.New("Smart expert bank exceeded hard persistence budget")
	}
	return data, generation, nil
}

func (b *ExpertBank) markPersisted(generation uint64) {
	if b == nil {
		return
	}
	b.mu.Lock()
	if generation > b.persisted {
		b.persisted = generation
	}
	b.mu.Unlock()
}

func (b *ExpertBank) MarkClean() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.persisted = b.generation
	b.mu.Unlock()
}

func (b *ExpertBank) Snapshot() ExpertBankSnapshot {
	if b == nil {
		return ExpertBankSnapshot{Version: 1}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return ExpertBankSnapshot{Version: 1, Experts: b.experts}
}
