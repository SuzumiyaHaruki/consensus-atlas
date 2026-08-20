package main

import (
	"errors"
	"sort"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

// closureScenarioCallLowerBound is shared by the two current leader-CFT
// target compositions, not by the protocol-neutral Runtime. With three or
// four participants every remaining follower is required and the Target can
// select the set without another Agent call. Larger sets require the Agent to
// choose floor(N/2) followers after selecting the intervention.
func closureScenarioCallLowerBound(nodeCount int) int {
	if nodeCount <= 0 {
		return 0
	}
	requiredFollowers := nodeCount / 2
	if nodeCount-2 <= requiredFollowers {
		return 1
	}
	return 1 + requiredFollowers
}

type etcdraftAlternateQuorumSet struct {
	Leader          control.NodeID
	DroppedFollower control.NodeID
	Candidates      []control.NodeID
	Selected        []control.NodeID
	Required        int
	Term            string
	ResponseIndex   uint64
}

// newEtcdraftScenarioClosureFactory exposes the verified alternate-quorum
// closure to the ordinary Scenario Agent composition. It activates only for
// the matching Risk after the Runtime has actually executed the target
// MsgAppResp drop; all other etcd/raft scenarios keep public natural progress.
func newEtcdraftScenarioClosureFactory() controlexperiment.ScenarioClosureFactory {
	return func(
		context controlexperiment.ScenarioClosureContext,
	) (controlexperiment.ScenarioClosureSelector, bool, error) {
		if context.Spec.RiskID != "append-response-loss-with-alternate-quorum" {
			return nil, false, nil
		}
		dropped := context.Intervention.Action
		if dropped.Kind != control.ActionDropMessage || dropped.MessageTypeHint != "MsgAppResp" {
			return nil, false, nil
		}
		participants, err := deriveEtcdraftAlternateQuorumSet(context.Trace, dropped)
		if err != nil {
			return nil, false, err
		}
		if selected, selectionErr := selectedEtcdraftClosureAlternates(
			context.Executed, context.Intervention.Decision, participants,
		); selectionErr != nil {
			return nil, false, selectionErr
		} else {
			participants.Selected = selected
		}
		return etcdraftAlternateQuorumSetSelector(participants), true, nil
	}
}

func etcdraftScenarioClosureSupports(spec semantic.RiskWitnessSpec) bool {
	return spec.RiskID == "append-response-loss-with-alternate-quorum"
}

func etcdraftAlternateQuorumSetSelector(
	participants etcdraftAlternateQuorumSet,
) controlexperiment.ScenarioClosureSelector {
	leader := participants.Leader
	candidates := make(map[control.NodeID]struct{}, len(participants.Candidates))
	selected := make(map[control.NodeID]struct{}, len(participants.Selected))
	for _, alternate := range participants.Candidates {
		candidates[alternate] = struct{}{}
	}
	for _, alternate := range participants.Selected {
		selected[alternate] = struct{}{}
	}
	return func(
		frontier controlexperiment.ActionFrontierView,
	) (controlexperiment.ScenarioClosureSelection, error) {
		if len(selected) < participants.Required {
			// Leader effects do not choose a follower and may be completed before
			// asking the Agent to select a quorum path.
			var leaderEffects []controlexperiment.FrontierActionRef
			for _, action := range frontier.Actions {
				if isEtcdraftReadyEffect(action, leader) {
					leaderEffects = append(leaderEffects, action)
				}
			}
			if len(leaderEffects) > 1 {
				return controlexperiment.ScenarioClosureSelection{
					Status:     controlexperiment.ScenarioClosureUnderdetermined,
					Candidates: leaderEffects,
				}, nil
			}
			if len(leaderEffects) == 1 {
				return controlexperiment.ScenarioClosureSelection{
					Status: controlexperiment.ScenarioClosureSelected, Action: leaderEffects[0],
				}, nil
			}
			var bindings []controlexperiment.FrontierActionRef
			for _, action := range frontier.Actions {
				candidate := etcdraftClosureActionAlternate(action, participants, candidates)
				if candidate == "" {
					continue
				}
				if _, alreadySelected := selected[candidate]; !alreadySelected {
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
				candidate := etcdraftClosureActionAlternate(bindings[0], participants, candidates)
				selected[candidate] = struct{}{}
				return controlexperiment.ScenarioClosureSelection{
					Status: controlexperiment.ScenarioClosureSelected, Action: bindings[0],
				}, nil
			}
		}
		tiers := []func(controlexperiment.FrontierActionRef) bool{
			func(action controlexperiment.FrontierActionRef) bool {
				_, ok := selected[action.Owner.Node]
				return ok && isEtcdraftReadyEffect(action, action.Owner.Node)
			},
			func(action controlexperiment.FrontierActionRef) bool {
				_, ok := selected[action.MessageSource.Node]
				return action.Kind == control.ActionDeliverMessage &&
					ok && action.MessageTarget == leader &&
					action.MessageTypeHint == "MsgAppResp" &&
					etcdraftClosureMessageCausallyMatches(action, participants)
			},
			func(action controlexperiment.FrontierActionRef) bool {
				return isEtcdraftReadyEffect(action, leader)
			},
			func(action controlexperiment.FrontierActionRef) bool {
				_, ok := selected[action.MessageTarget]
				return action.Kind == control.ActionDeliverMessage &&
					action.MessageSource.Node == leader && ok &&
					action.MessageTypeHint == "MsgApp" &&
					etcdraftClosureMessageCausallyMatches(action, participants)
			},
		}
		for _, matchesTier := range tiers {
			matches := make([]controlexperiment.FrontierActionRef, 0, len(participants.Selected))
			for _, action := range frontier.Actions {
				if matchesTier(action) {
					matches = append(matches, action)
				}
			}
			if len(matches) > 1 {
				if len(selected) > 1 {
					// Quorum membership was already selected through trusted
					// executed choices. Concurrent actions within that chosen
					// quorum are causally equivalent, so retain the Runtime's
					// stable ActionID order instead of asking the Agent to choose
					// the same membership again.
					return controlexperiment.ScenarioClosureSelection{
						Status: controlexperiment.ScenarioClosureSelected, Action: matches[0],
					}, nil
				}
				return controlexperiment.ScenarioClosureSelection{
					Status:     controlexperiment.ScenarioClosureUnderdetermined,
					Candidates: matches,
				}, nil
			}
			if len(matches) == 1 {
				return controlexperiment.ScenarioClosureSelection{
					Status: controlexperiment.ScenarioClosureSelected, Action: matches[0],
				}, nil
			}
		}
		return controlexperiment.ScenarioClosureSelection{
			Status: controlexperiment.ScenarioClosureNoEligible,
		}, nil
	}
}

func isEtcdraftReadyEffect(action controlexperiment.FrontierActionRef, owner control.NodeID) bool {
	return action.Kind == control.ActionCompleteEffect && action.Owner.Node == owner &&
		(action.EffectKind == "raft-ready-persist" || action.EffectKind == "raft-ready-advance")
}

func deriveEtcdraftAlternateQuorumSet(
	trace controlruntime.Trace,
	dropped controlexperiment.FrontierActionRef,
) (etcdraftAlternateQuorumSet, error) {
	responseIndex, indexErr := strconv.ParseUint(dropped.MessageMetadata["index"], 10, 64)
	if trace.Validate() != nil || dropped.Kind != control.ActionDropMessage ||
		dropped.MessageTypeHint != "MsgAppResp" || dropped.MessageSource.Node == "" ||
		dropped.MessageTarget == "" || dropped.MessageSource.Node == dropped.MessageTarget ||
		dropped.MessageMetadata["term"] == "" || indexErr != nil || responseIndex == 0 {
		return etcdraftAlternateQuorumSet{},
			errors.New("ETCDRAFT_CLOSURE_INTERVENTION_INVALID")
	}
	evidence, err := etcdraftv2.ProjectEvidence(latestScenarioEvidence(trace))
	if err != nil {
		return etcdraftAlternateQuorumSet{}, err
	}
	participants := etcdraftAlternateQuorumSet{
		Leader: dropped.MessageTarget, DroppedFollower: dropped.MessageSource.Node,
		Term: dropped.MessageMetadata["term"], ResponseIndex: responseIndex,
	}
	leaderFound, droppedFound := false, false
	for _, node := range evidence.Nodes {
		switch node.Node {
		case participants.Leader:
			leaderFound = node.Running && node.Role == "StateLeader"
		case participants.DroppedFollower:
			droppedFound = node.Running && node.Role == "StateFollower"
		default:
			if !node.Running || node.Role != "StateFollower" || node.Node == "" {
				return etcdraftAlternateQuorumSet{},
					errors.New("ETCDRAFT_CLOSURE_PARTICIPANTS_INVALID")
			}
			participants.Candidates = append(participants.Candidates, node.Node)
		}
	}
	if !leaderFound || !droppedFound || len(participants.Candidates) == 0 {
		return etcdraftAlternateQuorumSet{},
			errors.New("ETCDRAFT_CLOSURE_PARTICIPANTS_INVALID")
	}
	participants.Required = len(evidence.Nodes) / 2
	if participants.Required <= 0 || len(participants.Candidates) < participants.Required {
		return etcdraftAlternateQuorumSet{}, errors.New("ETCDRAFT_CLOSURE_QUORUM_UNAVAILABLE")
	}
	if len(participants.Candidates) == participants.Required {
		participants.Selected = append([]control.NodeID(nil), participants.Candidates...)
	}
	sort.Slice(participants.Candidates, func(i, j int) bool {
		return participants.Candidates[i] < participants.Candidates[j]
	})
	sort.Slice(participants.Selected, func(i, j int) bool { return participants.Selected[i] < participants.Selected[j] })
	return participants, nil
}

func selectedEtcdraftClosureAlternates(
	choices []controlexperiment.FrontierChoice,
	interventionDecision int,
	participants etcdraftAlternateQuorumSet,
) ([]control.NodeID, error) {
	candidates := make(map[control.NodeID]struct{}, len(participants.Candidates))
	for _, candidate := range participants.Candidates {
		candidates[candidate] = struct{}{}
	}
	selected := make(map[control.NodeID]struct{}, len(participants.Selected))
	for _, candidate := range participants.Selected {
		selected[candidate] = struct{}{}
	}
	for _, choice := range choices {
		if choice.Decision <= interventionDecision {
			continue
		}
		candidate := etcdraftClosureActionAlternate(choice.Action, participants, candidates)
		if candidate == "" {
			continue
		}
		selected[candidate] = struct{}{}
	}
	if len(selected) > participants.Required {
		return nil, errors.New("ETCDRAFT_CLOSURE_ALTERNATE_CONFLICT")
	}
	result := make([]control.NodeID, 0, len(selected))
	for candidate := range selected {
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

func etcdraftClosureActionAlternate(
	action controlexperiment.FrontierActionRef,
	participants etcdraftAlternateQuorumSet,
	candidates map[control.NodeID]struct{},
) control.NodeID {
	if action.Kind != control.ActionDeliverMessage ||
		!etcdraftClosureMessageCausallyMatches(action, participants) {
		return ""
	}
	if action.MessageSource.Node == participants.Leader && action.MessageTypeHint == "MsgApp" {
		if _, ok := candidates[action.MessageTarget]; ok {
			return action.MessageTarget
		}
	}
	if action.MessageTarget == participants.Leader && action.MessageTypeHint == "MsgAppResp" {
		if _, ok := candidates[action.MessageSource.Node]; ok {
			return action.MessageSource.Node
		}
	}
	return ""
}

func etcdraftClosureMessageCausallyMatches(
	action controlexperiment.FrontierActionRef,
	participants etcdraftAlternateQuorumSet,
) bool {
	// Direct selector tests may construct a legacy participant tuple. Actual
	// Target composition always derives and validates the term/index below.
	if participants.Term == "" || participants.ResponseIndex == 0 {
		return true
	}
	if action.MessageMetadata["term"] != participants.Term {
		return false
	}
	index, err := strconv.ParseUint(action.MessageMetadata["index"], 10, 64)
	if err != nil {
		return false
	}
	switch action.MessageTypeHint {
	case "MsgAppResp", "MsgApp":
		// A follower that did not acknowledge the dropped response may still be
		// one entry behind. Permit that request-adjacent catch-up step, but reject
		// older traffic and every other term.
		minimum := participants.ResponseIndex - 1
		return index >= minimum
	default:
		return false
	}
}
