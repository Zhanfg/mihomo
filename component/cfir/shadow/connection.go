package shadow

import (
	"fmt"

	"github.com/metacubex/mihomo/component/cfir"
	"github.com/metacubex/mihomo/component/cfir/legacybridge"
	C "github.com/metacubex/mihomo/constant"
)

type ConnectionSecurityResult struct {
	Plan             cfir.ExecutionPlan
	Evidence         cfir.SecurityContext
	EvidencePresent  bool
	EvidenceRequired bool
	Sealed           bool
}

func MaterializeSelectedProxyPlan(metadata *C.Metadata, proxy C.ProxyAdapter, generation cfir.Generation) (cfir.ExecutionPlan, error) {
	decision, err := legacybridge.ProjectProxyDecision(proxy, metadata)
	if err != nil {
		return cfir.ExecutionPlan{}, err
	}
	if decision.Action != cfir.RouteActionForward || !decision.Resolved {
		return cfir.ExecutionPlan{}, fmt.Errorf("cfir shadow: selected proxy is not a resolved forward protocol")
	}

	intent, err := legacybridge.IntentForLegacyDecision(metadata, generation, cfir.RouteActionForward)
	if err != nil {
		return cfir.ExecutionPlan{}, err
	}
	if platform, platformErr := cfir.RuntimePlatform(); platformErr == nil {
		intent.Platform = platform
	}

	registry := cfir.NewRegistry()
	if err := registry.RegisterProtocol(shadowProtocolAdapter{descriptor: decision.Family}); err != nil {
		return cfir.ExecutionPlan{}, err
	}
	if _, err := registry.ValidateProtocolCandidate(intent, decision.Instance); err != nil {
		return cfir.ExecutionPlan{}, err
	}

	return registry.MaterializeExecutionPlan(intent, cfir.ScoredCandidate{
		Candidate: cfir.PlanCandidate{Protocol: decision.Instance},
		Score: cfir.CandidateScore{
			Utility: 1,
			Confidence: 1,
			Uncertainty: 0,
		},
	})
}

func ObserveSelectedConnection(
	metadata *C.Metadata,
	proxy C.ProxyAdapter,
	connection any,
	generation cfir.Generation,
) (ConnectionSecurityResult, error) {
	plan, err := MaterializeSelectedProxyPlan(metadata, proxy, generation)
	if err != nil {
		return ConnectionSecurityResult{}, err
	}
	result := ConnectionSecurityResult{
		Plan: plan,
		EvidenceRequired: plan.SecurityOffer != (cfir.SecurityProfile{}),
	}

	evidence, ok := cfir.SecurityEvidenceFrom(connection)
	if !ok {
		if result.EvidenceRequired {
			return result, fmt.Errorf(
				"cfir shadow: selected secure protocol %s/%s returned no runtime security evidence",
				plan.Protocol, plan.Instance,
			)
		}
		return result, nil
	}

	result.Evidence = evidence
	result.EvidencePresent = true
	sealed, err := plan.WithSecurityEvidence(connection)
	if err != nil {
		return result, err
	}
	result.Plan = sealed
	result.Sealed = true
	return result, nil
}
