package main

import (
	"context"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
	raftfamily "github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic/raft"
)

func TestEtcdraftComposableActionsAreReachableAndReplayable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(180*time.Second))
	defer cancel()
	inputs, err := prepareEtcdraftAgenticEpisode(
		ctx, "", "../../plans/agent/etcdraft-agentic-calibration-v1.json",
		fixtureOpenRouterIntentClient(),
	)
	if err != nil {
		t.Fatal(err)
	}
	target, err := newEtcdraftAgenticEpisodeTarget(inputs)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := raftfamily.LeaderChangeWithInflightProposalWitness()
	if err != nil {
		t.Fatal(err)
	}
	projector := etcdraftSemanticPrefixProjector{}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(inputs.experiment.AdapterConfig)
	}
	root, err := buildWorkloadReadyRootForTest(
		ctx, inputs.root, inputs.experiment.Runtime, factory,
		etcdraftv2.WorkloadRouter{}, inputs.execution.workload, firstPublicWorkloadProgress,
	)
	if err != nil {
		t.Fatal(err)
	}
	rootRisk, err := projector.Project("etcdraft-action-reachability-root", spec, root)
	if err != nil {
		t.Fatal(err)
	}
	frontier, _, _, err := controlexperiment.ReconstructRiskFrontierState(
		ctx, "etcdraft-action-reachability-frontier", spec, rootRisk, root,
		len(root.Records), inputs.experiment.Runtime, inputs.experiment.faultEnvelope(), factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	traces := []controlruntime.Trace{root}
	for _, pair := range [][]control.ActionKind{
		{control.ActionCrash, control.ActionRestart},
		{control.ActionPartition, control.ActionHeal},
	} {
		if actionKindsCovered(traces)[pair[0]] && actionKindsCovered(traces)[pair[1]] {
			continue
		}
		selector := controlexperiment.FrontierActionSelector{Kind: pair[0]}
		if pair[0] == control.ActionCrash {
			selector.Node = firstFrontierAction(t, frontier, control.ActionCrash).Node.Node
		}
		second := controlexperiment.FrontierActionSelector{Kind: pair[1], Node: selector.Node}
		plan := controlexperiment.ScenarioPlan{ID: "reach-" + string(pair[0]), Steps: []controlexperiment.ScenarioStep{
			{ID: "first-" + string(pair[0]), Selector: selector},
			{ID: "then-" + string(pair[1]), Selector: second},
		}}
		execution, runErr := controlexperiment.ExecuteBoundedScenarioPlan(
			ctx, plan.ID, plan, 2, 2, spec, rootRisk, root,
			inputs.experiment.Runtime, inputs.experiment.faultEnvelope(), factory, projector, 0,
			prepareEtcdraftScenarioAction,
		)
		if runErr != nil || execution.Status != controlexperiment.ScenarioStatusCompleted {
			t.Fatalf("%v path was not reachable and replayable: %#v/%v", pair, execution, runErr)
		}
		traces = append(traces, execution.FinalTrace)
	}
	traces = appendMissingRootActions(
		t, ctx, traces, target.Surface.Capabilities.ComposableActions, frontier, spec, rootRisk,
		root, inputs.experiment.Runtime, inputs.experiment.faultEnvelope(), factory, projector,
	)
	assertComposableActionsCovered(t, target.Surface, traces)
}

func TestOmnipaxosComposableActionsAreReachableAndReplayable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(180*time.Second))
	defer cancel()
	workerPath := buildOmnipaxosScenarioWorker(t)
	inputs, err := prepareOmnipaxosAgenticEpisode(
		ctx, workerPath, "../../plans/agent/omnipaxos-agentic-calibration-v1.json",
	)
	if err != nil {
		t.Fatal(err)
	}
	target, err := newOmnipaxosAgenticEpisodeTarget(inputs)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := omnipaxosMessageLossWitness()
	if err != nil {
		t.Fatal(err)
	}
	projector := omnipaxosScenarioProjector{}
	rootRisk, err := projector.Project("omnipaxos-action-reachability-root", spec, inputs.Root)
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) {
		return omnipaxosv2.New(omnipaxosv2.Config{WorkerPath: workerPath})
	}
	frontier, _, _, err := controlexperiment.ReconstructRiskFrontierState(
		ctx, "omnipaxos-action-reachability-frontier", spec, rootRisk, inputs.Root,
		len(inputs.Root.Records), inputs.Experiment.Runtime, inputs.Experiment.faultEnvelope(), factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	traces := appendMissingRootActions(
		t, ctx, []controlruntime.Trace{inputs.Root}, target.Surface.Capabilities.ComposableActions,
		frontier, spec, rootRisk, inputs.Root, inputs.Experiment.Runtime,
		inputs.Experiment.faultEnvelope(), factory, projector,
	)
	assertComposableActionsCovered(t, target.Surface, traces)
}

func appendMissingRootActions(
	t *testing.T,
	ctx context.Context,
	traces []controlruntime.Trace,
	want []control.ActionKind,
	frontier controlexperiment.RiskFrontierView,
	spec semantic.RiskWitnessSpec,
	rootRisk semantic.RiskWitnessResult,
	root controlruntime.Trace,
	runtimeConfig controlexperiment.RuntimeConfig,
	envelope *controlexperiment.FaultEnvelope,
	factory controlexperiment.AdapterFactory,
	projector controlexperiment.SemanticPrefixProjector,
) []controlruntime.Trace {
	t.Helper()
	covered := actionKindsCovered(traces)
	for _, kind := range want {
		if covered[kind] {
			continue
		}
		action := firstFrontierAction(t, frontier, kind)
		plan := controlexperiment.ScenarioPlan{
			ID: "reach-" + string(kind),
			Steps: []controlexperiment.ScenarioStep{{
				ID:       "select-" + string(kind),
				Selector: controlexperiment.FrontierActionSelector{ActionID: action.ActionID},
			}},
		}
		execution, err := controlexperiment.ExecuteBoundedScenarioPlan(
			ctx, plan.ID, plan, 1, 1, spec, rootRisk, root, runtimeConfig, envelope,
			factory, projector, 0,
		)
		if err != nil || execution.Status != controlexperiment.ScenarioStatusCompleted {
			t.Fatalf("%s path was not reachable and replayable: %#v/%v", kind, execution, err)
		}
		traces = append(traces, execution.FinalTrace)
		covered[kind] = true
	}
	return traces
}

func firstFrontierAction(
	t *testing.T,
	frontier controlexperiment.RiskFrontierView,
	kind control.ActionKind,
) controlexperiment.FrontierActionRef {
	t.Helper()
	for _, action := range frontier.Actions {
		if action.Kind == kind {
			return action
		}
	}
	t.Fatalf("frontier does not offer %s: %#v", kind, frontier.Actions)
	return controlexperiment.FrontierActionRef{}
}

func actionKindsCovered(traces []controlruntime.Trace) map[control.ActionKind]bool {
	covered := make(map[control.ActionKind]bool)
	for _, trace := range traces {
		for _, record := range trace.Records {
			covered[record.Action.Kind] = true
		}
	}
	return covered
}

func assertComposableActionsCovered(
	t *testing.T,
	surface controlexperiment.AgentTargetSurface,
	traces []controlruntime.Trace,
) {
	t.Helper()
	covered := actionKindsCovered(traces)
	for _, kind := range surface.Capabilities.ComposableActions {
		if !covered[kind] {
			t.Fatalf("composable action %s has no target-owned executed/replayed path", kind)
		}
	}
}
