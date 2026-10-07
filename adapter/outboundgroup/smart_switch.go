package outboundgroup

import "github.com/metacubex/mihomo/component/linkprofile"

const commonModeWeakSwitchMargin = 0.20

// switchMarginForPath separates two cases that need opposite behavior:
//
//   - winner-specific degradation: use the live tunnel assessment and allow a
//     materially better node to take over sooner;
//   - common-mode weak network: keep a larger hysteresis margin because the
//     noise is likely shared by every node and switching itself adds more
//     sockets, TLS work and radio activity.
//
// A dead/unusable winner still bypasses hysteresis in stabilizeSmartOrder.
func switchMarginForPath(assessment linkprofile.Assessment, commonModeWeak bool) float64 {
	margin := smartSwitchMargin
	if assessment.Condition != linkprofile.ConditionUnknown && assessment.SwitchMargin > 0 && assessment.SwitchMargin < 1 {
		margin = assessment.SwitchMargin
	}
	if commonModeWeak && margin < commonModeWeakSwitchMargin {
		margin = commonModeWeakSwitchMargin
	}
	return margin
}
