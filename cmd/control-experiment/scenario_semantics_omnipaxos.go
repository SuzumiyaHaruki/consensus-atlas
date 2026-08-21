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
			hint.MessageClass = omnipaxosScenarioMessageClass(item.Value.Message.TypeHint)
		}
		hints = append(hints, hint)
	}
	exposure, err := controlexperiment.NewScenarioSemanticExposure(
		controlexperiment.ScenarioSemanticExposureFull, frontier, hints,
	)
	if err != nil {
		return controlexperiment.ScenarioSemanticExposure{}, err
	}
	coordination := omnipaxosScenarioCoordination(evidence)
	if coordination.Status == controlexperiment.ConsensusCoordinatorPresent {
		for _, action := range frontier.Actions {
			if action.Kind == control.ActionCompleteEffect &&
				(action.Node.Node == coordination.CoordinatorNode ||
					action.Owner.Node == coordination.CoordinatorNode) {
				coordination.InvokeReady = false
				break
			}
		}
	}
	exposure.Coordination = &coordination
	if err := exposure.Validate(frontier); err != nil {
		return controlexperiment.ScenarioSemanticExposure{}, err
	}
	if mode == controlexperiment.ScenarioSemanticExposureMasked {
		return controlexperiment.MaskScenarioSemanticExposure(exposure, frontier)
	}
	return exposure, nil
}

func omnipaxosScenarioCoordination(evidence omnipaxosv2.Evidence) controlexperiment.ConsensusCoordinationStatus {
	leaders := make(map[control.NodeID]struct{})
	changed := false
	var maxPromiseNumber, maxPromisePriority uint32
	var maxPromiseNode control.NodeID
	for _, node := range evidence.Nodes {
		if node.PromiseNumber > 0 || node.PromisePriority > 0 || node.PromiseNode != "" {
			changed = true
		}
		if node.Leader != "" {
			leaders[node.Leader] = struct{}{}
		}
		if node.PromiseNumber > maxPromiseNumber ||
			node.PromiseNumber == maxPromiseNumber && node.PromisePriority > maxPromisePriority ||
			node.PromiseNumber == maxPromiseNumber && node.PromisePriority == maxPromisePriority &&
				node.PromiseNode > maxPromiseNode {
			maxPromiseNumber = node.PromiseNumber
			maxPromisePriority = node.PromisePriority
			maxPromiseNode = node.PromiseNode
		}
	}
	candidates := make([]control.NodeID, 0, 1)
	if maxPromiseNode != "" {
		candidates = append(candidates, maxPromiseNode)
	}
	status := controlexperiment.ConsensusCoordinationStatus{
		Status: controlexperiment.ConsensusCoordinatorAbsent,
		ElectionProgress: controlexperiment.ConsensusElectionProgress{
			TermOrBallotChanged: changed, CandidateNodes: candidates,
		},
	}
	if len(leaders) == 1 {
		for leader := range leaders {
			status.Status = controlexperiment.ConsensusCoordinatorPresent
			status.CoordinatorNode = leader
			status.InvokeReady = true
		}
	} else if len(leaders) > 1 {
		status.Status = controlexperiment.ConsensusCoordinatorAmbiguous
	}
	return status
}

func omnipaxosScenarioMessageClass(messageType string) string {
	switch messageType {
	case "ble/heartbeat-request", "ble/heartbeat-reply":
		return controlexperiment.ConsensusMessageHeartbeat
	case "sequence-paxos/proposal-forward":
		return controlexperiment.ConsensusMessageProposal
	case "sequence-paxos/accept-decide", "sequence-paxos/accepted", "sequence-paxos/decide":
		return controlexperiment.ConsensusMessageReplication
	case "sequence-paxos/prepare-req", "sequence-paxos/accept-sync":
		return controlexperiment.ConsensusMessageRecovery
	default:
		return controlexperiment.ConsensusSemanticUnknown
	}
}
