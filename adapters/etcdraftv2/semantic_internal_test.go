package etcdraftv2

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
)

func TestCoreMappingUsesRelativeEpochAndDecisionIdentity(t *testing.T) {
	leftSnapshot, leftEvidence := semanticFixture(
		10, []uint64{8, 8, 7}, []uint64{11, 11, 9}, []uint64{11, 9, 9},
		[]string{"digest-a", "digest-b", "digest-b"},
	)
	rightSnapshot, rightEvidence := semanticFixture(
		900, []uint64{200, 200, 100}, []uint64{90, 90, 40}, []uint64{90, 40, 40},
		[]string{"renamed-x", "renamed-y", "renamed-y"},
	)

	leftObservation, err := MapCoreEvidence(leftEvidence)
	if err != nil {
		t.Fatal(err)
	}
	rightObservation, err := MapCoreEvidence(rightEvidence)
	if err != nil {
		t.Fatal(err)
	}
	left, err := psscore.Project(leftSnapshot, CorePSSMappingID, leftObservation)
	if err != nil {
		t.Fatal(err)
	}
	right, err := psscore.Project(rightSnapshot, CorePSSMappingID, rightObservation)
	if err != nil {
		t.Fatal(err)
	}
	if left.Digest != right.Digest {
		t.Fatalf("relative-equivalent evidence differs: %s != %s", left.Digest, right.Digest)
	}
}

func semanticFixture(
	logicalTime uint64,
	terms, commits, applied []uint64,
	digests []string,
) (controlruntime.Snapshot, control.EvidenceEnvelope) {
	nodes := []control.NodeID{"n1", "n2", "n3"}
	roles := []string{"StateLeader", "StateFollower", "StateCandidate"}
	runtimeSnapshot := controlruntime.Snapshot{LogicalTime: logicalTime}
	evidence := clusterSnapshot{LogicalTime: logicalTime}
	for index, node := range nodes {
		runtimeSnapshot.Nodes = append(runtimeSnapshot.Nodes, controlruntime.NodeSnapshot{
			Ref: control.NodeRef{Node: node, Incarnation: 1}, Lifecycle: control.NodeRunning,
		})
		evidence.Nodes = append(evidence.Nodes, nodeSnapshot{
			Node: node, Incarnation: 1, Running: true, Role: roles[index],
			Term: terms[index], Commit: commits[index], Applied: applied[index],
			ApplicationDigest: digests[index], ApplicationCommands: 1,
		})
	}
	payload, err := control.NewJSONPayload(evidenceSchema, evidence)
	if err != nil {
		panic(err)
	}
	return runtimeSnapshot, control.EvidenceEnvelope{Yield: "fixture-yield", Payload: payload}
}
