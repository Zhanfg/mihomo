//go:build with_cfir_shadow

package outboundgroup

import (
	"sync/atomic"

	"github.com/metacubex/mihomo/component/cfir"
	"github.com/metacubex/mihomo/component/cfir/legacybridge"
	"github.com/metacubex/mihomo/component/cfir/shadow"
	"github.com/metacubex/mihomo/component/netstate"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/log"
)

var (
	cfirSmartRankingReports  atomic.Uint64
	cfirSmartSecurityReports atomic.Uint64
)

func observeCFIRSmartRanking(metadata *C.Metadata, ranked []C.Proxy) {
	observeCFIRSmartRankingWithScores(metadata, ranked, nil)
}

func observeCFIRSmartRankingWithScores(
	metadata *C.Metadata,
	ranked []C.Proxy,
	scores map[string]cfir.CandidateScore,
) {
	generation := cfir.Generation{Network: netstate.CurrentEpoch()}
	result, err := shadow.ObserveRankedCandidates(metadata, ranked, generation)
	bridged, bridgeErr := shadow.RankSnapshotThroughCFIRWithScores(metadata, ranked, generation, scores)
	plan, planErr := shadow.MaterializeSnapshotPlanWithScores(metadata, ranked, generation, scores)

	bridgeMatches := false
	if bridgeErr == nil {
		if len(bridged) == 0 {
			bridgeMatches = result.EligibleCount == 0
		} else {
			bridgeMatches =
				bridged[0].Candidate.Protocol.Instance == result.PlannerFirst.LeafName &&
				bridged[0].Candidate.Protocol.Protocol == result.PlannerFirst.Protocol
		}
	}

	planMatches := false
	if planErr == nil {
		if len(bridged) == 0 {
			planMatches = plan.Protocol == "" && plan.Instance == ""
		} else {
			planMatches =
				plan.Protocol == bridged[0].Candidate.Protocol.Protocol &&
				plan.Instance == bridged[0].Candidate.Protocol.Instance &&
				plan.Primitive == result.Intent.Primitive &&
				plan.Generation == generation
		}
	}

	if err == nil && result.Equal() && bridgeErr == nil && bridgeMatches && planErr == nil && planMatches {
		return
	}
	n := cfirSmartRankingReports.Add(1)
	if n <= 8 || n&(n-1) == 0 {
		if err != nil {
			log.Warnln("[CFIR Smart Shadow] ranking projection failed (#%d): %v", n, err)
			return
		}
		if bridgeErr != nil {
			log.Warnln("[CFIR Smart Shadow] ranker bridge failed (#%d): %v", n, bridgeErr)
			return
		}
		if planErr != nil {
			log.Warnln("[CFIR Smart Shadow] plan materialization failed (#%d): %v", n, planErr)
			return
		}
		bridgeLeaf := ""
		bridgeProtocol := cfir.ProtocolID("")
		if len(bridged) > 0 {
			bridgeLeaf = bridged[0].Candidate.Protocol.Instance
			bridgeProtocol = bridged[0].Candidate.Protocol.Protocol
		}
		log.Warnln(
			"[CFIR Smart Shadow] ranking mismatch (#%d): mask=0x%x ranked=%d eligible=%d legacy=%s/%s planner=%s/%s ranker=%s/%s plan=%s/%s",
			n,
			uint64(result.Mismatch),
			result.RankedCount,
			result.EligibleCount,
			result.LegacyFirst.LeafName,
			result.LegacyFirst.Protocol,
			result.PlannerFirst.LeafName,
			result.PlannerFirst.Protocol,
			bridgeLeaf,
			bridgeProtocol,
			plan.Instance,
			plan.Protocol,
		)
	}
}



type cfirSmartScoreRecorder struct {
	metadata *C.Metadata
	scores   map[string]cfir.CandidateScore
}

func newCFIRSmartScoreRecorder(metadata *C.Metadata) *cfirSmartScoreRecorder {
	return &cfirSmartScoreRecorder{
		metadata: metadata,
		scores:   make(map[string]cfir.CandidateScore, maxSelected),
	}
}

func (r *cfirSmartScoreRecorder) Record(
	proxy C.Proxy,
	learnedUtility float64,
	uncertainty float64,
	residualUncertainty float64,
	exploredUtility float64,
	finalUtility float64,
) {
	if r == nil || proxy == nil {
		return
	}
	if uncertainty < 0 {
		uncertainty = 0
	} else if uncertainty > 1 {
		uncertainty = 1
	}
	if residualUncertainty < 0 {
		residualUncertainty = 0
	} else if residualUncertainty > 1 {
		residualUncertainty = 1
	}
	r.scores[proxy.Name()] = cfir.CandidateScore{
		LearnedUtility:  learnedUtility,
		ExploredUtility: exploredUtility,
		Utility:         finalUtility,
		Confidence:          1 - uncertainty,
		Uncertainty:         uncertainty,
		ResidualUncertainty: residualUncertainty,
	}
}

func (r *cfirSmartScoreRecorder) Finish(ranked []C.Proxy) {
	if r == nil || len(ranked) == 0 {
		return
	}
	instanceScores := make(map[string]cfir.CandidateScore, len(ranked))
	for _, proxy := range ranked {
		if proxy == nil {
			return
		}
		score, ok := r.scores[proxy.Name()]
		if !ok {
			// A fallback/top-up candidate has no comparable learned utility.
			// The existing rank-only observer will validate this snapshot.
			return
		}
		decision, err := legacybridge.ProjectProxyDecision(proxy, r.metadata)
		if err != nil || !decision.Resolved || decision.LeafName == "" {
			return
		}
		instanceScores[decision.LeafName] = score
	}
	observeCFIRSmartRankingWithScores(r.metadata, ranked, instanceScores)
}

func observeCFIRSmartConnection(metadata *C.Metadata, proxy C.Proxy, conn C.Conn) {
	result, err := shadow.ObserveSelectedConnection(
		metadata,
		proxy,
		conn,
		cfir.Generation{Network: netstate.CurrentEpoch()},
	)
	if err == nil {
		return
	}
	n := cfirSmartSecurityReports.Add(1)
	if n <= 8 || n&(n-1) == 0 {
		log.Warnln(
			"[CFIR Smart Shadow] post-dial security mismatch (#%d): protocol=%s instance=%s evidence_required=%v evidence_present=%v sealed=%v error=%v",
			n,
			result.Plan.Protocol,
			result.Plan.Instance,
			result.EvidenceRequired,
			result.EvidencePresent,
			result.Sealed,
			err,
		)
	}
}
