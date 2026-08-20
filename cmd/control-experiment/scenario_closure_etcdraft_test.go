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

func TestEtcdraftClosureSupportDoesNotAdvertiseForUnrecognizedRisk(t *testing.T) {
	supported, err := semantic.NewRiskWitnessSpec(
		"supported-witness", "raft", "append-response-loss-with-alternate-quorum",
		[]string{"drop"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	unsupported, err := semantic.NewRiskWitnessSpec(
		"unsupported-witness", "raft", "agent-generated-message-loss",
		[]string{"drop"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !etcdraftScenarioClosureSupports(supported) || etcdraftScenarioClosureSupports(unsupported) {
		t.Fatal("closure support did not match the factory's accepted Risk identity")
	}
}

func TestEtcdraftExistingRiskScenarioOnlyEpisodeSkipsRiskProvider(t *testing.T) {
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
	originalScenarioInputs := target.ScenarioInputs
	target.ScenarioInputs = func(
		risk controlexperiment.ScenarioRiskHypothesis,
		projector controlexperiment.SemanticPrefixProjector,
	) (scenarioEpisodeCoreInputs, error) {
		core, inputErr := originalScenarioInputs(risk, projector)
		core.SingleStrategicAction = false // Preserve the historical one-call handoff calibration.
		return core, inputErr
	}
	existingRisk, riskDigest, err := loadExistingRiskInput(
		"../../plans/agent/etcdraft-alternate-quorum-risk-v1.json", target,
	)
	if err != nil || existingRisk == nil {
		t.Fatalf("load existing Risk: %#v/%v", existingRisk, err)
	}
	proposal, err := json.Marshal(controlexperiment.ScenarioInvestigationProposal{
		Intent: controlexperiment.ScenarioIntentContinue,
		Plan:   etcdraftOverSpecifiedAppendResponsePlan(),
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
			payload.ResponseFormat.JSONSchema.Name != scenarioInvestigationStructuredOutputName ||
			!bytes.Contains([]byte(payload.Messages[1].Content), []byte(`"post_intervention_closure": true`)) ||
			!bytes.Contains([]byte(payload.Messages[1].Content), []byte("End the plan at the intended fault intervention")) {
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
			MaxObservedTokens: 120_000, MaxScenarioPlanSteps: 6,
			MaxRuntimeDecisions: 17,
		}, nil, nil, existingRisk,
		func() error { return fmt.Errorf("Risk provider must not be activated") },
		func() error { return scenarioJournal.ActivateKey("fixture-key") },
	)
	if err != nil || providerCalls != 1 || len(result.RiskProviderCalls) != 0 ||
		result.RiskAgent.Accepted == nil || len(result.RiskAgent.Attempts) != 0 ||
		len(result.ScenarioProviderCalls) != 1 || result.Testing == nil ||
		result.Scenario == nil || result.Scenario.Agent.Execution == nil ||
		result.Scenario.Agent.DecisionsUsed != 17 ||
		!result.Scenario.Agent.Execution.ClosureHandoff ||
		result.Scenario.Agent.Execution.ClosureHandoffStepID != "drop-n2-append-response" ||
		len(result.Scenario.Agent.Execution.Steps) != 5 ||
		result.Scenario.Agent.Execution.FinalRisk.Status != semantic.RiskWitnessReached ||
		!result.Testing.Replay.Stable || result.Testing.Risk.Status != semantic.RiskWitnessReached ||
		len(result.Testing.Oracle.Violations) != 0 {
		t.Fatalf("existing-Risk Scenario-only episode did not close: %#v calls=%d err=%v",
			result, providerCalls, err)
	}
	artifactDirectory := t.TempDir()
	artifact, err := persistAgenticEpisodeArtifacts(
		artifactDirectory, target.ID, agenticEpisodeBudget{
			MaxRiskCalls: 1, MaxScenarioCalls: 1, MaxTotalCalls: 2,
			MaxObservedTokens: 120_000, MaxScenarioPlanSteps: 6,
			MaxRuntimeDecisions: 17,
		}, result,
	)
	if err != nil || artifact.RiskAttempts != 0 || artifact.ScenarioAttempts != 1 ||
		!artifact.ScenarioAttemptFeedback[0].ClosureHandoff ||
		artifact.ScenarioAttemptFeedback[0].ClosureHandoffStepID != "drop-n2-append-response" {
		t.Fatalf("existing-Risk artifact did not preserve Scenario-only accounting: %#v/%v",
			artifact, err)
	}
	reloaded, reloadedDigest, err := loadExistingRiskInput(
		filepath.Join(artifactDirectory, agenticEpisodeSummaryFile), target,
	)
	if err != nil || reloaded == nil || reloaded.Candidate.ID != existingRisk.Candidate.ID ||
		reloadedDigest != riskDigest {
		t.Fatalf("Agent-generated Risk summary was not reusable: %#v/%s/%v",
			reloaded, reloadedDigest, err)
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
	if public.StopReason != controlexperiment.ScenarioProgressSemanticYield ||
		len(public.Execution.Steps) != 3 {
		t.Fatalf("public fixed order did not yield on a changed strategic frontier: %#v", public)
	}
	publicExtended, err := controlexperiment.ExecuteScenarioNaturalProgress(
		ctx, "etcdraft-public-fixed-closure-extended", 32, spec, intervention.FinalRisk,
		intervention.FinalTrace, experiment.Runtime, experiment.faultEnvelope(), factory, projector,
	)
	if err != nil {
		t.Fatal(err)
	}
	if publicExtended.StopReason != controlexperiment.ScenarioProgressSemanticYield ||
		len(publicExtended.Execution.Steps) != 3 {
		t.Fatalf("public semantic-yield boundary changed with a larger budget: %#v", publicExtended)
	}

	selector, participants, err := newEtcdraftAlternateQuorumClosureSelector(
		intervention.FinalTrace, intervention.Steps[4].Choice.Action,
	)
	if err != nil {
		t.Fatal(err)
	}
	if participants.Leader != "n1" || participants.DroppedFollower != "n2" ||
		participants.Alternate != "n3" || participants.Term == "" || participants.ResponseIndex == 0 {
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

func TestM4l8EtcdraftSharedAgentPrefixBackendAblation(t *testing.T) {
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
	rootRisk, err := projector.Project("m4l8-root-risk", spec, root)
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(experiment.AdapterConfig)
	}

	// This is the exact semantic action prefix selected by both live M4l7
	// arms after their common 28-decision root. It deliberately excludes the
	// model-specific failed proposal attempts and every post-drop action.
	shared, err := controlexperiment.ExecuteSemanticBoundedScenarioPlan(
		ctx, "m4l8-shared-agent-prefix", controlexperiment.ScenarioPlan{
			ID: "m4l8-shared-agent-prefix-plan",
			Steps: []controlexperiment.ScenarioStep{
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
		}, 3, 3, spec, rootRisk, root, experiment.Runtime, experiment.faultEnvelope(), factory,
		projector, func(
			trace controlruntime.Trace,
			frontier controlexperiment.RiskFrontierView,
			snapshot controlruntime.Snapshot,
		) (controlexperiment.ScenarioSemanticExposure, error) {
			return projectEtcdraftScenarioSemantics(
				experiment.ScenarioSemanticExposure, trace, frontier, snapshot,
			)
		}, nil, 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Records) != 28 || len(shared.Steps) != 3 || len(shared.FinalTrace.Records) != 31 ||
		shared.Steps[2].Choice == nil ||
		shared.Steps[2].Choice.Action.Kind != control.ActionDropMessage ||
		shared.Steps[2].Choice.Action.MessageSource.Node != "n2" ||
		shared.Steps[2].Choice.Action.MessageTarget != "n1" ||
		shared.Steps[2].Choice.Action.MessageTypeHint != "MsgAppResp" ||
		shared.FinalRisk.Status != semantic.RiskWitnessNotReached ||
		!reflect.DeepEqual(shared.FinalRisk.MissingMilestones, []string{
			"alternate-quorum-decision-advanced",
		}) {
		t.Fatalf("M4l8 shared post-intervention prefix drifted: %#v", shared)
	}

	const equalBudget = 14
	publicEqual, err := controlexperiment.ExecuteScenarioNaturalProgress(
		ctx, "m4l8-public-equal", equalBudget, spec, shared.FinalRisk, shared.FinalTrace,
		experiment.Runtime, experiment.faultEnvelope(), factory, projector,
	)
	if err != nil {
		t.Fatal(err)
	}
	selector, participants, err := newEtcdraftAlternateQuorumClosureSelector(
		shared.FinalTrace, shared.Steps[2].Choice.Action,
	)
	if err != nil {
		t.Fatal(err)
	}
	targetEqual, err := controlexperiment.ExecuteScenarioNaturalProgressWithClosure(
		ctx, "m4l8-target-equal", equalBudget, spec, shared.FinalRisk, shared.FinalTrace,
		experiment.Runtime, experiment.faultEnvelope(), factory, projector, selector,
	)
	if err != nil {
		t.Fatal(err)
	}
	publicExtended, err := controlexperiment.ExecuteScenarioNaturalProgress(
		ctx, "m4l8-public-extended", 32, spec, shared.FinalRisk, shared.FinalTrace,
		experiment.Runtime, experiment.faultEnvelope(), factory, projector,
	)
	if err != nil {
		t.Fatal(err)
	}
	if targetEqual.StopReason != controlexperiment.ScenarioProgressClientTerminal ||
		len(targetEqual.Execution.Steps) != equalBudget ||
		targetEqual.Execution.FinalRisk.Status != semantic.RiskWitnessReached ||
		publicEqual.StopReason != controlexperiment.ScenarioProgressSemanticYield ||
		publicEqual.Execution.FinalRisk.Status != semantic.RiskWitnessNotReached ||
		publicExtended.StopReason != controlexperiment.ScenarioProgressSemanticYield ||
		publicExtended.Execution.FinalRisk.Status != semantic.RiskWitnessNotReached {
		t.Fatalf("M4l8 backend outcomes drifted: public=%#v target=%#v extended=%#v",
			publicEqual, targetEqual, publicExtended)
	}

	qualify := func(id string, progress controlexperiment.ScenarioProgressResult) scenarioTestingResult {
		combined := shared
		combined.PlanID = id
		combined.AutomaticProgress = append(
			append([]controlexperiment.ScenarioStepFeedback(nil), shared.AutomaticProgress...),
			progress.Execution.Steps...,
		)
		combined.NaturalProgressStop = progress.StopReason
		combined.FinalTrace = progress.Execution.FinalTrace
		combined.FinalRisk = progress.Execution.FinalRisk
		qualified, qualifyErr := executeEtcdraftScenarioQualifiedRisk(
			ctx, executionInputs, root, experiment, combined, spec, projector, "",
		)
		if qualifyErr != nil {
			t.Fatal(qualifyErr)
		}
		return qualified
	}
	publicEqualQualified := qualify("m4l8-public-equal-qualified", publicEqual)
	targetQualified := qualify("m4l8-target-qualified", targetEqual)
	publicExtendedQualified := qualify("m4l8-public-extended-qualified", publicExtended)
	wantOracleIDs := []string{
		"trace-integrity", "agreement", "etcdraft-client-application-binding",
		"etcdraft-log-progress",
	}
	for name, qualified := range map[string]scenarioTestingResult{
		"public-equal":    publicEqualQualified,
		"target-equal":    targetQualified,
		"public-extended": publicExtendedQualified,
	} {
		recomputed := etcdraftAgenticOracleRegistry().Check(qualified.Bundle)
		if !qualified.Replay.Stable || len(qualified.Oracle.Violations) != 0 ||
			!reflect.DeepEqual(qualified.Oracle, recomputed) ||
			!reflect.DeepEqual(qualified.Oracle.Checked, wantOracleIDs) {
			t.Fatalf("%s evaluator evidence drifted: %#v / %#v", name, qualified, recomputed)
		}
	}
	requestID := workload.Invocations[0].ID
	if len(targetQualified.Bundle.ClientHistory) != 1 ||
		targetQualified.Bundle.ClientHistory[0].Response.RequestID != requestID ||
		targetQualified.Bundle.ClientHistory[0].Response.Status != "committed" {
		t.Fatalf("M4l8 request identity drifted: target=%#v public=%#v",
			targetQualified.Bundle.ClientHistory, publicExtendedQualified.Bundle.ClientHistory)
	}

	summary := map[string]any{
		"shared_prefix_decisions": len(shared.Steps),
		"shared_trace_decisions":  len(shared.FinalTrace.Records),
		"shared_trace_digest":     shared.FinalTrace.Digest,
		"shared_actions":          closureChoiceSummaries(shared.Steps),
		"request_id":              requestID,
		"participants":            participants,
		"equal_budget":            equalBudget,
		"public_equal": map[string]any{
			"stop": publicEqual.StopReason, "decisions": len(publicEqual.Execution.Steps),
			"risk":              publicEqual.Execution.FinalRisk.Status,
			"work":              publicEqual.Execution.Work.TotalWorkUnits,
			"qualified_primary": publicEqualQualified.Bundle.Work.Primary.WorkUnits,
			"qualified_replay":  publicEqualQualified.Bundle.Work.Replay.WorkUnits,
			"trace_digest":      publicEqualQualified.Bundle.Trace.Digest,
			"bundle_digest":     publicEqualQualified.Bundle.Digest,
			"replay_stable":     publicEqualQualified.Replay.Stable,
		},
		"target_equal": map[string]any{
			"stop": targetEqual.StopReason, "decisions": len(targetEqual.Execution.Steps),
			"risk":              targetEqual.Execution.FinalRisk.Status,
			"work":              targetEqual.Execution.Work.TotalWorkUnits,
			"qualified_primary": targetQualified.Bundle.Work.Primary.WorkUnits,
			"qualified_replay":  targetQualified.Bundle.Work.Replay.WorkUnits,
			"trace_digest":      targetQualified.Bundle.Trace.Digest,
			"bundle_digest":     targetQualified.Bundle.Digest,
			"replay_stable":     targetQualified.Replay.Stable,
			"client_step":       targetQualified.Bundle.ClientHistory[0].Step,
		},
		"public_extended": map[string]any{
			"stop": publicExtended.StopReason, "decisions": len(publicExtended.Execution.Steps),
			"risk":              publicExtended.Execution.FinalRisk.Status,
			"work":              publicExtended.Execution.Work.TotalWorkUnits,
			"qualified_primary": publicExtendedQualified.Bundle.Work.Primary.WorkUnits,
			"qualified_replay":  publicExtendedQualified.Bundle.Work.Replay.WorkUnits,
			"trace_digest":      publicExtendedQualified.Bundle.Trace.Digest,
			"bundle_digest":     publicExtendedQualified.Bundle.Digest,
			"replay_stable":     publicExtendedQualified.Replay.Stable,
		},
		"oracle_checked": wantOracleIDs,
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("M4L8_BACKEND_ABLATION %s", encoded)
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
	if core.ClosureFactory != nil || core.ClosureSupport != nil ||
		target.ClosureFactory == nil || target.ClosureSupport == nil || !target.ClosureSupport(risk.Spec) ||
		!core.SingleStrategicAction {
		t.Fatal("closure factory was not owned exclusively by Target composition")
	}
	core.SingleStrategicAction = false // This legacy multi-step fixture tests handoff truncation.
	core.ClosureFactory = target.ClosureFactory
	core.ClosureSupport = target.ClosureSupport
	core.RootID = "etcdraft-agent-closure"
	core.TargetSurface = &target.Surface
	plannerCalls := 0
	scenario, err := runScenarioEpisodeCore(
		ctx, core, 3, 6, 17,
		func(_ context.Context, view controlexperiment.ScenarioAgentView) (
			[]byte, controlexperiment.ModelWork, error,
		) {
			plannerCalls++
			if plannerCalls > 1 {
				t.Fatal("closure returned control to the Agent before exhausting the global budget")
			}
			if view.RemainingDecisions != 17 || view.DecisionAllowance != 10 ||
				view.MaxSteps != 6 || !view.PostInterventionClosure {
				t.Fatalf("Scenario Agent budget view drifted: %#v", view)
			}
			encoded, marshalErr := json.Marshal(controlexperiment.ScenarioInvestigationProposal{
				Intent: controlexperiment.ScenarioIntentContinue,
				Plan:   etcdraftOverSpecifiedAppendResponsePlan(),
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
		scenario.Agent.StopReason != controlexperiment.ScenarioAgentStopWitnessInstantiated ||
		scenario.Agent.DecisionsUsed != 17 || scenario.Agent.Execution == nil ||
		len(scenario.Agent.Execution.Steps) != 5 ||
		!scenario.Agent.Execution.ClosureHandoff ||
		scenario.Agent.Execution.ClosureHandoffStepID != "drop-n2-append-response" ||
		len(scenario.Agent.Execution.AutomaticProgress) != 12 ||
		scenario.Agent.Execution.NaturalProgressStop != controlexperiment.ScenarioProgressClientTerminal ||
		scenario.Agent.Execution.FinalRisk.Status != semantic.RiskWitnessReached {
		t.Fatalf("Scenario Agent did not promote the exact 5+12 closure: %#v", scenario.Agent)
	}
	for _, record := range scenario.Agent.Execution.FinalTrace.Records {
		if record.Action.Kind == control.ActionCrash {
			t.Fatalf("closure lifecycle admitted an unnecessary later crash: %#v", record.Action)
		}
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

func TestEtcdraftFiveNodeScenarioAgentSelectsQuorumAndReplays(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(), controlExperimentTestTimeout(180*time.Second),
	)
	defer cancel()
	inputs, err := prepareEtcdraftAgenticEpisode(
		ctx, "", "../../plans/agent/etcdraft-agentic-five-node-v1.json",
		fixtureOpenRouterIntentClient(),
	)
	if err != nil {
		t.Fatal(err)
	}
	target, err := newEtcdraftAgenticEpisodeTarget(inputs)
	if err != nil || len(target.Surface.Nodes) != 5 {
		t.Fatalf("prepare five-node target: nodes=%v err=%v", target.Surface.Nodes, err)
	}
	candidate := etcdraftAlternateQuorumRiskCandidate()
	assessment, err := controlexperiment.AssessRiskCandidateForTarget(
		target.Knowledge, candidate, target.ObservationProjector.Capabilities(),
		target.Surface.Capabilities.ComposableActions, &target.Surface,
	)
	if err != nil || !assessment.Qualification.Qualified {
		t.Fatalf("five-node Risk qualification: %#v/%v", assessment, err)
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
	core.ClosureFactory = target.ClosureFactory
	core.ClosureSupport = target.ClosureSupport
	core.SingleStrategicAction = false // Preserve the scripted multi-step quorum fixture.
	core.RootID = "etcdraft-five-node-agent-closure"
	core.TargetSurface = &target.Surface
	plannerCalls := 0
	selectedTargets := make(map[control.NodeID]struct{})
	scenario, err := runScenarioEpisodeCore(
		ctx, core, 4, 5, 64,
		func(_ context.Context, view controlexperiment.ScenarioAgentView) (
			[]byte, controlexperiment.ModelWork, error,
		) {
			plannerCalls++
			proposal := controlexperiment.ScenarioInvestigationProposal{
				Intent: controlexperiment.ScenarioIntentContinue,
				Plan:   etcdraftAppendResponseInterventionPlan(),
			}
			if plannerCalls > 1 {
				if view.Prior == nil ||
					view.Prior.NaturalProgressStop != controlexperiment.ScenarioProgressClosureUnderdetermined ||
					len(view.Prior.ClosureCandidates) < 2 {
					t.Fatalf("five-node Agent did not receive narrow closure candidates: %#v", view.Prior)
				}
				var selected controlexperiment.FrontierActionRef
				for _, action := range view.Prior.ClosureCandidates {
					candidate := action.MessageTarget
					if candidate == "" {
						candidate = action.MessageSource.Node
					}
					if _, used := selectedTargets[candidate]; !used {
						selected = action
						selectedTargets[candidate] = struct{}{}
						break
					}
				}
				if selected.ActionID == "" {
					t.Fatalf("no new quorum path in %#v", view.Prior.ClosureCandidates)
				}
				proposal.Intent = controlexperiment.ScenarioIntentRevise
				proposal.Plan = controlexperiment.ScenarioPlan{
					ID: fmt.Sprintf("choose-five-node-quorum-%d", plannerCalls-1),
					Steps: []controlexperiment.ScenarioStep{{
						ID:       fmt.Sprintf("choose-quorum-action-%d", plannerCalls-1),
						Selector: controlexperiment.FrontierActionSelector{ActionID: selected.ActionID},
					}},
				}
			}
			encoded, marshalErr := json.Marshal(proposal)
			return encoded, controlexperiment.ModelWork{
				Calls: 1, InputTokens: 3, OutputTokens: 2, TotalTokens: 5,
			}, marshalErr
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if plannerCalls != 3 || len(selectedTargets) != 2 || scenario.Agent.Execution == nil ||
		scenario.Agent.StopReason != controlexperiment.ScenarioAgentStopWitnessInstantiated ||
		scenario.Agent.Execution.FinalRisk.Status != semantic.RiskWitnessReached {
		t.Fatalf("five-node quorum closure did not complete: calls=%d selected=%v result=%#v",
			plannerCalls, selectedTargets, scenario.Agent)
	}
	qualified, err := target.Execute(ctx, risk, projector, *scenario.Agent.Execution, "")
	if err != nil {
		t.Fatal(err)
	}
	if !qualified.Replay.Stable || len(qualified.Oracle.Violations) != 0 ||
		len(qualified.Bundle.ClientHistory) != 1 ||
		qualified.Bundle.ClientHistory[0].Response.RequestID != inputs.execution.workload.Invocations[0].ID ||
		qualified.Bundle.ClientHistory[0].Response.Status != "committed" {
		t.Fatalf("five-node closure evidence did not replay: %#v", qualified)
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

func etcdraftOverSpecifiedAppendResponsePlan() controlexperiment.ScenarioPlan {
	plan := etcdraftAppendResponseInterventionPlan()
	plan.ID = "etcdraft-drop-n2-append-response-over-specified-plan"
	plan.Steps = append(plan.Steps, controlexperiment.ScenarioStep{
		ID: "predict-n3-append-response", Selector: controlexperiment.FrontierActionSelector{
			Kind: control.ActionDeliverMessage, MessageSource: "n3", MessageTarget: "n1",
			MessageTypeHint: "MsgAppResp",
		},
	})
	return plan
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

func TestEtcdraftFiveNodeClosureRequiresExecutedQuorumChoices(t *testing.T) {
	participants := etcdraftAlternateQuorumSet{
		Leader: "n1", DroppedFollower: "n2",
		Candidates: []control.NodeID{"n3", "n4", "n5"}, Required: 2,
	}
	actions := []controlexperiment.FrontierActionRef{
		{ActionID: "to-n3", Kind: control.ActionDeliverMessage,
			MessageSource: control.NodeRef{Node: "n1"}, MessageTarget: "n3", MessageTypeHint: "MsgApp"},
		{ActionID: "to-n4", Kind: control.ActionDeliverMessage,
			MessageSource: control.NodeRef{Node: "n1"}, MessageTarget: "n4", MessageTypeHint: "MsgApp"},
		{ActionID: "to-n5", Kind: control.ActionDeliverMessage,
			MessageSource: control.NodeRef{Node: "n1"}, MessageTarget: "n5", MessageTypeHint: "MsgApp"},
	}
	selection, err := etcdraftAlternateQuorumSetSelector(participants)(
		controlexperiment.ActionFrontierView{Actions: actions},
	)
	if err != nil || selection.Status != controlexperiment.ScenarioClosureUnderdetermined ||
		len(selection.Candidates) != 3 {
		t.Fatalf("five-node closure hid quorum ambiguity: %#v/%v", selection, err)
	}

	selected, err := selectedEtcdraftClosureAlternates([]controlexperiment.FrontierChoice{
		{Decision: 11, Action: actions[1]},
	}, 10, participants)
	if err != nil || !reflect.DeepEqual(selected, []control.NodeID{"n4"}) {
		t.Fatalf("first executed quorum choice was not bound: %v/%v", selected, err)
	}
	participants.Selected = selected
	selection, err = etcdraftAlternateQuorumSetSelector(participants)(
		controlexperiment.ActionFrontierView{Actions: []controlexperiment.FrontierActionRef{
			actions[0], actions[2],
		}},
	)
	if err != nil || selection.Status != controlexperiment.ScenarioClosureUnderdetermined ||
		len(selection.Candidates) != 2 {
		t.Fatalf("remaining quorum alternatives were not exposed: %#v/%v", selection, err)
	}
	selected, err = selectedEtcdraftClosureAlternates([]controlexperiment.FrontierChoice{
		{Decision: 11, Action: actions[1]}, {Decision: 12, Action: actions[2]},
	}, 10, participants)
	if err != nil || !reflect.DeepEqual(selected, []control.NodeID{"n4", "n5"}) {
		t.Fatalf("executed quorum set was not bound: %v/%v", selected, err)
	}
}

func TestEtcdraftClosureRejectsAnotherTermOrOldIndex(t *testing.T) {
	participants := etcdraftAlternateQuorumSet{Term: "7", ResponseIndex: 9}
	base := controlexperiment.FrontierActionRef{
		Kind: control.ActionDeliverMessage, MessageTypeHint: "MsgAppResp",
		MessageMetadata: map[string]string{"term": "7", "index": "9"},
	}
	if !etcdraftClosureMessageCausallyMatches(base, participants) {
		t.Fatal("matching term/index was rejected")
	}
	wrongTerm := base
	wrongTerm.MessageMetadata = map[string]string{"term": "8", "index": "9"}
	if etcdraftClosureMessageCausallyMatches(wrongTerm, participants) {
		t.Fatal("another term entered the active closure")
	}
	oldIndex := base
	oldIndex.MessageMetadata = map[string]string{"term": "7", "index": "7"}
	if etcdraftClosureMessageCausallyMatches(oldIndex, participants) {
		t.Fatal("an old same-term response entered the request-adjacent closure")
	}
}
