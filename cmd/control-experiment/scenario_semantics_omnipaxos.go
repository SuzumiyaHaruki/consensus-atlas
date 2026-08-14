package main

import (
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

func projectOmnipaxosScenarioSemantics(
	mode controlexperiment.ScenarioSemanticExposureMode,
	trace controlruntime.Trace,
	frontier controlexperiment.RiskFrontierView,
	snapshot controlruntime.Snapshot,
) (controlexperiment.ScenarioSemanticExposure, error) {
	if mode.Validate() != nil || trace.Validate() != nil || frontier.PrefixTraceDigest != trace.Digest {
		return controlexperiment.ScenarioSemanticExposure{}, errors.New("OMNIPAXOS_SCENARIO_SEMANTICS_INPUT_INVALID")
	}
	snapshotDigest, err := snapshot.Digest()
	if err != nil || snapshotDigest != frontier.SnapshotDigest {
		return controlexperiment.ScenarioSemanticExposure{}, errors.New("OMNIPAXOS_SCENARIO_SEMANTICS_SNAPSHOT_MISMATCH")
	}
	evidence, err := omnipaxosv2.ProjectEvidence(latestScenarioEvidence(trace))
	if err != nil {
		return controlexperiment.ScenarioSemanticExposure{}, err
	}
	nodes := make(map[control.NodeID]omnipaxosv2.NodeEvidence, len(evidence.Nodes))
	for _, node := range evidence.Nodes {
		nodes[node.Node] = node
	}
	items := make(map[control.ItemID]controlruntime.ItemSnapshot, len(snapshot.Items))
	for _, item := range snapshot.Items {
		items[item.ID] = item
	}
	operation := controlexperiment.ConsensusOperationNone
	for _, milestone := range frontier.Progress.SatisfiedMilestones {
		if milestone == omnipaxosMilestoneWorkloadInvoked {
			operation = controlexperiment.ConsensusOperationInflight
		}
		if milestone == omnipaxosMilestoneDecisionAfterDrop {
			operation = controlexperiment.ConsensusOperationNone
		}
	}
	hints := make([]controlexperiment.ConsensusActionHint, 0, len(frontier.Actions))
	for _, action := range frontier.Actions {
		actor := action.Node.Node
		if action.MessageSource.Node != "" {
			actor = action.MessageSource.Node
		} else if action.Owner.Node != "" {
			actor = action.Owner.Node
		}
		hint := controlexperiment.ConsensusActionHint{
			ActionID: action.ActionID, ActionDigest: action.ActionDigest,
			ActorRole:      controlexperiment.ConsensusSemanticUnknown,
			MessageClass:   controlexperiment.ConsensusSemanticUnknown,
			EpochRelation:  controlexperiment.ConsensusSemanticUnknown,
			OperationState: operation,
		}
		if node, ok := nodes[actor]; ok {
			hint.ActorRole = controlexperiment.ConsensusActorReplica
			if node.Leader == node.Node {
				hint.ActorRole = controlexperiment.ConsensusActorLeader
			}
		}
		if item, ok := items[action.ItemID]; ok && item.Value.Message != nil {
			switch item.Value.Message.TypeHint {
			case "ble":
				hint.MessageClass = controlexperiment.ConsensusMessageVote
			case "sequence-paxos":
				hint.MessageClass = controlexperiment.ConsensusMessageReplication
			}
		}
		hints = append(hints, hint)
	}
	exposure, err := controlexperiment.NewScenarioSemanticExposure(
		controlexperiment.ScenarioSemanticExposureFull, frontier, hints,
	)
	if err != nil {
		return controlexperiment.ScenarioSemanticExposure{}, err
	}
	if mode == controlexperiment.ScenarioSemanticExposureMasked {
		return controlexperiment.MaskScenarioSemanticExposure(exposure, frontier)
	}
	return exposure, nil
}
