//go:build !with_cfir_shadow

package outboundgroup

import C "github.com/metacubex/mihomo/constant"

func observeCFIRSmartRanking(metadata *C.Metadata, ranked []C.Proxy) {}
