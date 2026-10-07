package smart

import "github.com/metacubex/mihomo/component/netstate"

const (
	routeEpochTCPKey = "__route_epoch_tcp"
	routeEpochUDPKey = "__route_epoch_udp"
)

// routeEvidenceCurrentAt intentionally accepts legacy epoch-0 evidence before
// the first observed handover of a process. As soon as netstate advances past
// its bootstrap epoch, only evidence learned on the current network may drive
// per-target Smart ordering.
func routeEvidenceCurrentAt(stored, current uint64) bool {
	if current <= 1 {
		return true
	}
	return stored == current
}

func routeEvidenceCurrent(stored uint64) bool {
	return routeEvidenceCurrentAt(stored, netstate.CurrentEpoch())
}

func RouteEpochWeightType(isUDP bool) string {
	if isUDP {
		return routeEpochUDPKey
	}
	return routeEpochTCPKey
}
