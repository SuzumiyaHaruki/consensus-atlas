package targetoracles

import (
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	pb "go.etcd.io/raft/v3/raftpb"
)

type electionEvidenceSnapshot struct {
	LogicalTime uint64                 `json:"logical_time"`
	Nodes       []electionEvidenceNode `json:"nodes"`
}

type electionEvidenceNode struct {
	Node                control.NodeID `json:"node"`
	RaftID              uint64         `json:"raft_id"`
	Incarnation         uint64         `json:"incarnation"`
	Running             bool           `json:"running"`
	Role                string         `json:"role"`
	Term                uint64         `json:"term"`
	Vote                uint64         `json:"vote"`
	ConfState           pb.ConfState   `json:"conf_state"`
	Commit              uint64         `json:"commit"`
	Applied             uint64         `json:"applied"`
	ApplicationDigest   string         `json:"application_digest"`
	ApplicationCommands int            `json:"application_commands"`
}

func TestElectionSafetyMonitorDetectsDistinctLeadersInOneTerm(t *testing.T) {
	bundle := controlexperiment.ExecutionBundle{Trace: controlruntime.Trace{
		InitialEvidence: electionEvidence(t,
			electionEvidenceNode{Node: "n1", RaftID: 1, Incarnation: 1, Running: true, Role: "StateLeader", Term: 7, Vote: 1},
			electionEvidenceNode{Node: "n2", RaftID: 2, Incarnation: 1, Running: true, Role: "StateLeader", Term: 7, Vote: 1},
			electionEvidenceNode{Node: "n3", RaftID: 3, Incarnation: 1, Running: true, Role: "StateFollower", Term: 7, Vote: 1},
		),
	}}
	violations := (ElectionSafetyMonitor{}).CheckBundle(bundle)
	if len(violations) != 1 || violations[0].Monitor != ElectionSafetyMonitorID ||
		!strings.Contains(violations[0].Message, "term 7") {
		t.Fatalf("double leader was not detected: %#v", violations)
	}
}

func TestElectionSafetyMonitorAcceptsOneLeaderPerTerm(t *testing.T) {
	bundle := controlexperiment.ExecutionBundle{Trace: controlruntime.Trace{
		InitialEvidence: electionEvidence(t,
			electionEvidenceNode{Node: "n1", RaftID: 1, Incarnation: 1, Running: true, Role: "StateLeader", Term: 7, Vote: 1},
			electionEvidenceNode{Node: "n2", RaftID: 2, Incarnation: 1, Running: true, Role: "StateFollower", Term: 7, Vote: 1},
			electionEvidenceNode{Node: "n3", RaftID: 3, Incarnation: 1, Running: true, Role: "StateFollower", Term: 7, Vote: 1},
		),
	}}
	if violations := (ElectionSafetyMonitor{}).CheckBundle(bundle); len(violations) != 0 {
		t.Fatalf("normal election evidence was rejected: %#v", violations)
	}
}

func TestElectionSafetyMonitorRejectsLeaderWithoutMajority(t *testing.T) {
	bundle := controlexperiment.ExecutionBundle{Trace: controlruntime.Trace{
		InitialEvidence: electionEvidence(t,
			electionEvidenceNode{Node: "n1", RaftID: 1, Incarnation: 1, Running: true, Role: "StateLeader", Term: 9, Vote: 1},
			electionEvidenceNode{Node: "n2", RaftID: 2, Incarnation: 1, Running: true, Role: "StateFollower", Term: 9, Vote: 1},
			electionEvidenceNode{Node: "n3", RaftID: 3, Incarnation: 1, Running: true, Role: "StateFollower", Term: 9, Vote: 3},
			electionEvidenceNode{Node: "n4", RaftID: 4, Incarnation: 1, Running: true, Role: "StateFollower", Term: 9, Vote: 4},
			electionEvidenceNode{Node: "n5", RaftID: 5, Incarnation: 1, Running: true, Role: "StateFollower", Term: 9, Vote: 5},
		),
	}}
	violations := (ElectionSafetyMonitor{}).CheckBundle(bundle)
	if len(violations) != 1 || !strings.Contains(violations[0].Message, "votes=2/5 need=3") {
		t.Fatalf("minority-backed leader was not detected: %#v", violations)
	}
}

func TestElectionSafetyMonitorRequiresBothJointMajorities(t *testing.T) {
	joint := pb.ConfState{Voters: []uint64{1, 2, 3}, VotersOutgoing: []uint64{1, 3, 4}}
	bundle := controlexperiment.ExecutionBundle{Trace: controlruntime.Trace{
		InitialEvidence: electionEvidence(t,
			electionEvidenceNode{Node: "n1", RaftID: 1, Incarnation: 1, Running: true, Role: "StateLeader", Term: 10, Vote: 1, ConfState: joint},
			electionEvidenceNode{Node: "n2", RaftID: 2, Incarnation: 1, Running: true, Role: "StateFollower", Term: 10, Vote: 1, ConfState: joint},
			electionEvidenceNode{Node: "n3", RaftID: 3, Incarnation: 1, Running: true, Role: "StateFollower", Term: 10, Vote: 3, ConfState: joint},
			electionEvidenceNode{Node: "n4", RaftID: 4, Incarnation: 1, Running: true, Role: "StateFollower", Term: 10, Vote: 4, ConfState: joint},
		),
	}}
	violations := (ElectionSafetyMonitor{}).CheckBundle(bundle)
	if len(violations) != 1 || !strings.Contains(violations[0].Message, "voters-outgoing") {
		t.Fatalf("joint quorum omission was not detected: %#v", violations)
	}
}

func TestElectionSafetyMonitorAcceptsHealthyJointElection(t *testing.T) {
	joint := pb.ConfState{Voters: []uint64{1, 2, 3}, VotersOutgoing: []uint64{1, 3, 4}}
	bundle := controlexperiment.ExecutionBundle{Trace: controlruntime.Trace{
		InitialEvidence: electionEvidence(t,
			electionEvidenceNode{Node: "n1", RaftID: 1, Incarnation: 1, Running: true, Role: "StateLeader", Term: 10, Vote: 1, ConfState: joint},
			electionEvidenceNode{Node: "n2", RaftID: 2, Incarnation: 1, Running: true, Role: "StateFollower", Term: 10, Vote: 1, ConfState: joint},
			electionEvidenceNode{Node: "n3", RaftID: 3, Incarnation: 1, Running: true, Role: "StateFollower", Term: 10, Vote: 1, ConfState: joint},
			electionEvidenceNode{Node: "n4", RaftID: 4, Incarnation: 1, Running: true, Role: "StateFollower", Term: 10, Vote: 4, ConfState: joint},
		),
	}}
	if violations := (ElectionSafetyMonitor{}).CheckBundle(bundle); len(violations) != 0 {
		t.Fatalf("healthy joint election was rejected: %#v", violations)
	}
}

func TestElectionSafetyMonitorRechecksLeaderAfterRestart(t *testing.T) {
	initial := electionEvidence(t,
		electionEvidenceNode{Node: "n1", RaftID: 1, Incarnation: 1, Running: true, Role: "StateLeader", Term: 11, Vote: 1},
		electionEvidenceNode{Node: "n2", RaftID: 2, Incarnation: 1, Running: true, Role: "StateFollower", Term: 11, Vote: 1},
		electionEvidenceNode{Node: "n3", RaftID: 3, Incarnation: 1, Running: true, Role: "StateFollower", Term: 11, Vote: 1},
	)
	afterRestart := electionEvidence(t,
		electionEvidenceNode{Node: "n1", RaftID: 1, Incarnation: 2, Running: true, Role: "StateLeader", Term: 11, Vote: 1},
		electionEvidenceNode{Node: "n2", RaftID: 2, Incarnation: 1, Running: true, Role: "StateFollower", Term: 11, Vote: 2},
		electionEvidenceNode{Node: "n3", RaftID: 3, Incarnation: 1, Running: true, Role: "StateFollower", Term: 11, Vote: 3},
	)
	bundle := controlexperiment.ExecutionBundle{Trace: controlruntime.Trace{
		InitialEvidence: initial,
		Records:         []controlruntime.ActionRecord{{Step: 1, Evidence: &afterRestart}},
	}}
	violations := (ElectionSafetyMonitor{}).CheckBundle(bundle)
	if len(violations) != 1 || !strings.Contains(violations[0].Message, "votes=1/3 need=2") {
		t.Fatalf("restarted leader reused the prior incarnation quorum check: %#v", violations)
	}
}

func electionEvidence(t *testing.T, nodes ...electionEvidenceNode) control.EvidenceEnvelope {
	t.Helper()
	allVoters := make([]uint64, len(nodes))
	for index := range nodes {
		allVoters[index] = nodes[index].RaftID
	}
	for index := range nodes {
		nodes[index].ApplicationDigest = strings.Repeat("0", 64)
		if len(nodes[index].ConfState.Voters) == 0 {
			nodes[index].ConfState.Voters = append([]uint64(nil), allVoters...)
		}
	}
	payload, err := control.NewJSONPayload(
		"consensus-atlas/etcdraft-v2-evidence/v2",
		electionEvidenceSnapshot{Nodes: nodes},
	)
	if err != nil {
		t.Fatal(err)
	}
	return control.EvidenceEnvelope{Payload: payload}
}
