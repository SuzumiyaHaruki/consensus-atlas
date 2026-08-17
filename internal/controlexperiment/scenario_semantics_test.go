package controlexperiment

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestScenarioSemanticExposureBindsActionsAndMasksOnlySemanticValues(t *testing.T) {
	frontier := RiskFrontierView{
		PrefixTraceDigest: "prefix", SnapshotDigest: "snapshot",
		Actions: []FrontierActionRef{
			{ActionID: "action-1", ActionDigest: "digest-1"},
			{ActionID: "action-2", ActionDigest: "digest-2"},
		},
	}
	full, err := NewScenarioSemanticExposure(
		ScenarioSemanticExposureFull, frontier, []ConsensusActionHint{
			{
				ActionID: "action-1", ActionDigest: "digest-1", ActorRole: ConsensusActorLeader,
				MessageClass: ConsensusMessageReplication, EpochRelation: ConsensusEpochCurrent,
				OperationState: ConsensusOperationInflight,
			},
			{
				ActionID: "action-2", ActionDigest: "digest-2", ActorRole: ConsensusActorReplica,
				MessageClass: ConsensusMessageHeartbeat, EpochRelation: ConsensusEpochStale,
				OperationState: ConsensusOperationDecidedNotApplied,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	masked, err := MaskScenarioSemanticExposure(full, frontier)
	if err != nil || masked.Mode != ScenarioSemanticExposureMasked ||
		masked.PrefixTraceDigest != full.PrefixTraceDigest || masked.SnapshotDigest != full.SnapshotDigest ||
		len(masked.ActionHints) != len(full.ActionHints) {
		t.Fatalf("masked exposure changed trusted bindings: %#v/%v", masked, err)
	}
	for index := range masked.ActionHints {
		if masked.ActionHints[index].ActionID != full.ActionHints[index].ActionID ||
			masked.ActionHints[index].ActionDigest != full.ActionHints[index].ActionDigest ||
			masked.ActionHints[index].ActorRole != ConsensusSemanticUnknown ||
			masked.ActionHints[index].MessageClass != ConsensusSemanticUnknown ||
			masked.ActionHints[index].EpochRelation != ConsensusSemanticUnknown ||
			masked.ActionHints[index].OperationState != ConsensusSemanticUnknown {
			t.Fatalf("masked hint retained semantics or changed identity: %#v", masked.ActionHints[index])
		}
	}
	tampered := full
	tampered.ActionHints = append([]ConsensusActionHint(nil), full.ActionHints...)
	tampered.ActionHints[0].ActionDigest = "another-action"
	if err := tampered.Validate(frontier); err == nil {
		t.Fatal("semantic hint detached from current Action was accepted")
	}
}

func TestScenarioSemanticSelectorUsesAllGenericHintFields(t *testing.T) {
	actions := []FrontierActionRef{
		{ActionID: "action-1", ActionDigest: "digest-1", Kind: control.ActionDeliverMessage},
		{ActionID: "action-2", ActionDigest: "digest-2", Kind: control.ActionDeliverMessage},
	}
	semantics := ScenarioSemanticExposure{ActionHints: []ConsensusActionHint{
		{ActionID: "action-1", ActionDigest: "digest-1", ActorRole: ConsensusActorLeader,
			MessageClass: ConsensusMessageReplication, EpochRelation: ConsensusEpochCurrent,
			OperationState: ConsensusOperationInflight},
		{ActionID: "action-2", ActionDigest: "digest-2", ActorRole: ConsensusActorReplica,
			MessageClass: ConsensusMessageVote, EpochRelation: ConsensusEpochFuture,
			OperationState: ConsensusOperationNone},
	}}
	selector := FrontierActionSelector{
		Kind: control.ActionDeliverMessage, ActorRole: ConsensusActorLeader,
		MessageClass: ConsensusMessageReplication, EpochRelation: ConsensusEpochCurrent,
		OperationState: ConsensusOperationInflight,
	}
	matches := scenarioMatches(actions, semantics, selector)
	if selector.validate() != nil || len(matches) != 1 || matches[0].ActionID != "action-1" {
		t.Fatalf("generic semantic selector did not bind one Action: %#v", matches)
	}
	trace := scenarioSelectorTrace(actions, semantics, selector)
	if len(trace) != 5 || trace[0].CandidateCount != 2 ||
		trace[len(trace)-1].Field != "operation_state" || trace[len(trace)-1].CandidateCount != 1 {
		t.Fatalf("semantic selector narrowing was not explained: %#v", trace)
	}
	selector.MessageClass = ConsensusSemanticUnknown
	if selector.validate() == nil {
		t.Fatal("unknown semantic absence was accepted as a selector value")
	}
}
