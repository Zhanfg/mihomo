//go:build with_cfir_shadow

package outboundgroup

import (
	"sync/atomic"

	"github.com/metacubex/mihomo/component/cfir"
	"github.com/metacubex/mihomo/component/cfir/shadow"
	"github.com/metacubex/mihomo/component/netstate"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/log"
)

var cfirSmartRankingReports atomic.Uint64

func observeCFIRSmartRanking(metadata *C.Metadata, ranked []C.Proxy) {
	generation := cfir.Generation{Network: netstate.CurrentEpoch()}
	result, err := shadow.ObserveRankedCandidates(metadata, ranked, generation)
	bridged, bridgeErr := shadow.RankSnapshotThroughCFIR(metadata, ranked, generation)
	plan, planErr := shadow.MaterializeSnapshotPlan(metadata, ranked, generation)

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
