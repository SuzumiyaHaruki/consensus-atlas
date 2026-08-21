package main

import (
	"errors"

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
	Term            string
	ResponseIndex   uint64
}

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
	return etcdraftAlternateQuorumSetSelector(etcdraftAlternateQuorumSet{
		Leader: participants.Leader, DroppedFollower: participants.DroppedFollower,
		Candidates: []control.NodeID{participants.Alternate},
		Selected:   []control.NodeID{participants.Alternate}, Required: 1,
		Term: participants.Term, ResponseIndex: participants.ResponseIndex,
	})
}

func deriveEtcdraftAlternateQuorumParticipants(
	trace controlruntime.Trace,
	dropped controlexperiment.FrontierActionRef,
) (etcdraftAlternateQuorumParticipants, error) {
	set, err := deriveEtcdraftAlternateQuorumSet(trace, dropped)
	if err != nil {
		return etcdraftAlternateQuorumParticipants{}, err
	}
	if len(set.Candidates) != 1 {
		return etcdraftAlternateQuorumParticipants{}, errEtcdraftClosureAlternateUnderdetermined
	}
	return etcdraftAlternateQuorumParticipants{
		Leader: set.Leader, DroppedFollower: set.DroppedFollower, Alternate: set.Candidates[0],
		Term: set.Term, ResponseIndex: set.ResponseIndex,
	}, nil
}

var errOmnipaxosClosureAlternateUnderdetermined = errors.New(
	"OMNIPAXOS_CLOSURE_ALTERNATE_UNDETERMINED",
)

type omnipaxosClosureParticipants struct {
	Leader          control.NodeID
	DroppedFollower control.NodeID
	Alternate       control.NodeID
	ReplicationLeaf string
	BallotConfigID  string
	BallotNumber    string
	BallotPID       string
	SequenceSession string
	SequenceCounter string
	RequestID       string
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
	return omnipaxosDecisionClosureSetSelector(omnipaxosClosureParticipantSet{
		Leader: participants.Leader, DroppedFollower: participants.DroppedFollower,
		Candidates: []control.NodeID{participants.Alternate},
		Selected:   []control.NodeID{participants.Alternate}, Required: 1,
		ReplicationLeaf: participants.ReplicationLeaf,
		BallotConfigID:  participants.BallotConfigID, BallotNumber: participants.BallotNumber,
		BallotPID: participants.BallotPID, SequenceSession: participants.SequenceSession,
		SequenceCounter: participants.SequenceCounter, RequestID: participants.RequestID,
	})
}

func deriveOmnipaxosClosureParticipants(
	trace controlruntime.Trace,
	dropped controlexperiment.FrontierActionRef,
) (omnipaxosClosureParticipants, error) {
	set, err := deriveOmnipaxosClosureParticipantSet(trace, dropped)
	if err != nil {
		return omnipaxosClosureParticipants{}, err
	}
	if len(set.Candidates) != 1 {
		return omnipaxosClosureParticipants{}, errOmnipaxosClosureAlternateUnderdetermined
	}
	return omnipaxosClosureParticipants{
		Leader: set.Leader, DroppedFollower: set.DroppedFollower,
		Alternate: set.Candidates[0], ReplicationLeaf: set.ReplicationLeaf,
		BallotConfigID: set.BallotConfigID, BallotNumber: set.BallotNumber,
		BallotPID: set.BallotPID, SequenceSession: set.SequenceSession,
		SequenceCounter: set.SequenceCounter, RequestID: set.RequestID,
	}, nil
}
