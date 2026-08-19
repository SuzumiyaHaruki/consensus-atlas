package main

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestM4eRiskCandidateRunsThroughScenarioRuntimeReplayAndOracle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(180*time.Second))
	defer cancel()
	workerPath := buildOmnipaxosScenarioWorker(t)
	inputs, err := prepareOmnipaxosAgenticEpisode(
		ctx, workerPath, "../../plans/agent/omnipaxos-agentic-calibration-v1.json",
	)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := (omnipaxosv2.ObservationProjector{}).Capabilities()
	actions := inputs.Qualification.Bundle.Manifest.Capabilities.Actions
	candidate := omnipaxosDiscoveredRiskCandidate()
	content, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	riskResult, err := controlexperiment.DiscoverRiskWithPlanner(
		ctx, controlexperiment.RiskAgentBudget{MaxCalls: 1, MaxTokens: 20},
		inputs.Knowledge, capabilities, actions, nil, nil, nil,
		func(context.Context, controlexperiment.RiskAgentView) ([]byte, controlexperiment.ModelWork, error) {
			return content, controlexperiment.ModelWork{
				Calls: 1, InputTokens: 4, OutputTokens: 3, TotalTokens: 7,
			}, nil
		},
	)
	if err != nil || riskResult.Accepted == nil {
		t.Fatalf("Risk Agent did not produce an accepted candidate: %#v/%v", riskResult, err)
	}
	scenarioRisk, err := controlexperiment.BuildScenarioRiskHypothesis(
		inputs.Knowledge, *riskResult.Accepted, capabilities, actions,
	)
	if err != nil {
		t.Fatal(err)
	}
	projector, err := controlexperiment.NewLinearObservationRiskProjector(
		scenarioRisk.Spec, scenarioRisk.Predicates, omnipaxosv2.ObservationProjector{},
	)
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) {
		return omnipaxosv2.New(omnipaxosv2.Config{WorkerPath: workerPath})
	}
	scenario, err := runScenarioEpisodeCore(ctx, scenarioEpisodeCoreInputs{
		RootID: "omnipaxos-discovered-risk", Knowledge: scenarioRisk.Knowledge,
		Hypothesis: scenarioRisk.Hypothesis, AcceptedHypothesis: &scenarioRisk.AcceptedHypothesis,
		RiskSpec: scenarioRisk.Spec, Root: inputs.Root,
		Runtime: inputs.Experiment.Runtime, FaultEnvelope: inputs.Experiment.faultEnvelope(),
		SemanticExposure: inputs.Experiment.ScenarioSemanticExposure,
		NewAdapter:       factory, RiskProjector: projector,
		SemanticProjector: func(trace controlruntime.Trace, frontier controlexperiment.RiskFrontierView,
			snapshot controlruntime.Snapshot) (controlexperiment.ScenarioSemanticExposure, error) {
			return projectOmnipaxosScenarioSemantics(
				inputs.Experiment.ScenarioSemanticExposure, trace, frontier, snapshot,
			)
		},
	}, 1, 3, inputs.Experiment.ScenarioMaxDecisions,
		func(_ context.Context, view controlexperiment.ScenarioAgentView) ([]byte, controlexperiment.ModelWork, error) {
			if view.Hypothesis.RiskID != candidate.ID || view.Knowledge.Digest != scenarioRisk.Knowledge.Digest ||
				view.AcceptedHypothesis == nil || view.AcceptedHypothesis.Candidate.ID != candidate.ID ||
				!reflect.DeepEqual(view.AvailableIntents, []string{controlexperiment.ScenarioIntentContinue}) {
				t.Fatalf("Scenario Agent did not retain validation and accepted contexts: %#v", view)
			}
			hasPrepare := false
			for _, action := range view.Frontier.Actions {
				hasPrepare = hasPrepare || action.Kind == control.ActionDeliverMessage &&
					action.MessageTypeHint == "sequence-paxos/prepare"
			}
			if !hasPrepare {
				t.Fatalf("Scenario Agent did not receive target-local leaf semantics: %#v", view.Frontier.Actions)
			}
			plan, err := json.Marshal(controlexperiment.ScenarioInvestigationProposal{
				Intent: controlexperiment.ScenarioIntentContinue,
				Plan: controlexperiment.ScenarioPlan{
					ID: "discovered-risk-scenario", Steps: []controlexperiment.ScenarioStep{
						{ID: "deliver-prepare", Selector: controlexperiment.FrontierActionSelector{
							Kind: control.ActionDeliverMessage, MessageSource: "n1", MessageTarget: "n2",
							MessageTypeHint: "sequence-paxos/prepare",
						}},
						{ID: "deliver-promise", Selector: controlexperiment.FrontierActionSelector{
							Kind: control.ActionDeliverMessage, MessageSource: "n2", MessageTarget: "n1",
							MessageTypeHint: "sequence-paxos/promise",
						}},
						{ID: "drop-operation-replication", Selector: controlexperiment.FrontierActionSelector{
							Kind: control.ActionDropMessage, MessageSource: "n1", MessageTarget: "n2",
							MessageTypeHint: "sequence-paxos/accept-sync",
						}},
					},
				},
			})
			return plan, controlexperiment.ModelWork{
				Calls: 1, InputTokens: 4, OutputTokens: 3, TotalTokens: 7,
			}, err
		},
	)
	if err != nil || scenario.Agent.Execution == nil ||
		scenario.Agent.Execution.FinalRisk.Status != semantic.RiskWitnessReached {
		t.Fatalf("discovered Risk did not reach through Scenario Runtime: %#v/%v", scenario.Agent, err)
	}
	methodSpecDigest := formalTestStringDigest("agentic-omnipaxos-method")
	testingResult, err := executeOmnipaxosScenarioQualifiedRisk(
		ctx, workerPath, inputs.Experiment, inputs.Workload, inputs.Qualification,
		inputs.Root, *scenario.Agent.Execution, scenarioRisk.Spec, projector, methodSpecDigest,
	)
	if err != nil || !testingResult.Replay.Stable || len(testingResult.Oracle.Violations) != 0 ||
		testingResult.Risk.RiskID != candidate.ID ||
		testingResult.Bundle.SchemaVersion != controlexperiment.ExecutionBundleSchemaVersionV3 ||
		testingResult.Bundle.Identity.MethodSpecDigest != methodSpecDigest ||
		testingResult.Bundle.Trace.Digest != scenario.Agent.Execution.FinalTrace.Digest {
		t.Fatalf("discovered Risk lost qualified evidence: %#v/%v", testingResult, err)
	}
}

func omnipaxosDiscoveredRiskCandidate() controlexperiment.RiskCandidate {
	return controlexperiment.RiskCandidate{
		ID:             "agent-message-loss-before-decision",
		PropertyRef:    "client-operation-continuity",
		InspirationRef: "message-loss-progress-coupling",
		Summary:        "Exercise an in-flight message loss and observe a later decision.",
		MechanismSteps: []controlexperiment.RiskMechanismStep{
			{MilestoneID: omnipaxosMilestoneWorkloadInvoked, Kind: semantic.ObservationWorkloadInvoked, Rationale: "Start the client operation.", SupportRefs: []string{"primer/ballot-and-log-progress"}},
			{MilestoneID: omnipaxosMilestoneMessageDropped, Kind: semantic.ObservationMessageDropped, Rationale: "Drop one in-flight replication message.", SupportRefs: []string{"primer/ballot-and-log-progress"}},
			{MilestoneID: omnipaxosMilestoneDecisionAfterDrop, Kind: semantic.ObservationDecisionAdvanced, Rationale: "Observe subsequent decision progress.", SupportRefs: []string{"primer/ballot-and-log-progress"}},
		},
		Predicates: []semantic.ObservationPredicate{
			{MilestoneID: omnipaxosMilestoneWorkloadInvoked, Kind: semantic.ObservationWorkloadInvoked,
				Constraints: []semantic.ObservationConstraint{
					{Field: semantic.ObservationFieldParticipantRole, Equals: "coordinator"},
					{Field: semantic.ObservationFieldRequestID, BindAs: "request"},
				}},
			{MilestoneID: omnipaxosMilestoneMessageDropped, Kind: semantic.ObservationMessageDropped,
				Constraints: []semantic.ObservationConstraint{
					{Field: semantic.ObservationFieldMessageRole, Equals: omnipaxosv2.ObservationMessageRoleOperationReplication},
					{Field: semantic.ObservationFieldOperationStage, Equals: "inflight"},
					{Field: semantic.ObservationFieldRequestID, BindAs: "request"},
				}},
			{MilestoneID: omnipaxosMilestoneDecisionAfterDrop, Kind: semantic.ObservationDecisionAdvanced,
				Constraints: []semantic.ObservationConstraint{{
					Field: semantic.ObservationFieldRequestID, BindAs: "request",
				}}},
		},
	}
}

func omnipaxosDiscoveredRiskPortfolio() controlexperiment.RiskCandidatePortfolio {
	primary := omnipaxosDiscoveredRiskCandidate()
	rejected := primary
	rejected.ID += "-single-binding"
	rejected.Summary += " Single-entity binding draft."
	rejected.Predicates = append([]semantic.ObservationPredicate(nil), primary.Predicates...)
	rejected.Predicates[0].Constraints = []semantic.ObservationConstraint{{
		Field: semantic.ObservationFieldParticipantRole, BindAs: "coordinator",
	}}
	return controlexperiment.RiskCandidatePortfolio{
		Candidates: []controlexperiment.RiskCandidate{rejected, primary},
	}
}
