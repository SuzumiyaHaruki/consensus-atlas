package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/sutbuild"
	qualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/etcdraftv2"
)

type formalCLIStageSummary struct {
	SchemaVersion         string `json:"schema_version"`
	ID                    string `json:"id"`
	ContractDigest        string `json:"contract_digest"`
	ExposureAuditDigest   string `json:"exposure_audit_digest"`
	InputSetDigest        string `json:"input_set_digest"`
	EvaluationDigest      string `json:"evaluation_digest"`
	Pairs                 int    `json:"pairs"`
	Trials                int    `json:"trials"`
	Controls              int    `json:"controls"`
	Candidates            int    `json:"candidates"`
	SurvivedCandidates    int    `json:"survived_candidates"`
	FalsePositives        int    `json:"false_positives"`
	InvalidTrials         int    `json:"invalid_trials"`
	PreflightBeforeRunner bool   `json:"preflight_before_runner"`
	NewOutputsRequired    bool   `json:"new_outputs_required"`
	DatasetClassification string `json:"dataset_classification"`
	FormalReady           bool   `json:"formal_ready"`
	NewModelCalls         int    `json:"new_model_calls"`
	NewSUTExecutions      int    `json:"new_sut_executions"`
	Digest                string `json:"digest"`
}

func TestFormalFreshCLIConsumesSixInputsAndWritesPrivateLedger(t *testing.T) {
	spec, report, bundle := formalCLITestExecution(t)
	root := t.TempDir()
	contract, exposure, inputsPath := writeFormalCLIFixture(t, root, spec, bundle)
	artifacts, output := filepath.Join(root, "artifacts"), filepath.Join(root, "evaluation.json")
	runnerCalls := 0
	runner := func(
		_ string, _ []byte, _ controlexperiment.MethodSpec,
	) (controlexperiment.Report, controlexperiment.ExecutionBundle, error) {
		runnerCalls++
		return report, bundle, nil
	}
	if err := runFormalFreshEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"), inputsPath,
		filepath.Join(root, "method-spec.json"), artifacts, output, runner,
	); err != nil {
		t.Fatal(err)
	}
	if runnerCalls != 6 {
		t.Fatalf("runner calls = %d, want 6", runnerCalls)
	}
	var evaluation defectbench.FormalFreshEvaluation
	if err := readStrictJSON(output, &evaluation); err != nil {
		t.Fatal(err)
	}
	if err := evaluation.Validate(); err != nil {
		t.Fatal(err)
	}
	want := defectbench.FormalEvaluationSummary{Controls: 3, Candidates: 3, RootCauses: 3}
	if evaluation.Summary != want {
		t.Fatalf("summary = %#v, want %#v", evaluation.Summary, want)
	}
	if err := runFormalFreshEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"), inputsPath,
		filepath.Join(root, "method-spec.json"), artifacts, output, runner,
	); err == nil || !strings.Contains(err.Error(), "FORMAL_CLI_OUTPUT_EXISTS") {
		t.Fatalf("reused output error = %v", err)
	}
	runnerCalls = 0
	overlapArtifacts := filepath.Join(root, "overlap")
	if err := runFormalFreshEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"), inputsPath,
		filepath.Join(root, "method-spec.json"), overlapArtifacts,
		filepath.Join(overlapArtifacts, "evaluation.json"), runner,
	); err == nil || !strings.Contains(err.Error(), "FORMAL_CLI_OUTPUT_PATHS_OVERLAP") || runnerCalls != 0 {
		t.Fatalf("overlapping output error = %v, runner calls = %d", err, runnerCalls)
	}
	var inputs formalFreshInputs
	if err := readStrictJSON(inputsPath, &inputs); err != nil {
		t.Fatal(err)
	}
	duplicate := inputs
	duplicate.Trials = append([]formalFreshTrialInput(nil), inputs.Trials...)
	duplicate.Trials[len(duplicate.Trials)-1] = duplicate.Trials[0]
	if _, _, err := validateFormalFreshInputs(inputsPath, duplicate, contract); err == nil ||
		!strings.Contains(err.Error(), "FORMAL_CLI_INPUT_SET_INVALID") {
		t.Fatalf("duplicate input error = %v", err)
	}
	lastBinary := filepath.Join(root, inputs.Trials[len(inputs.Trials)-1].BinaryPath)
	if err := os.WriteFile(lastBinary, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	runnerCalls = 0
	err := runFormalFreshEvaluation(
		filepath.Join(root, "contract.json"), filepath.Join(root, "exposure.json"), inputsPath,
		filepath.Join(root, "method-spec.json"), filepath.Join(root, "tamper-artifacts"),
		filepath.Join(root, "tamper-evaluation.json"), runner,
	)
	if err == nil || !strings.Contains(err.Error(), "does not match its build audit") || runnerCalls != 0 {
		t.Fatalf("preflight error = %v, runner calls = %d", err, runnerCalls)
	}

	inputBytes, err := os.ReadFile(inputsPath)
	if err != nil {
		t.Fatal(err)
	}
	summary := formalCLIStageSummary{
		SchemaVersion: "consensus-atlas/formal-cli-stage-summary/v1",
		ID:            "formal-multi-pair-cli-m5-21o", ContractDigest: contract.Digest,
		ExposureAuditDigest: exposure.Digest, InputSetDigest: digestBytes(inputBytes),
		EvaluationDigest: evaluation.Digest, Pairs: len(evaluation.Pairs), Trials: len(evaluation.Results),
		Controls: evaluation.Summary.Controls, Candidates: evaluation.Summary.Candidates,
		SurvivedCandidates: 3, FalsePositives: evaluation.Summary.FalsePositives,
		InvalidTrials: evaluation.Summary.InvalidTrials, PreflightBeforeRunner: true,
		NewOutputsRequired: true, DatasetClassification: "public-synthetic-same-correct-bundle-fixture-only",
		FormalReady: false, NewModelCalls: 0, NewSUTExecutions: 1,
	}
	summary.Digest, err = control.CanonicalDigest(summary)
	if err != nil {
		t.Fatal(err)
	}
	checkFormalCLIStageSummary(t, summary)
}

func TestFormalModeRejectsMixedPublicFlags(t *testing.T) {
	err := run([]string{
		"-formal-contract", "contract.json", "-formal-exposure-audit", "exposure.json",
		"-formal-inputs", "inputs.json", "-method-spec", "method.json",
		"-fresh-artifacts", "artifacts", "-out", "evaluation.json", "-manifest", "public.json",
	})
	if err == nil || !strings.Contains(err.Error(), "no public-pair flags") {
		t.Fatalf("mixed-mode error = %v", err)
	}
}

func formalCLITestExecution(
	t *testing.T,
) (controlexperiment.MethodSpec, controlexperiment.Report, controlexperiment.ExecutionBundle) {
	return formalCLITestExecutionForMethod(t, "public-formal-cli-fixture-m5-21o")
}

func formalCLITestExecutionForMethod(
	t *testing.T,
	methodID string,
) (controlexperiment.MethodSpec, controlexperiment.Report, controlexperiment.ExecutionBundle) {
	return formalCLITestExecutionForIdentity(t, methodID, "")
}

func formalCLITestExecutionWithDigest(
	t *testing.T,
	methodSpecDigest string,
) (controlexperiment.MethodSpec, controlexperiment.Report, controlexperiment.ExecutionBundle) {
	return formalCLITestExecutionForIdentity(t, "agentic-fixture-carrier", methodSpecDigest)
}

func formalCLITestExecutionForIdentity(
	t *testing.T,
	methodID string,
	methodSpecDigest string,
) (controlexperiment.MethodSpec, controlexperiment.Report, controlexperiment.ExecutionBundle) {
	t.Helper()
	qualified, err := qualification.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	admission, err := controlexperiment.BindExecutionAdmission(
		qualified.Qualification,
		controlexperiment.ExecutionRequirements{Capabilities: qualified.Profile.RequiredCapabilityIDs()},
	)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := etcdraftv2.InputPayload(etcdraftv2.Input{
		Operation: etcdraftv2.OperationPropose, RequestID: "m5.21o-write-1", Value: []byte("alpha"),
	})
	if err != nil {
		t.Fatal(err)
	}
	workload := controlexperiment.WorkloadPlan{
		SchemaVersion: controlexperiment.WorkloadPlanVersion, ID: "single-write-m5-21o",
		TargetSelector: controlexperiment.TargetSingleCoordinatingMember,
		Invocations: []controlexperiment.WorkloadInvocation{{
			ID: "m5.21o-write-1", Input: payload, ExpectedStatus: "committed",
		}},
	}
	faults := &controlexperiment.FaultEnvelope{
		MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
		MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
	}
	config := controlexperiment.Config{
		SchemaVersion: controlexperiment.SchemaVersionV2, ID: "public-formal-cli-fixture-m5-21o",
		PSSID: etcdraftv2.CorePSSMappingID,
		Runtime: controlexperiment.RuntimeConfig{
			SeedHex:   "6f6666696369616c2d65746364726166742d76322d636c75737465722d73656564",
			MaxClones: 1,
		},
		Admission: &admission, FaultEnvelope: faults, WorkloadRouterID: etcdraftv2.WorkloadRouterID,
		DecisionsPerRun: 32, RequireReplay: true,
		Runs: []controlexperiment.RunPlan{{
			Run: 1, Workload: &workload,
			Policy: controlexperiment.Policy{
				Version: controlexperiment.PolicyVersion, ID: "semantic-workload-progress-v1",
				Priority: []control.ActionKind{
					control.ActionInvoke, control.ActionCompleteEffect,
					control.ActionDeliverMessage, control.ActionFireTemporal,
				},
			},
		}},
	}
	projection, err := controlexperiment.MethodConfigProjectionDigest(config)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := controlexperiment.NewMethodSpec(controlexperiment.MethodSpec{
		ID: methodID, Strategy: "workload-evaluation-v3",
		Decisions: 32, PolicySeed: 1,
		Budget: controlexperiment.MethodBudget{
			MaxExecutionAttempts: 1, MaxPrimaryWorkUnits: 34, MaxReplayWorkUnits: 34,
		},
		PSSID: etcdraftv2.CorePSSMappingID, ProjectorID: etcdraftv2.DecisionProjectionID,
		ConfigProjectionDigest: projection, TimeoutMillis: 120_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if methodSpecDigest == "" {
		methodSpecDigest = spec.Digest
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	report, bundle, err := controlexperiment.ExecuteQualifiedBundleV3(
		context.Background(), config, qualified, factory, etcdraftv2.CorePSSMapper{},
		etcdraftv2.DecisionProjector{}, etcdraftv2.WorkloadRouter{}, methodSpecDigest,
	)
	if err != nil {
		t.Fatal(err)
	}
	return spec, report, bundle
}

func writeFormalCLIFixture(
	t *testing.T,
	root string,
	spec controlexperiment.MethodSpec,
	bundle controlexperiment.ExecutionBundle,
) (defectbench.FormalBenchmarkContract, defectbench.FormalExposureAudit, string) {
	t.Helper()
	var baseAudit sutbuild.Audit
	if err := readStrictJSON(
		"../../benchmarks/pilots/etcdraft-v2-method-evaluation-m5.18a/build-audit/control.json",
		&baseAudit,
	); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	inputs := formalFreshInputs{SchemaVersion: formalFreshInputsSchemaVersion}
	pairs := make([]defectbench.FormalPair, 0, 3)
	for pairIndex := 1; pairIndex <= 3; pairIndex++ {
		pair := defectbench.FormalPair{
			PairID:      "private-pair-" + string(rune('a'+pairIndex-1)),
			RootCauseID: "private-root-" + string(rune('a'+pairIndex-1)),
		}
		for candidateIndex, candidate := range []bool{false, true} {
			ordinal := (pairIndex-1)*2 + candidateIndex + 1
			trialID := "opaque-0" + string(rune('0'+ordinal))
			kind := "control"
			if candidate {
				kind = "candidate"
			}
			binaryName := trialID + ".bin"
			auditName := trialID + "-audit.json"
			binary := []byte("formal-cli-private-binary-" + trialID)
			audit := baseAudit
			audit.TrialID, audit.SUTBuildIdentity = trialID, bundle.Qualification.Manifest.BuildID
			audit.BinaryPath, audit.BinaryDigest = "<private>/"+binaryName, digestBytes(binary)
			if err := os.WriteFile(filepath.Join(dataDir, binaryName), binary, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := writeJSON(filepath.Join(dataDir, auditName), audit); err != nil {
				t.Fatal(err)
			}
			auditBytes, err := os.ReadFile(filepath.Join(dataDir, auditName))
			if err != nil {
				t.Fatal(err)
			}
			variant := defectbench.FormalVariant{
				TrialID: trialID, VariantID: "private-" + kind + "-" + string(rune('a'+pairIndex-1)),
				ExpectedBuildID:          bundle.Qualification.Manifest.BuildID,
				ExpectedConfigDigest:     bundle.Identity.ConfigDigest,
				ExpectedBuildAuditDigest: digestBytes(auditBytes), ExpectedBinaryDigest: audit.BinaryDigest,
			}
			if candidate {
				pair.Candidate = variant
			} else {
				pair.Control = variant
			}
			inputs.Trials = append(inputs.Trials, formalFreshTrialInput{
				TrialID: trialID, BuildAuditPath: filepath.Join("data", auditName),
				BinaryPath: filepath.Join("data", binaryName),
			})
		}
		pairs = append(pairs, pair)
	}
	contract, err := (defectbench.FormalBenchmarkContract{
		ID: "formal-cli-fixture-m5-21o", FamilyID: "leader-cft",
		ProfileDigest:    bundle.Qualification.Profile.Digest,
		BlindingNonce:    digestBytes([]byte("formal-cli-m5.21o-nonce")),
		MethodSpecDigest: spec.Digest, RequiredBundleSchema: spec.RequiredBundleSchema,
		Budget: defectbench.BundleBudget{
			MaxDecisions: spec.Decisions, MaxPrimaryWorkUnits: spec.Budget.MaxPrimaryWorkUnits,
		},
		Composition: defectbench.FormalCompositionSpec{
			ProjectorID: etcdraftv2.DecisionProjectionID, MonitorIDs: []string{"agreement"},
		},
		Pairs: pairs,
	}).Seal()
	if err != nil {
		t.Fatal(err)
	}
	view, err := contract.OpaqueView()
	if err != nil {
		t.Fatal(err)
	}
	exposure, err := defectbench.AuditFormalExposure(
		contract, view,
		[]defectbench.FormalPublicArtifact{{Bytes: []byte(`{"trial_id":"opaque-01"}`)}},
	)
	if err != nil {
		t.Fatal(err)
	}
	for path, value := range map[string]any{
		"contract.json": contract, "exposure.json": exposure,
		"method-spec.json": spec, "inputs.json": inputs,
	} {
		if err := writeJSON(filepath.Join(root, path), value); err != nil {
			t.Fatal(err)
		}
	}
	return contract, exposure, filepath.Join(root, "inputs.json")
}

func checkFormalCLIStageSummary(t *testing.T, recomputed formalCLIStageSummary) {
	t.Helper()
	var archived formalCLIStageSummary
	if err := readStrictJSON(
		"../../benchmarks/experiments/formal-multi-pair-cli-m5.21o/summary.json", &archived,
	); err != nil {
		encoded, _ := json.MarshalIndent(recomputed, "", "  ")
		t.Fatalf("read stage summary: %v\n%s", err, append(encoded, '\n'))
	}
	archivedDigest := archived.Digest
	archived.Digest = ""
	sealed, err := control.CanonicalDigest(archived)
	if err != nil || sealed != archivedDigest {
		t.Fatalf("archived stage summary identity is invalid: %s/%v", archivedDigest, err)
	}
	if recomputed.Digest == "" || recomputed.Trials != recomputed.Controls+recomputed.Candidates {
		t.Fatalf("recomputed stage summary is invalid: %#v", recomputed)
	}
}
