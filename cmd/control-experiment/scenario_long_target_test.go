package main

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
	raftfamily "github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic/raft"
	"github.com/SuzumiyaHaruki/consensus-atlas/targetoracles"
)

const (
	realTargetLongDecisions = 40
	realTargetLongCalls     = 16
)

type longTargetBundleComposition struct {
	id            string
	pssID         string
	runtime       controlexperiment.RuntimeConfig
	admission     controlexperiment.ExecutionAdmission
	envelope      *controlexperiment.FaultEnvelope
	qualification conformance.QualificationBundle
	factory       controlexperiment.AdapterFactory
	mapper        psscore.SemanticMapper
	projector     semantic.DecisionProjector
}

type longTargetObservationProjector interface {
	Project(controlruntime.Trace) (semantic.ObservationHistory, error)
}

func TestEtcdraftLongNaturalElectionInvestigationProducesTrustedEvidence(t *testing.T) {
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
	root, err := controlexperiment.ExecutionTracePrefix(inputs.root, 0)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := raftfamily.LeaderChangeWithInflightProposalWitness()
	if err != nil {
		t.Fatal(err)
	}
	projector := etcdraftSemanticPrefixProjector{}
	knowledge, hypothesis := longTargetHypothesis(t, inputs.knowledge, spec)
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(inputs.experiment.AdapterConfig)
	}
	started := time.Now()
	scenario := runLongTargetScenario(t, ctx, scenarioEpisodeCoreInputs{
		RootID: "etcdraft-long-natural-election", Knowledge: knowledge, Hypothesis: hypothesis,
		RiskSpec: spec, Root: root, Runtime: inputs.experiment.Runtime,
		FaultEnvelope: inputs.experiment.faultEnvelope(), TargetSurface: &target.Surface,
		SemanticExposure: inputs.experiment.ScenarioSemanticExposure,
		NewAdapter:       factory, RiskProjector: projector,
		SemanticProjector: func(
			trace controlruntime.Trace,
			frontier controlexperiment.RiskFrontierView,
			snapshot controlruntime.Snapshot,
		) (controlexperiment.ScenarioSemanticExposure, error) {
			return projectEtcdraftScenarioSemantics(
				inputs.experiment.ScenarioSemanticExposure, trace, frontier, snapshot,
			)
		},
	})
	bundle := executeLongTargetBundle(t, ctx, root, *scenario.Agent.Execution,
		longTargetBundleComposition{
			id: "etcdraft-long-natural-election", pssID: etcdraftv2.CorePSSMappingID,
			runtime: inputs.experiment.Runtime, admission: inputs.execution.admission,
			envelope: inputs.experiment.faultEnvelope(), qualification: inputs.execution.qualification,
			factory: factory, mapper: etcdraftv2.CorePSSMapper{},
			projector: etcdraftv2.DecisionProjector{},
		})
	assertLongTargetEvidence(
		t, "etcdraft", scenario, bundle, spec, projector,
		etcdraftv2.ObservationProjector{}, etcdraftv2.ObservationRaftTermAdvanced,
		etcdraftAgenticOracleRegistry(), time.Since(started),
	)
}

func TestOmnipaxosLongNaturalBallotInvestigationProducesTrustedEvidence(t *testing.T) {
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
	root, err := controlexperiment.ExecutionTracePrefix(inputs.Root, 0)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := omnipaxosMessageLossWitness()
	if err != nil {
		t.Fatal(err)
	}
	projector := omnipaxosScenarioProjector{}
	knowledge, hypothesis := longTargetHypothesis(t, inputs.Knowledge, spec)
	factory := func() (control.Adapter, error) {
		return omnipaxosv2.New(omnipaxosv2.Config{WorkerPath: workerPath})
	}
	started := time.Now()
	scenario := runLongTargetScenario(t, ctx, scenarioEpisodeCoreInputs{
		RootID: "omnipaxos-long-natural-ballot", Knowledge: knowledge, Hypothesis: hypothesis,
		RiskSpec: spec, Root: root, Runtime: inputs.Experiment.Runtime,
		FaultEnvelope: inputs.Experiment.faultEnvelope(), TargetSurface: &target.Surface,
		SemanticExposure: inputs.Experiment.ScenarioSemanticExposure,
		NewAdapter:       factory, RiskProjector: projector,
		SemanticProjector: func(
			trace controlruntime.Trace,
			frontier controlexperiment.RiskFrontierView,
			snapshot controlruntime.Snapshot,
		) (controlexperiment.ScenarioSemanticExposure, error) {
			return projectOmnipaxosScenarioSemantics(
				inputs.Experiment.ScenarioSemanticExposure, trace, frontier, snapshot,
			)
		},
	})
	bundle := executeLongTargetBundle(t, ctx, root, *scenario.Agent.Execution,
		longTargetBundleComposition{
			id: "omnipaxos-long-natural-ballot", pssID: omnipaxosv2.CorePSSMappingID,
			runtime: inputs.Experiment.Runtime, admission: inputs.Qualification.Admission,
			envelope: inputs.Experiment.faultEnvelope(), qualification: inputs.Qualification.Bundle,
			factory: factory, mapper: omnipaxosv2.CorePSSMapper{},
			projector: omnipaxosv2.DecisionProjector{},
		})
	assertLongTargetEvidence(
		t, "omnipaxos", scenario, bundle, spec, projector,
		omnipaxosv2.ObservationProjector{}, omnipaxosv2.ObservationPromiseRaised,
		omnipaxosAgenticOracleRegistry(), time.Since(started),
	)
}

func longTargetHypothesis(
	t *testing.T,
	base controlexperiment.ProtocolKnowledgePack,
	spec semantic.RiskWitnessSpec,
) (controlexperiment.ProtocolKnowledgePack, controlexperiment.TestHypothesis) {
	t.Helper()
	base.Digest = ""
	base.Risks = []controlexperiment.ProtocolRisk{{
		ID: spec.RiskID, Summary: "Calibrate a long natural timer investigation without pre-seeding its witness.",
		RequiredCapabilities: []string{"natural-time"},
		RequiredActions:      []control.ActionKind{control.ActionFireTemporal},
		AllowedBackendIDs:    []string{controlexperiment.ScenarioPlanningBackendID},
	}}
	knowledge, err := controlexperiment.NewProtocolKnowledgePack(base)
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := controlexperiment.NewTestHypothesis(
		"long-natural-investigation", knowledge, spec,
		"Use periodic trusted feedback to study natural coordinator progress.",
		controlexperiment.ScenarioPlanningBackendID,
	)
	if err != nil {
		t.Fatal(err)
	}
	return knowledge, hypothesis
}

func runLongTargetScenario(
	t *testing.T,
	ctx context.Context,
	inputs scenarioEpisodeCoreInputs,
) scenarioAgentEpisodeResult {
	t.Helper()
	calls := 0
	result, err := runScenarioEpisodeCore(
		ctx, inputs, realTargetLongCalls, 1, realTargetLongDecisions,
		func(_ context.Context, view controlexperiment.ScenarioAgentView) (
			[]byte, controlexperiment.ModelWork, error,
		) {
			if calls > 0 {
				prior := view.Prior
				if prior == nil || prior.ProgressDelta == nil ||
					prior.ProgressDelta.Decisions <= 0 ||
					prior.ProgressDelta.Decisions > controlexperiment.ScenarioNaturalProgressSlice+1 ||
					prior.ProgressDelta.FirstMissingMilestone == "" ||
					len(prior.ProgressDelta.RecentActions) == 0 ||
					len(prior.ProgressDelta.RecentActions) > 8 {
					t.Fatalf("long Target feedback drifted at call %d: %#v", calls+1, prior)
				}
			}
			var selected controlexperiment.FrontierActionRef
			selectedIndex := -1
			for _, kind := range []control.ActionKind{
				control.ActionFireTemporal, control.ActionCompleteEffect, control.ActionDeliverMessage,
			} {
				for index, action := range view.Frontier.Actions {
					if action.Kind == kind {
						selected = action
						selectedIndex = index
						break
					}
				}
				if selected.ActionID != "" {
					break
				}
			}
			if selected.ActionID == "" || selectedIndex < 0 {
				t.Fatalf("real Target frontier has no natural-progress Action at call %d", calls+1)
			}
			hint := view.Semantics.ActionHints[selectedIndex]
			selector := controlexperiment.FrontierActionSelector{
				Kind: selected.Kind, Node: selected.Node.Node, TemporalKind: selected.TemporalKind,
			}
			if hint.ActorRole != controlexperiment.ConsensusSemanticUnknown {
				selector.ActorRole = hint.ActorRole
			}
			if hint.OperationState != controlexperiment.ConsensusSemanticUnknown {
				selector.OperationState = hint.OperationState
			}
			calls++
			encoded, marshalErr := json.Marshal(controlexperiment.ScenarioInvestigationProposal{
				Intent: controlexperiment.ScenarioIntentContinue,
				Plan: controlexperiment.ScenarioPlan{
					ID: "long-natural-plan-" + string(selected.Node.Node),
					Steps: []controlexperiment.ScenarioStep{{
						ID:       "advance-natural-progress",
						Selector: selector,
					}},
				},
			})
			return encoded, controlexperiment.ModelWork{}, marshalErr
		},
	)
	if err != nil || calls <= 0 || calls > realTargetLongCalls || result.Agent.Status != controlexperiment.ScenarioAgentCompleted ||
		result.Agent.Execution == nil || len(result.Agent.Execution.FinalTrace.Records) != realTargetLongDecisions ||
		len(result.Agent.Attempts) != calls ||
		result.Agent.Attempts[calls-1].Feedback.ProgressDelta == nil ||
		result.Agent.Attempts[calls-1].Feedback.ProgressDelta.Decisions <= 0 ||
		result.Agent.Execution.FinalRisk.Status != semantic.RiskWitnessNotReached ||
		result.Agent.ExecutionWork.FrontierReconstruction.SetupAttempts != calls ||
		result.Agent.ExecutionWork.ChildVerification.SetupAttempts != calls ||
		result.Agent.ExecutionWork.ChildMaterialization.SchedulerDecisions != realTargetLongDecisions {
		traceDecisions := 0
		finalRisk := ""
		if result.Agent.Execution != nil {
			traceDecisions = len(result.Agent.Execution.FinalTrace.Records)
			finalRisk = result.Agent.Execution.FinalRisk.Status
		}
		t.Fatalf("real Target long Scenario did not close: status=%s stop=%s calls=%d attempts=%d trace=%d risk=%s reconstruct=%d verify=%d materialize=%d err=%v",
			result.Agent.Status, result.Agent.StopReason, calls, len(result.Agent.Attempts),
			traceDecisions, finalRisk,
			result.Agent.ExecutionWork.FrontierReconstruction.SetupAttempts,
			result.Agent.ExecutionWork.ChildVerification.SetupAttempts,
			result.Agent.ExecutionWork.ChildMaterialization.SchedulerDecisions, err)
	}
	return result
}

func executeLongTargetBundle(
	t *testing.T,
	ctx context.Context,
	root controlruntime.Trace,
	execution controlexperiment.ScenarioExecution,
	composition longTargetBundleComposition,
) controlexperiment.ExecutionBundle {
	t.Helper()
	policy, err := controlexperiment.RecordedScenarioSchedulePolicy(
		composition.id+"-recorded-schedule", root, execution,
	)
	if err != nil {
		t.Fatal(err)
	}
	config := controlexperiment.Config{
		SchemaVersion: controlexperiment.SchemaVersionV2, ID: composition.id,
		PSSID: composition.pssID, Runtime: composition.runtime,
		Admission: &composition.admission, FaultEnvelope: composition.envelope,
		DecisionsPerRun: len(execution.FinalTrace.Records), RequireReplay: true,
		Runs: []controlexperiment.RunPlan{{Run: 1, Policy: policy}},
	}
	_, bundle, err := controlexperiment.ExecuteQualifiedRecordedBundle(
		ctx, config, execution.FinalTrace, composition.qualification, composition.factory,
		composition.mapper, composition.projector, nil,
	)
	if err != nil || bundle.Trace.Digest != execution.FinalTrace.Digest || !bundle.Run.Replay.Stable {
		t.Fatalf("long Target Bundle did not reproduce Scenario Trace: %s/%s/%v",
			bundle.Trace.Digest, execution.FinalTrace.Digest, err)
	}
	return bundle
}

func assertLongTargetEvidence(
	t *testing.T,
	target string,
	scenario scenarioAgentEpisodeResult,
	bundle controlexperiment.ExecutionBundle,
	spec semantic.RiskWitnessSpec,
	riskProjector controlexperiment.SemanticPrefixProjector,
	observationProjector longTargetObservationProjector,
	targetProgressKind semantic.ObservationKind,
	registry targetoracles.Registry,
	duration time.Duration,
) {
	t.Helper()
	history, err := observationProjector.Project(bundle.Trace)
	if err != nil || history.TraceDigest != bundle.Trace.Digest || len(history.Events) == 0 {
		t.Fatalf("%s long Trace did not project target observations: %#v/%v", target, history, err)
	}
	targetProgress := 0
	for _, event := range history.Events {
		if event.Kind == targetProgressKind {
			targetProgress++
		}
	}
	if targetProgress == 0 {
		t.Fatalf("%s long Trace did not expose %s", target, targetProgressKind)
	}
	risk, err := riskProjector.Project(
		scenario.Agent.Execution.FinalRisk.ID, spec, bundle.Trace,
	)
	if err != nil || !reflect.DeepEqual(risk, scenario.Agent.Execution.FinalRisk) {
		t.Fatalf("%s long Trace changed Risk projection: %#v/%v", target, risk, err)
	}
	states := make([]psscore.State, len(bundle.CorePSS))
	for index, sample := range bundle.CorePSS {
		states[index] = sample.State
	}
	views, err := psscore.SummarizeStates(states)
	verdict := registry.Check(bundle)
	if err != nil || views.Validate() != nil || views.Samples != realTargetLongDecisions+1 ||
		views.ProtocolStates < 2 || views.ControlStates < 2 || views.JointStates < 2 ||
		len(verdict.Checked) != len(registry.Capabilities()) || len(verdict.Violations) != 0 {
		t.Fatalf("%s long evidence incomplete: views=%#v oracle=%#v err=%v", target, views, verdict, err)
	}
	t.Logf("%s 128 actions: observations=%d target_progress=%d protocol_pss=%d control_pss=%d joint_pss=%d reconstruction=%d verification=%d verification_decisions=%d scenario_work=%d primary=%d replay=%d duration=%s",
		target, len(history.Events), targetProgress, views.ProtocolStates, views.ControlStates, views.JointStates,
		scenario.Agent.ExecutionWork.FrontierReconstruction.SetupAttempts,
		scenario.Agent.ExecutionWork.ChildVerification.SetupAttempts,
		scenario.Agent.ExecutionWork.ChildVerification.SchedulerDecisions,
		scenario.Agent.ExecutionWork.TotalWorkUnits,
		bundle.Work.Primary.WorkUnits, bundle.Work.Replay.WorkUnits, duration.Round(time.Millisecond))
}
