package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/targetoracles"
)

func TestEtcdraftClientApplicationBindingAcceptsRealBundleAndRejectsMismatches(t *testing.T) {
	_, bundle := sharedEtcdraftWorkloadBundleFixture(t)
	verdict := oracle.CheckBundle(bundle, targetoracles.ClientApplicationBindingMonitor{})
	if len(verdict.Violations) != 0 || len(verdict.Checked) != 1 ||
		verdict.Checked[0] != targetoracles.ClientApplicationBindingMonitorID {
		t.Fatalf("real bundle binding verdict = %#v", verdict)
	}

	var committedIndex int
	var committed etcdraftv2.ClientResult
	for index, entry := range bundle.ClientHistory {
		result, err := etcdraftv2.ProjectClientResult(entry.Response)
		if err != nil {
			t.Fatal(err)
		}
		if result.Status == "committed" {
			committedIndex = index
			committed = result
			break
		}
	}
	if committed.RequestID == "" {
		t.Fatal("real bundle has no committed client result")
	}

	t.Run("returned-value", func(t *testing.T) {
		mutated := bundle
		mutated.ClientHistory = append(
			[]controlexperiment.ClientHistoryEntry(nil), bundle.ClientHistory...,
		)
		entry := mutated.ClientHistory[committedIndex]
		payload, err := control.NewJSONPayload(entry.Response.Payload.SchemaVersion, struct {
			Index uint64 `json:"index"`
			Term  uint64 `json:"term"`
			Value []byte `json:"value"`
		}{Index: committed.Index, Term: committed.Term, Value: []byte("wrong-return")})
		if err != nil {
			t.Fatal(err)
		}
		entry.Response.Payload = payload
		mutated.ClientHistory[committedIndex] = entry
		violations := oracle.CheckBundle(
			mutated, targetoracles.ClientApplicationBindingMonitor{},
		).Violations
		assertEtcdraftClientApplicationBindingViolation(t, violations, "does not match its invoke")
	})

	t.Run("applied-command-witness", func(t *testing.T) {
		returnEntry := bundle.ClientHistory[committedIndex]
		mutated := mutateEtcdraftReadyAdvancedWitness(
			t, bundle, committed.RequestID, returnEntry.Response.Owner, returnEntry.Step,
			func(advanced *etcdraftv2.ReadyAdvancedEvidence, commandIndex int) {
				advanced.Commands[commandIndex].Value = []byte("wrong-command")
			},
		)
		violations := oracle.CheckBundle(
			mutated, targetoracles.ClientApplicationBindingMonitor{},
		).Violations
		assertEtcdraftClientApplicationBindingViolation(t, violations, "exact applied-command witnesses")
	})

	t.Run("applied-command-without-invoke", func(t *testing.T) {
		returnEntry := bundle.ClientHistory[committedIndex]
		mutated := mutateEtcdraftReadyAdvancedWitness(
			t, bundle, committed.RequestID, returnEntry.Response.Owner, returnEntry.Step,
			func(advanced *etcdraftv2.ReadyAdvancedEvidence, commandIndex int) {
				advanced.Commands[commandIndex].RequestID = "uninvoked-request"
			},
		)
		mutated.ClientHistory = nil
		violations := oracle.CheckBundle(
			mutated, targetoracles.ClientApplicationBindingMonitor{},
		).Violations
		assertEtcdraftClientApplicationBindingViolation(t, violations, "has no prior invoke")
	})

	t.Run("request-at-multiple-log-positions", func(t *testing.T) {
		returnEntry := bundle.ClientHistory[committedIndex]
		mutated := mutateEtcdraftReadyAdvancedWitness(
			t, bundle, committed.RequestID, returnEntry.Response.Owner, returnEntry.Step,
			func(advanced *etcdraftv2.ReadyAdvancedEvidence, commandIndex int) {
				duplicate := advanced.Commands[commandIndex]
				duplicate.Index = advanced.Commands[len(advanced.Commands)-1].Index + 1
				advanced.Commands = append(advanced.Commands, duplicate)
			},
		)
		mutated.ClientHistory = nil
		violations := oracle.CheckBundle(
			mutated, targetoracles.ClientApplicationBindingMonitor{},
		).Violations
		assertEtcdraftClientApplicationBindingViolation(t, violations, "multiple log positions")
	})
}

func mutateEtcdraftReadyAdvancedWitness(
	t *testing.T,
	bundle controlexperiment.ExecutionBundle,
	requestID string,
	owner control.NodeRef,
	step int,
	mutate func(*etcdraftv2.ReadyAdvancedEvidence, int),
) controlexperiment.ExecutionBundle {
	t.Helper()
	bundle.FinalSnapshot.Items = append(
		[]controlruntime.ItemSnapshot(nil), bundle.FinalSnapshot.Items...,
	)
	itemSteps := make(map[control.ItemID]int)
	for _, record := range bundle.Trace.Records {
		for _, transition := range record.ItemTransitions {
			if _, exists := itemSteps[transition.Item]; !exists {
				itemSteps[transition.Item] = int(record.Step)
			}
		}
	}
	for index, snapshotItem := range bundle.FinalSnapshot.Items {
		if snapshotItem.Kind != control.ItemObservation || snapshotItem.Value.Observation == nil ||
			snapshotItem.Value.Observation.Kind != etcdraftv2.ReadyAdvancedObservationKind ||
			snapshotItem.Owner != owner || itemSteps[snapshotItem.ID] != step {
			continue
		}
		advanced, err := etcdraftv2.ProjectReadyAdvancedObservation(*snapshotItem.Value.Observation)
		if err != nil {
			t.Fatal(err)
		}
		for commandIndex := range advanced.Commands {
			if advanced.Commands[commandIndex].RequestID != requestID {
				continue
			}
			mutate(&advanced, commandIndex)
			observation := *snapshotItem.Value.Observation
			observation.Payload, err = control.NewJSONPayload(
				observation.Payload.SchemaVersion, advanced,
			)
			if err != nil {
				t.Fatal(err)
			}
			snapshotItem.Value.Observation = &observation
			bundle.FinalSnapshot.Items[index] = snapshotItem
			return bundle
		}
	}
	t.Fatal("real bundle has no matching applied-command witness to mutate")
	return controlexperiment.ExecutionBundle{}
}

func assertEtcdraftClientApplicationBindingViolation(
	t *testing.T,
	violations []oracle.Violation,
	want string,
) {
	t.Helper()
	if len(violations) != 1 || violations[0].Monitor != targetoracles.ClientApplicationBindingMonitorID ||
		violations[0].Step <= 0 || !strings.Contains(violations[0].Message, want) {
		t.Fatalf("binding violations = %#v, want %q", violations, want)
	}
}

func TestEtcdraftLogProgressAcceptsMonotonicEvidenceAndRejectsMutations(t *testing.T) {
	monotonic := controlexperiment.ExecutionBundle{Trace: controlruntime.Trace{
		InitialEvidence: etcdraftLogProgressFixtureEvidence(t, 0, 0, 0, 1),
		Records: []controlruntime.ActionRecord{
			{Step: 1, Evidence: etcdraftLogProgressFixtureEvidencePointer(t, 1, 2, 1, 1)},
			{Step: 2, Evidence: etcdraftLogProgressFixtureEvidencePointer(t, 2, 5, 3, 1)},
			// A stopped node has no volatile RawNode commit view. Its placeholder
			// must not be treated as an online regression.
			{Step: 3, Evidence: etcdraftLogProgressFixtureEvidenceRunningPointer(t, 3, 0, 3, 1, false)},
			// Restart restores the target's durable application image.
			{Step: 4, Evidence: etcdraftLogProgressFixtureEvidencePointer(t, 4, 5, 3, 2)},
			// A later restart may recover an older durable commit while retaining
			// the durable applied image.
			{Step: 5, Evidence: etcdraftLogProgressFixtureEvidencePointer(t, 5, 3, 3, 3)},
		},
	}}
	result := oracle.CheckBundle(monotonic, targetoracles.LogProgressMonitor{})
	if len(result.Violations) != 0 || len(result.Checked) != 1 ||
		result.Checked[0] != targetoracles.LogProgressMonitorID {
		t.Fatalf("monotonic evidence was rejected: %#v", result)
	}

	tests := []struct {
		name        string
		commit      uint64
		applied     uint64
		incarnation uint64
		step        int
		want        string
	}{
		{name: "commit-regression", commit: 1, applied: 1, incarnation: 1, step: 2, want: "commit frontier regressed"},
		{name: "applied-exceeds-commit", commit: 2, applied: 3, incarnation: 1, step: 2, want: "exceeds commit frontier"},
		{name: "restart-loses-durable-application", commit: 0, applied: 0, incarnation: 2, step: 4, want: "applied frontier regressed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutated := monotonic
			mutated.Trace.Records = append([]controlruntime.ActionRecord(nil), monotonic.Trace.Records[:test.step]...)
			mutated.Trace.Records[test.step-1].Evidence = etcdraftLogProgressFixtureEvidencePointer(
				t, uint64(test.step), test.commit, test.applied, test.incarnation,
			)
			violations := oracle.CheckBundle(mutated, targetoracles.LogProgressMonitor{}).Violations
			if len(violations) != 1 || violations[0].Monitor != targetoracles.LogProgressMonitorID ||
				violations[0].Step != test.step || !strings.Contains(violations[0].Message, test.want) {
				t.Fatalf("mutation was not detected: %#v", violations)
			}
		})
	}
	t.Run("durable-commit-regression", func(t *testing.T) {
		mutated := monotonic
		mutated.Trace.Records = append([]controlruntime.ActionRecord(nil), monotonic.Trace.Records[:4]...)
		mutated.Trace.Records[3].Evidence = etcdraftLogProgressFixtureEvidenceWithStoragePointer(
			t, 4, 5, 2, 3, 2, true,
		)
		violations := oracle.CheckBundle(mutated, targetoracles.LogProgressMonitor{}).Violations
		if len(violations) != 1 || violations[0].Monitor != targetoracles.LogProgressMonitorID ||
			violations[0].Step != 4 || !strings.Contains(violations[0].Message, "durable commit frontier regressed") {
			t.Fatalf("durable commit mutation was not detected: %#v", violations)
		}
	})
}

type etcdraftLogProgressFixtureNode struct {
	Node                string                             `json:"node"`
	Incarnation         uint64                             `json:"incarnation"`
	Running             bool                               `json:"running"`
	Role                string                             `json:"role"`
	Term                uint64                             `json:"term"`
	Commit              uint64                             `json:"commit"`
	StorageCommit       uint64                             `json:"storage_commit"`
	Applied             uint64                             `json:"applied"`
	ApplicationDigest   string                             `json:"application_digest"`
	ApplicationCommands int                                `json:"application_commands"`
	ApplicationPrefixes []etcdraftLogProgressFixturePrefix `json:"application_prefixes,omitempty"`
}

type etcdraftLogProgressFixturePrefix struct {
	Position uint64 `json:"position"`
	Digest   string `json:"digest"`
}

type etcdraftLogProgressFixture struct {
	LogicalTime uint64                           `json:"logical_time"`
	Nodes       []etcdraftLogProgressFixtureNode `json:"nodes"`
}

func etcdraftLogProgressFixtureEvidence(
	t *testing.T,
	logicalTime uint64,
	commit uint64,
	applied uint64,
	incarnation uint64,
) control.EvidenceEnvelope {
	return etcdraftLogProgressFixtureEvidenceRunning(
		t, logicalTime, commit, applied, incarnation, true,
	)
}

func etcdraftLogProgressFixtureEvidenceRunning(
	t *testing.T,
	logicalTime uint64,
	commit uint64,
	applied uint64,
	incarnation uint64,
	running bool,
) control.EvidenceEnvelope {
	t.Helper()
	prefixes := make([]etcdraftLogProgressFixturePrefix, 0, applied)
	for position := uint64(1); position <= applied; position++ {
		digest, err := control.CanonicalDigest(struct {
			Position uint64 `json:"position"`
		}{Position: position})
		if err != nil {
			t.Fatal(err)
		}
		prefixes = append(prefixes, etcdraftLogProgressFixturePrefix{Position: position, Digest: digest})
	}
	applicationDigest, err := control.CanonicalDigest(struct {
		Position uint64 `json:"position"`
	}{Position: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(prefixes) > 0 {
		applicationDigest = prefixes[len(prefixes)-1].Digest
	}
	payload, err := control.NewJSONPayload(
		"consensus-atlas/etcdraft-v2-evidence/v2", etcdraftLogProgressFixture{
			LogicalTime: logicalTime,
			Nodes: []etcdraftLogProgressFixtureNode{{
				Node: "n1", Incarnation: incarnation, Running: running, Role: "StateFollower",
				Term: 2, Commit: commit, StorageCommit: applied, Applied: applied,
				ApplicationDigest: applicationDigest, ApplicationCommands: int(applied),
				ApplicationPrefixes: prefixes,
			}},
		})
	if err != nil {
		t.Fatal(err)
	}
	return control.EvidenceEnvelope{Yield: "fixture-yield", Payload: payload}
}

func etcdraftLogProgressFixtureEvidenceWithStoragePointer(
	t *testing.T,
	logicalTime uint64,
	commit uint64,
	storageCommit uint64,
	applied uint64,
	incarnation uint64,
	running bool,
) *control.EvidenceEnvelope {
	t.Helper()
	evidence := etcdraftLogProgressFixtureEvidenceRunning(
		t, logicalTime, commit, applied, incarnation, running,
	)
	var fixture etcdraftLogProgressFixture
	if err := json.Unmarshal(evidence.Payload.Bytes, &fixture); err != nil {
		t.Fatal(err)
	}
	fixture.Nodes[0].StorageCommit = storageCommit
	payload, err := control.NewJSONPayload(evidence.Payload.SchemaVersion, fixture)
	if err != nil {
		t.Fatal(err)
	}
	evidence.Payload = payload
	return &evidence
}

func etcdraftLogProgressFixtureEvidenceRunningPointer(
	t *testing.T,
	logicalTime uint64,
	commit uint64,
	applied uint64,
	incarnation uint64,
	running bool,
) *control.EvidenceEnvelope {
	evidence := etcdraftLogProgressFixtureEvidenceRunning(
		t, logicalTime, commit, applied, incarnation, running,
	)
	return &evidence
}

func etcdraftLogProgressFixtureEvidencePointer(
	t *testing.T,
	logicalTime uint64,
	commit uint64,
	applied uint64,
	incarnation uint64,
) *control.EvidenceEnvelope {
	evidence := etcdraftLogProgressFixtureEvidence(t, logicalTime, commit, applied, incarnation)
	return &evidence
}
