package semantic

import (
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

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
