package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestEtcdraftFixedRiskScenarioOnlyEpisodeSkipsRiskProvider(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(), controlExperimentTestTimeout(180*time.Second),
	)
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
	fixedRisk, _, err := loadFixedRiskInput(
		"../../plans/agent/etcdraft-alternate-quorum-fixed-risk-v1.json", target,
	)
	if err != nil || fixedRisk == nil {
		t.Fatalf("load fixed Risk: %#v/%v", fixedRisk, err)
	}
	proposal, err := json.Marshal(controlexperiment.ScenarioInvestigationProposal{
		Intent: controlexperiment.ScenarioIntentContinue,
		Plan:   etcdraftAppendResponseInterventionPlan(),
	})
	if err != nil {
		t.Fatal(err)
	}
	providerCalls := 0
	client := fixtureOpenRouterIntentClient()
	client.HTTP = agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
		providerCalls++
		var payload openRouterChatRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil ||
			payload.ResponseFormat.JSONSchema.Name != scenarioInvestigationStructuredOutputName {
			t.Fatalf("unexpected provider call: %#v/%v", payload, err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(bytes.NewReader(fixtureOpenRouterResponse(
				t, providerCalls, proposal,
			))),
		}, nil
	})
	directory := t.TempDir()
	riskJournal, err := newStatelessAgentCallJournal(
		filepath.Join(directory, "risk"), client, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	scenarioJournal, err := newScenarioAgentCallJournal(
		filepath.Join(directory, "scenario"), client, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runAgenticEpisode(
		ctx, target, riskJournal, scenarioJournal, agenticEpisodeBudget{
			MaxRiskCalls: 1, MaxScenarioCalls: 1, MaxTotalCalls: 2,
			MaxObservedTokens: 120_000, MaxScenarioPlanSteps: 5,
			MaxRuntimeDecisions: 17,
		}, nil, nil, fixedRisk,
		func() error { return fmt.Errorf("Risk provider must not be activated") },
		func() error { return scenarioJournal.ActivateKey("fixture-key") },
	)
	if err != nil || providerCalls != 1 || len(result.RiskProviderCalls) != 0 ||
		result.RiskAgent.Accepted == nil || len(result.RiskAgent.Attempts) != 0 ||
		len(result.ScenarioProviderCalls) != 1 || result.Testing == nil ||
		result.Scenario == nil || result.Scenario.Agent.Execution == nil ||
		result.Scenario.Agent.DecisionsUsed != 17 ||
		result.Scenario.Agent.Execution.FinalRisk.Status != semantic.RiskWitnessReached ||
		!result.Testing.Replay.Stable || result.Testing.Risk.Status != semantic.RiskWitnessReached ||
		len(result.Testing.Oracle.Violations) != 0 {
		t.Fatalf("fixed-Risk Scenario-only episode did not close: %#v calls=%d err=%v",
			result, providerCalls, err)
	}
	artifact, err := persistAgenticEpisodeArtifacts(
		t.TempDir(), target.ID, agenticEpisodeBudget{
			MaxRiskCalls: 1, MaxScenarioCalls: 1, MaxTotalCalls: 2,
			MaxObservedTokens: 120_000, MaxScenarioPlanSteps: 5,
			MaxRuntimeDecisions: 17,
		}, result,
	)
	if err != nil || artifact.RiskAttempts != 0 || artifact.ScenarioAttempts != 1 {
		t.Fatalf("fixed-Risk artifact did not preserve Scenario-only accounting: %#v/%v",
			artifact, err)
	}
}

func TestEtcdraftAlternateQuorumClosureAfterDroppedAppendResponse(t *testing.T) {
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
	spec, predicates, err := etcdraftAlternateQuorumRisk()
	if err != nil {
		t.Fatal(err)
	}
	projector, err := controlexperiment.NewLinearObservationRiskProjector(
		spec, predicates, etcdraftv2.ObservationProjector{},
	)
	if err != nil {
		t.Fatal(err)
	}
	rootRisk, err := projector.Project("etcdraft-closure-root-risk", spec, root)
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(experiment.AdapterConfig)
	}
	intervention, err := controlexperiment.ExecuteSemanticBoundedScenarioPlan(
		ctx,
		"etcdraft-drop-n2-append-response",
		etcdraftAppendResponseInterventionPlan(),
		5,
		5,
		spec,
		rootRisk,
		root,
		experiment.Runtime,
		experiment.faultEnvelope(),
		factory,
		projector,
		func(
			trace controlruntime.Trace,
			frontier controlexperiment.RiskFrontierView,
			snapshot controlruntime.Snapshot,
		) (controlexperiment.ScenarioSemanticExposure, error) {
			return projectEtcdraftScenarioSemantics(
				experiment.ScenarioSemanticExposure, trace, frontier, snapshot,
			)
		},
		nil,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if intervention.Status != controlexperiment.ScenarioStatusCompleted ||
		len(intervention.Steps) != 5 ||
		intervention.Steps[4].Choice == nil ||
		intervention.Steps[4].Choice.Action.Kind != control.ActionDropMessage ||
		intervention.Steps[4].Choice.Action.MessageTypeHint != "MsgAppResp" ||
		intervention.Steps[4].Choice.Action.MessageSource.Node != "n2" ||
		intervention.Steps[4].Choice.Action.MessageTarget != "n1" {
		t.Fatalf("intervention did not reach the exact target drop: %#v", intervention.Steps)
	}
	if intervention.FinalRisk.Status != semantic.RiskWitnessNotReached ||
		!reflect.DeepEqual(intervention.FinalRisk.SatisfiedMilestones, []string{
			"workload-invoked-at-coordinator",
			"append-response-dropped-while-replication-inflight",
		}) || !reflect.DeepEqual(intervention.FinalRisk.MissingMilestones, []string{
		"alternate-quorum-decision-advanced",
	}) {
		t.Fatalf("intervention risk prefix is not the accepted alternate-quorum hypothesis: %#v",
			intervention.FinalRisk)
	}

	public, err := controlexperiment.ExecuteScenarioNaturalProgress(
		ctx, "etcdraft-public-fixed-closure", 11, spec, intervention.FinalRisk,
		intervention.FinalTrace, experiment.Runtime, experiment.faultEnvelope(), factory, projector,
	)
	if err != nil {
		t.Fatal(err)
	}
	if public.StopReason != controlexperiment.ScenarioProgressBudget {
		t.Fatalf("public fixed order unexpectedly closed: %#v", public)
	}
	publicExtended, err := controlexperiment.ExecuteScenarioNaturalProgress(
		ctx, "etcdraft-public-fixed-closure-extended", 32, spec, intervention.FinalRisk,
		intervention.FinalTrace, experiment.Runtime, experiment.faultEnvelope(), factory, projector,
	)
	if err != nil {
		t.Fatal(err)
	}
	if publicExtended.StopReason != controlexperiment.ScenarioProgressClientTerminal ||
		len(publicExtended.Execution.Steps) != 17 {
		t.Fatalf("extended public fixed order changed: %#v", publicExtended)
	}

	selector, participants, err := newEtcdraftAlternateQuorumClosureSelector(
		intervention.FinalTrace, intervention.Steps[4].Choice.Action,
	)
	if err != nil {
		t.Fatal(err)
	}
	if participants != (etcdraftAlternateQuorumParticipants{
		Leader: "n1", DroppedFollower: "n2", Alternate: "n3",
	}) {
		t.Fatalf("closure participants were not derived from the intervention: %#v", participants)
	}
	closed, err := controlexperiment.ExecuteScenarioNaturalProgressWithClosure(
		ctx, "etcdraft-n3-alternate-quorum-closure", 12, spec, intervention.FinalRisk,
		intervention.FinalTrace, experiment.Runtime, experiment.faultEnvelope(), factory,
		projector, selector,
	)
	if err != nil {
		t.Fatal(err)
	}
	requestID := workload.Invocations[0].ID
	if closed.StopReason != controlexperiment.ScenarioProgressClientTerminal {
		t.Fatalf("target closure did not return request %s: %#v", requestID, closed)
	}
	if len(closed.Execution.Steps) != 12 {
		t.Fatalf("target closure decisions = %d, want 12", len(closed.Execution.Steps))
	}
	if closed.Execution.FinalRisk.Status != semantic.RiskWitnessReached ||
		len(closed.Execution.FinalRisk.MissingMilestones) != 0 {
		t.Fatalf("alternate-quorum risk did not close: %#v", closed.Execution.FinalRisk)
	}

	combined := intervention
	combined.AutomaticProgress = append(
		append([]controlexperiment.ScenarioStepFeedback(nil), intervention.AutomaticProgress...),
		closed.Execution.Steps...,
	)
	combined.NaturalProgressStop = closed.StopReason
	combined.FinalTrace = closed.Execution.FinalTrace
	combined.FinalRisk = closed.Execution.FinalRisk
	qualified, err := executeEtcdraftScenarioQualifiedRisk(
		ctx, executionInputs, root, experiment, combined, spec, projector, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !qualified.Replay.Stable || len(qualified.Oracle.Violations) != 0 ||
		qualified.Bundle.OperationHistory != nil || qualified.Bundle.Identity.MethodSpecDigest != "" ||
		len(qualified.Bundle.ClientHistory) != 1 {
		t.Fatalf("qualified closure evidence invalid: %#v", qualified)
	}
	wantOracleIDs := []string{
		"trace-integrity", "agreement", "etcdraft-client-application-binding",
		"etcdraft-log-progress",
	}
	if !reflect.DeepEqual(qualified.Oracle.Checked, wantOracleIDs) {
		t.Fatalf("qualified closure Oracle set = %v, want %v", qualified.Oracle.Checked, wantOracleIDs)
	}
	history, err := (etcdraftv2.ObservationProjector{}).Project(qualified.Bundle.Trace)
	if err != nil {
		t.Fatal(err)
	}
	invokeStep := 0
	for _, observation := range history.Events {
		if observation.Kind == semantic.ObservationWorkloadInvoked &&
			observation.RequestID == requestID {
			invokeStep = int(observation.Step)
			break
		}
	}
	returned := qualified.Bundle.ClientHistory[0]
	if invokeStep != 28 || returned.Step != 45 || returned.Response.RequestID != requestID ||
		returned.Response.Status != "committed" {
		t.Fatalf("request identity did not survive closure: invoke=%d return=%#v", invokeStep, returned)
	}

	summary := etcdraftClosureTestSummary{
		RequestID:            requestID,
		InterventionDecision: intervention.Steps[4].Decision,
		PublicStop:           public.StopReason,
		PublicDecisions:      len(public.Execution.Steps),
		PublicChoices:        closureChoiceSummaries(public.Execution.Steps),
		PublicExtendedStop:   publicExtended.StopReason,
		PublicExtendedSteps:  len(publicExtended.Execution.Steps),
		TargetStop:           closed.StopReason,
		TargetDecisions:      len(closed.Execution.Steps),
		TargetChoices:        closureChoiceSummaries(closed.Execution.Steps),
		RiskStatus:           qualified.Risk.Status,
		Leader:               participants.Leader,
		DroppedFollower:      participants.DroppedFollower,
		Alternate:            participants.Alternate,
		ReplayStable:         qualified.Replay.Stable,
		OracleChecked:        append([]string(nil), qualified.Oracle.Checked...),
		OracleViolations:     len(qualified.Oracle.Violations),
		ReturnDecision:       returned.Step,
		AmbiguousFrontiers:   0,
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ETCDRAFT_CLOSURE_RESULT %s", encoded)
}

func TestEtcdraftAlternateQuorumClosureRunsThroughScenarioAgentEpisode(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(), controlExperimentTestTimeout(180*time.Second),
	)
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
	candidate := etcdraftAlternateQuorumRiskCandidate()
	assessment, err := controlexperiment.AssessRiskCandidateForTarget(
		target.Knowledge, candidate, target.ObservationProjector.Capabilities(),
		target.Surface.Capabilities.ComposableActions, &target.Surface,
	)
	if err != nil || !assessment.Qualification.Qualified {
		t.Fatalf("alternate-quorum Risk did not qualify: %#v/%v", assessment, err)
	}
	risk, err := controlexperiment.BuildScenarioRiskHypothesis(
		target.Knowledge, assessment, target.ObservationProjector.Capabilities(),
		target.Surface.Capabilities.ComposableActions, &target.Surface,
	)
	if err != nil {
		t.Fatal(err)
	}
	projector, err := controlexperiment.NewLinearObservationRiskProjector(
		risk.Spec, risk.Predicates, target.ObservationProjector,
	)
	if err != nil {
		t.Fatal(err)
	}
	core, err := target.ScenarioInputs(risk, projector)
	if err != nil {
		t.Fatal(err)
	}
	if core.ClosureFactory != nil || target.ClosureFactory == nil {
		t.Fatal("closure factory was not owned exclusively by Target composition")
	}
	core.ClosureFactory = target.ClosureFactory
	core.RootID = "etcdraft-agent-closure"
	core.TargetSurface = &target.Surface
	plannerCalls := 0
	scenario, err := runScenarioEpisodeCore(
		ctx, core, 1, 5, 17,
		func(_ context.Context, view controlexperiment.ScenarioAgentView) (
			[]byte, controlexperiment.ModelWork, error,
		) {
			plannerCalls++
			if view.RemainingDecisions != 17 || view.DecisionAllowance != 17 ||
				view.MaxSteps != 5 {
				t.Fatalf("Scenario Agent budget view drifted: %#v", view)
			}
			encoded, marshalErr := json.Marshal(controlexperiment.ScenarioInvestigationProposal{
				Intent: controlexperiment.ScenarioIntentContinue,
				Plan:   etcdraftAppendResponseInterventionPlan(),
			})
			return encoded, controlexperiment.ModelWork{
				Calls: 1, InputTokens: 3, OutputTokens: 2, TotalTokens: 5,
			}, marshalErr
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if plannerCalls != 1 || scenario.Agent.Status != controlexperiment.ScenarioAgentCompleted ||
		scenario.Agent.StopReason != controlexperiment.ScenarioAgentStopRiskReached ||
		scenario.Agent.DecisionsUsed != 17 || scenario.Agent.Execution == nil ||
		len(scenario.Agent.Execution.Steps) != 5 ||
		len(scenario.Agent.Execution.AutomaticProgress) != 12 ||
		scenario.Agent.Execution.NaturalProgressStop != controlexperiment.ScenarioProgressClientTerminal ||
		scenario.Agent.Execution.FinalRisk.Status != semantic.RiskWitnessReached {
		t.Fatalf("Scenario Agent did not promote the exact 5+12 closure: %#v", scenario.Agent)
	}
	qualified, err := target.Execute(ctx, risk, projector, *scenario.Agent.Execution, "")
	if err != nil {
		t.Fatal(err)
	}
	requestID := inputs.execution.workload.Invocations[0].ID
	wantOracleIDs := []string{
		"trace-integrity", "agreement", "etcdraft-client-application-binding",
		"etcdraft-log-progress",
	}
	if !qualified.Replay.Stable || qualified.Risk.Status != semantic.RiskWitnessReached ||
		len(qualified.Oracle.Violations) != 0 ||
		!reflect.DeepEqual(qualified.Oracle.Checked, wantOracleIDs) ||
		len(qualified.Bundle.ClientHistory) != 1 ||
		qualified.Bundle.ClientHistory[0].Response.RequestID != requestID ||
		qualified.Bundle.ClientHistory[0].Response.Status != "committed" {
		t.Fatalf("Agent closure lost Replay, Oracle or request identity: %#v", qualified)
	}
}

func etcdraftAlternateQuorumRisk() (
	semantic.RiskWitnessSpec,
	[]semantic.ObservationPredicate,
	error,
) {
	const (
		invoked  = "workload-invoked-at-coordinator"
		dropped  = "append-response-dropped-while-replication-inflight"
		advanced = "alternate-quorum-decision-advanced"
	)
	spec, err := semantic.NewRiskWitnessSpec(
		"etcdraft-append-response-loss-alternate-quorum-v1", "raft",
		"append-response-loss-with-alternate-quorum",
		[]string{invoked, dropped, advanced},
		[]semantic.RiskWitnessOrder{
			{Before: invoked, After: dropped},
			{Before: dropped, After: advanced},
		},
	)
	if err != nil {
		return semantic.RiskWitnessSpec{}, nil, err
	}
	return spec, []semantic.ObservationPredicate{
		{
			MilestoneID: invoked, Kind: semantic.ObservationWorkloadInvoked,
			Constraints: []semantic.ObservationConstraint{{
				Field: semantic.ObservationFieldParticipantRole, Equals: "coordinator",
			}},
		},
		{
			MilestoneID: dropped, Kind: semantic.ObservationMessageDropped,
			Constraints: []semantic.ObservationConstraint{{
				Field: semantic.ObservationFieldMessageRole, Equals: "MsgAppResp",
			}},
		},
		{MilestoneID: advanced, Kind: semantic.ObservationDecisionAdvanced},
	}, nil
}

func etcdraftAlternateQuorumRiskCandidate() controlexperiment.RiskCandidate {
	_, predicates, err := etcdraftAlternateQuorumRisk()
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

func etcdraftAppendResponseInterventionPlan() controlexperiment.ScenarioPlan {
	return controlexperiment.ScenarioPlan{
		ID: "etcdraft-drop-n2-append-response-plan",
		Steps: []controlexperiment.ScenarioStep{
			{ID: "persist-leader-proposal", Selector: controlexperiment.FrontierActionSelector{
				Kind: control.ActionCompleteEffect, Owner: "n1", EffectKind: "raft-ready-persist",
			}},
			{ID: "advance-leader-proposal", Selector: controlexperiment.FrontierActionSelector{
				Kind: control.ActionCompleteEffect, Owner: "n1", EffectKind: "raft-ready-advance",
			}},
			{ID: "deliver-proposal-to-n2", Selector: controlexperiment.FrontierActionSelector{
				Kind: control.ActionDeliverMessage, MessageSource: "n1", MessageTarget: "n2",
				MessageTypeHint: "MsgApp",
			}},
			{ID: "persist-n2-proposal", Selector: controlexperiment.FrontierActionSelector{
				Kind: control.ActionCompleteEffect, Owner: "n2", EffectKind: "raft-ready-persist",
			}},
			{ID: "drop-n2-append-response", Selector: controlexperiment.FrontierActionSelector{
				Kind: control.ActionDropMessage, MessageSource: "n2", MessageTarget: "n1",
				MessageTypeHint: "MsgAppResp",
			}},
		},
	}
}

type etcdraftClosureTestSummary struct {
	RequestID            string                 `json:"request_id"`
	InterventionDecision int                    `json:"intervention_decision"`
	PublicStop           string                 `json:"public_stop"`
	PublicDecisions      int                    `json:"public_decisions"`
	PublicChoices        []closureChoiceSummary `json:"public_choices"`
	PublicExtendedStop   string                 `json:"public_extended_stop"`
	PublicExtendedSteps  int                    `json:"public_extended_steps"`
	TargetStop           string                 `json:"target_stop"`
	TargetDecisions      int                    `json:"target_decisions"`
	TargetChoices        []closureChoiceSummary `json:"target_choices"`
	RiskStatus           string                 `json:"risk_status"`
	Leader               control.NodeID         `json:"leader"`
	DroppedFollower      control.NodeID         `json:"dropped_follower"`
	Alternate            control.NodeID         `json:"alternate"`
	ReplayStable         bool                   `json:"replay_stable"`
	OracleChecked        []string               `json:"oracle_checked"`
	OracleViolations     int                    `json:"oracle_violations"`
	ReturnDecision       int                    `json:"return_decision"`
	AmbiguousFrontiers   int                    `json:"ambiguous_frontiers"`
}

type closureChoiceSummary struct {
	Decision    int                `json:"decision"`
	Kind        control.ActionKind `json:"kind"`
	Owner       control.NodeID     `json:"owner,omitempty"`
	EffectKind  string             `json:"effect_kind,omitempty"`
	MessageType string             `json:"message_type,omitempty"`
	Source      control.NodeID     `json:"source,omitempty"`
	Target      control.NodeID     `json:"target,omitempty"`
}

func closureChoiceSummaries(steps []controlexperiment.ScenarioStepFeedback) []closureChoiceSummary {
	result := make([]closureChoiceSummary, 0, len(steps))
	for _, step := range steps {
		if step.Choice == nil {
			continue
		}
		action := step.Choice.Action
		result = append(result, closureChoiceSummary{
			Decision: step.Decision, Kind: action.Kind, Owner: action.Owner.Node,
			EffectKind: action.EffectKind, MessageType: action.MessageTypeHint,
			Source: action.MessageSource.Node, Target: action.MessageTarget,
		})
	}
	return result
}

func TestEtcdraftAlternateQuorumClosureReportsAmbiguousFrontier(t *testing.T) {
	selector := etcdraftAlternateQuorumSelector(etcdraftAlternateQuorumParticipants{
		Leader: "n1", DroppedFollower: "n2", Alternate: "n3",
	})
	selection, err := selector(controlexperiment.ActionFrontierView{Actions: []controlexperiment.FrontierActionRef{
		{ActionID: "effect-a", Kind: control.ActionCompleteEffect, Owner: control.NodeRef{Node: "n3", Incarnation: 1}, EffectKind: "raft-ready-persist"},
		{ActionID: "effect-b", Kind: control.ActionCompleteEffect, Owner: control.NodeRef{Node: "n3", Incarnation: 1}, EffectKind: "raft-ready-advance"},
	}})
	if err != nil || selection.Status != controlexperiment.ScenarioClosureUnderdetermined ||
		selection.Action.ActionID != "" {
		t.Fatalf("ambiguous closure frontier = %#v/%v", selection, err)
	}
}
