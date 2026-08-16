package semantic

import (
	"reflect"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestNamespacedObservationUsesDeclaredTypedFieldsWithoutCoreKindRegistration(t *testing.T) {
	kind := ObservationKind("fixture/promise-raised")
	field := ObservationField("fixture/ballot-number")
	capabilities := []ObservationCapability{{
		Kind: kind, Fields: []ObservationField{field},
		FieldTypes: map[ObservationField]ObservationValueType{field: ObservationValueUint},
	}}
	if err := ValidateObservationCapabilities(capabilities); err != nil {
		t.Fatal(err)
	}
	predicates := []ObservationPredicate{{
		MilestoneID: "promise", Kind: kind,
		Constraints: []ObservationConstraint{{Field: field, Equals: "7"}},
	}}
	qualification, err := QualifyRisk(predicates, capabilities, nil)
	if err != nil || !qualification.Qualified {
		t.Fatalf("namespaced risk was not mechanically qualified: %#v/%v", qualification, err)
	}
	digest := strings.Repeat("a", 64)
	event := Observation{
		Kind: kind, Step: 1, SourceDigest: digest,
		Attributes: []ObservationAttribute{{Field: field, Type: ObservationValueUint, Value: "7"}},
	}
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	spec, err := NewRiskWitnessSpec(
		"fixture-promise-risk", "fixture-consensus", "promise-raised", []string{"promise"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	matched, err := MatchLinearRiskWitness(spec, predicates, ObservationHistory{
		ProjectorID: "fixture-projector", TraceDigest: digest, Events: []Observation{event},
	})
	if err != nil || len(matched) != 1 || matched[0].Step != 1 {
		t.Fatalf("namespaced observation did not match: %#v/%v", matched, err)
	}
	cloned := cloneObservations([]Observation{event})
	if !reflect.DeepEqual(cloned, []Observation{event}) {
		t.Fatalf("namespaced attributes were not cloned: %#v", cloned)
	}
	invalid := event
	invalid.Attributes = []ObservationAttribute{{Field: field, Type: ObservationValueUint, Value: "07"}}
	if invalid.Validate() == nil {
		t.Fatal("non-canonical typed scalar was accepted")
	}
}

func TestMatchLinearRiskWitnessBacktracksParticipantBinding(t *testing.T) {
	spec, err := NewRiskWitnessSpec(
		"leader-change-recovery-v1", "leader-round", "leader-change-recovery",
		[]string{"invoke", "change", "restart"},
		[]RiskWitnessOrder{{Before: "invoke", After: "change"}, {Before: "change", After: "restart"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	n1 := control.NodeRef{Node: "n1", Incarnation: 1}
	n2 := control.NodeRef{Node: "n2", Incarnation: 1}
	n3 := control.NodeRef{Node: "n3", Incarnation: 1}
	history := ObservationHistory{
		ProjectorID: "fixture-observations", TraceDigest: digest,
		Events: []Observation{
			{Kind: ObservationWorkloadInvoked, Step: 1, SourceDigest: digest, Participant: &n1},
			{Kind: ObservationWorkloadInvoked, Step: 2, SourceDigest: digest, Participant: &n2},
			{Kind: ObservationCoordinatorChange, Step: 3, SourceDigest: digest, Participant: &n3, RelatedParticipant: &n2},
			{Kind: ObservationNodeRestarted, Step: 4, SourceDigest: digest, Participant: &n2},
		},
	}
	predicates := []ObservationPredicate{
		{MilestoneID: "invoke", Kind: ObservationWorkloadInvoked, Constraints: []ObservationConstraint{
			{Field: ObservationFieldParticipant, BindAs: "old"},
		}},
		{MilestoneID: "change", Kind: ObservationCoordinatorChange, Constraints: []ObservationConstraint{
			{Field: ObservationFieldRelatedParticipant, BindAs: "old"},
			{Field: ObservationFieldParticipant, BindAs: "new"},
		}},
		{MilestoneID: "restart", Kind: ObservationNodeRestarted, Constraints: []ObservationConstraint{
			{Field: ObservationFieldParticipant, BindAs: "old"},
		}},
	}
	matched, err := MatchLinearRiskWitness(spec, predicates, history)
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 3 || matched[0].Step != 2 || matched[1].Step != 3 || matched[2].Step != 4 {
		t.Fatalf("unexpected binding match: %+v", matched)
	}
}
