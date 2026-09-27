package statistic

import (
	"os"
	"sync"
	"time"

	"github.com/metacubex/mihomo/common/atomic"
	"github.com/metacubex/mihomo/common/xsync"
	"github.com/metacubex/mihomo/component/memory"
)

var DefaultManager *Manager

func init() {
	DefaultManager = &Manager{
		uploadTemp:    atomic.NewInt64(0),
		downloadTemp:  atomic.NewInt64(0),
		uploadBlip:    atomic.NewInt64(0),
		downloadBlip:  atomic.NewInt64(0),
		uploadTotal:   atomic.NewInt64(0),
		downloadTotal: atomic.NewInt64(0),
		pid:           int32(os.Getpid()),
	}
	DefaultManager.ensureRateWake()

	go DefaultManager.handle()
}

type Manager struct {
	connections   xsync.Map[string, Tracker]
	smartTarget   xsync.Map[string, *xsync.Map[string, bool]]
	uploadTemp    atomic.Int64
	downloadTemp  atomic.Int64
	uploadBlip    atomic.Int64
	downloadBlip  atomic.Int64
	uploadTotal   atomic.Int64
	downloadTotal atomic.Int64
	pid           int32
	memory        uint64

	// The old rate sampler woke the process once per second forever, including
	// while the core was completely idle. rateWake is a single coalesced edge:
	// the first byte after an idle period arms sampling; sustained traffic keeps
	// the loop active without one signal per packet.
	rateInit    sync.Once
	rateWake    chan struct{}
	trafficWake chan struct{}
	rateActive  atomic.Bool
}

func (m *Manager) Join(c Tracker) {
	m.connections.Store(c.ID(), c)
	m.joinSmartTarget(c)
}

func (m *Manager) Leave(c Tracker) {
	m.connections.Delete(c.ID())
	m.leaveSmartTarget(c)
}

func (m *Manager) Get(id string) (c Tracker) {
	if value, ok := m.connections.Load(id); ok {
		c = value
	}
	return
}

func (m *Manager) Range(f func(c Tracker) bool) {
	m.connections.Range(func(key string, value Tracker) bool {
		return f(value)
	})
}

func (m *Manager) PushUploaded(size int64) {
	m.uploadTemp.Add(size)
	m.uploadTotal.Add(size)
	if size != 0 {
		m.wakeRateLoop()
	}
}

func (m *Manager) PushDownloaded(size int64) {
	m.downloadTemp.Add(size)
	m.downloadTotal.Add(size)
	if size != 0 {
		m.wakeRateLoop()
	}
}

func (m *Manager) ensureRateWake() chan struct{} {
	m.rateInit.Do(func() {
		m.rateWake = make(chan struct{}, 1)
		m.trafficWake = make(chan struct{}, 1)
	})
	return m.rateWake
}

func (m *Manager) publishTrafficWake() {
	m.ensureRateWake()
	select {
	case m.trafficWake <- struct{}{}:
	default:
		// Consumers need only one idle->active edge, not one event per byte.
	}
}

func (m *Manager) wakeRateLoop() {
	// One cheap load on the hot path while sampling is already armed. Only the
	// idle->active transition pays a CAS and channel send.
	if m.rateActive.Load() || !m.rateActive.CompareAndSwap(false, true) {
		return
	}
	wake := m.ensureRateWake()
	select {
	case wake <- struct{}{}:
	default:
		// A queued wake already represents the same active edge.
	}
	m.publishTrafficWake()
}

func (m *Manager) reclaimActiveFromPending() bool {
	if m.uploadTemp.Load() == 0 && m.downloadTemp.Load() == 0 {
		return false
	}
	if m.rateActive.CompareAndSwap(false, true) {
		// This is the narrow handoff where bytes landed after the sampler
		// cleared rateActive but before the producer reached wakeRateLoop.
		// We claimed the active edge ourselves, so we must publish the
		// maintenance edge that the producer will now intentionally skip.
		m.publishTrafficWake()
	}
	return m.rateActive.Load()
}

// TrafficWake reports the first real traffic after the rate sampler had parked.
// It is a coalesced edge for low-frequency maintenance such as stalled-flow
// recovery; it is deliberately not a per-packet notification.
func (m *Manager) TrafficWake() <-chan struct{} {
	m.ensureRateWake()
	return m.trafficWake
}

// TrafficActive reports whether the rate sampler still observes continuous
// traffic. It becomes false after one quiet sampling interval.
func (m *Manager) TrafficActive() bool {
	return m.rateActive.Load()
}

func (m *Manager) Now() (up int64, down int64) {
	return m.uploadBlip.Load(), m.downloadBlip.Load()
}

func (m *Manager) Total() (up, down int64) {
	return m.uploadTotal.Load(), m.downloadTotal.Load()
}

func (m *Manager) Memory() uint64 {
	m.updateMemory()
	return m.memory
}

func (m *Manager) Snapshot() *Snapshot {
	var connections []*TrackerInfo
	m.Range(func(c Tracker) bool {
		connections = append(connections, c.Info())
		return true
	})
	return &Snapshot{
		UploadTotal:   m.uploadTotal.Load(),
		DownloadTotal: m.downloadTotal.Load(),
		Connections:   connections,
		Memory:        m.memory,
	}
}

func (m *Manager) updateMemory() {
	stat, err := memory.GetMemoryInfo(m.pid)
	if err != nil {
		return
	}
	m.memory = stat.RSS
}

func (m *Manager) ResetStatistic() {
	m.uploadTemp.Store(0)
	m.uploadBlip.Store(0)
	m.uploadTotal.Store(0)
	m.downloadTemp.Store(0)
	m.downloadBlip.Store(0)
	m.downloadTotal.Store(0)
}

func (m *Manager) handle() {
	m.runRateLoop(time.Second, nil, nil)
}

// runRateLoop preserves the public ~1 Hz traffic-rate semantics without a
// permanent ticker. When idle it owns no armed timer and blocks only on the
// first real byte (or stop in tests). After traffic ceases, one final interval
// publishes zero and the loop parks again.
//
// The rateActive handoff is deliberately two-phase. We clear it before checking
// the temp counters; any concurrent Push then either flips it back and sends a
// wake, or our own recheck sees the bytes and flips it back. Therefore an
// idle-transition race cannot strand unsampled traffic.
func (m *Manager) runRateLoop(interval time.Duration, stop <-chan struct{}, onSample func(up, down int64)) {
	if interval <= 0 {
		interval = time.Second
	}
	wake := m.ensureRateWake()
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()

	for {
		if !m.rateActive.Load() {
			select {
			case <-stop:
				return
			case <-wake:
			}
			if !m.rateActive.Load() {
				continue
			}
		} else {
			// Drain an edge that raced with our idle->active handoff. The state
			// bit, not channel depth, is authoritative.
			select {
			case <-wake:
			default:
			}
		}

		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(interval)

		select {
		case <-stop:
			return
		case <-timer.C:
		}

		up := m.uploadTemp.Swap(0)
		down := m.downloadTemp.Swap(0)
		m.uploadBlip.Store(up)
		m.downloadBlip.Store(down)
		if onSample != nil {
			onSample(up, down)
		}

		if up != 0 || down != 0 {
			continue
		}

		// No bytes in this interval: publish zero above, then attempt to park.
		// A concurrent producer after Store(false) owns the wake edge.
		m.rateActive.Store(false)
		// If bytes landed in the handoff window, either the producer has
		// already reclaimed the state (and queued both wakes), or this sampler
		// reclaims it and publishes the maintenance edge itself.
		m.reclaimActiveFromPending()
	}
}

type Snapshot struct {
	DownloadTotal int64          `json:"downloadTotal"`
	UploadTotal   int64          `json:"uploadTotal"`
	Connections   []*TrackerInfo `json:"connections"`
	Memory        uint64         `json:"memory"`
}

func (m *Manager) joinSmartTarget(c Tracker) {
	info := c.Info()
	target := info.Metadata.SmartTarget

	if target == "" {
		return
	}

	id := c.ID()

	result, _ := m.smartTarget.LoadOrStoreFn(target, func() *xsync.Map[string, bool] {
		return xsync.NewMap[string, bool]()
	})
	result.Store(id, true)
}

func (m *Manager) leaveSmartTarget(c Tracker) {
	info := c.Info()
	target := info.Metadata.SmartTarget

	if target == "" {
		return
	}

	id := c.ID()

	m.smartTarget.Compute(target, func(result *xsync.Map[string, bool], loaded bool) (*xsync.Map[string, bool], xsync.ComputeOp) {
		if loaded {
			result.Delete(id)
			if result.IsEmpty() {
				return result, xsync.DeleteOp
			}
			return result, xsync.UpdateOp
		}
		return result, xsync.CancelOp
	})
}

func (m *Manager) RangeSmartTarget(target string, fn func(id string) bool) {
	if result, ok := m.smartTarget.Load(target); ok {
		result.Range(func(id string, _ bool) bool {
			return fn(id)
		})
	}
}

