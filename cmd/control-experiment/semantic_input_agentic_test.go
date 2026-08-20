package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
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
		knowledge.TargetDossier == nil || len(knowledge.Properties) < 6 ||
		len(knowledge.IssuePatterns) < 6 || len(knowledge.TargetDossier.BlindSpots) < 3 ||
		experiment.validateAgentic() != nil || workload.Validate() != nil {
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

func TestEtcdraftAgenticNodeCountFlowsIntoQualificationAndRoot(t *testing.T) {
	var source etcdraftAgenticAuthoringSource
	if err := readStrictJSONFile(etcdraftAgenticTestInputPath, etcdraftSemanticInputLimit, &source); err != nil {
		t.Fatal(err)
	}
	source.Experiment.AdapterConfig = etcdraftv2.Config{
		NodeCount: 5, ElectionTick: 7, HeartbeatTick: 2,
	}
	path := writeAgenticInputFixture(t, source)
	_, experiment, workload, err := loadEtcdraftAgenticAuthoringSource(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(30*time.Second))
	defer cancel()
	execution, root, err := prepareEtcdraftAgenticExecutionInputs(ctx, workload, experiment)
	if err != nil {
		t.Fatal(err)
	}
	want := []control.NodeID{"n1", "n2", "n3", "n4", "n5"}
	if !reflect.DeepEqual(execution.qualification.Manifest.Nodes, want) {
		t.Fatalf("qualified nodes = %v, want %v", execution.qualification.Manifest.Nodes, want)
	}
	if root.ManifestDigest != execution.admission.ManifestDigest ||
		execution.qualification.Qualification.ManifestDigest != execution.admission.ManifestDigest {
		t.Fatalf("five-node identity drift: root=%s admission=%s qualification=%s",
			root.ManifestDigest, execution.admission.ManifestDigest,
			execution.qualification.Qualification.ManifestDigest)
	}
}

func TestEtcdraftAgenticMissingMembershipUsesThreeNodeDefault(t *testing.T) {
	var source etcdraftAgenticAuthoringSource
	if err := readStrictJSONFile(etcdraftAgenticTestInputPath, etcdraftSemanticInputLimit, &source); err != nil {
		t.Fatal(err)
	}
	source.Experiment.AdapterConfig = etcdraftv2.Config{}
	path := writeAgenticInputFixture(t, source)
	_, experiment, _, err := loadEtcdraftAgenticAuthoringSource(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(experiment.AdapterConfig, etcdraftv2.ThreeNodeConfig()) {
		t.Fatalf("missing etcd/raft membership resolved to %#v", experiment.AdapterConfig)
	}
}

func TestOmnipaxosAgenticAuthoringHasNoSeededRiskOrHypothesis(t *testing.T) {
	knowledge, experiment, workload, err := loadOmnipaxosAgenticAuthoringSource(
		omnipaxosAgenticTestInputPath,
	)
	if err != nil || knowledge.ValidateAgentMaterials() != nil || len(knowledge.Risks) != 0 ||
		knowledge.Protocol != "omnipaxos" || knowledge.Family != "paxos" ||
		knowledge.TargetDossier == nil || len(knowledge.Properties) < 6 ||
		len(knowledge.IssuePatterns) < 6 || len(knowledge.TargetDossier.BlindSpots) < 3 ||
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

func TestOmnipaxosAgenticNodeCountFlowsIntoQualificationAndRoot(t *testing.T) {
	var source omnipaxosAgenticAuthoringSource
	if err := readStrictJSONFile(omnipaxosAgenticTestInputPath, omnipaxosSemanticInputLimit, &source); err != nil {
		t.Fatal(err)
	}
	source.Experiment.AdapterConfig = omnipaxosv2.Config{NodeCount: 5}
	path := writeAgenticInputFixture(t, source)
	workerPath := buildOmnipaxosScenarioWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(90*time.Second))
	defer cancel()
	inputs, err := prepareOmnipaxosAgenticEpisode(ctx, workerPath, path)
	if err != nil {
		t.Fatal(err)
	}
	want := []control.NodeID{"n1", "n2", "n3", "n4", "n5"}
	if !reflect.DeepEqual(inputs.Qualification.Bundle.Manifest.Nodes, want) {
		t.Fatalf("qualified nodes = %v, want %v", inputs.Qualification.Bundle.Manifest.Nodes, want)
	}
	if inputs.Root.ManifestDigest != inputs.Qualification.Admission.ManifestDigest ||
		inputs.Qualification.Bundle.Qualification.ManifestDigest != inputs.Qualification.Admission.ManifestDigest {
		t.Fatalf("five-node OmniPaxos identity drift: root=%s admission=%s qualification=%s",
			inputs.Root.ManifestDigest, inputs.Qualification.Admission.ManifestDigest,
			inputs.Qualification.Bundle.Qualification.ManifestDigest)
	}
}

func TestActiveAgenticDossiersExposeReadableDeclaredSources(t *testing.T) {
	etcdKnowledge, _, _, err := loadEtcdraftAgenticAuthoringSource(etcdraftAgenticTestInputPath)
	if err != nil {
		t.Fatal(err)
	}
	omniKnowledge, _, _, err := loadOmnipaxosAgenticAuthoringSource(omnipaxosAgenticTestInputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		knowledge controlexperiment.ProtocolKnowledgePack
		reference string
		contains  string
	}{
		{
			name: "etcdraft", knowledge: etcdKnowledge,
			reference: "adapters/etcdraftv2/adapter.go:Check", contains: "Check",
		},
		{
			name: "omnipaxos", knowledge: omniKnowledge,
			reference: "adapters/omnipaxosv2/adapter.go:Manifest", contains: "Manifest",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog, err := controlexperiment.KnowledgeSourceCatalog(test.knowledge)
			if err != nil || len(catalog) < 10 {
				t.Fatalf("active Dossier did not produce a useful source catalog: %d/%v", len(catalog), err)
			}
			result, err := controlexperiment.ReadDeclaredKnowledgeSource(
				"../..", test.knowledge,
				controlexperiment.KnowledgeReadRequest{Reference: test.reference, MaxLines: 40},
			)
			if err != nil || result.Validate() != nil ||
				result.Status != controlexperiment.KnowledgeDiscoveryCompleted ||
				!strings.Contains(result.Text, test.contains) {
				t.Fatalf("active declared source was not readable: %#v/%v", result, err)
			}
		})
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
