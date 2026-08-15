package semantic

import (
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestQualifyRiskReportsMissingProjectorFieldAndManifestAction(t *testing.T) {
	predicates := []ObservationPredicate{
		{MilestoneID: "invoke", Kind: ObservationWorkloadInvoked, Constraints: []ObservationConstraint{{
			Field: ObservationFieldParticipantRole, Equals: "coordinator",
		}}},
		{MilestoneID: "drop", Kind: ObservationMessageDropped, Constraints: []ObservationConstraint{{
			Field: ObservationFieldOperationStage, Equals: "inflight",
		}}},
		{MilestoneID: "decision", Kind: ObservationDecisionAdvanced},
	}
	capabilities := []ObservationCapability{
		{Kind: ObservationWorkloadInvoked, Fields: []ObservationField{ObservationFieldParticipantRole},
			Values: map[ObservationField][]string{ObservationFieldParticipantRole: {"coordinator"}}},
		{Kind: ObservationMessageDropped},
		{Kind: ObservationDecisionAdvanced},
	}
	result, err := QualifyRisk(predicates, capabilities, []control.ActionKind{
		control.ActionInvoke, control.ActionDeliverMessage,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantActions := []control.ActionKind{control.ActionDropMessage, control.ActionInvoke}
	wantIssues := []RiskQualificationIssue{
		{Code: RiskIssueMissingAction, Action: control.ActionDropMessage},
		{Code: RiskIssueMissingObservationField, Kind: ObservationMessageDropped, Field: ObservationFieldOperationStage},
	}
	if result.Qualified || !reflect.DeepEqual(result.Requirements.Actions, wantActions) ||
		!reflect.DeepEqual(result.Issues, wantIssues) {
		t.Fatalf("unexpected qualification: %#v", result)
	}
}

func TestQualifyRiskRejectsUndeclaredSemanticLiteral(t *testing.T) {
	predicates := []ObservationPredicate{
		{MilestoneID: "invoke", Kind: ObservationWorkloadInvoked},
		{MilestoneID: "change", Kind: ObservationCoordinatorChange, Constraints: []ObservationConstraint{{
			Field: ObservationFieldOperationStage, Equals: "after-commit",
		}}},
	}
	result, err := QualifyRisk(predicates, []ObservationCapability{
		{Kind: ObservationWorkloadInvoked},
		{Kind: ObservationCoordinatorChange, Fields: []ObservationField{ObservationFieldOperationStage},
			Values: map[ObservationField][]string{ObservationFieldOperationStage: {"inflight"}}},
	}, []control.ActionKind{control.ActionInvoke})
	if err != nil {
		t.Fatal(err)
	}
	if result.Qualified || len(result.Issues) != 1 ||
		result.Issues[0] != (RiskQualificationIssue{
			Code: RiskIssueMissingObservationValue, Kind: ObservationCoordinatorChange,
			Field: ObservationFieldOperationStage, Value: "after-commit",
		}) {
		t.Fatalf("undeclared semantic literal was accepted: %#v", result)
	}
}

func TestQualifyRiskAcceptsMechanicallyCoveredRequirements(t *testing.T) {
	predicates := []ObservationPredicate{{
		MilestoneID: "restart", Kind: ObservationNodeRestarted,
		Constraints: []ObservationConstraint{{Field: ObservationFieldParticipantNode, BindAs: "node"}},
	}}
	result, err := QualifyRisk(predicates, []ObservationCapability{{
		Kind: ObservationNodeRestarted, Fields: []ObservationField{ObservationFieldParticipantNode},
	}}, []control.ActionKind{control.ActionRestart})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Qualified || len(result.Issues) != 0 {
		t.Fatalf("unexpected rejection: %#v", result)
	}
}
