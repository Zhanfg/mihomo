//go:build !with_cfir_shadow

package tunnel

import C "github.com/metacubex/mihomo/constant"

// Production builds pay no runtime sampling or atomic-load cost for CFIR
// migration validation. The compiler can inline this empty hook away.
func observeCFIRShadow(metadata *C.Metadata) {}


func observeCFIRDecisionShadow(metadata *C.Metadata, proxy C.ProxyAdapter) {}
