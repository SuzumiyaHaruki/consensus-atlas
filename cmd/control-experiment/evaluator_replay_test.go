package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/sutbuild"
	omnipaxosqualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/omnipaxosv2"
)

func TestEvaluatorOwnedReplayExecutesSealedRecipe(t *testing.T) {
	methodDigest := strings.Repeat("a", 64)
	report, reference, err := etcdraftBundleV3(context.Background(), "workload-evaluation-v3", 96, 1, methodDigest)
	if err != nil {
		t.Fatal(err)
	}
	config := report.Config
	config.Runs[0].Policy.ID = "etcdraft-evaluator-recorded-schedule"
	config.Runs[0].Policy.Rules = nil
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	report, source, err := controlexperiment.ExecuteQualifiedRecordedBundleV3(
		context.Background(), config, reference.Trace, reference.Qualification, factory,
		etcdraftv2.CorePSSMapper{}, etcdraftv2.DecisionProjector{}, etcdraftv2.WorkloadRouter{}, methodDigest,
	)
	if err != nil || source.Trace.Digest != reference.Trace.Digest {
		t.Fatalf("recorded source execution drifted: %s/%s/%v",
			source.Trace.Digest, reference.Trace.Digest, err)
	}
	targetConfig, err := json.Marshal(etcdraftv2.ThreeNodeConfig())
	if err != nil {
		t.Fatal(err)
	}
	source, err = source.WithExecutionRecipe(controlexperiment.ExecutionRecipe{
		TargetID: "etcdraft-v2", Config: report.Config, TargetConfig: targetConfig,
	})
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	input := filepath.Join(directory, "source.json")
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	var persistedSource controlexperiment.ExecutionBundle
	if err := readStrictJSONFile(input, 64<<20, &persistedSource); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "fresh.json")
	if err := runEvaluatorReplayCLI(context.Background(), controlExperimentOptions{
		Strategy: evaluatorReplayStrategy, Target: "etcdraft-v2", BundleIn: input,
		BundleOut: output, Out: filepath.Join(directory, "report.json"), InvestigationEpisodes: 1,
	}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var fresh controlexperiment.ExecutionBundle
	if err := readStrictJSONFile(output, 64<<20, &fresh); err != nil {
		t.Fatal(err)
	}
	if fresh.Validate() != nil || fresh.Trace.Digest != persistedSource.Trace.Digest ||
		len(fresh.Trace.Records) != len(persistedSource.Trace.Records) || fresh.Recipe == nil {
		t.Fatalf("fresh evaluator replay drifted: source=%s fresh=%s", source.Trace.Digest, fresh.Trace.Digest)
	}
}

func TestEvaluatorOwnedReplayUsesCargoAuditedOmnipaxosWorker(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	protocolDigest, err := sutbuild.SourceTreeDigest(filepath.Join(repositoryRoot, "suts", "omnipaxos"))
	if err != nil {
		t.Fatal(err)
	}
	workerDigest, err := sutbuild.SourceTreeDigest(
		filepath.Join(repositoryRoot, "adapters", "omnipaxosv2", "worker"),
	)
	if err != nil {
		t.Fatal(err)
	}
	outputRoot := filepath.Join(repositoryRoot, "artifacts", "test-evaluator-cargo", t.Name())
	t.Cleanup(func() { _ = os.RemoveAll(outputRoot) })
	outputRelative := filepath.ToSlash(filepath.Join(
		"artifacts", "test-evaluator-cargo", t.Name(), "worker",
	))
	audit, err := sutbuild.BuildCargo(repositoryRoot, sutbuild.CargoSpec{
		Version: sutbuild.CargoSpecVersion, ID: "omnipaxos-evaluator-replay",
		TrialID:            "omnipaxos-cargo-evaluator-trial",
		Module:             sutbuild.ModuleIdentity{Path: "crates.io/omnipaxos", Version: "0.2.2"},
		ProtocolSourceRoot: "suts/omnipaxos", ProtocolSourceDigest: protocolDigest,
		WorkerSourceRoot: "adapters/omnipaxosv2/worker", WorkerSourceDigest: workerDigest,
		ManifestPath: "adapters/omnipaxosv2/worker/Cargo.toml",
		Package:      "consensus-atlas-omnipaxos-worker", BinaryName: "consensus-atlas-omnipaxos-worker",
		CommandAllowlist: []string{
			sutbuild.CommandCargoBuild, sutbuild.CommandCargoMetadata,
			sutbuild.CommandCargoVersion, sutbuild.CommandRustcVersion,
		},
		OutputPath: outputRelative,
	})
	if err != nil || audit.Validate() != nil {
		t.Fatalf("Cargo audit failed: audit=%+v err=%v", audit, err)
	}
	workerPath := filepath.Join(repositoryRoot, filepath.FromSlash(outputRelative))
	source := omnipaxosEvaluatorReplayBundle(t, workerPath)
	if source.Qualification.Manifest.BuildID != audit.SUTBuildIdentity {
		t.Fatalf("Bundle BuildID %q != Cargo audit identity %q",
			source.Qualification.Manifest.BuildID, audit.SUTBuildIdentity)
	}
	directory := t.TempDir()
	input := filepath.Join(directory, "source.json")
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, encoded, 0o600); err != nil {
		t.Fatalf("write source Bundle: %v", err)
	}
	output := filepath.Join(directory, "fresh.json")
	if err := runEvaluatorReplayCLI(context.Background(), controlExperimentOptions{
		Strategy: evaluatorReplayStrategy, Target: "omnipaxos-v2", WorkerPath: workerPath,
		BundleIn: input, BundleOut: output, Out: filepath.Join(directory, "report.json"),
		InvestigationEpisodes: 1,
	}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var fresh controlexperiment.ExecutionBundle
	if err := readStrictJSONFile(output, 64<<20, &fresh); err != nil {
		t.Fatal(err)
	}
	if fresh.Validate() != nil || fresh.Trace.Digest != source.Trace.Digest ||
		fresh.Qualification.Manifest.BuildID != audit.SUTBuildIdentity {
		t.Fatalf("Cargo-audited evaluator replay drifted: source=%s fresh=%s build=%s",
			source.Trace.Digest, fresh.Trace.Digest, fresh.Qualification.Manifest.BuildID)
	}
}

func omnipaxosEvaluatorReplayBundle(t *testing.T, workerPath string) controlexperiment.ExecutionBundle {
	t.Helper()
	adapterConfig := omnipaxosv2.Config{WorkerPath: workerPath, NodeCount: 3}
	qualification, err := omnipaxosqualification.RunWithConfig(context.Background(), adapterConfig)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := controlexperiment.BindExecutionAdmission(
		qualification.Qualification,
		controlexperiment.ExecutionRequirements{Capabilities: []string{
			conformance.CapabilityStrictYieldEvidence, conformance.CapabilityPureEnabledCheck,
			conformance.CapabilityNaturalTemporal, conformance.CapabilityRuntimeOwnedMessage,
			conformance.CapabilityStrictDecisionReplay, conformance.CapabilityOpaqueInvokeBoundary,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := omnipaxosv2.InputPayload(omnipaxosv2.Input{
		RequestID: "cargo-evaluator-request", Value: []byte("cargo-evaluator-value"),
	})
	if err != nil {
		t.Fatal(err)
	}
	config := controlexperiment.Config{
		SchemaVersion: controlexperiment.SchemaVersionV2, ID: "omnipaxos-cargo-evaluator-replay",
		PSSID: omnipaxosv2.CorePSSMappingID, WorkloadRouterID: omnipaxosv2.WorkloadRouterID,
		Admission: &admission, Runtime: controlexperiment.RuntimeConfig{
			SeedHex: hex.EncodeToString([]byte("omnipaxos-cargo-evaluator")), MaxClones: 1,
		},
		DecisionsPerRun: 96, RequireReplay: true,
		Runs: []controlexperiment.RunPlan{{
			Run: 1, StopAfterWorkload: true,
			Policy: controlexperiment.Policy{
				Version: controlexperiment.BoundedActionClassPolicyVersion,
				ID:      "omnipaxos-cargo-evaluator-policy", SeedHex: "01",
				Priority: []control.ActionKind{control.ActionInvoke, control.ActionDeliverMessage},
				SelectableActions: []control.ActionKind{
					control.ActionDeliverMessage, control.ActionFireTemporal, control.ActionInvoke,
				},
			},
			Workload: &controlexperiment.WorkloadPlan{
				SchemaVersion: controlexperiment.WorkloadPlanVersion, ID: "cargo-evaluator-workload",
				TargetSelector: controlexperiment.TargetSingleCoordinatingMember,
				Invocations: []controlexperiment.WorkloadInvocation{{
					ID: "cargo-evaluator-request", Input: payload, ExpectedStatus: "decided",
				}},
			},
		}},
	}
	factory := func() (control.Adapter, error) { return omnipaxosv2.New(adapterConfig) }
	_, bundle, err := controlexperiment.ExecuteQualifiedBundleV3(
		context.Background(), config, qualification, factory, omnipaxosv2.CorePSSMapper{},
		omnipaxosv2.DecisionProjector{}, omnipaxosv2.WorkloadRouter{}, strings.Repeat("b", 64),
	)
	if err != nil {
		t.Fatal(err)
	}
	targetConfig, err := json.Marshal(omnipaxosv2.Config{NodeCount: 3})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err = bundle.WithExecutionRecipe(controlexperiment.ExecutionRecipe{
		TargetID: "omnipaxos-v2", Config: config, TargetConfig: targetConfig,
	})
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}
