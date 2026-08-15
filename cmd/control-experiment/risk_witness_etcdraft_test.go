package main

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestEtcdraftRiskIsReachableThroughNaturalElectionBeyondAgentHorizon(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	inputs, err := prepareEtcdraftSemanticCalibration(
		ctx, etcdraftTestRootCorpusPath, etcdraftTestSemanticInputPath, fixtureOpenRouterIntentClient(),
	)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := hex.DecodeString(inputs.experiment.Runtime.SeedHex)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := etcdraftv2.NewWithConfig(inputs.experiment.AdapterConfig)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := controlruntime.Replay(ctx, adapter, controlruntime.Config{
		Seed: seed, ClockError: inputs.experiment.Runtime.ClockError,
		MaxClones: inputs.experiment.Runtime.MaxClones,
	}, inputs.root)
	if err != nil {
		t.Fatal(err)
	}
	oldCoordinator := inputs.root.Records[len(inputs.root.Records)-1].Action.Node
	if inputs.root.Records[len(inputs.root.Records)-1].Action.Kind != control.ActionInvoke ||
		etcdraftReachabilityHasClientReturn(runtime.Snapshot()) {
		t.Fatal("semantic root is not an in-flight invocation at the old coordinator")
	}
	crash, ok, err := etcdraftReachabilityAction(ctx, runtime, control.ActionCrash, oldCoordinator.Node)
	if err != nil || !ok {
		t.Fatalf("old coordinator crash unavailable: %#v/%v", crash, err)
	}
	if _, err := runtime.Select(ctx, crash.ID); err != nil {
		t.Fatal(err)
	}

	projector := etcdraftSemanticPrefixProjector{}
	extension := []control.ActionKind{control.ActionCrash}
	for decision := 0; decision < 256; decision++ {
		trace, err := runtime.Trace()
		if err != nil {
			t.Fatal(err)
		}
		risk, err := projector.Project("etcdraft-a6e-reachability", inputs.riskSpec, trace)
		if err != nil {
			t.Fatal(err)
		}
		if len(risk.SatisfiedMilestones) >= 2 {
			if etcdraftReachabilityHasClientReturn(runtime.Snapshot()) {
				t.Fatal("workload returned before the coordinator-change milestone")
			}
			restart, found, err := etcdraftReachabilityAction(
				ctx, runtime, control.ActionRestart, oldCoordinator.Node,
			)
			if err != nil || !found {
				t.Fatalf("old coordinator restart unavailable after change: %#v/%v", restart, err)
			}
			if _, err := runtime.Select(ctx, restart.ID); err != nil {
				t.Fatal(err)
			}
			extension = append(extension, restart.Kind)
			break
		}
		action, found, err := etcdraftReachabilityProgressAction(ctx, runtime)
		if err != nil || !found {
			t.Fatalf("natural election stalled after %d extension decisions: %v", len(extension), err)
		}
		if _, err := runtime.Select(ctx, action.ID); err != nil {
			t.Fatal(err)
		}
		extension = append(extension, action.Kind)
	}
	finalTrace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	finalRisk, err := projector.Project("etcdraft-a6e-reachability-final", inputs.riskSpec, finalTrace)
	if err != nil || finalRisk.Status != semantic.RiskWitnessReached {
		t.Fatalf("real Adapter schedule did not reach risk: %#v/%v", finalRisk, err)
	}
	if len(extension) <= inputs.experiment.ScenarioMaxSteps {
		t.Fatalf("calibration no longer demonstrates the configured horizon: %v", extension)
	}
	fresh, err := etcdraftv2.NewWithConfig(inputs.experiment.AdapterConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controlruntime.Replay(ctx, fresh, controlruntime.Config{
		Seed: seed, ClockError: inputs.experiment.Runtime.ClockError,
		MaxClones: inputs.experiment.Runtime.MaxClones,
	}, finalTrace); err != nil {
		t.Fatalf("reachable witness is not replay-stable: %v", err)
	}
	t.Logf("reachable extension decisions=%d kinds=%v", len(extension), extension)
}

func TestScenarioNaturalProgressUsesTrustedCheckpointAfterEtcdraftMilestone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	inputs, err := prepareEtcdraftSemanticCalibration(
		ctx, etcdraftTestRootCorpusPath, etcdraftTestSemanticInputPath, fixtureOpenRouterIntentClient(),
	)
	if err != nil {
		t.Fatal(err)
	}
	projector := etcdraftSemanticPrefixProjector{}
	rootRisk, err := projector.Project("scenario-progress-root", inputs.riskSpec, inputs.root)
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(inputs.experiment.AdapterConfig)
	}
	frontier, _, _, err := controlexperiment.ReconstructRiskFrontierState(
		ctx, "scenario-progress-frontier", inputs.riskSpec, rootRisk, inputs.root,
		len(inputs.root.Records), inputs.experiment.Runtime, inputs.experiment.faultEnvelope(), factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	var crash controlexperiment.FrontierActionRef
	for _, action := range frontier.Actions {
		if action.Kind == control.ActionCrash && action.Node.Node == "n1" {
			crash = action
			break
		}
	}
	intervention, err := controlexperiment.ExecuteBoundedScenarioPlan(
		ctx, "scenario-progress-crash",
		controlexperiment.ScenarioPlan{ID: "scenario-progress-crash", Steps: []controlexperiment.ScenarioStep{{
			ID:       "crash-old-coordinator",
			Selector: controlexperiment.FrontierActionSelector{ActionID: crash.ActionID},
		}}},
		1, inputs.riskSpec, rootRisk, inputs.root, inputs.experiment.Runtime,
		inputs.experiment.faultEnvelope(), factory, projector,
	)
	if err != nil {
		t.Fatal(err)
	}
	progress, err := controlexperiment.ExecuteScenarioNaturalProgress(
		ctx, "scenario-progress-closure", 31, inputs.riskSpec,
		intervention.FinalRisk, intervention.FinalTrace, inputs.experiment.Runtime,
		inputs.experiment.faultEnvelope(), factory, projector,
	)
	if err != nil {
		t.Fatal(err)
	}
	kinds := make([]control.ActionKind, 0, len(progress.Execution.Steps))
	for _, step := range progress.Execution.Steps {
		kinds = append(kinds, step.Choice.Action.Kind)
	}
	if progress.StopReason != controlexperiment.ScenarioProgressPlanningCheckpoint ||
		len(progress.Execution.Steps) != controlexperiment.ScenarioNaturalProgressCheckpointActions ||
		len(progress.Execution.FinalRisk.SatisfiedMilestones) < 2 {
		t.Fatalf("natural progress did not preserve the trusted checkpoint: stop=%s kinds=%v risk=%#v",
			progress.StopReason, kinds, progress.Execution.FinalRisk)
	}
	t.Logf("natural progress decisions=%d kinds=%v", len(kinds), kinds)
}

func etcdraftReachabilityProgressAction(
	ctx context.Context,
	runtime *controlruntime.Runtime,
) (control.Action, bool, error) {
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		return control.Action{}, false, err
	}
	for _, kind := range []control.ActionKind{
		control.ActionCompleteEffect, control.ActionDeliverMessage, control.ActionFireTemporal,
	} {
		for _, action := range actions {
			if action.Kind == kind {
				return action, true, nil
			}
		}
	}
	return control.Action{}, false, nil
}

func etcdraftReachabilityAction(
	ctx context.Context,
	runtime *controlruntime.Runtime,
	kind control.ActionKind,
	node control.NodeID,
) (control.Action, bool, error) {
	actions, err := runtime.EnabledActions(ctx)
	if err != nil {
		return control.Action{}, false, err
	}
	for _, action := range actions {
		if action.Kind == kind && action.Node.Node == node {
			return action, true, nil
		}
	}
	return control.Action{}, false, nil
}

func etcdraftReachabilityHasClientReturn(snapshot controlruntime.Snapshot) bool {
	for _, item := range snapshot.Items {
		if item.Kind == control.ItemClientResult && item.State == control.ItemCompleted &&
			item.Value.Response != nil {
			return true
		}
	}
	return false
}
