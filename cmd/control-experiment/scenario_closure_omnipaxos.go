package main

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

var errOmnipaxosClosureAlternateUnderdetermined = errors.New(
	"OMNIPAXOS_CLOSURE_ALTERNATE_UNDETERMINED",
)

type omnipaxosClosureParticipants struct {
	Leader          control.NodeID
	DroppedFollower control.NodeID
	Alternate       control.NodeID
	ReplicationLeaf string
}

// newOmnipaxosScenarioClosureFactory recognizes only the existing
// message-loss-before-decision Risk and an actually executed operation-carrying
// AcceptDecide or initial AcceptSync drop. It never creates protocol work; it
// only chooses among currently enabled public DeliverMessage Actions.
func newOmnipaxosScenarioClosureFactory() controlexperiment.ScenarioClosureFactory {
	return func(context controlexperiment.ScenarioClosureContext) (
		controlexperiment.ScenarioClosureSelector, bool, error,
	) {
		if context.Spec.RiskID != omnipaxosMessageLossRiskID {
			return nil, false, nil
		}
		dropped := context.Intervention.Action
		if !omnipaxosClosureReplicationIntervention(dropped) ||
			!omnipaxosClosureDropMilestoneMatches(context) ||
			!omnipaxosClosureSingleRequestMatches(context.Trace, dropped.MessageMetadata["request_id"]) {
			return nil, false, nil
		}
		selector, _, err := newOmnipaxosDecisionClosureSelector(context.Trace, dropped)
		if errors.Is(err, errOmnipaxosClosureAlternateUnderdetermined) {
			return func(controlexperiment.ActionFrontierView) (
				controlexperiment.ScenarioClosureSelection, error,
			) {
				return controlexperiment.ScenarioClosureSelection{
					Status: controlexperiment.ScenarioClosureUnderdetermined,
				}, nil
			}, true, nil
		}
		if err != nil {
			return nil, false, err
		}
		return selector, true, nil
	}
}

func newOmnipaxosDecisionClosureSelector(
	trace controlruntime.Trace,
	dropped controlexperiment.FrontierActionRef,
) (controlexperiment.ScenarioClosureSelector, omnipaxosClosureParticipants, error) {
	participants, err := deriveOmnipaxosClosureParticipants(trace, dropped)
	if err != nil {
		return nil, omnipaxosClosureParticipants{}, err
	}
	return omnipaxosDecisionClosureSelector(participants), participants, nil
}

func omnipaxosDecisionClosureSelector(
	participants omnipaxosClosureParticipants,
) controlexperiment.ScenarioClosureSelector {
	return func(frontier controlexperiment.ActionFrontierView) (
		controlexperiment.ScenarioClosureSelection, error,
	) {
		tiers := []func(controlexperiment.FrontierActionRef) bool{
			func(action controlexperiment.FrontierActionRef) bool {
				return omnipaxosClosureMessage(action, participants.Leader, participants.Alternate,
					"sequence-paxos/prepare")
			},
			func(action controlexperiment.FrontierActionRef) bool {
				return omnipaxosClosureMessage(action, participants.Alternate, participants.Leader,
					"sequence-paxos/promise")
			},
			func(action controlexperiment.FrontierActionRef) bool {
				return omnipaxosClosureMessage(action, participants.Leader, participants.Alternate,
					participants.ReplicationLeaf)
			},
			func(action controlexperiment.FrontierActionRef) bool {
				return omnipaxosClosureMessage(action, participants.Alternate, participants.Leader,
					"sequence-paxos/accepted")
			},
			func(action controlexperiment.FrontierActionRef) bool {
				return action.Kind == control.ActionDeliverMessage &&
					action.MessageSource.Node == participants.Leader &&
					(action.MessageTarget == participants.Alternate ||
						action.MessageTarget == participants.DroppedFollower) &&
					action.MessageTypeHint == "sequence-paxos/decide"
			},
		}
		for _, tier := range tiers {
			var selected controlexperiment.FrontierActionRef
			matches := 0
			for _, action := range frontier.Actions {
				if tier(action) {
					selected = action
					matches++
				}
			}
			if matches > 1 {
				return controlexperiment.ScenarioClosureSelection{
					Status: controlexperiment.ScenarioClosureUnderdetermined,
				}, nil
			}
			if matches == 1 {
				return controlexperiment.ScenarioClosureSelection{
					Status: controlexperiment.ScenarioClosureSelected, Action: selected,
				}, nil
			}
		}
		return controlexperiment.ScenarioClosureSelection{
			Status: controlexperiment.ScenarioClosureNoEligible,
		}, nil
	}
}

func omnipaxosClosureReplicationLeaf(leaf string) bool {
	return leaf == "sequence-paxos/accept-decide" || leaf == "sequence-paxos/accept-sync"
}

func omnipaxosClosureReplicationIntervention(action controlexperiment.FrontierActionRef) bool {
	entryCount, err := strconv.ParseUint(action.MessageMetadata["entry_count"], 10, 64)
	return action.Kind == control.ActionDropMessage &&
		omnipaxosClosureReplicationLeaf(action.MessageTypeHint) &&
		action.MessageMetadata["request_id"] != "" &&
		err == nil && entryCount > 0
}

// The closure is intentionally scoped to the current single-request Target.
// It activates only when the trusted Risk projection selected this exact Drop
// as its operation-replication milestone. This prevents a later, unrelated
// request from borrowing an earlier Risk prefix in a multi-request trace.
func omnipaxosClosureDropMilestoneMatches(context controlexperiment.ScenarioClosureContext) bool {
	if context.Risk.Validate(context.Spec) != nil || context.Intervention.Decision <= 0 {
		return false
	}
	for _, milestone := range context.Risk.Milestones {
		if milestone.MilestoneID == omnipaxosMilestoneMessageDropped {
			return milestone.Step == uint64(context.Intervention.Decision)
		}
	}
	return false
}

func omnipaxosClosureSingleRequestMatches(trace controlruntime.Trace, requestID string) bool {
	if requestID == "" || trace.Validate() != nil {
		return false
	}
	return omnipaxosClosureInvokeRecordsMatch(trace.Records, requestID)
}

func omnipaxosClosureInvokeRecordsMatch(
	records []controlruntime.ActionRecord,
	requestID string,
) bool {
	invocations := 0
	for _, record := range records {
		if record.Action.Kind != control.ActionInvoke {
			continue
		}
		invocations++
		var parameters control.AdapterInvokeParameters
		if json.Unmarshal(record.Action.Parameters, &parameters) != nil {
			return false
		}
		input, err := omnipaxosv2.ProjectInput(parameters.Input)
		if err != nil || input.RequestID != requestID {
			return false
		}
	}
	return invocations == 1
}

func omnipaxosClosureMessage(
	action controlexperiment.FrontierActionRef,
	source control.NodeID,
	target control.NodeID,
	leafType string,
) bool {
	return action.Kind == control.ActionDeliverMessage &&
		action.MessageSource.Node == source && action.MessageTarget == target &&
		action.MessageTypeHint == leafType
}

func deriveOmnipaxosClosureParticipants(
	trace controlruntime.Trace,
	dropped controlexperiment.FrontierActionRef,
) (omnipaxosClosureParticipants, error) {
	if trace.Validate() != nil || !omnipaxosClosureReplicationIntervention(dropped) ||
		dropped.MessageSource.Node == "" || dropped.MessageTarget == "" ||
		dropped.MessageSource.Node == dropped.MessageTarget {
		return omnipaxosClosureParticipants{}, errors.New("OMNIPAXOS_CLOSURE_INTERVENTION_INVALID")
	}
	evidence, err := omnipaxosv2.ProjectEvidence(latestScenarioEvidence(trace))
	if err != nil {
		return omnipaxosClosureParticipants{}, err
	}
	participants := omnipaxosClosureParticipants{
		Leader: dropped.MessageSource.Node, DroppedFollower: dropped.MessageTarget,
		ReplicationLeaf: dropped.MessageTypeHint,
	}
	leaderFound, droppedFound := false, false
	for _, node := range evidence.Nodes {
		switch node.Node {
		case participants.Leader:
			leaderFound = node.Leader == participants.Leader
		case participants.DroppedFollower:
			droppedFound = node.Leader == participants.Leader
		default:
			// The alternate may not yet have processed the leader's Prepare, so
			// its local Leader field can legitimately be empty at this prefix.
			// Membership evidence is sufficient to identify the unique third
			// participant; the selector still requires actual leader-originated
			// enabled messages before it can advance that path.
			if node.Node == "" || participants.Alternate != "" {
				return omnipaxosClosureParticipants{}, errOmnipaxosClosureAlternateUnderdetermined
			}
			participants.Alternate = node.Node
		}
	}
	if !leaderFound || !droppedFound || participants.Alternate == "" {
		return omnipaxosClosureParticipants{}, errors.New("OMNIPAXOS_CLOSURE_PARTICIPANTS_INVALID")
	}
	return participants, nil
}
