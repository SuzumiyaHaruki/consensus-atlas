package targetoracles

import (
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

type electionEvidenceSnapshot struct {
	LogicalTime uint64                 `json:"logical_time"`
	Nodes       []electionEvidenceNode `json:"nodes"`
}

type electionEvidenceNode struct {
	Node                control.NodeID `json:"node"`
	Incarnation         uint64         `json:"incarnation"`
	Running             bool           `json:"running"`
	Role                string         `json:"role"`
	Term                uint64         `json:"term"`
	Commit              uint64         `json:"commit"`
	Applied             uint64         `json:"applied"`
	ApplicationDigest   string         `json:"application_digest"`
	ApplicationCommands int            `json:"application_commands"`
}

func TestElectionSafetyMonitorDetectsDistinctLeadersInOneTerm(t *testing.T) {
	bundle := controlexperiment.ExecutionBundle{Trace: controlruntime.Trace{
		InitialEvidence: electionEvidence(t,
			electionEvidenceNode{Node: "n1", Incarnation: 1, Running: true, Role: "StateLeader", Term: 7},
			electionEvidenceNode{Node: "n2", Incarnation: 1, Running: true, Role: "StateLeader", Term: 7},
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
			electionEvidenceNode{Node: "n1", Incarnation: 1, Running: true, Role: "StateLeader", Term: 7},
			electionEvidenceNode{Node: "n2", Incarnation: 1, Running: true, Role: "StateFollower", Term: 7},
		),
	}}
	if violations := (ElectionSafetyMonitor{}).CheckBundle(bundle); len(violations) != 0 {
		t.Fatalf("normal election evidence was rejected: %#v", violations)
	}
}

func electionEvidence(t *testing.T, nodes ...electionEvidenceNode) control.EvidenceEnvelope {
	t.Helper()
	for index := range nodes {
		nodes[index].ApplicationDigest = strings.Repeat("0", 64)
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
