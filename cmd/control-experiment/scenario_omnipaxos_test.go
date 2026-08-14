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

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestA7bOmniPaxosProducesQualifiedScenarioTestingResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	workerPath := buildOmnipaxosScenarioWorker(t)
	spec, err := omnipaxosMessageLossWitness()
	if err != nil {
		t.Fatal(err)
	}
	knowledge, hypothesis, experiment, workload, err := loadOmnipaxosScenarioAuthoringSource(
		"../../plans/agent/omnipaxos-message-loss-before-decision-v1.json", spec,
	)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := hex.DecodeString(experiment.Runtime.SeedHex)
	if err != nil {
		t.Fatal(err)
	}
	runtimeConfig := experiment.Runtime
	envelope := experiment.faultEnvelope()

	rootAdapter, err := omnipaxosv2.New(omnipaxosv2.Config{WorkerPath: workerPath})
	if err != nil {
		t.Fatal(err)
	}
	rootRuntime, err := controlruntime.New(ctx, rootAdapter, controlruntime.Config{Seed: seed, MaxClones: 1})
	if err != nil {
		t.Fatal(err)
	}
	coordinator := driveOmnipaxosScenarioToCoordinator(t, ctx, rootRuntime)
	payload := workload.Invocations[0].Input
	invoke, err := rootRuntime.OfferInvoke(ctx, coordinator, payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rootRuntime.Select(ctx, invoke); err != nil {
		t.Fatal(err)
	}
	root, err := rootRuntime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	if err := rootAdapter.Close(); err != nil {
		t.Fatal(err)
	}

	projector := omnipaxosScenarioProjector{}
	rootRisk, err := projector.Project("omnipaxos-a7a-root-risk", spec, root)
	if err != nil || !reflect.DeepEqual(rootRisk.SatisfiedMilestones, []string{omnipaxosMilestoneWorkloadInvoked}) {
		t.Fatalf("unexpected root risk: %#v err=%v", rootRisk, err)
	}
	var opened []*omnipaxosv2.Adapter
	factory := func() (control.Adapter, error) {
		adapter, err := omnipaxosv2.New(omnipaxosv2.Config{WorkerPath: workerPath})
		if err == nil {
			opened = append(opened, adapter)
		}
		return adapter, err
	}
	defer func() {
		for _, adapter := range opened {
			if err := adapter.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	frontier, snapshot, _, err := controlexperiment.ReconstructRiskFrontierState(
		ctx, "omnipaxos-a7a-root-frontier", spec, rootRisk, root, len(root.Records),
		runtimeConfig, envelope, factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	semantics, err := projectOmnipaxosScenarioSemantics(
		controlexperiment.ScenarioSemanticExposureFull, root, frontier, snapshot,
	)
	if err != nil {
		t.Fatal(err)
	}
	plannerCalls := 0
	result, err := controlexperiment.ExploreScenarioWithPlanner(
		ctx, experiment.ScenarioMaxCalls, experiment.ScenarioMaxSteps,
		experiment.ScenarioMaxDecisions, knowledge, hypothesis, spec, frontier, semantics, rootRisk, root,
		runtimeConfig, envelope, factory, projector,
		func(trace controlruntime.Trace, next controlexperiment.RiskFrontierView, state controlruntime.Snapshot) (
			controlexperiment.ScenarioSemanticExposure, error,
		) {
			return projectOmnipaxosScenarioSemantics(
				controlexperiment.ScenarioSemanticExposureFull, trace, next, state,
			)
		},
		func(_ context.Context, view controlexperiment.ScenarioAgentView) ([]byte, controlexperiment.ModelWork, error) {
			plannerCalls++
			var selected controlexperiment.FrontierActionRef
			for index, action := range view.Frontier.Actions {
				if action.Kind == control.ActionDropMessage &&
					view.Semantics.ActionHints[index].MessageClass == controlexperiment.ConsensusMessageReplication {
					selected = action
					break
				}
			}
			if selected.ActionID == "" {
				t.Fatalf("no in-flight OmniPaxos replication message can be dropped: %#v", view.Frontier.Actions)
			}
			encoded, err := json.Marshal(controlexperiment.ScenarioPlan{
				ID: "omnipaxos-a7a-plan", Steps: []controlexperiment.ScenarioStep{{
					ID: "drop-replication", Selector: controlexperiment.FrontierActionSelector{ActionID: selected.ActionID},
				}},
			})
			return encoded, controlexperiment.ModelWork{
				Calls: 1, InputTokens: 100, OutputTokens: 20, TotalTokens: 120,
			}, err
		},
	)
	if err != nil || result.Execution == nil || result.Execution.FinalRisk.Status != semantic.RiskWitnessReached ||
		plannerCalls != 1 || len(result.Attempts) != 1 ||
		len(result.Attempts[0].Feedback.NaturalProgress) == 0 ||
		result.Attempts[0].Feedback.NaturalProgressStop != controlexperiment.ScenarioProgressRiskChanged {
		t.Fatalf("OmniPaxos Scenario episode did not close: %#v calls=%d err=%v", result, plannerCalls, err)
	}
	qualification, err := qualifyOmnipaxosScenario(ctx, workerPath)
	if err != nil {
		t.Fatal(err)
	}
	testingResult, err := executeOmnipaxosScenarioQualified(
		ctx, workerPath, experiment, workload, qualification, root, *result.Execution,
	)
	if err != nil {
		t.Fatal(err)
	}
	if testingResult.Outcome != scenarioTestingPassed ||
		testingResult.Bundle.Trace.Digest != result.Execution.FinalTrace.Digest ||
		testingResult.CorePSSSamples == 0 || testingResult.UniqueCorePSSStates == 0 ||
		!testingResult.Replay.Required || !testingResult.Replay.Stable ||
		len(testingResult.Oracle.Violations) != 0 ||
		!reflect.DeepEqual(testingResult.Oracle.Checked, []string{"trace-integrity", "agreement"}) {
		t.Fatalf("OmniPaxos qualified testing result invalid: %#v", testingResult)
	}
	t.Logf("root_decisions=%d extension=%d risk=%s replay=%t pss=%d/%d oracle=%d qualification=%s",
		len(root.Records), len(result.Execution.Steps), result.Execution.FinalRisk.Status,
		testingResult.Replay.Stable, testingResult.CorePSSSamples,
		testingResult.UniqueCorePSSStates, len(testingResult.Oracle.Violations),
		qualification.Admission.Digest)
}

func driveOmnipaxosScenarioToCoordinator(
	t *testing.T,
	ctx context.Context,
	runtime *controlruntime.Runtime,
) control.NodeID {
	t.Helper()
	router := omnipaxosv2.WorkloadRouter{}
	for decisions := 0; decisions < 256; decisions++ {
		trace, err := runtime.Trace()
		if err != nil {
			t.Fatal(err)
		}
		route, err := router.Route(
			controlexperiment.TargetSingleCoordinatingMember, latestScenarioEvidence(trace),
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(route.Candidates) == 1 {
			return route.Candidates[0]
		}
		actions, err := runtime.EnabledActions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		selected, ok := firstOmnipaxosScenarioProgress(actions)
		if !ok {
			t.Fatalf("OmniPaxos election became quiescent: %#v", actions)
		}
		if _, err := runtime.Select(ctx, selected.ID); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("OmniPaxos did not elect a coordinator")
	return ""
}

func firstOmnipaxosScenarioProgress(actions []control.Action) (control.Action, bool) {
	for _, kind := range []control.ActionKind{control.ActionDeliverMessage, control.ActionFireTemporal} {
		for _, action := range actions {
			if action.Kind == kind {
				return action, true
			}
		}
	}
	return control.Action{}, false
}

func buildOmnipaxosScenarioWorker(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo is required for the OmniPaxos Scenario test")
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
