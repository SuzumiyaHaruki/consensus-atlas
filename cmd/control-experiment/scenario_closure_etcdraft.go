package main

import (
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

var errEtcdraftClosureAlternateUnderdetermined = errors.New(
	"ETCDRAFT_CLOSURE_ALTERNATE_UNDETERMINED",
)

type etcdraftAlternateQuorumParticipants struct {
	Leader          control.NodeID
	DroppedFollower control.NodeID
	Alternate       control.NodeID
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
		selector, _, err := newEtcdraftAlternateQuorumClosureSelector(context.Trace, dropped)
		if errors.Is(err, errEtcdraftClosureAlternateUnderdetermined) {
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

// newEtcdraftAlternateQuorumClosureSelector closes one already-intervened
// write through the alternate follower derived from that intervention and
// current Adapter evidence. It is deliberately narrower than a scheduler: it
// may only choose an enabled Ready effect or replication message on
// leader<->alternate, and ambiguity stops the closure normally.
func newEtcdraftAlternateQuorumClosureSelector(
	trace controlruntime.Trace,
	dropped controlexperiment.FrontierActionRef,
) (controlexperiment.ScenarioClosureSelector, etcdraftAlternateQuorumParticipants, error) {
	participants, err := deriveEtcdraftAlternateQuorumParticipants(trace, dropped)
	if err != nil {
		return nil, etcdraftAlternateQuorumParticipants{}, err
	}
	return etcdraftAlternateQuorumSelector(participants), participants, nil
}

func etcdraftAlternateQuorumSelector(
	participants etcdraftAlternateQuorumParticipants,
) controlexperiment.ScenarioClosureSelector {
	leader, alternate := participants.Leader, participants.Alternate
	return func(
		frontier controlexperiment.ActionFrontierView,
	) (controlexperiment.ScenarioClosureSelection, error) {
		tiers := []func(controlexperiment.FrontierActionRef) bool{
			func(action controlexperiment.FrontierActionRef) bool {
				return isEtcdraftReadyEffect(action, alternate)
			},
			func(action controlexperiment.FrontierActionRef) bool {
				return action.Kind == control.ActionDeliverMessage &&
					action.MessageSource.Node == alternate && action.MessageTarget == leader &&
					action.MessageTypeHint == "MsgAppResp"
			},
			func(action controlexperiment.FrontierActionRef) bool {
				return isEtcdraftReadyEffect(action, leader)
			},
			func(action controlexperiment.FrontierActionRef) bool {
				return action.Kind == control.ActionDeliverMessage &&
					action.MessageSource.Node == leader && action.MessageTarget == alternate &&
					action.MessageTypeHint == "MsgApp"
			},
		}
		for _, matchesTier := range tiers {
			var selected controlexperiment.FrontierActionRef
			matches := 0
			for _, action := range frontier.Actions {
				if matchesTier(action) {
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

func isEtcdraftReadyEffect(action controlexperiment.FrontierActionRef, owner control.NodeID) bool {
	return action.Kind == control.ActionCompleteEffect && action.Owner.Node == owner &&
		(action.EffectKind == "raft-ready-persist" || action.EffectKind == "raft-ready-advance")
}

// deriveEtcdraftAlternateQuorumParticipants binds the fixed closure strategy
// to the actual executed intervention and latest Adapter evidence. It does not
// infer participants from node names or from an Agent assertion.
func deriveEtcdraftAlternateQuorumParticipants(
	trace controlruntime.Trace,
	dropped controlexperiment.FrontierActionRef,
) (etcdraftAlternateQuorumParticipants, error) {
	if trace.Validate() != nil || dropped.Kind != control.ActionDropMessage ||
		dropped.MessageTypeHint != "MsgAppResp" || dropped.MessageSource.Node == "" ||
		dropped.MessageTarget == "" || dropped.MessageSource.Node == dropped.MessageTarget {
		return etcdraftAlternateQuorumParticipants{},
			errors.New("ETCDRAFT_CLOSURE_INTERVENTION_INVALID")
	}
	evidence, err := etcdraftv2.ProjectEvidence(latestScenarioEvidence(trace))
	if err != nil {
		return etcdraftAlternateQuorumParticipants{}, err
	}
	participants := etcdraftAlternateQuorumParticipants{
		Leader: dropped.MessageTarget, DroppedFollower: dropped.MessageSource.Node,
	}
	leaderFound, droppedFound := false, false
	for _, node := range evidence.Nodes {
		switch node.Node {
		case participants.Leader:
			leaderFound = node.Running && node.Role == "StateLeader"
		case participants.DroppedFollower:
			droppedFound = node.Running && node.Role == "StateFollower"
		default:
			if !node.Running || node.Role != "StateFollower" || participants.Alternate != "" {
				return etcdraftAlternateQuorumParticipants{}, errEtcdraftClosureAlternateUnderdetermined
			}
			participants.Alternate = node.Node
		}
	}
	if !leaderFound || !droppedFound || participants.Alternate == "" {
		return etcdraftAlternateQuorumParticipants{},
			errors.New("ETCDRAFT_CLOSURE_PARTICIPANTS_INVALID")
	}
	return participants, nil
}
