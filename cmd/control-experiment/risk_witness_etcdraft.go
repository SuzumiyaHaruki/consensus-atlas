package main

import (
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
	raftfamily "github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic/raft"
)

func projectEtcdraftLeaderChangeRiskMilestones(
	trace controlruntime.Trace,
	clients []controlexperiment.ClientHistoryEntry,
	operations *controlexperiment.OperationHistory,
	plannedWorkload int,
) ([]semantic.RiskWitnessMilestoneEvidence, error) {
	var invoke *controlruntime.ActionRecord
	var oldCoordinator control.NodeRef
	var oldTerm uint64
	for index := range trace.Records {
		record := trace.Records[index]
		if record.Action.Kind != control.ActionInvoke {
			continue
		}
		if record.Evidence == nil {
			return nil, errors.New("ETCDRAFT_RISK_WITNESS_INVOKE_EVIDENCE_REQUIRED")
		}
		evidence, err := etcdraftv2.ProjectEvidence(*record.Evidence)
		if err != nil {
			return nil, err
		}
		target, ok := etcdraftRiskWitnessNode(evidence, record.Action.Node)
		if !ok || !target.Running || target.Role != "StateLeader" {
			continue
		}
		copyRecord := record
		invoke = &copyRecord
		oldCoordinator = record.Action.Node
		oldTerm = target.Term
		break
	}
	if invoke == nil {
		return make([]semantic.RiskWitnessMilestoneEvidence, 0), nil
	}
	terminalStep, err := etcdraftRiskWitnessTerminalStep(
		uint64(invoke.Step), clients, operations, plannedWorkload,
	)
	if err != nil {
		return nil, err
	}
	invokeDigest, err := control.CanonicalDigest(*invoke)
	if err != nil {
		return nil, err
	}
	oldAtInvoke := oldCoordinator
	milestones := []semantic.RiskWitnessMilestoneEvidence{{
		MilestoneID: raftfamily.MilestoneWorkloadInvokedAtCoordinator,
		Step:        invoke.Step, Kind: "trace-action", EvidenceDigest: invokeDigest,
		Participant: &oldAtInvoke,
	}}

	var changed *controlruntime.ActionRecord
	var newCoordinator control.NodeRef
	oldCoordinatorStopped := false
	for index := range trace.Records {
		record := trace.Records[index]
		if record.Step <= invoke.Step {
			continue
		}
		if terminalStep > 0 && record.Step >= terminalStep {
			break
		}
		if record.Evidence == nil {
			continue
		}
		evidence, err := etcdraftv2.ProjectEvidence(*record.Evidence)
		if err != nil {
			return nil, err
		}
		old, oldPresent := etcdraftRiskWitnessNode(evidence, oldCoordinator)
		if oldPresent {
			if old.Running && old.Role == "StateLeader" {
				continue
			}
			oldCoordinatorStopped = !old.Running
		} else if etcdraftRiskWitnessStoppedTransition(record, oldCoordinator) {
			oldCoordinatorStopped = true
		} else if !oldCoordinatorStopped {
			return nil, errors.New("ETCDRAFT_RISK_WITNESS_OLD_COORDINATOR_EVIDENCE_MISSING")
		}
		for _, node := range evidence.Nodes {
			if node.Node == oldCoordinator.Node || !node.Running || node.Role != "StateLeader" ||
				node.Term <= oldTerm {
				continue
			}
			copyRecord := record
			changed = &copyRecord
			newCoordinator = control.NodeRef{Node: node.Node, Incarnation: node.Incarnation}
			break
		}
		if changed != nil {
			break
		}
	}
	if changed == nil {
		return milestones, nil
	}
	changeDigest, err := control.CanonicalDigest(*changed)
	if err != nil {
		return nil, err
	}
	oldAtChange := oldCoordinator
	newAtChange := newCoordinator
	milestones = append(milestones, semantic.RiskWitnessMilestoneEvidence{
		MilestoneID: raftfamily.MilestoneCoordinatorChangedInflight,
		Step:        changed.Step, Kind: "target-evidence", EvidenceDigest: changeDigest,
		Participant: &newAtChange, RelatedParticipant: &oldAtChange,
	})

	for _, record := range trace.Records {
		if record.Step <= changed.Step {
			continue
		}
		for _, transition := range record.NodeTransitions {
			if transition.Node != oldCoordinator.Node ||
				transition.Before.Lifecycle != control.NodeStopped ||
				transition.After.Lifecycle != control.NodeRunning ||
				transition.After.Ref.Node != oldCoordinator.Node ||
				transition.After.Ref.Incarnation <= oldCoordinator.Incarnation {
				continue
			}
			restartDigest, err := control.CanonicalDigest(record)
			if err != nil {
				return nil, err
			}
			restarted := transition.After.Ref
			newAtRestart := newCoordinator
			milestones = append(milestones, semantic.RiskWitnessMilestoneEvidence{
				MilestoneID: raftfamily.MilestoneOldCoordinatorRestarted,
				Step:        record.Step, Kind: "node-transition", EvidenceDigest: restartDigest,
				Participant: &restarted, RelatedParticipant: &newAtRestart,
			})
			return milestones, nil
		}
	}
	return milestones, nil
}

func etcdraftRiskWitnessStoppedTransition(
	record controlruntime.ActionRecord,
	coordinator control.NodeRef,
) bool {
	for _, transition := range record.NodeTransitions {
		if transition.Node == coordinator.Node && transition.Before.Ref == coordinator &&
			transition.Before.Lifecycle == control.NodeRunning &&
			transition.After.Lifecycle == control.NodeStopped {
			return true
		}
	}
	return false
}

func etcdraftRiskWitnessNode(
	evidence etcdraftv2.Evidence,
	ref control.NodeRef,
) (etcdraftv2.NodeEvidence, bool) {
	for _, node := range evidence.Nodes {
		if node.Node == ref.Node && node.Incarnation == ref.Incarnation {
			return node, true
		}
	}
	return etcdraftv2.NodeEvidence{}, false
}

func etcdraftRiskWitnessTerminalStep(
	invokeStep uint64,
	clients []controlexperiment.ClientHistoryEntry,
	operations *controlexperiment.OperationHistory,
	plannedWorkload int,
) (uint64, error) {
	if operations != nil {
		for _, operation := range operations.Operations {
			if uint64(operation.InvokeStep) != invokeStep {
				continue
			}
			if operation.Response == nil {
				return 0, nil
			}
			return uint64(operation.ReturnStep), nil
		}
		return 0, errors.New("ETCDRAFT_RISK_WITNESS_OPERATION_HISTORY_MISSING")
	}
	if plannedWorkload != 1 {
		return 0, fmt.Errorf("ETCDRAFT_RISK_WITNESS_OPERATION_HISTORY_REQUIRED: %d", plannedWorkload)
	}
	if len(clients) == 0 {
		return 0, nil
	}
	if len(clients) != 1 || clients[0].Step <= 0 || uint64(clients[0].Step) < invokeStep {
		return 0, errors.New("ETCDRAFT_RISK_WITNESS_CLIENT_HISTORY_AMBIGUOUS")
	}
	return uint64(clients[0].Step), nil
}
