package linkprofile

import "testing"

func TestAssessPathStates(t *testing.T) {
	healthy := Assess(TunnelMetrics{RTTMs: 70, RTTVarMs: 3, Cwnd: 30, Unacked: 2}, 8)
	if healthy.Condition != ConditionHealthy || healthy.SwitchMargin <= 0.12 {
		t.Fatalf("healthy=%+v", healthy)
	}

	weak := Assess(TunnelMetrics{RTTMs: 650, RTTVarMs: 260, LossRate: 0.02, Cwnd: 10, Unacked: 18}, 8)
	if weak.Condition != ConditionWeak && weak.Condition != ConditionUnstable {
		t.Fatalf("weak=%+v", weak)
	}
	if weak.HedgeDelay >= healthy.HedgeDelay || weak.SwitchMargin >= healthy.SwitchMargin {
		t.Fatalf("weak policy should fail over earlier: healthy=%+v weak=%+v", healthy, weak)
	}
}

func TestAssessUnknownWithoutEvidence(t *testing.T) {
	got := Assess(TunnelMetrics{}, 0)
	if got.Condition != ConditionUnknown || got.HedgeDelay <= 0 {
		t.Fatalf("unknown=%+v", got)
	}
}

func TestAssessSparseEvidenceIsCautious(t *testing.T) {
	one := Assess(TunnelMetrics{RTTMs: 80, RTTVarMs: 8}, 1)
	many := Assess(TunnelMetrics{RTTMs: 80, RTTVarMs: 8}, 10)
	if one.Stress < many.Stress {
		t.Fatalf("sparse evidence should not be more optimistic: one=%+v many=%+v", one, many)
	}
}
