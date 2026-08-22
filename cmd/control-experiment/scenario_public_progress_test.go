package main

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestEtcdraftPublicProgressClosesDroppedAppendResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(), controlExperimentTestTimeout(180*time.Second),
	)
	defer cancel()
	_, experiment, workload, err := loadEtcdraftAgenticAuthoringSource(
		"../../plans/agent/etcdraft-agentic-calibration-v1.json",
	)
	if err != nil {
		t.Fatal(err)
	}
	executionInputs, root, err := prepareEtcdraftAgenticExecutionInputs(ctx, workload, experiment)
	if err != nil {
		t.Fatal(err)
	}
	spec, predicates, err := publicEtcdraftDropRisk()
	if err != nil {
		t.Fatal(err)
	}
	projector, err := controlexperiment.NewLinearObservationRiskProjector(
		spec, predicates, etcdraftv2.ObservationProjector{},
	)
	if err != nil {
		t.Fatal(err)
	}
	rootRisk, err := projector.Project("etcdraft-public-root-risk", spec, root)
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(experiment.AdapterConfig)
	}
	intervention, err := controlexperiment.ExecuteSemanticBoundedScenarioPlan(
		ctx, "etcdraft-public-drop", publicEtcdraftDropPlan(), 5, 5,
		spec, rootRisk, root, experiment.Runtime, experiment.faultEnvelope(), factory,
		projector, func(
			trace controlruntime.Trace,
			frontier controlexperiment.RiskFrontierView,
			snapshot controlruntime.Snapshot,
		) (controlexperiment.ScenarioSemanticExposure, error) {
			return projectEtcdraftScenarioSemantics(
				experiment.ScenarioSemanticExposure, trace, frontier, snapshot,
			)
		}, 0,
	)
	if err != nil || len(intervention.Steps) != 5 ||
		intervention.Steps[4].Choice == nil ||
		intervention.Steps[4].Choice.Action.Kind != control.ActionDropMessage {
		t.Fatalf("prepare etcd intervention: %#v/%v", intervention, err)
	}
	progress, err := controlexperiment.ExecuteScenarioNaturalProgress(
		ctx, "etcdraft-public-after-drop", 32, spec, intervention.FinalRisk,
		intervention.FinalTrace, experiment.Runtime, experiment.faultEnvelope(), factory, projector,
	)
	if err != nil || progress.StopReason != controlexperiment.ScenarioProgressClientTerminal ||
		len(progress.Execution.Steps) != 17 ||
		progress.Execution.FinalRisk.Status != semantic.RiskWitnessReached {
		t.Fatalf("public etcd progress did not close: %#v/%v", progress, err)
	}
	combined := intervention
	combined.PlanID = "etcdraft-public-after-drop-qualified"
	combined.AutomaticProgress = append(
		append([]controlexperiment.ScenarioStepFeedback(nil), intervention.AutomaticProgress...),
		progress.Execution.Steps...,
	)
	combined.NaturalProgressStop = progress.StopReason
	combined.FinalTrace, combined.FinalRisk = progress.Execution.FinalTrace, progress.Execution.FinalRisk
	qualified, err := executeEtcdraftScenarioQualifiedRisk(
		ctx, executionInputs, root, experiment, combined, spec, projector, "",
	)
	if err != nil || !qualified.Replay.Stable || len(qualified.Oracle.Violations) != 0 ||
		len(qualified.Bundle.ClientHistory) != 1 ||
		qualified.Bundle.ClientHistory[0].Response.RequestID != workload.Invocations[0].ID ||
		qualified.Bundle.ClientHistory[0].Response.Status != "committed" {
		t.Fatalf("public etcd evidence did not close: %#v/%v", qualified, err)
	}
}

func TestOmnipaxosPublicProgressClosesDroppedReplication(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(), controlExperimentTestTimeout(180*time.Second),
	)
	defer cancel()
	workerPath := buildOmnipaxosScenarioWorker(t)
	inputs, err := prepareOmnipaxosAgenticEpisode(
		ctx, workerPath, "../../plans/agent/omnipaxos-agentic-calibration-v1.json",
	)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := omnipaxosMessageLossWitness()
	if err != nil {
		t.Fatal(err)
	}
	projector := omnipaxosScenarioProjector{}
	factory := func() (control.Adapter, error) {
		return omnipaxosv2.New(omnipaxosv2.Config{WorkerPath: workerPath})
	}
	semanticProjector := func(
		trace controlruntime.Trace,
		frontier controlexperiment.RiskFrontierView,
		snapshot controlruntime.Snapshot,
	) (controlexperiment.ScenarioSemanticExposure, error) {
		return projectOmnipaxosScenarioSemantics(
			inputs.Experiment.ScenarioSemanticExposure, trace, frontier, snapshot,
		)
	}
	intervention, err := advanceOmnipaxosToPublicReplicationDrop(
		ctx, inputs.Root, spec, inputs.Experiment.Runtime, inputs.Experiment.faultEnvelope(),
		factory, projector, semanticProjector,
	)
	if err != nil || len(intervention.Steps) != 1 || intervention.Steps[0].Choice == nil ||
		intervention.Steps[0].Choice.Action.Kind != control.ActionDropMessage {
		t.Fatalf("prepare OmniPaxos intervention: %#v/%v", intervention, err)
	}
	progress, err := controlexperiment.ExecuteScenarioNaturalProgress(
		ctx, "omnipaxos-public-after-drop", 32, spec, intervention.FinalRisk,
		intervention.FinalTrace, inputs.Experiment.Runtime, inputs.Experiment.faultEnvelope(),
		factory, projector,
	)
	if err != nil || progress.StopReason != controlexperiment.ScenarioProgressClientTerminal ||
		progress.Execution.FinalRisk.Status != semantic.RiskWitnessReached ||
		len(progress.Execution.Steps) <= 4 {
		t.Fatalf("public OmniPaxos progress did not close: %#v/%v", progress, err)
	}
	combined := intervention
	combined.PlanID = "omnipaxos-public-after-drop-qualified"
	prefixProgress, err := publicRecordedProgress(
		inputs.Root, intervention.FinalTrace, len(intervention.Steps),
	)
	if err != nil {
		t.Fatal(err)
	}
	combined.AutomaticProgress = append(
		prefixProgress,
		progress.Execution.Steps...,
	)
	combined.NaturalProgressStop = progress.StopReason
	combined.FinalTrace, combined.FinalRisk = progress.Execution.FinalTrace, progress.Execution.FinalRisk
	qualified, err := executeOmnipaxosScenarioQualifiedRisk(
		ctx, workerPath, inputs.Experiment, inputs.Workload, inputs.Qualification,
		inputs.Root, combined, spec, projector, "",
	)
	if err != nil || !qualified.Replay.Stable || len(qualified.Oracle.Violations) != 0 ||
		len(qualified.Bundle.ClientHistory) != 1 ||
		qualified.Bundle.ClientHistory[0].State != control.ItemCompleted {
		t.Fatalf("public OmniPaxos evidence did not close: %#v/%v", qualified, err)
	}
}

func publicRecordedProgress(
	root controlruntime.Trace,
	trace controlruntime.Trace,
	trailingStrategicSteps int,
) ([]controlexperiment.ScenarioStepFeedback, error) {
	end := len(trace.Records) - trailingStrategicSteps
	if root.Validate() != nil || trace.Validate() != nil || end < len(root.Records) {
		return nil, errors.New("PUBLIC_RECORDED_PROGRESS_INPUT_INVALID")
	}
	result := make([]controlexperiment.ScenarioStepFeedback, 0, end-len(root.Records))
	for index := len(root.Records); index < end; index++ {
		record := trace.Records[index]
		digest, err := control.CanonicalDigest(record.Action)
		if err != nil {
			return nil, err
		}
		decision := int(record.Step)
		result = append(result, controlexperiment.ScenarioStepFeedback{
			StepID: "public-prefix", Outcome: controlexperiment.ScenarioStepApplied,
			Decision: decision, MatchCount: 1,
			Choice: &controlexperiment.FrontierChoice{
				Decision: decision,
				Action: controlexperiment.FrontierActionRef{
					ActionID: record.Action.ID, ActionDigest: digest, Kind: record.Action.Kind,
					Node: record.Action.Node, ItemID: record.Action.Item,
				},
			},
		})
	}
	return result, nil
}

func publicEtcdraftDropRisk() (
	semantic.RiskWitnessSpec,
	[]semantic.ObservationPredicate,
	error,
) {
	const invoked = "workload-invoked-at-coordinator"
	const dropped = "append-response-dropped-while-replication-inflight"
	const advanced = "alternate-quorum-decision-advanced"
	spec, err := semantic.NewRiskWitnessSpec(
		"etcdraft-public-append-response-loss-v1", "raft",
		"append-response-loss-with-alternate-quorum",
		[]string{invoked, dropped, advanced},
		[]semantic.RiskWitnessOrder{{Before: invoked, After: dropped}, {Before: dropped, After: advanced}},
	)
	if err != nil {
		return semantic.RiskWitnessSpec{}, nil, err
	}
	return spec, []semantic.ObservationPredicate{
		{MilestoneID: invoked, Kind: semantic.ObservationWorkloadInvoked,
			Constraints: []semantic.ObservationConstraint{{
				Field: semantic.ObservationFieldParticipantRole, Equals: "coordinator",
			}}},
		{MilestoneID: dropped, Kind: semantic.ObservationMessageDropped,
			Constraints: []semantic.ObservationConstraint{{
				Field: semantic.ObservationFieldMessageRole, Equals: "MsgAppResp",
			}}},
		{MilestoneID: advanced, Kind: semantic.ObservationDecisionAdvanced},
	}, nil
}

func publicEtcdraftDropPlan() controlexperiment.ScenarioPlan {
	return controlexperiment.ScenarioPlan{ID: "etcdraft-public-drop-plan", Steps: []controlexperiment.ScenarioStep{
		{ID: "persist-leader", Selector: controlexperiment.FrontierActionSelector{
			Kind: control.ActionCompleteEffect, Owner: "n1", EffectKind: "raft-ready-persist",
		}},
		{ID: "advance-leader", Selector: controlexperiment.FrontierActionSelector{
			Kind: control.ActionCompleteEffect, Owner: "n1", EffectKind: "raft-ready-advance",
		}},
		{ID: "deliver-proposal", Selector: controlexperiment.FrontierActionSelector{
			Kind: control.ActionDeliverMessage, MessageSource: "n1", MessageTarget: "n2", MessageTypeHint: "MsgApp",
		}},
		{ID: "persist-follower", Selector: controlexperiment.FrontierActionSelector{
			Kind: control.ActionCompleteEffect, Owner: "n2", EffectKind: "raft-ready-persist",
		}},
		{ID: "drop-response", Selector: controlexperiment.FrontierActionSelector{
			Kind: control.ActionDropMessage, MessageSource: "n2", MessageTarget: "n1", MessageTypeHint: "MsgAppResp",
		}},
	}}
}

func etcdraftAlternateQuorumRiskCandidate() controlexperiment.RiskCandidate {
	_, predicates, err := publicEtcdraftDropRisk()
	if err != nil {
		panic(err)
	}
	return controlexperiment.RiskCandidate{
		ID:          "append-response-loss-with-alternate-quorum",
		PropertyRef: "client-operation-continuity", InspirationRef: "original",
		Summary:            "Drop one append response and close the same request through the alternate quorum.",
		SuspectedMechanism: "An append response is lost while another follower remains able to form a quorum for the same request.",
		Predicates:         predicates,
	}
}

func advanceOmnipaxosToPublicReplicationDrop(
	ctx context.Context,
	root controlruntime.Trace,
	spec semantic.RiskWitnessSpec,
	runtimeConfig controlexperiment.RuntimeConfig,
	faultEnvelope *controlexperiment.FaultEnvelope,
	adapterFactory controlexperiment.AdapterFactory,
	projector controlexperiment.SemanticPrefixProjector,
	semanticProjector controlexperiment.ScenarioSemanticProjector,
) (controlexperiment.ScenarioExecution, error) {
	seed, err := hex.DecodeString(runtimeConfig.SeedHex)
	if err != nil || len(seed) == 0 || faultEnvelope == nil || faultEnvelope.MaxMessageDrops < 1 {
		return controlexperiment.ScenarioExecution{}, errors.New("OMNIPAXOS_PUBLIC_PREFIX_INPUT_INVALID")
	}
	adapter, err := adapterFactory()
	if err != nil {
		return controlexperiment.ScenarioExecution{}, err
	}
	runtime, err := controlruntime.Replay(ctx, adapter, controlruntime.Config{
		Seed: seed, ClockError: runtimeConfig.ClockError, MaxClones: runtimeConfig.MaxClones,
	}, root)
	if err != nil {
		return controlexperiment.ScenarioExecution{}, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = runtime.Close()
		}
	}()
	var drop control.Action
	for decision := 0; decision < 128; decision++ {
		actions, enabledErr := runtime.EnabledActions(ctx)
		if enabledErr != nil {
			return controlexperiment.ScenarioExecution{}, enabledErr
		}
		snapshot := runtime.Snapshot()
		for _, action := range actions {
			if action.Kind != control.ActionDropMessage {
				continue
			}
			item, ok := publicSnapshotItem(snapshot, action.Item)
			if ok && item.Value.Message != nil &&
				(item.Value.Message.TypeHint == "sequence-paxos/accept-decide" ||
					item.Value.Message.TypeHint == "sequence-paxos/accept-sync") {
				drop = action
				break
			}
		}
		if drop.ID != "" {
			break
		}
		selected, ok := firstOmnipaxosScenarioProgress(actions)
		if !ok {
			return controlexperiment.ScenarioExecution{}, errors.New("OMNIPAXOS_PUBLIC_PREFIX_QUIESCENT")
		}
		if _, err := runtime.Select(ctx, selected.ID); err != nil {
			return controlexperiment.ScenarioExecution{}, err
		}
	}
	if drop.ID == "" {
		return controlexperiment.ScenarioExecution{}, errors.New("OMNIPAXOS_PUBLIC_PREFIX_BUDGET_EXHAUSTED")
	}
	prefix, err := runtime.Trace()
	if err != nil {
		return controlexperiment.ScenarioExecution{}, err
	}
	if err := runtime.Close(); err != nil {
		return controlexperiment.ScenarioExecution{}, err
	}
	closed = true
	prefixRisk, err := projector.Project("omnipaxos-public-prefix-risk", spec, prefix)
	if err != nil {
		return controlexperiment.ScenarioExecution{}, err
	}
	return controlexperiment.ExecuteSemanticBoundedScenarioPlan(
		ctx, "omnipaxos-public-intervention",
		controlexperiment.ScenarioPlan{ID: "omnipaxos-public-drop-plan", Steps: []controlexperiment.ScenarioStep{{
			ID: "drop-replication", Selector: controlexperiment.FrontierActionSelector{ActionID: drop.ID},
		}}},
		1, 1, spec, prefixRisk, prefix, runtimeConfig, faultEnvelope, adapterFactory,
		projector, semanticProjector, 0,
	)
}

func publicSnapshotItem(
	snapshot controlruntime.Snapshot,
	id control.ItemID,
) (controlruntime.ItemSnapshot, bool) {
	for _, item := range snapshot.Items {
		if item.ID == id {
			return item, true
		}
	}
	return controlruntime.ItemSnapshot{}, false
}

func TestPublicDropRiskDefinitionsRemainStable(t *testing.T) {
	spec, predicates, err := publicEtcdraftDropRisk()
	if err != nil || spec.Validate() != nil || len(predicates) != 3 ||
		len(spec.Milestones) != 3 ||
		spec.Milestones[0].ID != "workload-invoked-at-coordinator" ||
		spec.Milestones[1].ID != "append-response-dropped-while-replication-inflight" ||
		spec.Milestones[2].ID != "alternate-quorum-decision-advanced" {
		t.Fatalf("public drop fixture drifted: %#v/%#v/%v", spec, predicates, err)
	}
}
