package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/sutbuild"
)

func TestPairedScenarioEvidenceKeepsCampaignCostSeparateFromBundle(t *testing.T) {
	_, _, bundle := formalCLITestExecution(t)
	root := t.TempDir()
	target := bundle.Trace.ManifestDigest
	writePairedScenarioArm(t, root, pairedScenarioPlannerDeterministic, target, bundle)
	writePairedScenarioArm(t, root, pairedScenarioPlannerAgent, target, bundle)

	evidence, err := loadPairedScenarioTrialEvidence(root)
	if err != nil || evidence.TargetIdentity != target ||
		evidence.Deterministic.Bundle.Digest != bundle.Digest || evidence.Agent.Bundle.Digest != bundle.Digest ||
		evidence.Deterministic.CampaignWork.Primary.WorkUnits <= bundle.Work.Primary.WorkUnits ||
		evidence.Agent.CampaignWork.Primary.WorkUnits <= bundle.Work.Primary.WorkUnits ||
		evidence.Deterministic.CampaignWork.Model.Calls != 0 ||
		evidence.Agent.CampaignWork.Model.Calls != 1 {
		t.Fatalf("paired evidence lost method or full-cost identity: %#v err=%v", evidence, err)
	}
}

func TestPairedScenarioEvidenceRejectsDifferentSUTIdentity(t *testing.T) {
	_, _, bundle := formalCLITestExecution(t)
	root := t.TempDir()
	writePairedScenarioArm(t, root, pairedScenarioPlannerDeterministic, bundle.Trace.ManifestDigest, bundle)
	writePairedScenarioArm(t, root, pairedScenarioPlannerAgent, strings.Repeat("a", 64), bundle)
	if _, err := loadPairedScenarioTrialEvidence(root); err == nil ||
		!strings.Contains(err.Error(), "ARTIFACT_INVALID") {
		t.Fatalf("mismatched target identity error = %v", err)
	}
}

func TestPairedScenarioLauncherUsesFreshRootsAndBindsBuildIdentity(t *testing.T) {
	_, _, bundle := formalCLITestExecution(t)
	root := t.TempDir()
	template := filepath.Join(root, "template")
	target := bundle.Trace.ManifestDigest
	writePairedScenarioArm(t, template, pairedScenarioPlannerDeterministic, target, bundle)
	writePairedScenarioArm(t, template, pairedScenarioPlannerAgent, target, bundle)
	if err := writeJSON(filepath.Join(template, "summary.json"), map[string]any{
		"classification":         "public-calibration-not-agent-effectiveness-holdout-or-correctness",
		"target_id":              "fixture-target",
		"target_identity_digest": target,
		"semantic_exposure":      "full",
		"root_mode":              pairedScenarioFreshRootMode,
		"source_bundle_digest":   digestBytes([]byte("source")),
		"root_corpus_digest":     digestBytes([]byte("corpus")),
		"root_selection_rule":    pairedScenarioFreshRootRule,
		"root_trace_digest":      digestBytes([]byte("root")),
		"root_decisions":         28,
		"same_trace":             true,
		"deterministic":          map[string]any{},
		"agent":                  map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	script := []byte(fmt.Sprintf(`#!/bin/sh
strategy=""
output=""
semantic=""
key=""
model=""
resume=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    -strategy) shift; strategy="$1" ;;
    -campaign-dir) shift; output="$1" ;;
    -semantic-input) shift; semantic="$1" ;;
    -agent-key-file) shift; key="$1" ;;
    -agent-model) shift; model="$1" ;;
    -campaign-resume) resume=1 ;;
    -stateless-corpus) exit 41 ;;
  esac
  shift
done
[ "$strategy" = %q ] && [ -n "$output" ] && [ -n "$semantic" ] && [ -n "$key" ] && [ -n "$model" ] || exit 42
[ ! -e "$output" ] || { [ "$resume" -eq 1 ] && exit 0; exit 43; }
/bin/cp -R %q "$output"
`, pairedScenarioBinaryStrategy, template))
	var audit sutbuild.Audit
	if err := readStrictJSON(
		"../../benchmarks/pilots/etcdraft-v2-method-evaluation-m5.18a/build-audit/control.json",
		&audit,
	); err != nil {
		t.Fatal(err)
	}
	audit.TrialID = "opaque-launch"
	audit.SUTBuildIdentity = bundle.Qualification.Manifest.BuildID
	audit.BinaryPath, audit.BinaryDigest = "<fixture>/sut", digestBytes(script)
	auditBytes, err := json.Marshal(audit)
	if err != nil {
		t.Fatal(err)
	}
	semanticPath, keyPath := filepath.Join(root, "semantic.json"), filepath.Join(root, "key.txt")
	if err := os.WriteFile(semanticPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("fixture-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	current := loadedFreshTrial{
		trialID: "opaque-launch", audit: audit, auditDigest: digestBytes(auditBytes),
		binary: script, binaryDigest: digestBytes(script),
	}
	evidence, err := runPairedScenarioBinary(context.Background(), current, pairedScenarioLaunchConfig{
		SemanticInputPath: semanticPath, AgentKeyFile: keyPath,
		AgentModel: "fixture/model", Timeout: 10 * time.Second,
	}, filepath.Join(root, "launched"))
	if err != nil || evidence.TrialID != current.trialID ||
		evidence.BuildID != bundle.Qualification.Manifest.BuildID ||
		evidence.Scenario.TargetIdentity != target || evidence.Root.RootMode != pairedScenarioFreshRootMode ||
		evidence.Root.RootRule != pairedScenarioFreshRootRule {
		t.Fatalf("launched paired evidence = %#v, err = %v", evidence, err)
	}
	recovered, err := runPairedScenarioBinary(context.Background(), current, pairedScenarioLaunchConfig{
		SemanticInputPath: semanticPath, AgentKeyFile: keyPath,
		AgentModel: "fixture/model", Timeout: 10 * time.Second,
	}, filepath.Join(root, "launched"))
	if err != nil || recovered.BuildAuditDigest != evidence.BuildAuditDigest ||
		recovered.Scenario.Agent.Bundle.Digest != evidence.Scenario.Agent.Bundle.Digest {
		t.Fatalf("resumed paired evidence = %#v, err = %v", recovered, err)
	}

	mismatch := current
	mismatch.audit.SUTBuildIdentity = "different-build"
	if _, err := runPairedScenarioBinary(context.Background(), mismatch, pairedScenarioLaunchConfig{
		SemanticInputPath: semanticPath, AgentKeyFile: keyPath,
		AgentModel: "fixture/model", Timeout: 10 * time.Second,
	}, filepath.Join(root, "mismatched")); err == nil ||
		!strings.Contains(err.Error(), "BUILD_IDENTITY_MISMATCH") {
		t.Fatalf("mismatched build identity error = %v", err)
	}
}

func TestPairedScenarioBatchPreflightsAllTrialsAndUsesCanonicalOrder(t *testing.T) {
	spec, _, bundle := formalCLITestExecution(t)
	root := t.TempDir()
	contract, _, inputsPath := writeFormalCLIFixture(t, root, spec, bundle)
	var inputs formalFreshInputs
	if err := readStrictJSON(inputsPath, &inputs); err != nil {
		t.Fatal(err)
	}
	sources, variants, err := validateFormalFreshInputs(inputsPath, inputs, contract)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadFreshTrials(sources)
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	runner := func(
		_ context.Context,
		current loadedFreshTrial,
		_ pairedScenarioLaunchConfig,
		_ string,
	) (pairedScenarioFreshEvidence, error) {
		calls = append(calls, current.trialID)
		arm := pairedScenarioArmEvidence{Bundle: bundle}
		return pairedScenarioFreshEvidence{
			TrialID: current.trialID, BuildID: current.audit.SUTBuildIdentity,
			BuildAuditDigest: current.auditDigest, BinaryDigest: current.binaryDigest,
			Root: pairedScenarioRootSummary{
				RootMode: pairedScenarioFreshRootMode, RootRule: pairedScenarioFreshRootRule,
			},
			Scenario: pairedScenarioTrialEvidence{
				TargetIdentity: bundle.Trace.ManifestDigest, Deterministic: arm, Agent: arm,
			},
		}, nil
	}
	evidence, err := executePairedScenarioTrials(
		context.Background(), loaded, variants, pairedScenarioLaunchConfig{
			SemanticInputPath: "semantic.json", AgentKeyFile: "key.txt",
			AgentModel: "fixture/model", Timeout: time.Second,
		},
		filepath.Join(root, "paired-artifacts"), runner,
	)
	wantOrder := "opaque-01,opaque-02,opaque-03,opaque-04,opaque-05,opaque-06"
	if err != nil || len(evidence) != 6 || strings.Join(calls, ",") != wantOrder {
		t.Fatalf("batch evidence=%d calls=%v err=%v", len(evidence), calls, err)
	}

	tampered := make(map[string]defectbench.FormalVariant, len(variants))
	for id, variant := range variants {
		tampered[id] = variant
	}
	variant := tampered["opaque-06"]
	variant.ExpectedBinaryDigest = digestBytes([]byte("different"))
	tampered["opaque-06"] = variant
	calls = nil
	badArtifacts := filepath.Join(root, "bad-artifacts")
	if _, err := executePairedScenarioTrials(
		context.Background(), loaded, tampered, pairedScenarioLaunchConfig{
			SemanticInputPath: "semantic.json", AgentKeyFile: "key.txt",
			AgentModel: "fixture/model", Timeout: time.Second,
		}, badArtifacts, runner,
	); err == nil || !strings.Contains(err.Error(), "BATCH_PREFLIGHT_FAILED") || len(calls) != 0 {
		t.Fatalf("tampered batch error=%v calls=%v", err, calls)
	}
	if _, err := os.Lstat(badArtifacts); !os.IsNotExist(err) {
		t.Fatalf("failed preflight created artifacts: %v", err)
	}
}

func TestFormalPairedScenarioBatchRequiresAuditedSemanticsAndRecoversCompletedTrials(t *testing.T) {
	spec, _, bundle := formalCLITestExecution(t)
	root := t.TempDir()
	contract, _, inputsPath := writeFormalCLIFixture(t, root, spec, bundle)
	semantic := []byte("{\"protocol\":\"fixture\"}\n")
	semanticPath := filepath.Join(root, "semantic.json")
	if err := os.WriteFile(semanticPath, semantic, 0o600); err != nil {
		t.Fatal(err)
	}
	view, err := contract.OpaqueView()
	if err != nil {
		t.Fatal(err)
	}
	exposure, err := defectbench.AuditFormalExposure(
		contract, view, []defectbench.FormalPublicArtifact{{Bytes: semantic}},
	)
	if err != nil {
		t.Fatal(err)
	}
	exposurePath := filepath.Join(root, "paired-exposure.json")
	if err := writeJSON(exposurePath, exposure); err != nil {
		t.Fatal(err)
	}
	config := pairedScenarioLaunchConfig{
		SemanticInputPath: semanticPath, AgentKeyFile: "key.txt",
		AgentModel: "fixture/model", Timeout: time.Second,
	}
	artifacts := filepath.Join(root, "paired-artifacts")
	runnerCalls := 0
	runner := func(
		_ context.Context,
		current loadedFreshTrial,
		_ pairedScenarioLaunchConfig,
		directory string,
	) (pairedScenarioFreshEvidence, error) {
		runnerCalls++
		target := bundle.Trace.ManifestDigest
		writePairedScenarioArm(t, directory, pairedScenarioPlannerDeterministic, target, bundle)
		writePairedScenarioArm(t, directory, pairedScenarioPlannerAgent, target, bundle)
		rootSummary := pairedScenarioRootSummary{
			Classification: "public-calibration-not-agent-effectiveness-holdout-or-correctness",
			TargetID:       "fixture-target", TargetIdentity: target, SemanticMode: "full",
			RootMode: pairedScenarioFreshRootMode, RootRule: pairedScenarioFreshRootRule,
			SourceDigest: digestBytes([]byte("source")), CorpusDigest: digestBytes([]byte("corpus")),
			RootDigest: digestBytes([]byte("root")), RootDecisions: 1, SameTrace: true,
			Deterministic: json.RawMessage(`{}`), Agent: json.RawMessage(`{}`),
		}
		if err := writeJSON(filepath.Join(directory, "summary.json"), rootSummary); err != nil {
			return pairedScenarioFreshEvidence{}, err
		}
		return recoverPairedScenarioFreshEvidence(current, directory)
	}
	first, err := runFormalPairedScenarioBatch(
		context.Background(), filepath.Join(root, "contract.json"), exposurePath, inputsPath,
		config, artifacts, runner,
	)
	if err != nil || len(first.Evidence) != 6 || runnerCalls != 6 ||
		first.Deterministic.Summary.Controls != 3 || first.Deterministic.Summary.Candidates != 3 ||
		first.Deterministic.Summary.FalsePositives != 0 || first.Deterministic.Summary.KilledCandidates != 0 ||
		first.Agent.Summary != first.Deterministic.Summary {
		t.Fatalf("first paired batch outcome=%#v calls=%d err=%v", first, runnerCalls, err)
	}
	methodDrift := make(map[string]pairedScenarioFreshEvidence, len(first.Evidence))
	for trialID, current := range first.Evidence {
		methodDrift[trialID] = current
	}
	drifted := methodDrift["opaque-06"]
	if drifted.Scenario.Agent.Bundle.SchemaVersion == controlexperiment.ExecutionBundleSchemaVersionV3 {
		drifted.Scenario.Agent.Bundle.SchemaVersion = controlexperiment.ExecutionBundleSchemaVersionV2
	} else {
		drifted.Scenario.Agent.Bundle.SchemaVersion = controlexperiment.ExecutionBundleSchemaVersionV3
	}
	methodDrift["opaque-06"] = drifted
	separate, err := evaluatePairedScenarioMethods(contract, methodDrift)
	if err != nil || separate.Deterministic.Summary.InvalidTrials != 0 ||
		separate.Agent.Summary.InvalidTrials != 1 {
		t.Fatalf("method axes were not evaluated separately: %#v err=%v", separate, err)
	}
	runnerCalls = 0
	second, err := runFormalPairedScenarioBatch(
		context.Background(), filepath.Join(root, "contract.json"), exposurePath, inputsPath,
		config, artifacts, runner,
	)
	if err != nil || len(second.Evidence) != 6 || runnerCalls != 0 ||
		second.Agent.Summary != first.Agent.Summary || second.Deterministic.Summary != first.Deterministic.Summary {
		t.Fatalf("resumed paired batch outcome=%#v calls=%d err=%v", second, runnerCalls, err)
	}
	if err := run([]string{
		"-paired-scenario", "-formal-contract", filepath.Join(root, "contract.json"),
		"-formal-exposure-audit", exposurePath, "-formal-inputs", inputsPath,
		"-fresh-artifacts", artifacts, "-semantic-input", semanticPath,
		"-agent-key-file", filepath.Join(root, "missing-key.txt"),
		"-agent-model", "fixture/model", "-paired-timeout", "1s",
	}); err != nil {
		t.Fatalf("terminal CLI recovery accessed SUT or key: %v", err)
	}
	if err := os.WriteFile(semanticPath, []byte("{\"protocol\":\"changed\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runFormalPairedScenarioBatch(
		context.Background(), filepath.Join(root, "contract.json"), exposurePath, inputsPath,
		config, artifacts, runner,
	); err == nil || !strings.Contains(err.Error(), "SEMANTIC_INPUT_NOT_AUDITED") || runnerCalls != 0 {
		t.Fatalf("unaudited semantics error=%v calls=%d", err, runnerCalls)
	}
}

func writePairedScenarioArm(
	t *testing.T,
	root string,
	planner string,
	targetIdentity string,
	bundle controlexperiment.ExecutionBundle,
) {
	t.Helper()
	budget := controlexperiment.CampaignLogicalBudget{
		MaxAttempts: 1, MaxPrimarySchedulerDecisions: 10_000,
		MaxPrimaryWorkUnits: 20_000, MaxReplayWorkUnits: 20_000,
	}
	if planner == pairedScenarioPlannerAgent {
		budget.MaxModelCalls, budget.MaxModelTokens = 4, 40_000
	}
	config, err := controlexperiment.NewCampaignConfig(
		"a8-"+planner, "fixture-target", targetIdentity,
		digestBytes([]byte("experiment-"+planner)), budget, 60_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, planner)
	recovered, err := controlexperiment.CreateCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := json.Marshal(map[string]any{
		"episode":           map[string]any{"planner": planner},
		"semantic_exposure": "full",
		"testing_evidence":  map[string]any{"execution_bundle": bundle},
	})
	if err != nil {
		t.Fatal(err)
	}
	work := bundle.Work
	work.Primary.PrepareActions += 100
	work.Primary.WorkUnits += 100
	work.Replay.PrepareActions += 100
	work.Replay.WorkUnits += 100
	if planner == pairedScenarioPlannerAgent {
		work.Model = controlexperiment.ModelWork{Calls: 1, InputTokens: 100, OutputTokens: 20, TotalTokens: 120}
	}
	provider := controlexperiment.CampaignAttemptProviderFunc(func(
		context.Context,
		controlexperiment.CampaignAttemptRequest,
	) (controlexperiment.CampaignAttemptResult, error) {
		return controlexperiment.CampaignAttemptResult{
			Outcome: controlexperiment.CampaignAttemptCompleted, Work: work, Artifact: artifact,
		}, nil
	})
	coordinator, err := controlexperiment.NewCampaignCoordinator(&recovered, provider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}
