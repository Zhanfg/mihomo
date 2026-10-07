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
	result, err := shadow.ObserveRankedCandidates(metadata, ranked, cfir.Generation{
		Network: netstate.CurrentEpoch(),
	})
	if err == nil && result.Equal() {
		return
	}
	n := cfirSmartRankingReports.Add(1)
	if n <= 8 || n&(n-1) == 0 {
		if err != nil {
			log.Warnln("[CFIR Smart Shadow] ranking projection failed (#%d): %v", n, err)
			return
		}
		log.Warnln(
			"[CFIR Smart Shadow] ranking mismatch (#%d): mask=0x%x ranked=%d eligible=%d legacy=%s/%s planner=%s/%s",
			n,
			uint64(result.Mismatch),
			result.RankedCount,
			result.EligibleCount,
			result.LegacyFirst.LeafName,
			result.LegacyFirst.Protocol,
			result.PlannerFirst.LeafName,
			result.PlannerFirst.Protocol,
		)
	}
}
