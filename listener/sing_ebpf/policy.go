//go:build with_ebpf && (linux || android)

package sing_ebpf

import (
	"net/netip"

	ECommon "github.com/metacubex/mihomo/common/ebpf"
	P "github.com/metacubex/mihomo/constant/provider"
	"github.com/metacubex/mihomo/log"

	E "github.com/metacubex/sing/common/exceptions"

	"go4.org/netipx"
)

type toIpCidr interface {
	ToIpCidr() *netipx.IPSet
}

func (i *Inbound) startBypassRuleSets() error {
	i.bypassRuleSetAccess.Lock()
	defer i.bypassRuleSetAccess.Unlock()
	if i.bypassRuleSetStarted {
		return nil
	}
	rp, ok := i.tunnel.(P.Tunnel)
	if !ok {
		return E.New("tunnel does not expose rule providers")
	}
	i.bypassRuleSetCallback = rp.RuleUpdateCallback().Register(i.updateBypassRuleSet)
	i.bypassRuleSetStarted = true
	err := i.refreshBypassCIDRsLocked()
	if err != nil {
		i.stopBypassRuleSetsLocked()
		return err
	}
	return nil
}

func (i *Inbound) stopBypassRuleSets() {
	i.bypassRuleSetAccess.Lock()
	defer i.bypassRuleSetAccess.Unlock()
	i.stopBypassRuleSetsLocked()
}

func (i *Inbound) stopBypassRuleSetsLocked() {
	if !i.bypassRuleSetStarted {
		return
	}
	if i.bypassRuleSetCallback != nil {
		_ = i.bypassRuleSetCallback.Close()
		i.bypassRuleSetCallback = nil
	}
	i.bypassRuleSetStarted = false
}

func (i *Inbound) updateBypassRuleSet(P.RuleProvider) {
	i.bypassRuleSetAccess.Lock()
	defer i.bypassRuleSetAccess.Unlock()
	if !i.bypassRuleSetStarted {
		return
	}
	if err := i.refreshBypassCIDRsLocked(); err != nil {
		if backend := i.tcBackend(); backend != nil {
			log.Errorln("[EBPF] refresh TC eBPF bypass_rule_set; keeping previous policy: %s", err.Error())
		}
		// A rule-provider update is the only thing that would otherwise ever
		// ask for this refresh again: nothing about this rule set is
		// guaranteed to change a second time, so a transient failure here
		// sticks until a restart. Hand it to the interface-update scheduler,
		// which owns the backoff, and wake it now -- setting the flag alone
		// would leave the first retry waiting on a netlink event or the drift
		// check (tcDriftCheckInterval, ten minutes) instead of the seconds
		// every other TC failure gets.
		i.bypassRuleSetNeedsRetry = true
		i.notifyTCInterfaceUpdate()
		return
	}
	i.bypassRuleSetNeedsRetry = false
}

// retryBypassRuleSetIfNeeded is updateTCInterfaces' hook into the
// bypass_rule_set half of this file: it does nothing, and reports settled,
// unless a previous refresh actually failed, so a healthy bypass_rule_set
// costs a round nothing beyond the lock and a boolean check.
func (i *Inbound) retryBypassRuleSetIfNeeded() tcSharedRewriteOutcome {
	i.bypassRuleSetAccess.Lock()
	defer i.bypassRuleSetAccess.Unlock()
	if !i.bypassRuleSetStarted {
		return tcSharedRewriteSettled
	}
	if i.bypassRuleSetBackendRequiresRebuildLocked() {
		i.bypassRuleSetNeedsRetry = false
		return tcSharedRewriteUnrecoverable
	}
	if !i.bypassRuleSetNeedsRetry {
		return tcSharedRewriteSettled
	}
	if err := i.refreshBypassCIDRsLocked(); err != nil {
		i.interfaceWarnings.bypassRuleSet.warn(i.logWarn, "retry TC eBPF bypass_rule_set refresh: ", err)
		if i.bypassRuleSetBackendRequiresRebuildLocked() {
			i.bypassRuleSetNeedsRetry = false
			return tcSharedRewriteUnrecoverable
		}
		return tcSharedRewriteRecoverable
	}
	i.bypassRuleSetNeedsRetry = false
	return tcSharedRewriteSettled
}

// bypassRuleSetBackendRequiresRebuildLocked reports whether any backend a
// refresh would have to write the compiled policy into has already been
// invalidated by a failed rollback of its own. Every write to such a backend is
// refused from then on, so repeating the refresh cannot succeed.
func (i *Inbound) bypassRuleSetBackendRequiresRebuildLocked() bool {
	if i.tcBackend().RequiresRebuild() {
		return true
	}
	if i.cgroupBackendInstance().RequiresRebuild() {
		return true
	}
	if i.sharedRewrite != nil && i.sharedRewrite.sharedBackendInstance().RequiresRebuild() {
		return true
	}
	return false
}

func (i *Inbound) refreshBypassCIDRsLocked() error {
	var prefixes []netip.Prefix
	for _, ruleSet := range i.bypassRuleSet {
		strategy := ruleSet.Strategy()
		ipCidrStrategy, ok := strategy.(toIpCidr)
		if !ok {
			continue
		}
		ipSet := ipCidrStrategy.ToIpCidr()
		if ipSet == nil {
			continue
		}
		prefixes = append(prefixes, ipSet.Prefixes()...)
	}
	if conflicts := i.fakeIPBypassConflictCount(prefixes); conflicts > 0 {
		log.Warnln("[EBPF] FakeIP force interception overrides bypass_rule_set CIDRs: overlaps=%d", conflicts)
	}
	policy, err := ECommon.CompileBypassCIDRPolicy(prefixes)
	if err != nil {
		return err
	}
	i.bypassRuleSetPolicy = policy
	i.bypassCIDR = policy.Prefixes()
	if backend := i.tcBackend(); backend != nil {
		if _, err = backend.UpdateCompiledBypassCIDR(policy); err != nil {
			return err
		}
	}
	if backend := i.cgroupBackendInstance(); backend != nil {
		if _, err = backend.UpdateCompiledBypassCIDR(policy); err != nil {
			return err
		}
	}
	if i.sharedRewrite != nil {
		if backend := i.sharedRewrite.sharedBackendInstance(); backend != nil {
			if cgroupBackend := i.cgroupBackendInstance(); cgroupBackend != nil {
				ipv4Count, ipv6Count := cgroupBackend.BypassCIDRCount()
				if err = backend.SetBypassCIDRState(ipv4Count, ipv6Count); err != nil {
					return err
				}
			} else if _, err = backend.UpdateCompiledBypassCIDR(policy); err != nil {
				return err
			}
		}
	}
	// Recompute the set the DNS fake-ip middleware consults, so domains whose
	// real addresses fall inside it keep their real IP and the kernel eBPF
	// bypass can engage. Only bypass_rule_set feeds it; publishing the private
	// ranges here would make every A/AAAA query resolve for real before
	// fake-ip could answer it. The registry unions it with the other inbounds'
	// sets and drops this inbound's share when it closes.
	i.dnsBypassSet = nil
	if len(i.bypassRuleSet) > 0 {
		var builder netipx.IPSetBuilder
		for _, prefix := range i.bypassCIDR {
			builder.AddPrefix(prefix)
		}
		if bypassSet, buildErr := builder.IPSet(); buildErr == nil {
			i.dnsBypassSet = bypassSet
		}
	}
	i.publishBypassPolicyLocked()
	return nil
}
