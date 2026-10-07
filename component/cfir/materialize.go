package cfir

import "fmt"

// MaterializeExecutionPlan converts an admitted/ranked candidate into the
// immutable handoff consumed by protocol/platform execution. Ranking policy is
// intentionally absent here; this function only seals already-approved
// semantics into a plan and re-validates them against the registry.
func (r *Registry) MaterializeExecutionPlan(intent ExecutionIntent, scored ScoredCandidate) (ExecutionPlan, error) {
	if err := intent.Validate(); err != nil {
		return ExecutionPlan{}, err
	}
	if intent.Action != RouteActionForward {
		return ExecutionPlan{}, fmt.Errorf("cfir: cannot materialize protocol candidate for non-forward action %d", intent.Action)
	}
	if err := scored.Score.Validate(); err != nil {
		return ExecutionPlan{}, err
	}

	plan := ExecutionPlan{
		Action:              intent.Action,
		Protocol:            scored.Candidate.Protocol.Protocol,
		Instance:            scored.Candidate.Protocol.Instance,
		Capabilities:        scored.Candidate.Protocol.Capabilities,
		Security:            scored.Candidate.Protocol.Security,
		Backend:             scored.Candidate.Backend.Backend,
		BackendInstance:     scored.Candidate.Backend.Instance,
		BackendCapabilities: scored.Candidate.Backend.Capabilities,
		Platform:            intent.Platform,
		Primitive:           intent.Primitive,
		Requirements:        intent.Requirements,
		BackendRequirements: intent.BackendRequirements,
		SecurityFloor:       intent.SecurityFloor,
		Transport:           scored.Candidate.Transport,
		Generation:          intent.Generation,
	}
	if err := plan.Validate(r); err != nil {
		return ExecutionPlan{}, err
	}
	return plan, nil
}

func MaterializeLocalExecutionPlan(intent ExecutionIntent) (ExecutionPlan, error) {
	if err := intent.Validate(); err != nil {
		return ExecutionPlan{}, err
	}
	if intent.Action == RouteActionForward {
		return ExecutionPlan{}, fmt.Errorf("cfir: forward action requires a protocol candidate")
	}
	return ExecutionPlan{
		Action:              intent.Action,
		Platform:            intent.Platform,
		Primitive:           intent.Primitive,
		BackendRequirements: intent.BackendRequirements,
		Transport:           intent.Transport,
		Generation:          intent.Generation,
	}, nil
}
