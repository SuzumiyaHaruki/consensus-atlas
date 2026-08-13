package main

import (
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
	raftfamily "github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic/raft"
)

func TestEtcdraftRiskWitnessProjectorRequiresOrderedSemanticEvidence(t *testing.T) {
	n1v1 := control.NodeRef{Node: "n1", Incarnation: 1}
	n1v2 := control.NodeRef{Node: "n1", Incarnation: 2}
	trace := controlruntime.Trace{Records: []controlruntime.ActionRecord{
		{
			Step:   1,
			Action: control.Action{ID: "invoke-1", Kind: control.ActionInvoke, Node: n1v1},
			Evidence: etcdraftRiskWitnessFixtureEvidence(t, 1,
				etcdraftRiskWitnessFixtureNode{"n1", 1, true, "StateLeader", 1},
				etcdraftRiskWitnessFixtureNode{"n2", 1, true, "StateFollower", 1},
			),
		},
		{
			Step:   2,
			Action: control.Action{ID: "crash-1", Kind: control.ActionCrash, Node: n1v1},
			Evidence: etcdraftRiskWitnessFixtureEvidence(t, 2,
				etcdraftRiskWitnessFixtureNode{"n1", 1, false, "StateStopped", 1},
				etcdraftRiskWitnessFixtureNode{"n2", 1, true, "StateLeader", 2},
			),
		},
		{
			Step:   3,
			Action: control.Action{ID: "restart-1", Kind: control.ActionRestart, Node: n1v2},
			Evidence: etcdraftRiskWitnessFixtureEvidence(t, 3,
				etcdraftRiskWitnessFixtureNode{"n1", 2, true, "StateFollower", 2},
				etcdraftRiskWitnessFixtureNode{"n2", 1, true, "StateLeader", 2},
			),
			NodeTransitions: []controlruntime.NodeTransition{{
				Node:   "n1",
				Before: controlruntime.NodeSnapshot{Ref: n1v1, Lifecycle: control.NodeStopped},
				After:  controlruntime.NodeSnapshot{Ref: n1v2, Lifecycle: control.NodeRunning},
			}},
		},
	}}
	milestones, err := projectEtcdraftLeaderChangeRiskMilestones(trace, nil, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(milestones) != 3 ||
		milestones[0].MilestoneID != raftfamily.MilestoneWorkloadInvokedAtCoordinator ||
		milestones[1].MilestoneID != raftfamily.MilestoneCoordinatorChangedInflight ||
		milestones[2].MilestoneID != raftfamily.MilestoneOldCoordinatorRestarted {
		t.Fatalf("target milestones drifted: %#v", milestones)
	}
	spec, err := raftfamily.LeaderChangeWithInflightProposalWitness()
	if err != nil {
		t.Fatal(err)
	}
	result, err := semantic.NewRiskWitnessResult(
		"fixture-reached", spec, strings.Repeat("a", 64), strings.Repeat("b", 64),
		etcdraftSemanticPrefixProjectorID, milestones,
	)
	if err != nil || result.Status != semantic.RiskWitnessReached {
		t.Fatalf("ordered target evidence did not reach witness: %#v err=%v", result, err)
	}
}

func TestEtcdraftRiskWitnessProjectorDoesNotCallCompletedWorkInflight(t *testing.T) {
	n1 := control.NodeRef{Node: "n1", Incarnation: 1}
	trace := controlruntime.Trace{Records: []controlruntime.ActionRecord{
		{
			Step:   1,
			Action: control.Action{ID: "invoke-1", Kind: control.ActionInvoke, Node: n1},
			Evidence: etcdraftRiskWitnessFixtureEvidence(t, 1,
				etcdraftRiskWitnessFixtureNode{"n1", 1, true, "StateLeader", 1},
				etcdraftRiskWitnessFixtureNode{"n2", 1, true, "StateFollower", 1},
			),
		},
		{
			Step:   2,
			Action: control.Action{ID: "complete-1", Kind: control.ActionCompleteEffect, Node: n1},
			Evidence: etcdraftRiskWitnessFixtureEvidence(t, 2,
				etcdraftRiskWitnessFixtureNode{"n1", 1, false, "StateStopped", 1},
				etcdraftRiskWitnessFixtureNode{"n2", 1, true, "StateLeader", 2},
			),
		},
	}}
	clients := []controlexperiment.ClientHistoryEntry{{Step: 2}}
	milestones, err := projectEtcdraftLeaderChangeRiskMilestones(trace, clients, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(milestones) != 1 ||
		milestones[0].MilestoneID != raftfamily.MilestoneWorkloadInvokedAtCoordinator {
		t.Fatalf("completed workload was treated as inflight: %#v", milestones)
	}
}

type etcdraftRiskWitnessFixtureNode struct {
	Node        control.NodeID `json:"node"`
	Incarnation uint64         `json:"incarnation"`
	Running     bool           `json:"running"`
	Role        string         `json:"role"`
	Term        uint64         `json:"term"`
}

func etcdraftRiskWitnessFixtureEvidence(
	t *testing.T,
	logicalTime uint64,
	nodes ...etcdraftRiskWitnessFixtureNode,
) *control.EvidenceEnvelope {
	t.Helper()
	type fixtureNode struct {
		etcdraftRiskWitnessFixtureNode
		ApplicationDigest   string `json:"application_digest"`
		ApplicationCommands int    `json:"application_commands"`
	}
	type fixtureCluster struct {
		LogicalTime uint64        `json:"logical_time"`
		Nodes       []fixtureNode `json:"nodes"`
	}
	cluster := fixtureCluster{LogicalTime: logicalTime}
	for _, node := range nodes {
		cluster.Nodes = append(cluster.Nodes, fixtureNode{
			etcdraftRiskWitnessFixtureNode: node,
			ApplicationDigest:              "fixture-state",
		})
	}
	payload, err := control.NewJSONPayload("consensus-atlas/etcdraft-v2-evidence/v1", cluster)
	if err != nil {
		t.Fatal(err)
	}
	return &control.EvidenceEnvelope{Yield: control.YieldID("fixture-yield"), Payload: payload}
}
