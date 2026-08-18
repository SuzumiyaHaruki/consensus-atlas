package main

import (
	"errors"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	raftfamily "github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic/raft"
)

func projectEtcdraftScenarioSemantics(
	mode controlexperiment.ScenarioSemanticExposureMode,
	trace controlruntime.Trace,
	frontier controlexperiment.RiskFrontierView,
	snapshot controlruntime.Snapshot,
) (controlexperiment.ScenarioSemanticExposure, error) {
	if mode.Validate() != nil || trace.Validate() != nil || frontier.PrefixTraceDigest != trace.Digest {
		return controlexperiment.ScenarioSemanticExposure{}, errors.New("ETCDRAFT_SCENARIO_SEMANTICS_INPUT_INVALID")
	}
	snapshotDigest, err := snapshot.Digest()
	if err != nil || snapshotDigest != frontier.SnapshotDigest {
		return controlexperiment.ScenarioSemanticExposure{}, errors.New("ETCDRAFT_SCENARIO_SEMANTICS_SNAPSHOT_MISMATCH")
	}
	evidence, err := etcdraftv2.ProjectEvidence(latestScenarioEvidence(trace))
	if err != nil {
		return controlexperiment.ScenarioSemanticExposure{}, err
	}
	nodes := make(map[control.NodeID]etcdraftv2.NodeEvidence, len(evidence.Nodes))
	for _, node := range evidence.Nodes {
		nodes[node.Node] = node
	}
	items := make(map[control.ItemID]controlruntime.ItemSnapshot, len(snapshot.Items))
	for _, item := range snapshot.Items {
		items[item.ID] = item
	}
	operation, err := etcdraftScenarioOperationState(trace, frontier, evidence)
	if err != nil {
		return controlexperiment.ScenarioSemanticExposure{}, err
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
			hint.ActorRole = etcdraftScenarioActorRole(node.Role)
		}
		if item, ok := items[action.ItemID]; ok && item.Value.Message != nil {
			hint.MessageClass = etcdraftScenarioMessageClass(item.Value.Message.TypeHint)
			hint.EpochRelation = etcdraftScenarioEpochRelation(item.Value.Message.Metadata, nodes[actor])
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

func etcdraftScenarioActorRole(role string) string {
	switch role {
	case "StateLeader":
		return controlexperiment.ConsensusActorLeader
	case "StateFollower":
		return controlexperiment.ConsensusActorReplica
	case "StateCandidate", "StatePreCandidate":
		return controlexperiment.ConsensusActorContender
	default:
		return controlexperiment.ConsensusSemanticUnknown
	}
}

func etcdraftScenarioMessageClass(messageType string) string {
	switch messageType {
	case "MsgVote", "MsgVoteResp", "MsgPreVote", "MsgPreVoteResp":
		return controlexperiment.ConsensusMessageVote
	case "MsgProp":
		return controlexperiment.ConsensusMessageProposal
	case "MsgApp", "MsgAppResp", "MsgSnap":
		return controlexperiment.ConsensusMessageReplication
	case "MsgHeartbeat", "MsgHeartbeatResp":
		return controlexperiment.ConsensusMessageHeartbeat
	case "MsgSnapStatus", "MsgUnreachable", "MsgTransferLeader", "MsgTimeoutNow":
		return controlexperiment.ConsensusMessageRecovery
	default:
		return controlexperiment.ConsensusSemanticUnknown
	}
}

func etcdraftScenarioEpochRelation(
	metadata map[string]string,
	actor etcdraftv2.NodeEvidence,
) string {
	term, err := strconv.ParseUint(metadata["term"], 10, 64)
	if err != nil || term == 0 || actor.Node == "" || actor.Term == 0 {
		return controlexperiment.ConsensusSemanticUnknown
	}
	if term < actor.Term {
		return controlexperiment.ConsensusEpochStale
	}
	if term > actor.Term {
		return controlexperiment.ConsensusEpochFuture
	}
	return controlexperiment.ConsensusEpochCurrent
}

func etcdraftScenarioOperationState(
	trace controlruntime.Trace,
	frontier controlexperiment.RiskFrontierView,
	evidence etcdraftv2.Evidence,
) (string, error) {
	for _, node := range evidence.Nodes {
		if node.Commit > node.Applied {
			return controlexperiment.ConsensusOperationDecidedNotApplied, nil
		}
	}
	for _, milestone := range frontier.Progress.SatisfiedMilestones {
		if milestone == raftfamily.MilestoneWorkloadInvokedAtCoordinator {
			applied, err := etcdraftSingleWorkloadApplied(trace)
			if err != nil {
				return "", err
			}
			if applied {
				return controlexperiment.ConsensusOperationNone, nil
			}
			return controlexperiment.ConsensusOperationInflight, nil
		}
	}
	return controlexperiment.ConsensusOperationNone, nil
}

// The current authoring input contains one proposal. Until operation identity
// is available in planning prefixes, an application-command increase after its
// invoke is the conservative fact that this one workload is no longer in flight.
func etcdraftSingleWorkloadApplied(trace controlruntime.Trace) (bool, error) {
	for _, record := range trace.Records {
		if record.Action.Kind != control.ActionInvoke {
			continue
		}
		if record.Evidence == nil {
			return false, errors.New("ETCDRAFT_SCENARIO_INVOKE_EVIDENCE_REQUIRED")
		}
		evidence, err := etcdraftv2.ProjectEvidence(*record.Evidence)
		if err != nil {
			return false, err
		}
		applicationCommandsAtInvoke := 0
		for _, node := range evidence.Nodes {
			if node.ApplicationCommands > applicationCommandsAtInvoke {
				applicationCommandsAtInvoke = node.ApplicationCommands
			}
		}
		terminalStep, err := etcdraftAppliedTerminalStep(
			trace, record.Step, applicationCommandsAtInvoke,
		)
		return terminalStep > 0, err
	}
	return false, nil
}

func etcdraftAppliedTerminalStep(
	trace controlruntime.Trace,
	invokeStep uint64,
	applicationCommandsAtInvoke int,
) (uint64, error) {
	for _, record := range trace.Records {
		if record.Step <= invokeStep || record.Evidence == nil {
			continue
		}
		evidence, err := etcdraftv2.ProjectEvidence(*record.Evidence)
		if err != nil {
			return 0, err
		}
		for _, node := range evidence.Nodes {
			if node.ApplicationCommands > applicationCommandsAtInvoke {
				return record.Step, nil
			}
		}
	}
	return 0, nil
}
