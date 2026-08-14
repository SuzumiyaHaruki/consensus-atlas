package controlexperiment

import "testing"

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
