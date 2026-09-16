//go:build with_ebpf && (linux || android)

package sing_ebpf

import (
	"net/netip"
	"slices"

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
		// Logged regardless of which data planes are running: a cgroup-only or
		// shared-only inbound used to fail here in complete silence.
		log.Errorln("[EBPF] refresh eBPF bypass_rule_set; keeping previous policy: %s", err.Error())
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

// bypassPolicyStep is one data plane's share of installing a compiled bypass
// policy, paired with how to put the previous one back.
type bypassPolicyStep struct {
	name   string
	apply  func() error
	revert func() error
}

// applyBypassPolicySteps installs a policy across every data plane, or none of
// them. Applying in sequence and stopping at the first error is what left the
// planes disagreeing about what to bypass -- TC proxying a CIDR cgroup lets
// past, or the reverse -- silently and for good, since nothing revisits a rule
// set that does not change again.
//
// A revert that itself fails is reported alongside the original error rather
// than replacing it: the cause of the outage is the first failure, and the
// backend whose rollback failed marks itself as needing a rebuild, which the
// retry scheduler reads to stop retrying it.
func applyBypassPolicySteps(steps []bypassPolicyStep) error {
	var applied []bypassPolicyStep
	for _, step := range steps {
		if err := step.apply(); err != nil {
			err = E.Cause(err, "apply ", step.name, " bypass policy")
			for _, undo := range slices.Backward(applied) {
				if undoErr := undo.revert(); undoErr != nil {
					err = E.Errors(err, E.Cause(undoErr, "restore ", undo.name, " bypass policy"))
				}
			}
			return err
		}
		applied = append(applied, step)
	}
	return nil
}

// sharedRewriteBackend is the shared packet-rewrite backend, or nil when that
// data plane is not running.
func (i *Inbound) sharedRewriteBackend() *ECommon.SharedNetworkBackend {
	if i.sharedRewrite == nil {
		return nil
	}
	return i.sharedRewrite.sharedBackendInstance()
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
	// One policy has to reach every data plane or none of them. Applying in
	// sequence and returning on the first error leaves the planes disagreeing
	// about what to bypass -- TC proxying a CIDR cgroup lets past, or the
	// reverse -- silently and permanently, since nothing revisits a rule set
	// that did not change again. So each write records how to undo itself, and
	// a failure puts the previous policy back before reporting.
	previousPolicy, previousCIDR := i.bypassRuleSetPolicy, i.bypassCIDR
	var steps []bypassPolicyStep
	if backend := i.tcBackend(); backend != nil {
		steps = append(steps, bypassPolicyStep{
			name:   "TC",
			apply:  func() error { _, applyErr := backend.UpdateCompiledBypassCIDR(policy); return applyErr },
			revert: func() error { _, undoErr := backend.UpdateCompiledBypassCIDR(previousPolicy); return undoErr },
		})
	}
	if backend := i.cgroupBackendInstance(); backend != nil {
		previousIPv4, previousIPv6 := backend.BypassCIDRCount()
		steps = append(steps, bypassPolicyStep{
			name:   "cgroup",
			apply:  func() error { _, applyErr := backend.UpdateCompiledBypassCIDR(policy); return applyErr },
			revert: func() error { _, undoErr := backend.UpdateCompiledBypassCIDR(previousPolicy); return undoErr },
		})
		if shared := i.sharedRewriteBackend(); shared != nil {
			steps = append(steps, bypassPolicyStep{
				name: "shared packet-rewrite",
				// The shared plane mirrors the cgroup's counts rather than
				// compiling its own copy, so this reads them after the cgroup
				// step has installed the new policy.
				apply: func() error {
					ipv4Count, ipv6Count := backend.BypassCIDRCount()
					return shared.SetBypassCIDRState(ipv4Count, ipv6Count)
				},
				revert: func() error { return shared.SetBypassCIDRState(previousIPv4, previousIPv6) },
			})
		}
	} else if shared := i.sharedRewriteBackend(); shared != nil {
		steps = append(steps, bypassPolicyStep{
			name:   "shared packet-rewrite",
			apply:  func() error { _, applyErr := shared.UpdateCompiledBypassCIDR(policy); return applyErr },
			revert: func() error { _, undoErr := shared.UpdateCompiledBypassCIDR(previousPolicy); return undoErr },
		})
	}

	i.bypassRuleSetPolicy = policy
	i.bypassCIDR = policy.Prefixes()
	if err = applyBypassPolicySteps(steps); err != nil {
		i.bypassRuleSetPolicy, i.bypassCIDR = previousPolicy, previousCIDR
		return err
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
