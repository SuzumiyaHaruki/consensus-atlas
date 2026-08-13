package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	omniadapter "github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	omniqualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/omnipaxosv2"
)

const m523bExperimentDirectory = "../../benchmarks/experiments/omnipaxos-v2-stateless-dfs-m5.23b"

func TestM523bOmniPaxosBoundedStatelessDFSIsDeterministicAndExecutable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	workerPath := buildOmniWorkerM523b(t)
	inputs, err := newStatelessOmniM523bInputs(ctx, workerPath)
	if err != nil {
		t.Fatal(err)
	}
	source, err := executeStatelessOmniSourceM523b(ctx, inputs)
	if err != nil {
		t.Fatal(err)
	}
	root, err := controlexperiment.ExecutionTracePrefix(source.Trace, 1)
	if err != nil {
		t.Fatal(err)
	}
	envelope := inputs.FaultEnvelope
	runtimeConfig := omniRuntimeConfigM523b("m5-22b")
	spec, err := controlexperiment.NewStatelessDFSSpec(
		"omnipaxos-m5-23b", root, runtimeConfig, &envelope, 2, 6, 1000,
	)
	if err != nil {
		t.Fatal(err)
	}
	var opened []*omniadapter.Adapter
	factory := func() (control.Adapter, error) {
		adapter, factoryErr := omniadapter.New(omniadapter.Config{WorkerPath: workerPath})
		if factoryErr == nil {
			opened = append(opened, adapter)
		}
		return adapter, factoryErr
	}
	defer func() {
		for _, adapter := range opened {
			_ = adapter.Close()
		}
	}()
	first, err := controlexperiment.ExploreBoundedStatelessDFS(ctx, spec, root, factory)
	if err != nil {
		t.Fatal(err)
	}
	second, err := controlexperiment.ExploreBoundedStatelessDFS(ctx, spec, root, factory)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || first.Digest != second.Digest ||
		len(first.Items) != spec.MaxWorkItems ||
		first.StopReason != controlexperiment.StatelessDFSStopItems {
		t.Fatalf("OmniPaxos DFS did not retain frozen order/bounds: first=%#v second=%#v", first, second)
	}
	for _, item := range first.Items {
		if !item.ReplayStable {
			t.Fatalf("OmniPaxos DFS emitted an unverified child: %#v", item)
		}
	}
	leaf := first.Items[len(first.Items)-1]
	if leaf.Path.Depth != spec.MaxDepth {
		t.Fatalf("OmniPaxos DFS did not reach the requested depth: %#v", leaf)
	}
	policy, err := controlexperiment.CompileStatelessDFSPath(
		"omnipaxos-m5-23b-leaf", first, root, leaf.Ordinal,
		[]control.ActionKind{
			control.ActionInvoke,
			control.ActionDeliverMessage,
			control.ActionFireTemporal,
		},
		inputs.Decisions,
	)
	if err != nil {
		t.Fatal(err)
	}
	report, bundle := executeOmniFrontierPolicyM523b(
		t, ctx, inputs, policy, runtimeConfig, &envelope,
	)
	executedPrefix, err := controlexperiment.ExecutionTracePrefix(bundle.Trace, leaf.Path.Decision)
	if err != nil {
		t.Fatal(err)
	}
	if executedPrefix.Digest != leaf.ChildPrefixDigest ||
		executedPrefix.FinalStateDigest != leaf.ChildStateDigest ||
		!report.Runs[0].Replay.Stable || first.Work.TotalWorkUnits > spec.MaxWorkUnits {
		t.Fatalf("OmniPaxos DFS WorkItem did not execute through the qualified path: result=%#v run=%#v", first, report.Runs[0])
	}
	summary := statelessDFSCalibrationSummary{
		SchemaVersion: "consensus-atlas/stateless-dfs-calibration-summary/v1",
		Stage:         "M5.23b", Date: "2026-08-12",
		SourceBundleDigest: source.Digest, SourceTraceDigest: source.Trace.Digest,
		RootPrefixDigest: root.Digest, RootDecisions: len(root.Records),
		SpecDigest: spec.Digest, ResultDigest: first.Digest,
		MaxDepth: spec.MaxDepth, MaxWorkItems: spec.MaxWorkItems, MaxWorkUnits: spec.MaxWorkUnits,
		StatesExpanded: first.StatesExpanded, WorkItems: len(first.Items),
		StopReason: first.StopReason, SearchWork: first.Work,
		LeafOrdinal: leaf.Ordinal, LeafDigest: leaf.Digest,
		LeafPrefixDigest: leaf.ChildPrefixDigest, LeafStateDigest: leaf.ChildStateDigest,
		PolicyDigest: mustPolicyDigest(t, policy), ReportDigest: report.Digest,
		BundleDigest: bundle.Digest, ExecutionTraceDigest: bundle.Trace.Digest,
		ExecutionWork: report.Work, ExecutionReplayStable: report.Runs[0].Replay.Stable,
		DeterministicRepeat:  reflect.DeepEqual(first, second),
		LeafPrefixReproduced: executedPrefix.Digest == leaf.ChildPrefixDigest,
		ModelCalls:           0,
		Classification:       "bounded-exact-prefix-stateless-dfs-omnipaxos-portability-gate-not-cross-target-method-ranking",
		Items:                make([]statelessDFSItemSummary, 0, len(first.Items)),
	}
	for _, item := range first.Items {
		summary.Items = append(summary.Items, statelessDFSItemSummary{
			Ordinal: item.Ordinal, ParentOrdinal: item.Path.ParentOrdinal,
			Depth: item.Path.Depth, Decision: item.Path.Decision,
			Kind: item.Action.Kind, ActionID: item.Action.ActionID,
			Digest: item.Digest, ChildPrefixDigest: item.ChildPrefixDigest,
		})
	}
	checked := readM521hJSONFile[statelessDFSCalibrationSummary](
		t, m523bExperimentDirectory+"/summary.json", 96<<10,
	)
	if !reflect.DeepEqual(checked, summary) {
		encodedSummary, marshalErr := json.MarshalIndent(summary, "", "  ")
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		t.Fatalf("checked-in M5.23b summary drifted:\n%s", encodedSummary)
	}
	t.Logf(
		"source=%s/%s spec=%s result=%s root=%s items=%d states=%d stop=%s work=%#v leaf=%s/%s policy=%s report=%s bundle=%s execution=%s execution_work=%#v decisions=%d",
		source.Digest, source.Trace.Digest,
		spec.Digest, first.Digest, root.Digest, len(first.Items), first.StatesExpanded,
		first.StopReason, first.Work, leaf.Digest, leaf.ChildPrefixDigest,
		mustPolicyDigest(t, policy), report.Digest, bundle.Digest, bundle.Trace.Digest,
		report.Work, report.Runs[0].ChargedDecisions,
	)
	for _, item := range first.Items {
		t.Logf("item=%d parent=%d depth=%d decision=%d kind=%s action=%s digest=%s child=%s",
			item.Ordinal, item.Path.ParentOrdinal, item.Path.Depth, item.Path.Decision,
			item.Action.Kind, item.Action.ActionID, item.Digest, item.ChildPrefixDigest)
	}
}

type statelessOmniM523bInputs struct {
	Qualification        omniqualification.Bundle
	WorkerPath           string
	RequiredCapabilities []string
	FaultEnvelope        controlexperiment.FaultEnvelope
	Decisions            int
}

func newStatelessOmniM523bInputs(
	ctx context.Context,
	workerPath string,
) (statelessOmniM523bInputs, error) {
	qualified, err := omniqualification.Run(ctx, workerPath)
	if err != nil {
		return statelessOmniM523bInputs{}, err
	}
	return statelessOmniM523bInputs{
		Qualification: qualified,
		WorkerPath:    workerPath,
		RequiredCapabilities: []string{
			conformance.CapabilityNaturalTemporal,
			conformance.CapabilityOpaqueInvokeBoundary,
			conformance.CapabilityPureEnabledCheck,
			conformance.CapabilityRuntimeOwnedMessage,
			conformance.CapabilityStrictDecisionReplay,
			conformance.CapabilityStrictYieldEvidence,
		},
		Decisions: 96,
	}, nil
}

func executeStatelessOmniSourceM523b(
	ctx context.Context,
	inputs statelessOmniM523bInputs,
) (controlexperiment.ExecutionBundle, error) {
	policy := controlexperiment.Policy{
		Version: controlexperiment.BoundedActionClassPolicyVersion,
		ID:      "portable-omni-bounded-action-class",
		SeedHex: randomPolicySeed(1, 1),
		Priority: []control.ActionKind{
			control.ActionInvoke,
			control.ActionDeliverMessage,
		},
		SelectableActions: []control.ActionKind{
			control.ActionDeliverMessage,
			control.ActionFireTemporal,
			control.ActionInvoke,
		},
	}
	if err := policy.Validate(inputs.Decisions); err != nil {
		return controlexperiment.ExecutionBundle{}, err
	}
	report, bundle, err := executeQualifiedOmniM523b(
		ctx, inputs, "portable-omni-target-m5-22b", policy,
		omniRuntimeConfigM523b("m5-22b"), &inputs.FaultEnvelope,
	)
	if err != nil {
		return controlexperiment.ExecutionBundle{}, err
	}
	if report.Runs[0].Replay.Stable == false {
		return controlexperiment.ExecutionBundle{}, context.Canceled
	}
	return bundle, nil
}

func buildOmniWorkerM523b(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo is required for the M5.23b OmniPaxos portability test")
	}
	manifest := filepath.Join("..", "..", "adapters", "omnipaxosv2", "worker", "Cargo.toml")
	command := exec.Command("cargo", "build", "--locked", "--quiet", "--manifest-path", manifest)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build OmniPaxos worker: %v\n%s", err, output)
	}
	path, err := filepath.Abs(filepath.Join(
		"..", "..", "adapters", "omnipaxosv2", "worker", "target", "debug",
		"consensus-atlas-omnipaxos-worker",
	))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func omniRuntimeConfigM523b(stageID string) controlexperiment.RuntimeConfig {
	return controlexperiment.RuntimeConfig{
		SeedHex:   hex.EncodeToString([]byte("portable-omni-" + stageID)),
		MaxClones: 1,
	}
}

func executeOmniFrontierPolicyM523b(
	t *testing.T,
	ctx context.Context,
	inputs statelessOmniM523bInputs,
	policy controlexperiment.Policy,
	runtimeConfig controlexperiment.RuntimeConfig,
	envelope *controlexperiment.FaultEnvelope,
) (controlexperiment.Report, controlexperiment.ExecutionBundle) {
	t.Helper()
	report, bundle, err := executeQualifiedOmniM523b(
		ctx, inputs, "portable-omni-target-m5-23b-dfs-leaf", policy, runtimeConfig, envelope,
	)
	if err != nil {
		t.Fatal(err)
	}
	return report, bundle
}

func executeQualifiedOmniM523b(
	ctx context.Context,
	inputs statelessOmniM523bInputs,
	configID string,
	policy controlexperiment.Policy,
	runtimeConfig controlexperiment.RuntimeConfig,
	envelope *controlexperiment.FaultEnvelope,
) (controlexperiment.Report, controlexperiment.ExecutionBundle, error) {
	admission, err := controlexperiment.BindExecutionAdmission(
		inputs.Qualification.Qualification,
		controlexperiment.ExecutionRequirements{Capabilities: inputs.RequiredCapabilities},
	)
	if err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	requestID := "portable-request-m5-22b"
	payload, err := omniadapter.InputPayload(omniadapter.Input{
		RequestID: requestID, Value: []byte("portable-value"),
	})
	if err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	workload := controlexperiment.WorkloadPlan{
		SchemaVersion:  controlexperiment.WorkloadPlanVersion,
		ID:             "portable-single-opaque-input",
		TargetSelector: controlexperiment.TargetSingleCoordinatingMember,
		Invocations: []controlexperiment.WorkloadInvocation{{
			ID: requestID, Input: payload, ExpectedStatus: "decided",
		}},
	}
	config := controlexperiment.Config{
		SchemaVersion:    controlexperiment.SchemaVersionV2,
		ID:               configID,
		PSSID:            omniadapter.CorePSSMappingID,
		WorkloadRouterID: omniadapter.WorkloadRouterID,
		Runtime:          runtimeConfig,
		Admission:        &admission,
		FaultEnvelope:    envelope,
		DecisionsPerRun:  inputs.Decisions,
		RequireReplay:    true,
		Runs: []controlexperiment.RunPlan{{
			Run: 1, StopAfterWorkload: true, Policy: policy, Workload: &workload,
		}},
	}
	var opened []*omniadapter.Adapter
	factory := func() (control.Adapter, error) {
		adapter, factoryErr := omniadapter.New(omniadapter.Config{WorkerPath: inputs.WorkerPath})
		if factoryErr == nil {
			opened = append(opened, adapter)
		}
		return adapter, factoryErr
	}
	defer func() {
		for _, adapter := range opened {
			_ = adapter.Close()
		}
	}()
	report, bundle, err := controlexperiment.ExecuteQualifiedBundle(
		ctx, config, inputs.Qualification, factory,
		omniadapter.CorePSSMapper{}, omniadapter.DecisionProjector{}, omniadapter.WorkloadRouter{},
	)
	if err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	if err := bundle.ValidateProjection(omniadapter.DecisionProjector{}); err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	return report, bundle, nil
}
