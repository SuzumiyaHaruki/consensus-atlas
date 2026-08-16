package etcdraftv2

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
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

func TestA9e2UnequalFrontierAgreementDetectsSharedPrefixConflict(t *testing.T) {
	origin := control.NodeRef{Node: "n1", Incarnation: 1}
	alpha := appliedCommand{Index: 1, RequestID: "alpha", Origin: origin, Value: []byte("alpha")}
	left, err := sealApplicationImage(applicationImage{
		Applied: 2, Commands: []appliedCommand{
			alpha,
			{Index: 2, RequestID: "beta", Origin: origin, Value: []byte("beta")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	conflict, err := sealApplicationImage(applicationImage{
		Applied: 1, Commands: []appliedCommand{{
			Index: 1, RequestID: "conflict", Origin: origin, Value: []byte("conflict"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	leftPrefixes, err := left.prefixes()
	if err != nil {
		t.Fatal(err)
	}
	conflictPrefixes, err := conflict.prefixes()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := clusterSnapshot{Nodes: []nodeSnapshot{
		{Node: "n1", Incarnation: 1, Running: true, Applied: 2,
			ApplicationDigest: left.Digest, ApplicationCommands: 2, ApplicationPrefixes: leftPrefixes},
		{Node: "n2", Incarnation: 1, Running: true, Applied: 1,
			ApplicationDigest: conflict.Digest, ApplicationCommands: 1, ApplicationPrefixes: conflictPrefixes},
	}}
	payload, err := control.NewJSONPayload(evidenceSchema, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	observations, err := (DecisionProjector{}).Project(control.EvidenceEnvelope{Payload: payload})
	if err != nil || len(observations) != 3 {
		t.Fatalf("unequal-frontier calibration projection invalid: %#v err=%v", observations, err)
	}
	result := oracle.CheckBundle(controlexperiment.ExecutionBundle{
		Decisions: controlexperiment.DecisionHistory{Observations: observations},
	}, oracle.BundleAgreement{})
	if len(result.Violations) != 1 || result.Violations[0].Monitor != "agreement" {
		t.Fatalf("shared-prefix conflict was not detected: %#v", result)
	}

	consistent, err := sealApplicationImage(applicationImage{Applied: 1, Commands: []appliedCommand{alpha}})
	if err != nil {
		t.Fatal(err)
	}
	consistentPrefixes, err := consistent.prefixes()
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Nodes[1].ApplicationDigest = consistent.Digest
	snapshot.Nodes[1].ApplicationPrefixes = consistentPrefixes
	payload, err = control.NewJSONPayload(evidenceSchema, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	observations, err = (DecisionProjector{}).Project(control.EvidenceEnvelope{Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	result = oracle.CheckBundle(controlexperiment.ExecutionBundle{
		Decisions: controlexperiment.DecisionHistory{Observations: observations},
	}, oracle.BundleAgreement{})
	if len(result.Violations) != 0 {
		t.Fatalf("consistent shared prefix produced a false positive: %#v", result)
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
		prefixes := make([]ApplicationPrefixEvidence, 0, applied[index])
		for position := uint64(1); position <= applied[index]; position++ {
			digest, err := control.CanonicalDigest(struct {
				Pattern  string `json:"pattern"`
				Position uint64 `json:"position"`
			}{Pattern: digests[index], Position: position})
			if err != nil {
				panic(err)
			}
			prefixes = append(prefixes, ApplicationPrefixEvidence{Position: position, Digest: digest})
		}
		runtimeSnapshot.Nodes = append(runtimeSnapshot.Nodes, controlruntime.NodeSnapshot{
			Ref: control.NodeRef{Node: node, Incarnation: 1}, Lifecycle: control.NodeRunning,
		})
		evidence.Nodes = append(evidence.Nodes, nodeSnapshot{
			Node: node, Incarnation: 1, Running: true, Role: roles[index],
			Term: terms[index], Commit: commits[index], Applied: applied[index],
			ApplicationDigest: prefixes[len(prefixes)-1].Digest, ApplicationCommands: 1,
			ApplicationPrefixes: prefixes,
		})
	}
	payload, err := control.NewJSONPayload(evidenceSchema, evidence)
	if err != nil {
		panic(err)
	}
	return runtimeSnapshot, control.EvidenceEnvelope{Yield: "fixture-yield", Payload: payload}
}
