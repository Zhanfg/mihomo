//go:build !with_cfir_shadow

package outboundgroup

import C "github.com/metacubex/mihomo/constant"

func observeCFIRSmartRanking(metadata *C.Metadata, ranked []C.Proxy) {}


func observeCFIRSmartConnection(metadata *C.Metadata, proxy C.Proxy, conn C.Conn) {}


type cfirSmartScoreRecorder struct{}

func newCFIRSmartScoreRecorder(metadata *C.Metadata) *cfirSmartScoreRecorder {
	return nil
}

func (r *cfirSmartScoreRecorder) Record(
	proxy C.Proxy,
	learnedUtility float64,
	uncertainty float64,
	exploredUtility float64,
	finalUtility float64,
) {}

func (r *cfirSmartScoreRecorder) Finish(ranked []C.Proxy) {}
