//go:build with_cfir_shadow

package tunnel

import (
	"sync/atomic"

	"github.com/metacubex/mihomo/component/cfir"
	"github.com/metacubex/mihomo/component/cfir/shadow"
	"github.com/metacubex/mihomo/component/netstate"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/log"
)

var cfirShadowCounters shadow.Counters
var cfirShadowReports atomic.Uint64

func observeCFIRShadow(metadata *C.Metadata) {
	result, err := shadow.ObserveMetadata(metadata, cfir.Generation{
		Network: netstate.CurrentEpoch(),
	})
	cfirShadowCounters.Record(result, err)
	if err == nil && result.Equal() {
		return
	}

	// Bound diagnostic noise even if an early CFIR build has a systematic
	// mismatch. First eight observations are logged, then powers of two.
	n := cfirShadowReports.Add(1)
	if n <= 8 || n&(n-1) == 0 {
		if err != nil {
			log.Warnln("[CFIR Shadow] metadata conversion failed (#%d): %v", n, err)
			return
		}
		log.Warnln("[CFIR Shadow] metadata semantic mismatch (#%d): mask=0x%x", n, uint64(result.Mismatch))
	}
}
