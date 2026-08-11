package semantic

import (
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestRiskWitnessClassificationIsMechanicalAndTraceBound(t *testing.T) {
	spec := riskWitnessTestSpec(t)
	n1 := control.NodeRef{Node: "n1", Incarnation: 1}
	n2 := control.NodeRef{Node: "n2", Incarnation: 1}
	restarted := control.NodeRef{Node: "n1", Incarnation: 2}
	evidence := []RiskWitnessMilestoneEvidence{
		{
			MilestoneID: "workload-invoked", Step: 3, Kind: "trace-action",
			EvidenceDigest: strings.Repeat("a", 64), Participant: &n1,
		},
		{
			MilestoneID: "coordinator-changed", Step: 7, Kind: "target-evidence",
			EvidenceDigest: strings.Repeat("b", 64), Participant: &n2, RelatedParticipant: &n1,
		},
		{
			MilestoneID: "old-coordinator-restarted", Step: 9, Kind: "node-transition",
			EvidenceDigest: strings.Repeat("c", 64), Participant: &restarted, RelatedParticipant: &n2,
		},
	}
	result, err := NewRiskWitnessResult(
		"complete-witness", spec, strings.Repeat("d", 64), strings.Repeat("e", 64),
		"fixture/projector-v1", evidence,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != RiskWitnessReached || result.ReasonCode != "" ||
		len(result.SatisfiedMilestones) != 3 || len(result.MissingMilestones) != 0 ||
		len(result.OrderViolations) != 0 {
		t.Fatalf("complete witness was not reached: %#v", result)
	}

	tampered := result
	tampered.Status = RiskWitnessNotReached
	if err := tampered.Validate(spec); err == nil {
		t.Fatal("target-authored witness status was accepted")
	}
}

func TestRiskWitnessSeparatesMissingFromWrongOrder(t *testing.T) {
	spec := riskWitnessTestSpec(t)
	missing, err := NewRiskWitnessResult(
		"missing-witness", spec, strings.Repeat("a", 64), strings.Repeat("b", 64),
		"fixture/projector-v1", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if missing.Status != RiskWitnessNotReached ||
		missing.ReasonCode != RiskWitnessReasonMilestoneMissing ||
		len(missing.SatisfiedMilestones) != 0 || len(missing.MissingMilestones) != 3 {
		t.Fatalf("missing witness classification drifted: %#v", missing)
	}

	wrongOrder := []RiskWitnessMilestoneEvidence{
		{MilestoneID: "old-coordinator-restarted", Step: 2, Kind: "node-transition", EvidenceDigest: strings.Repeat("c", 64)},
		{MilestoneID: "workload-invoked", Step: 3, Kind: "trace-action", EvidenceDigest: strings.Repeat("d", 64)},
		{MilestoneID: "coordinator-changed", Step: 4, Kind: "target-evidence", EvidenceDigest: strings.Repeat("e", 64)},
	}
	ordered, err := NewRiskWitnessResult(
		"wrong-order-witness", spec, strings.Repeat("a", 64), strings.Repeat("b", 64),
		"fixture/projector-v1", wrongOrder,
	)
	if err != nil {
		t.Fatal(err)
	}
	if ordered.Status != RiskWitnessNotReached ||
		ordered.ReasonCode != RiskWitnessReasonOrderUnsatisfied ||
		len(ordered.MissingMilestones) != 0 || len(ordered.OrderViolations) != 1 ||
		len(ordered.SatisfiedMilestones) != 2 {
		t.Fatalf("wrong-order witness classification drifted: %#v", ordered)
	}
}

func TestRiskWitnessSpecRejectsCycles(t *testing.T) {
	_, err := NewRiskWitnessSpec(
		"cycle", "raft", "risk",
		[]string{"first", "second"},
		[]RiskWitnessOrder{{Before: "first", After: "second"}, {Before: "second", After: "first"}},
	)
	if err == nil {
		t.Fatal("cyclic family witness was accepted")
	}
}

func riskWitnessTestSpec(t *testing.T) RiskWitnessSpec {
	t.Helper()
	spec, err := NewRiskWitnessSpec(
		"leader-change-witness", "raft", "leader-change-risk",
		[]string{"workload-invoked", "coordinator-changed", "old-coordinator-restarted"},
		[]RiskWitnessOrder{
			{Before: "workload-invoked", After: "coordinator-changed"},
			{Before: "coordinator-changed", After: "old-coordinator-restarted"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}
