package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	etcdraftAgenticTestInputPath  = "../../plans/agent/etcdraft-agentic-calibration-v1.json"
	omnipaxosAgenticTestInputPath = "../../plans/agent/omnipaxos-agentic-calibration-v1.json"
)

func TestEtcdraftAgenticAuthoringHasNoSeededRiskOrHypothesis(t *testing.T) {
	knowledge, experiment, workload, err := loadEtcdraftAgenticAuthoringSource(
		etcdraftAgenticTestInputPath,
	)
	if err != nil || knowledge.ValidateAgentMaterials() != nil || len(knowledge.Risks) != 0 ||
		knowledge.Protocol != "etcdraft" || knowledge.Family != "raft" ||
		experiment.validateAgentic() != nil || experiment.SearchMaxDepth != 0 ||
		experiment.SearchMaxWorkItems != 0 || experiment.SearchMaxWorkUnits != 0 ||
		experiment.ExplorerBudget != (controlexperiment.SemanticExplorerBudget{}) ||
		workload.Validate() != nil {
		t.Fatalf("etcd/raft A9e1 materials did not load: %#v/%#v/%#v/%v",
			knowledge, experiment, workload, err)
	}

	withHypothesis := agenticInputWithExtraField(
		t, etcdraftAgenticTestInputPath, "test_hypothesis", map[string]any{},
	)
	if _, _, _, err := loadEtcdraftAgenticAuthoringSource(withHypothesis); err == nil ||
		!strings.Contains(err.Error(), "INPUT_FILE_INVALID") {
		t.Fatalf("empty pre-seeded etcd/raft hypothesis was accepted: %v", err)
	}

	var source etcdraftAgenticAuthoringSource
	if err := readStrictJSONFile(etcdraftAgenticTestInputPath, etcdraftSemanticInputLimit, &source); err != nil {
		t.Fatal(err)
	}
	source.Knowledge.Risks = []controlexperiment.ProtocolRisk{seededAgenticRisk()}
	withRisk := writeAgenticInputFixture(t, source)
	if _, _, _, err := loadEtcdraftAgenticAuthoringSource(withRisk); err == nil ||
		!strings.Contains(err.Error(), "INPUT_MATERIALS_INVALID") {
		t.Fatalf("pre-seeded etcd/raft Risk was accepted: %v", err)
	}
}

func TestOmnipaxosAgenticAuthoringHasNoSeededRiskOrHypothesis(t *testing.T) {
	knowledge, experiment, workload, err := loadOmnipaxosAgenticAuthoringSource(
		omnipaxosAgenticTestInputPath,
	)
	if err != nil || knowledge.ValidateAgentMaterials() != nil || len(knowledge.Risks) != 0 ||
		knowledge.Protocol != "omnipaxos" || knowledge.Family != "paxos" ||
		experiment.validate() != nil || workload.Validate() != nil {
		t.Fatalf("OmniPaxos A9e1 materials did not load: %#v/%#v/%#v/%v",
			knowledge, experiment, workload, err)
	}

	withHypothesis := agenticInputWithExtraField(
		t, omnipaxosAgenticTestInputPath, "test_hypothesis", map[string]any{},
	)
	if _, _, _, err := loadOmnipaxosAgenticAuthoringSource(withHypothesis); err == nil ||
		!strings.Contains(err.Error(), "INPUT_FILE_INVALID") {
		t.Fatalf("empty pre-seeded OmniPaxos hypothesis was accepted: %v", err)
	}

	var source omnipaxosAgenticAuthoringSource
	if err := readStrictJSONFile(omnipaxosAgenticTestInputPath, omnipaxosSemanticInputLimit, &source); err != nil {
		t.Fatal(err)
	}
	source.Knowledge.Risks = []controlexperiment.ProtocolRisk{seededAgenticRisk()}
	withRisk := writeAgenticInputFixture(t, source)
	if _, _, _, err := loadOmnipaxosAgenticAuthoringSource(withRisk); err == nil ||
		!strings.Contains(err.Error(), "INPUT_MATERIALS_INVALID") {
		t.Fatalf("pre-seeded OmniPaxos Risk was accepted: %v", err)
	}
}

func seededAgenticRisk() controlexperiment.ProtocolRisk {
	return controlexperiment.ProtocolRisk{
		ID:                   "preseeded-risk",
		Summary:              "A curated Risk must not enter the A9e1 Agentic input.",
		RequiredCapabilities: []string{"runtime-owned-message-control"},
		RequiredActions:      []control.ActionKind{control.ActionDropMessage},
		AllowedBackendIDs:    []string{controlexperiment.ScenarioPlanningBackendID},
	}
}

func agenticInputWithExtraField(t *testing.T, sourcePath string, name string, value any) string {
	t.Helper()
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	var source map[string]any
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	source[name] = value
	return writeAgenticInputFixture(t, source)
}

func writeAgenticInputFixture(t *testing.T, source any) string {
	t.Helper()
	encoded, err := json.MarshalIndent(source, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "agentic-input.json")
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
