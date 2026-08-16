package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	raftfamily "github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic/raft"
)

func TestEtcdraftAuthoringSourceBuildsTrustedInputs(t *testing.T) {
	riskSpec, err := raftfamily.LeaderChangeWithInflightProposalWitness()
	if err != nil {
		t.Fatal(err)
	}
	knowledge, hypothesis, experiment, workload, err := loadEtcdraftSemanticAuthoringSource(
		etcdraftTestSemanticInputPath, riskSpec,
	)
	if err != nil || knowledge.Validate() != nil ||
		hypothesis.Validate(knowledge, riskSpec, controlexperiment.SemanticBestFirstAlgorithmID) != nil ||
		hypothesis.Validate(knowledge, riskSpec, controlexperiment.ScenarioPlanningBackendID) != nil ||
		experiment.validate() != nil || len(experiment.AdapterConfig.Nodes) != 3 ||
		experiment.SearchMaxWorkItems != 16 || experiment.ModelReasoningEffort != "high" ||
		!experiment.ModelExcludeReasoning || experiment.ModelMaxOutputTokens != 32000 ||
		experiment.ScenarioMaxCalls != 8 || experiment.ScenarioMaxSteps != 1 ||
		experiment.ScenarioMaxDecisions != 32 ||
		experiment.ScenarioSemanticExposure != controlexperiment.ScenarioSemanticExposureFull ||
		workload.Validate() != nil || workload.ID != "single-write-v1" ||
		len(workload.Invocations) != 1 || workload.Invocations[0].ID != "m5.15-write-1" {
		t.Fatalf("editable semantic input did not build trusted values: %#v/%#v/%#v/%#v/%v",
			knowledge, hypothesis, experiment, workload, err)
	}

	var source etcdraftSemanticAuthoringSource
	if err := readStrictJSONFile(etcdraftTestSemanticInputPath, etcdraftSemanticInputLimit, &source); err != nil {
		t.Fatal(err)
	}
	source.Knowledge.Digest = strings.Repeat("0", 64)
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "presealed-input.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := loadEtcdraftSemanticAuthoringSource(path, riskSpec); err == nil ||
		!strings.Contains(err.Error(), "AUTHORING_FIELDS_INVALID") {
		t.Fatalf("author-supplied trusted digest was accepted: %v", err)
	}

	source.Knowledge.Digest = ""
	source.Experiment.ScenarioMaxCalls = controlexperiment.ScenarioAgentMaxCalls + 1
	encoded, err = json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := loadEtcdraftSemanticAuthoringSource(path, riskSpec); err == nil ||
		!strings.Contains(err.Error(), "EXPERIMENT_INVALID") {
		t.Fatalf("out-of-bound runtime config was accepted: %v", err)
	}

	source.Experiment.ScenarioMaxCalls = 2
	source.Experiment.ScenarioSemanticExposure = "unbounded-detail"
	encoded, err = json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := loadEtcdraftSemanticAuthoringSource(path, riskSpec); err == nil ||
		!strings.Contains(err.Error(), "EXPERIMENT_INVALID") {
		t.Fatalf("unknown semantic exposure mode was accepted: %v", err)
	}
}

func TestEtcdraftAdapterConfigurationMustMatchRootCorpus(t *testing.T) {
	var source etcdraftSemanticAuthoringSource
	if err := readStrictJSONFile(etcdraftTestSemanticInputPath, etcdraftSemanticInputLimit, &source); err != nil {
		t.Fatal(err)
	}
	source.Experiment.AdapterConfig.ElectionTick++
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "different-topology.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(60*time.Second))
	defer cancel()
	if _, err := prepareEtcdraftSemanticCalibration(
		ctx, etcdraftTestRootCorpusPath, path, fixtureOpenRouterIntentClient(),
	); err == nil || !strings.Contains(err.Error(), "FRONTIER_INVALID") {
		t.Fatalf("adapter configuration drift reused the old root corpus: %v", err)
	}
}

func TestEtcdraftWorkloadMustMatchRootCorpus(t *testing.T) {
	var source etcdraftSemanticAuthoringSource
	if err := readStrictJSONFile(etcdraftTestSemanticInputPath, etcdraftSemanticInputLimit, &source); err != nil {
		t.Fatal(err)
	}
	source.Workload.Invocations[0].Value = "beta"
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "different-workload.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(60*time.Second))
	defer cancel()
	if _, err := prepareEtcdraftSemanticCalibration(
		ctx, etcdraftTestRootCorpusPath, path, fixtureOpenRouterIntentClient(),
	); err == nil || !strings.Contains(err.Error(), "STATELESS_CORPUS_INVALID") {
		t.Fatalf("workload drift reused the old root corpus: %v", err)
	}
}
