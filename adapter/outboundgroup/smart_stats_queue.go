package outboundgroup

import (
	"sync"

	"github.com/metacubex/mihomo/component/smart"
	"github.com/metacubex/mihomo/component/smart/tcpstats"
	C "github.com/metacubex/mihomo/constant"
)

// Smart connection statistics used to spawn one goroutine for every closed
// connection. On Android a browser/app burst can close hundreds of short-lived
// connections together, turning learning itself into a CPU/RAM spike.
//
// One bounded process-wide queue keeps learning asynchronous without allowing
// goroutine growth to follow connection churn. Four workers are enough because
// the work is mostly small in-memory updates plus batched store writes.
const (
	smartStatsWorkerCount = 4
	smartStatsQueueSize   = 256
)

type smartStatsJob struct {
	group               *Smart
	metadata            *C.Metadata
	proxy               C.Proxy
	connectTime          int64
	latency              int64
	uploadTotal          int64
	downloadTotal        int64
	maxUploadRate        int64
	maxDownloadRate      int64
	connectionDuration   int64
	tcpStats             *tcpstats.Stats
	err                  error
	markCloseFailure     bool
}

var (
	smartStatsQueue     = make(chan smartStatsJob, smartStatsQueueSize)
	smartStatsWorkerOnce sync.Once
)

func startSmartStatsWorkers() {
	smartStatsWorkerOnce.Do(func() {
		for range smartStatsWorkerCount {
			go func() {
				for job := range smartStatsQueue {
					s := job.group
					if s == nil {
						continue
					}
					if job.markCloseFailure && job.err != nil && job.metadata.SmartBlock != "degraded" {
						s.markNodeFailure(job.metadata, job.proxy.Name(), true, true, smart.BlockDialFailure, 0)
					}
					s.recordConnectionStats(
						job.metadata,
						job.proxy,
						job.connectTime,
						job.latency,
						job.uploadTotal,
						job.downloadTotal,
						job.maxUploadRate,
						job.maxDownloadRate,
						job.connectionDuration,
						job.tcpStats,
						job.err,
					)
					s.finishBackgroundWork()
				}
			}()
		}
	})
}

func (s *Smart) enqueueConnectionStats(
	metadata *C.Metadata,
	proxy C.Proxy,
	connectTime, latency, uploadTotal, downloadTotal, maxUploadRate, maxDownloadRate,
	connectionDuration int64,
	tcpStatsValue *tcpstats.Stats,
	err error,
	markCloseFailure bool,
) bool {
	if s == nil || metadata == nil || proxy == nil || !s.beginBackgroundWork() {
		return false
	}
	startSmartStatsWorkers()

	job := smartStatsJob{
		group:             s,
		metadata:          metadata,
		proxy:             proxy,
		connectTime:       connectTime,
		latency:           latency,
		uploadTotal:       uploadTotal,
		downloadTotal:     downloadTotal,
		maxUploadRate:     maxUploadRate,
		maxDownloadRate:   maxDownloadRate,
		connectionDuration: connectionDuration,
		tcpStats:           tcpStatsValue,
		err:                err,
		markCloseFailure:   markCloseFailure,
	}

	select {
	case smartStatsQueue <- job:
		return true
	default:
		// Overload should reduce learning work, not damage connectivity. Keep
		// the safety signal for a failed connection, but drop redundant success
		// telemetry when the bounded queue is saturated.
		if markCloseFailure && err != nil && metadata.SmartBlock != "degraded" {
			s.markNodeFailure(metadata, proxy.Name(), true, true, smart.BlockDialFailure, 0)
		}
		s.finishBackgroundWork()
		return false
	}
}
