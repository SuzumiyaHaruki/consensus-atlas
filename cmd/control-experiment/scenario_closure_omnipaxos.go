package main

import (
	"encoding/json"
	"errors"
	"sort"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

type omnipaxosClosureParticipantSet struct {
	Leader          control.NodeID
	DroppedFollower control.NodeID
	Candidates      []control.NodeID
	Selected        []control.NodeID
	Required        int
	ReplicationLeaf string
	BallotConfigID  string
	BallotNumber    string
	BallotPID       string
	SequenceSession string
	SequenceCounter string
	RequestID       string
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
		participants, err := deriveOmnipaxosClosureParticipantSet(context.Trace, dropped)
		if err != nil {
			return nil, false, err
		}
		selected, err := selectedOmnipaxosClosureAlternates(
			context.Executed, context.Intervention.Decision, participants,
		)
		if err != nil {
			return nil, false, err
		}
		participants.Selected = selected
		return omnipaxosDecisionClosureSetSelector(participants), true, nil
	}
}

func omnipaxosScenarioClosureSupports(spec semantic.RiskWitnessSpec) bool {
	return spec.RiskID == omnipaxosMessageLossRiskID
}

func omnipaxosDecisionClosureSetSelector(
	participants omnipaxosClosureParticipantSet,
) controlexperiment.ScenarioClosureSelector {
	candidates := make(map[control.NodeID]struct{}, len(participants.Candidates))
	selectedAlternates := make(map[control.NodeID]struct{}, len(participants.Selected))
	for _, candidate := range participants.Candidates {
		candidates[candidate] = struct{}{}
	}
	for _, candidate := range participants.Selected {
		selectedAlternates[candidate] = struct{}{}
	}
	return func(frontier controlexperiment.ActionFrontierView) (
		controlexperiment.ScenarioClosureSelection, error,
	) {
		if len(selectedAlternates) < participants.Required {
			selectionTiers := []string{
				"sequence-paxos/prepare", "sequence-paxos/promise",
				participants.ReplicationLeaf, "sequence-paxos/accepted",
			}
			for _, leaf := range selectionTiers {
				var bindings []controlexperiment.FrontierActionRef
				for _, action := range frontier.Actions {
					candidate := omnipaxosClosureActionAlternate(
						action, participants, candidates,
					)
					if candidate == "" || action.MessageTypeHint != leaf {
						continue
					}
					if _, alreadySelected := selectedAlternates[candidate]; !alreadySelected {
						bindings = append(bindings, action)
					}
				}
				if len(bindings) > 1 {
					return controlexperiment.ScenarioClosureSelection{
						Status:     controlexperiment.ScenarioClosureUnderdetermined,
						Candidates: bindings,
					}, nil
				}
				if len(bindings) == 1 {
					candidate := omnipaxosClosureActionAlternate(
						bindings[0], participants, candidates,
					)
					selectedAlternates[candidate] = struct{}{}
					return controlexperiment.ScenarioClosureSelection{
						Status: controlexperiment.ScenarioClosureSelected, Action: bindings[0],
					}, nil
				}
			}
		}
		tiers := []func(controlexperiment.FrontierActionRef) bool{
			func(action controlexperiment.FrontierActionRef) bool {
				_, ok := selectedAlternates[action.MessageTarget]
				return ok && omnipaxosClosureMessage(action, participants,
					participants.Leader, action.MessageTarget, "sequence-paxos/prepare")
			},
			func(action controlexperiment.FrontierActionRef) bool {
				_, ok := selectedAlternates[action.MessageSource.Node]
				return ok && omnipaxosClosureMessage(action, participants,
					action.MessageSource.Node, participants.Leader, "sequence-paxos/promise")
			},
			func(action controlexperiment.FrontierActionRef) bool {
				_, ok := selectedAlternates[action.MessageTarget]
				return ok && omnipaxosClosureMessage(action, participants,
					participants.Leader, action.MessageTarget, participants.ReplicationLeaf)
			},
			func(action controlexperiment.FrontierActionRef) bool {
				_, ok := selectedAlternates[action.MessageSource.Node]
				return ok && omnipaxosClosureMessage(action, participants,
					action.MessageSource.Node, participants.Leader, "sequence-paxos/accepted")
			},
			func(action controlexperiment.FrontierActionRef) bool {
				_, selectedTarget := selectedAlternates[action.MessageTarget]
				return action.Kind == control.ActionDeliverMessage &&
					action.MessageSource.Node == participants.Leader &&
					(selectedTarget ||
						action.MessageTarget == participants.DroppedFollower) &&
					action.MessageTypeHint == "sequence-paxos/decide" &&
					omnipaxosClosureMessageCausallyMatches(action, participants)
			},
		}
		for _, tier := range tiers {
			var selectedAction controlexperiment.FrontierActionRef
			matches := 0
			for _, action := range frontier.Actions {
				if tier(action) {
					if selectedAction.ActionID == "" {
						selectedAction = action
					}
					matches++
				}
			}
			if matches > 1 {
				if len(selectedAlternates) > 1 {
					return controlexperiment.ScenarioClosureSelection{
						Status: controlexperiment.ScenarioClosureSelected, Action: selectedAction,
					}, nil
				}
				return controlexperiment.ScenarioClosureSelection{
					Status: controlexperiment.ScenarioClosureUnderdetermined,
					Candidates: append([]controlexperiment.FrontierActionRef(nil),
						frontierTierMatches(frontier.Actions, tier)...),
				}, nil
			}
			if matches == 1 {
				return controlexperiment.ScenarioClosureSelection{
					Status: controlexperiment.ScenarioClosureSelected, Action: selectedAction,
				}, nil
			}
		}
		return controlexperiment.ScenarioClosureSelection{
			Status: controlexperiment.ScenarioClosureNoEligible,
		}, nil
	}
}

func frontierTierMatches(
	actions []controlexperiment.FrontierActionRef,
	tier func(controlexperiment.FrontierActionRef) bool,
) []controlexperiment.FrontierActionRef {
	result := make([]controlexperiment.FrontierActionRef, 0, len(actions))
	for _, action := range actions {
		if tier(action) {
			result = append(result, action)
		}
	}
	return result
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
	participants omnipaxosClosureParticipantSet,
	source control.NodeID,
	target control.NodeID,
	leafType string,
) bool {
	return action.Kind == control.ActionDeliverMessage &&
		action.MessageSource.Node == source && action.MessageTarget == target &&
		action.MessageTypeHint == leafType &&
		omnipaxosClosureMessageCausallyMatches(action, participants)
}

func deriveOmnipaxosClosureParticipantSet(
	trace controlruntime.Trace,
	dropped controlexperiment.FrontierActionRef,
) (omnipaxosClosureParticipantSet, error) {
	if trace.Validate() != nil || !omnipaxosClosureReplicationIntervention(dropped) ||
		dropped.MessageSource.Node == "" || dropped.MessageTarget == "" ||
		dropped.MessageSource.Node == dropped.MessageTarget ||
		dropped.MessageMetadata["ballot_config_id"] == "" ||
		dropped.MessageMetadata["ballot_number"] == "" ||
		dropped.MessageMetadata["ballot_pid"] == "" ||
		dropped.MessageMetadata["sequence_session"] == "" ||
		dropped.MessageMetadata["sequence_counter"] == "" {
		return omnipaxosClosureParticipantSet{}, errors.New("OMNIPAXOS_CLOSURE_INTERVENTION_INVALID")
	}
	evidence, err := omnipaxosv2.ProjectEvidence(latestScenarioEvidence(trace))
	if err != nil {
		return omnipaxosClosureParticipantSet{}, err
	}
	participants := omnipaxosClosureParticipantSet{
		Leader: dropped.MessageSource.Node, DroppedFollower: dropped.MessageTarget,
		ReplicationLeaf: dropped.MessageTypeHint,
		BallotConfigID:  dropped.MessageMetadata["ballot_config_id"],
		BallotNumber:    dropped.MessageMetadata["ballot_number"],
		BallotPID:       dropped.MessageMetadata["ballot_pid"],
		SequenceSession: dropped.MessageMetadata["sequence_session"],
		SequenceCounter: dropped.MessageMetadata["sequence_counter"],
		RequestID:       dropped.MessageMetadata["request_id"],
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
			if node.Node == "" {
				return omnipaxosClosureParticipantSet{},
					errors.New("OMNIPAXOS_CLOSURE_PARTICIPANTS_INVALID")
			}
			participants.Candidates = append(participants.Candidates, node.Node)
		}
	}
	if !leaderFound || !droppedFound || len(participants.Candidates) == 0 {
		return omnipaxosClosureParticipantSet{}, errors.New("OMNIPAXOS_CLOSURE_PARTICIPANTS_INVALID")
	}
	participants.Required = len(evidence.Nodes) / 2
	if participants.Required <= 0 || len(participants.Candidates) < participants.Required {
		return omnipaxosClosureParticipantSet{}, errors.New("OMNIPAXOS_CLOSURE_QUORUM_UNAVAILABLE")
	}
	sort.Slice(participants.Candidates, func(i, j int) bool {
		return participants.Candidates[i] < participants.Candidates[j]
	})
	if len(participants.Candidates) == participants.Required {
		participants.Selected = append([]control.NodeID(nil), participants.Candidates...)
	}
	return participants, nil
}

func selectedOmnipaxosClosureAlternates(
	choices []controlexperiment.FrontierChoice,
	interventionDecision int,
	participants omnipaxosClosureParticipantSet,
) ([]control.NodeID, error) {
	candidates := make(map[control.NodeID]struct{}, len(participants.Candidates))
	selected := make(map[control.NodeID]struct{}, len(participants.Selected))
	for _, candidate := range participants.Candidates {
		candidates[candidate] = struct{}{}
	}
	for _, candidate := range participants.Selected {
		selected[candidate] = struct{}{}
	}
	for _, choice := range choices {
		if choice.Decision <= interventionDecision {
			continue
		}
		candidate := omnipaxosClosureActionAlternate(choice.Action, participants, candidates)
		if candidate != "" {
			selected[candidate] = struct{}{}
		}
	}
	if len(selected) > participants.Required {
		return nil, errors.New("OMNIPAXOS_CLOSURE_ALTERNATE_CONFLICT")
	}
	result := make([]control.NodeID, 0, len(selected))
	for candidate := range selected {
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

func omnipaxosClosureActionAlternate(
	action controlexperiment.FrontierActionRef,
	participants omnipaxosClosureParticipantSet,
	candidates map[control.NodeID]struct{},
) control.NodeID {
	if action.Kind != control.ActionDeliverMessage ||
		!omnipaxosClosureMessageCausallyMatches(action, participants) {
		return ""
	}
	if action.MessageSource.Node == participants.Leader &&
		(action.MessageTypeHint == "sequence-paxos/prepare" ||
			omnipaxosClosureReplicationLeaf(action.MessageTypeHint)) {
		if _, ok := candidates[action.MessageTarget]; ok {
			return action.MessageTarget
		}
	}
	if action.MessageTarget == participants.Leader &&
		(action.MessageTypeHint == "sequence-paxos/promise" ||
			action.MessageTypeHint == "sequence-paxos/accepted") {
		if _, ok := candidates[action.MessageSource.Node]; ok {
			return action.MessageSource.Node
		}
	}
	return ""
}

func omnipaxosClosureMessageCausallyMatches(
	action controlexperiment.FrontierActionRef,
	participants omnipaxosClosureParticipantSet,
) bool {
	// Direct selector tests may intentionally omit a protocol instance. Actual
	// Target composition always derives these fields from the dropped message.
	if participants.BallotConfigID == "" {
		return true
	}
	metadata := action.MessageMetadata
	if metadata["ballot_config_id"] != participants.BallotConfigID ||
		metadata["ballot_number"] != participants.BallotNumber ||
		metadata["ballot_pid"] != participants.BallotPID {
		return false
	}
	switch action.MessageTypeHint {
	case "sequence-paxos/accept-sync", "sequence-paxos/accept-decide":
		return metadata["sequence_session"] == participants.SequenceSession &&
			metadata["sequence_counter"] == participants.SequenceCounter &&
			metadata["request_id"] == participants.RequestID
	case "sequence-paxos/decide":
		return metadata["sequence_session"] == participants.SequenceSession &&
			metadata["sequence_counter"] == participants.SequenceCounter
	case "sequence-paxos/prepare", "sequence-paxos/promise", "sequence-paxos/accepted":
		return true
	default:
		return false
	}
}
