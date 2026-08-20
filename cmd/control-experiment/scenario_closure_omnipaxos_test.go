package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
	"github.com/SuzumiyaHaruki/consensus-atlas/targetoracles"
)

func TestOmnipaxosMessageLossClosureSharedPrefix(t *testing.T) {
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
	adapterFactory := func() (control.Adapter, error) {
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
	intervention, err := advanceOmnipaxosToReplicationDrop(
		ctx, inputs.Root, spec, inputs.Experiment.Runtime,
		inputs.Experiment.faultEnvelope(), adapterFactory, projector, semanticProjector,
	)
	if err != nil {
		t.Fatal(err)
	}
	if intervention.Status != controlexperiment.ScenarioStatusCompleted ||
		len(intervention.Steps) != 1 || intervention.Steps[0].Choice == nil ||
		!omnipaxosClosureReplicationLeaf(intervention.Steps[0].Choice.Action.MessageTypeHint) ||
		intervention.Steps[0].Choice.Action.Kind != control.ActionDropMessage ||
		intervention.FinalRisk.Status != semantic.RiskWitnessNotReached ||
		!reflect.DeepEqual(intervention.FinalRisk.SatisfiedMilestones, []string{
			omnipaxosMilestoneWorkloadInvoked, omnipaxosMilestoneMessageDropped,
		}) {
		t.Fatalf("OmniPaxos intervention prefix invalid: %#v", intervention)
	}
	factory := newOmnipaxosScenarioClosureFactory()
	closureContext := controlexperiment.ScenarioClosureContext{
		Spec: spec, Risk: intervention.FinalRisk, Trace: intervention.FinalTrace,
		Intervention: *intervention.Steps[0].Choice,
	}
	if selector, active, err := factory(closureContext); err != nil || !active || selector == nil {
		t.Fatalf("matching operation Drop did not activate closure: active=%t selector=%v err=%v",
			active, selector != nil, err)
	}
	mismatchedRequest := closureContext
	mismatchedRequest.Intervention = controlexperiment.FrontierChoice{
		SchemaVersion: closureContext.Intervention.SchemaVersion,
		ID:            closureContext.Intervention.ID, ViewDigest: closureContext.Intervention.ViewDigest,
		Decision: closureContext.Intervention.Decision, Digest: closureContext.Intervention.Digest,
		Action: closureContext.Intervention.Action,
	}
	mismatchedRequest.Intervention.Action.MessageMetadata = map[string]string{
		"entry_count": closureContext.Intervention.Action.MessageMetadata["entry_count"],
		"request_id":  "different-request",
	}
	if selector, active, err := factory(mismatchedRequest); err != nil || active || selector != nil {
		t.Fatalf("Drop for a different RequestID activated closure: active=%t selector=%v err=%v",
			active, selector != nil, err)
	}
	closureContext.Intervention.Decision++
	if selector, active, err := factory(closureContext); err != nil || active || selector != nil {
		t.Fatalf("Drop outside the trusted Risk milestone activated closure: active=%t selector=%v err=%v",
			active, selector != nil, err)
	}
	_, participants, err := newOmnipaxosDecisionClosureSelector(
		intervention.FinalTrace, intervention.Steps[0].Choice.Action,
	)
	if err != nil {
		evidence, _ := omnipaxosv2.ProjectEvidence(latestScenarioEvidence(intervention.FinalTrace))
		t.Fatalf("closure participant derivation failed: %v drop=%#v nodes=%#v",
			err, intervention.Steps[0].Choice.Action, evidence.Nodes)
	}
	if participants.Leader == "" || participants.DroppedFollower == "" ||
		participants.Alternate == "" || participants.Leader == participants.DroppedFollower ||
		participants.Leader == participants.Alternate ||
		participants.DroppedFollower == participants.Alternate ||
		participants.ReplicationLeaf != intervention.Steps[0].Choice.Action.MessageTypeHint {
		t.Fatalf("closure participants were not derived from evidence: %#v", participants)
	}
	const equalBudget = 4
	public, err := controlexperiment.ExecuteScenarioNaturalProgress(
		ctx, "omnipaxos-public-shared-prefix", equalBudget, spec, intervention.FinalRisk,
		intervention.FinalTrace, inputs.Experiment.Runtime, inputs.Experiment.faultEnvelope(),
		adapterFactory, projector,
	)
	if err != nil {
		t.Fatal(err)
	}
	prefix, err := controlexperiment.ExecutionTracePrefix(
		intervention.FinalTrace, len(intervention.FinalTrace.Records)-1,
	)
	if err != nil {
		t.Fatal(err)
	}
	prefixRisk, err := projector.Project("omnipaxos-target-prefix-risk", spec, prefix)
	if err != nil {
		t.Fatal(err)
	}
	targetPlan := controlexperiment.ScenarioPlan{
		ID: "omnipaxos-target-closure-plan",
		Steps: []controlexperiment.ScenarioStep{{
			ID: "drop-replication-message",
			Selector: controlexperiment.FrontierActionSelector{
				ActionID: intervention.Steps[0].Choice.Action.ActionID,
			},
		}},
	}
	target, err := controlexperiment.ExecuteSemanticBoundedScenarioPlan(
		ctx, "omnipaxos-target-closure", targetPlan, 1, equalBudget+1, spec,
		prefixRisk, prefix, inputs.Experiment.Runtime, inputs.Experiment.faultEnvelope(),
		adapterFactory, projector, semanticProjector, newOmnipaxosScenarioClosureFactory(),
		equalBudget,
	)
	if err != nil {
		t.Fatal(err)
	}
	publicExtended, err := controlexperiment.ExecuteScenarioNaturalProgress(
		ctx, "omnipaxos-public-shared-prefix-extended", 32, spec, intervention.FinalRisk,
		intervention.FinalTrace, inputs.Experiment.Runtime, inputs.Experiment.faultEnvelope(),
		adapterFactory, projector,
	)
	if err != nil {
		t.Fatal(err)
	}
	if public.StopReason != controlexperiment.ScenarioProgressBudget ||
		len(public.Execution.Steps) != equalBudget ||
		public.Execution.FinalRisk.Status == semantic.RiskWitnessReached ||
		!target.ClosureHandoff || len(target.Steps) != 1 ||
		len(target.AutomaticProgress) != equalBudget ||
		target.NaturalProgressStop != controlexperiment.ScenarioProgressClientTerminal ||
		target.FinalRisk.Status != semantic.RiskWitnessReached ||
		publicExtended.StopReason != controlexperiment.ScenarioProgressClientTerminal ||
		len(publicExtended.Execution.Steps) <= equalBudget ||
		publicExtended.Execution.FinalRisk.Status != semantic.RiskWitnessReached {
		t.Fatalf("OmniPaxos shared-prefix comparison drifted: public=%s/%d/%s target=%s/%d/%s extended=%s/%d/%s",
			public.StopReason, len(public.Execution.Steps), public.Execution.FinalRisk.Status,
			target.NaturalProgressStop, len(target.AutomaticProgress), target.FinalRisk.Status,
			publicExtended.StopReason, len(publicExtended.Execution.Steps), publicExtended.Execution.FinalRisk.Status)
	}
	wantClosureLeaves := []string{
		"sequence-paxos/prepare", "sequence-paxos/promise",
		"sequence-paxos/accept-sync", "sequence-paxos/accepted",
	}
	gotClosureLeaves := make([]string, 0, len(target.AutomaticProgress))
	for _, step := range target.AutomaticProgress {
		if step.Choice == nil || step.Choice.Action.Kind != control.ActionDeliverMessage {
			t.Fatalf("OmniPaxos closure selected a non-delivery: %#v", step)
		}
		gotClosureLeaves = append(gotClosureLeaves, step.Choice.Action.MessageTypeHint)
	}
	if !reflect.DeepEqual(gotClosureLeaves, wantClosureLeaves) {
		t.Fatalf("OmniPaxos closure path drifted: got=%v want=%v", gotClosureLeaves, wantClosureLeaves)
	}
	qualified, err := executeOmnipaxosScenarioQualifiedRisk(
		ctx, workerPath, inputs.Experiment, inputs.Workload, inputs.Qualification,
		prefix, target, spec, projector, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	mismatchedRegistry := etcdraftAgenticOracleRegistry()
	mismatchedResult := newScenarioTestingResult(
		target.PlanID, qualified.Bundle, qualified.Risk, mismatchedRegistry,
	)
	if validateScenarioTestingRisk(
		mismatchedResult, spec, projector, omnipaxosv2.DecisionProjector{}, mismatchedRegistry,
	) == nil {
		t.Fatal("OmniPaxos Decision Projector was accepted with the etcd Oracle registry")
	}
	droppedRequestID := intervention.Steps[0].Choice.Action.MessageMetadata["request_id"]
	if !qualified.Replay.Stable || qualified.Risk.Status != semantic.RiskWitnessReached ||
		len(qualified.Oracle.Violations) != 0 ||
		!reflect.DeepEqual(qualified.Oracle.Checked, []string{
			"trace-integrity", "agreement", targetoracles.OmnipaxosClientDecisionBindingMonitorID,
		}) ||
		droppedRequestID == "" || len(qualified.Bundle.ClientHistory) != 1 ||
		qualified.Bundle.ClientHistory[0].State != control.ItemCompleted ||
		qualified.Bundle.ClientHistory[0].Response.RequestID != droppedRequestID {
		t.Fatalf("OmniPaxos qualified closure evidence invalid: request=%s replay=%#v oracle=%#v clients=%#v",
			droppedRequestID, qualified.Replay, qualified.Oracle, qualified.Bundle.ClientHistory)
	}
	summary, err := json.Marshal(map[string]any{
		"shared_prefix_decisions": len(intervention.FinalTrace.Records) - 1,
		"intervention_step":       len(intervention.FinalTrace.Records),
		"intervention_leaf":       intervention.Steps[0].Choice.Action.MessageTypeHint,
		"intervention_source":     participants.Leader,
		"intervention_target":     participants.DroppedFollower,
		"alternate":               participants.Alternate,
		"request_id":              droppedRequestID,
		"equal_budget":            equalBudget,
		"public_equal": map[string]any{
			"decisions": len(public.Execution.Steps), "risk": public.Execution.FinalRisk.Status,
			"stop": public.StopReason, "work": public.Execution.Work.TotalWorkUnits,
		},
		"target_equal": map[string]any{
			"decisions": len(target.AutomaticProgress), "risk": target.FinalRisk.Status,
			"stop": target.NaturalProgressStop, "work": target.Work.TotalWorkUnits,
			"trace_digest": target.FinalTrace.Digest,
		},
		"public_extended": map[string]any{
			"decisions": len(publicExtended.Execution.Steps),
			"risk":      publicExtended.Execution.FinalRisk.Status,
			"stop":      publicExtended.StopReason, "work": publicExtended.Execution.Work.TotalWorkUnits,
		},
		"qualified": map[string]any{
			"bundle_digest":     qualified.Bundle.Digest,
			"primary_work":      qualified.Bundle.Work.Primary.WorkUnits,
			"replay_work":       qualified.Bundle.Work.Replay.WorkUnits,
			"replay_stable":     qualified.Replay.Stable,
			"oracle_checked":    qualified.Oracle.Checked,
			"oracle_violations": len(qualified.Oracle.Violations),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("OMNIPAXOS_CLOSURE_RESULT %s", summary)
}

func TestOmnipaxosExistingRiskRunsThroughScenarioAgentClosure(t *testing.T) {
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
	target, err := newOmnipaxosAgenticEpisodeTarget(inputs)
	if err != nil {
		t.Fatal(err)
	}
	existingRisk, _, err := loadExistingRiskInput(
		"../../plans/agent/omnipaxos-message-loss-risk-v2.json", target,
	)
	if err != nil || existingRisk == nil ||
		existingRisk.Candidate.ID != omnipaxosMessageLossRiskID {
		t.Fatalf("load OmniPaxos existing Risk: %#v/%v", existingRisk, err)
	}
	proposal, err := json.Marshal(controlexperiment.ScenarioInvestigationProposal{
		Intent: controlexperiment.ScenarioIntentContinue,
		Plan: controlexperiment.ScenarioPlan{
			ID: "omnipaxos-existing-risk-closure",
			Steps: []controlexperiment.ScenarioStep{
				{ID: "deliver-prepare-to-n2", Selector: controlexperiment.FrontierActionSelector{
					Kind: control.ActionDeliverMessage, MessageSource: "n1", MessageTarget: "n2",
					MessageTypeHint: "sequence-paxos/prepare",
				}},
				{ID: "deliver-promise-to-n1", Selector: controlexperiment.FrontierActionSelector{
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
			!bytes.Contains([]byte(payload.Messages[1].Content),
				[]byte(`"post_intervention_closure": true`)) {
			t.Fatalf("unexpected OmniPaxos Scenario provider call: %#v/%v", payload, err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(bytes.NewReader(fixtureOpenRouterResponse(
				t, providerCalls, proposal,
			))),
		}, nil
	})
	directory := t.TempDir()
	riskJournal, err := newStatelessAgentCallJournal(filepath.Join(directory, "risk"), client, "")
	if err != nil {
		t.Fatal(err)
	}
	scenarioJournal, err := newScenarioAgentCallJournal(filepath.Join(directory, "scenario"), client, "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := runAgenticEpisode(
		ctx, target, riskJournal, scenarioJournal, agenticEpisodeBudget{
			MaxRiskCalls: 1, MaxScenarioCalls: 1, MaxTotalCalls: 2,
			MaxObservedTokens: 120_000, MaxScenarioPlanSteps: 3,
			MaxRuntimeDecisions: 7,
		}, nil, nil, existingRisk,
		func() error { return errors.New("Risk provider must not be activated") },
		func() error { return scenarioJournal.ActivateKey("fixture-key") },
	)
	if err != nil || providerCalls != 1 || len(result.RiskProviderCalls) != 0 ||
		result.RiskAgent.Accepted == nil || len(result.RiskAgent.Attempts) != 0 ||
		len(result.ScenarioProviderCalls) != 1 || result.Scenario == nil ||
		result.Scenario.Agent.Execution == nil || result.Testing == nil {
		t.Fatalf("OmniPaxos existing-Risk Agent path failed: %#v calls=%d err=%v",
			result, providerCalls, err)
	}
	execution := result.Scenario.Agent.Execution
	if result.Scenario.Agent.DecisionsUsed != 7 || !execution.ClosureHandoff ||
		execution.ClosureHandoffStepID != "drop-operation-replication" ||
		len(execution.Steps) != 3 || len(execution.AutomaticProgress) != 4 ||
		execution.FinalRisk.Status != semantic.RiskWitnessReached ||
		!result.Testing.Replay.Stable || result.Testing.Risk.Status != semantic.RiskWitnessReached ||
		len(result.Testing.Oracle.Violations) != 0 ||
		!reflect.DeepEqual(result.Testing.Oracle.Checked, []string{
			"trace-integrity", "agreement", targetoracles.OmnipaxosClientDecisionBindingMonitorID,
		}) ||
		len(result.Testing.Bundle.ClientHistory) != 1 ||
		result.Testing.Bundle.ClientHistory[0].State != control.ItemCompleted ||
		result.Testing.Bundle.ClientHistory[0].Response.RequestID != "omnipaxos-a9e1-request" {
		t.Fatalf("OmniPaxos Agent closure evidence drifted: %#v", result)
	}
	interventions := 0
	for _, record := range execution.FinalTrace.Records {
		switch record.Action.Kind {
		case control.ActionDropMessage:
			interventions++
		case control.ActionCrash, control.ActionDuplicateMessage, control.ActionPartition:
			t.Fatalf("closure admitted a later intervention: %#v", record.Action)
		}
	}
	if interventions != 1 {
		t.Fatalf("Agent path did not retain exactly one intervention: %d", interventions)
	}
	assertOmnipaxosClientDecisionBinding(t, result.Testing.Bundle)
	wantClosureLeaves := []string{
		"sequence-paxos/prepare", "sequence-paxos/promise",
		"sequence-paxos/accept-sync", "sequence-paxos/accepted",
	}
	gotClosureLeaves := make([]string, 0, len(execution.AutomaticProgress))
	for _, step := range execution.AutomaticProgress {
		if step.Choice == nil {
			t.Fatalf("Agent closure omitted an exact choice: %#v", step)
		}
		gotClosureLeaves = append(gotClosureLeaves, step.Choice.Action.MessageTypeHint)
	}
	if !reflect.DeepEqual(gotClosureLeaves, wantClosureLeaves) {
		t.Fatalf("Agent closure path drifted: got=%v want=%v", gotClosureLeaves, wantClosureLeaves)
	}
	summary, err := json.Marshal(map[string]any{
		"risk_id": existingRisk.Candidate.ID, "risk_provider_calls": 0,
		"scenario_planner_calls": providerCalls, "planned_decisions": len(execution.Steps),
		"closure_decisions":       len(execution.AutomaticProgress),
		"closure_handoff_step_id": execution.ClosureHandoffStepID,
		"closure_leaves":          gotClosureLeaves, "request_id": "omnipaxos-a9e1-request",
		"risk": execution.FinalRisk.Status, "replay_stable": result.Testing.Replay.Stable,
		"oracle_checked":    result.Testing.Oracle.Checked,
		"oracle_violations": len(result.Testing.Oracle.Violations),
		"trace_digest":      execution.FinalTrace.Digest,
		"bundle_digest":     result.Testing.Bundle.Digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("OMNIPAXOS_AGENT_CLOSURE_RESULT %s", summary)
}

func assertOmnipaxosClientDecisionBinding(
	t *testing.T,
	bundle controlexperiment.ExecutionBundle,
) {
	t.Helper()
	monitor := targetoracles.OmnipaxosClientDecisionBindingMonitor{}
	verdict := oracle.CheckBundle(bundle, monitor)
	if len(verdict.Violations) != 0 || !reflect.DeepEqual(
		verdict.Checked, []string{targetoracles.OmnipaxosClientDecisionBindingMonitorID},
	) {
		t.Fatalf("real OmniPaxos Bundle binding verdict = %#v", verdict)
	}
	if len(bundle.ClientHistory) != 1 {
		t.Fatalf("real OmniPaxos Bundle client history = %#v", bundle.ClientHistory)
	}

	t.Run("tampered-client-request-id", func(t *testing.T) {
		mutated := bundle
		mutated.ClientHistory = append(
			[]controlexperiment.ClientHistoryEntry(nil), bundle.ClientHistory...,
		)
		mutated.ClientHistory[0].Response.RequestID = "tampered-request"
		assertOmnipaxosBindingViolation(t, oracle.CheckBundle(mutated, monitor), "projection failed")
	})

	t.Run("tampered-decision-content", func(t *testing.T) {
		mutated := bundle
		mutated.ClientHistory = append(
			[]controlexperiment.ClientHistoryEntry(nil), bundle.ClientHistory...,
		)
		entry := mutated.ClientHistory[0]
		decision := decodeOmnipaxosDecisionPayload(t, entry.Response.Payload)
		decision.Value = []byte("tampered-decision")
		entry.Response.Payload = encodeOmnipaxosDecisionPayload(t, entry.Response.Payload, decision)
		mutated.ClientHistory[0] = entry
		assertOmnipaxosBindingViolation(t, oracle.CheckBundle(mutated, monitor), "does not match its invoke")
	})

	t.Run("tampered-request-decision-mapping", func(t *testing.T) {
		mutated := bundle
		mutated.ClientHistory = append(
			[]controlexperiment.ClientHistoryEntry(nil), bundle.ClientHistory...,
		)
		entry := mutated.ClientHistory[0]
		decision := decodeOmnipaxosDecisionPayload(t, entry.Response.Payload)
		decision.RequestID = "uninvoked-request"
		entry.Response.RequestID = decision.RequestID
		entry.Response.Payload = encodeOmnipaxosDecisionPayload(t, entry.Response.Payload, decision)
		mutated.ClientHistory[0] = entry
		assertOmnipaxosBindingViolation(t, oracle.CheckBundle(mutated, monitor), "has no invoke")
	})

	t.Run("same-request-multiple-decisions", func(t *testing.T) {
		mutated := bundle
		mutated.ClientHistory = append(
			[]controlexperiment.ClientHistoryEntry(nil), bundle.ClientHistory...,
		)
		entry := mutated.ClientHistory[0]
		decision := decodeOmnipaxosDecisionPayload(t, entry.Response.Payload)
		decision.Index++
		entry.Step++
		entry.Item = control.ItemID("duplicate-decision-binding")
		entry.Response.Payload = encodeOmnipaxosDecisionPayload(t, entry.Response.Payload, decision)
		mutated.ClientHistory = append(mutated.ClientHistory, entry)
		assertOmnipaxosBindingViolation(t, oracle.CheckBundle(mutated, monitor), "maps to different decisions")
	})

	t.Run("missing-decided-prefix-witness", func(t *testing.T) {
		mutated := bundle
		mutated.Decisions.Observations = nil
		assertOmnipaxosBindingViolation(t, oracle.CheckBundle(mutated, monitor), "no decided-prefix witness")
	})

	t.Run("pending-result-is-not-a-violation", func(t *testing.T) {
		mutated := bundle
		mutated.ClientHistory = append(
			[]controlexperiment.ClientHistoryEntry(nil), bundle.ClientHistory...,
		)
		mutated.ClientHistory[0].State = control.ItemEnabled
		mutated.ClientHistory[0].Response.RequestID = "not-yet-returned"
		verdict := oracle.CheckBundle(mutated, monitor)
		if len(verdict.Violations) != 0 {
			t.Fatalf("pending request produced a binding violation: %#v", verdict)
		}
	})
}

type omnipaxosDecisionPayloadFixture struct {
	Node      uint64 `json:"node"`
	Index     uint64 `json:"index"`
	RequestID string `json:"request_id"`
	Origin    uint64 `json:"origin"`
	Value     []byte `json:"value"`
}

func decodeOmnipaxosDecisionPayload(
	t *testing.T,
	payload control.PayloadEnvelope,
) omnipaxosDecisionPayloadFixture {
	t.Helper()
	var decision omnipaxosDecisionPayloadFixture
	if err := json.Unmarshal(payload.Bytes, &decision); err != nil {
		t.Fatal(err)
	}
	return decision
}

func encodeOmnipaxosDecisionPayload(
	t *testing.T,
	original control.PayloadEnvelope,
	decision omnipaxosDecisionPayloadFixture,
) control.PayloadEnvelope {
	t.Helper()
	payload, err := control.NewJSONPayload(original.SchemaVersion, decision)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func assertOmnipaxosBindingViolation(t *testing.T, result oracle.Result, message string) {
	t.Helper()
	if len(result.Violations) != 1 ||
		result.Violations[0].Monitor != targetoracles.OmnipaxosClientDecisionBindingMonitorID ||
		!bytes.Contains([]byte(result.Violations[0].Message), []byte(message)) {
		t.Fatalf("OmniPaxos binding mutation was not detected: %#v", result)
	}
}

func TestOmnipaxosClosureFactoryRejectsUnrecognizedInputs(t *testing.T) {
	spec, err := omnipaxosMessageLossWitness()
	if err != nil {
		t.Fatal(err)
	}
	factory := newOmnipaxosScenarioClosureFactory()
	nonmatching := spec
	nonmatching.RiskID = "different-risk"
	if selector, active, err := factory(controlexperiment.ScenarioClosureContext{
		Spec: nonmatching,
	}); err != nil || active || selector != nil {
		t.Fatalf("nonmatching Risk activated closure: active=%t selector=%v err=%v",
			active, selector != nil, err)
	}
	if selector, active, err := factory(controlexperiment.ScenarioClosureContext{
		Spec: spec,
		Intervention: controlexperiment.FrontierChoice{Action: controlexperiment.FrontierActionRef{
			Kind: control.ActionDropMessage, MessageTypeHint: "sequence-paxos/accept-sync",
			MessageMetadata: map[string]string{"entry_count": "1"},
		}},
	}); err != nil || active || selector != nil {
		t.Fatalf("non-operation message activated closure: active=%t selector=%v err=%v",
			active, selector != nil, err)
	}
}

func TestOmnipaxosClosureRejectsInvalidEntryCountAndMultipleInvokes(t *testing.T) {
	for _, entryCount := range []string{"-1", "garbage", "0"} {
		action := controlexperiment.FrontierActionRef{
			Kind: control.ActionDropMessage, MessageTypeHint: "sequence-paxos/accept-sync",
			MessageMetadata: map[string]string{
				"entry_count": entryCount, "request_id": "request-1",
			},
		}
		if omnipaxosClosureReplicationIntervention(action) {
			t.Fatalf("invalid entry_count %q activated operation replication", entryCount)
		}
	}
	payload, err := omnipaxosv2.InputPayload(omnipaxosv2.Input{
		RequestID: "request-1", Value: []byte("value"),
	})
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := json.Marshal(control.AdapterInvokeParameters{Input: payload})
	if err != nil {
		t.Fatal(err)
	}
	record := controlruntime.ActionRecord{Action: control.Action{
		Kind: control.ActionInvoke, Parameters: parameters,
	}}
	if omnipaxosClosureInvokeRecordsMatch(
		[]controlruntime.ActionRecord{record, record}, "request-1",
	) {
		t.Fatal("multiple Invoke records were accepted by the single-request closure")
	}
}

func TestOmnipaxosClosureSelectorReportsAmbiguousTier(t *testing.T) {
	selector := omnipaxosDecisionClosureSelector(omnipaxosClosureParticipants{
		Leader: "n1", DroppedFollower: "n2", Alternate: "n3",
		ReplicationLeaf: "sequence-paxos/accept-sync",
	})
	selection, err := selector(controlexperiment.ActionFrontierView{Actions: []controlexperiment.FrontierActionRef{
		{ActionID: "prepare-a", Kind: control.ActionDeliverMessage,
			MessageSource: control.NodeRef{Node: "n1"}, MessageTarget: "n3",
			MessageTypeHint: "sequence-paxos/prepare"},
		{ActionID: "prepare-b", Kind: control.ActionDeliverMessage,
			MessageSource: control.NodeRef{Node: "n1"}, MessageTarget: "n3",
			MessageTypeHint: "sequence-paxos/prepare"},
	}})
	if err != nil || selection.Status != controlexperiment.ScenarioClosureUnderdetermined {
		t.Fatalf("ambiguous tier was not preserved: %#v/%v", selection, err)
	}
}

func TestOmnipaxosClosureSelectorReportsNoEligibleAction(t *testing.T) {
	selector := omnipaxosDecisionClosureSelector(omnipaxosClosureParticipants{
		Leader: "n1", DroppedFollower: "n2", Alternate: "n3",
		ReplicationLeaf: "sequence-paxos/accept-sync",
	})
	selection, err := selector(controlexperiment.ActionFrontierView{Actions: []controlexperiment.FrontierActionRef{
		{ActionID: "heartbeat", Kind: control.ActionDeliverMessage,
			MessageSource: control.NodeRef{Node: "n1"}, MessageTarget: "n3",
			MessageTypeHint: "ble/heartbeat-request"},
	}})
	if err != nil || selection.Status != controlexperiment.ScenarioClosureNoEligible {
		t.Fatalf("missing closure message did not stop normally: %#v/%v", selection, err)
	}
}

// advanceOmnipaxosToReplicationDrop constructs only the experiment's shared
// prefix. One live Runtime follows the same public natural-progress priority
// and stops before the first enabled operation-carrying replication message.
// The final step is executed through the ordinary Scenario path as the one
// intervention under study. No protocol state or message content is modified.
func advanceOmnipaxosToReplicationDrop(
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
	if err != nil || len(seed) == 0 || faultEnvelope == nil ||
		faultEnvelope.MaxMessageDrops < 1 {
		return controlexperiment.ScenarioExecution{}, errors.New("OMNIPAXOS_CLOSURE_PREFIX_INPUT_INVALID")
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
	seenMessageTypes := map[string]int{}
	for decision := 0; decision < 128; decision++ {
		actions, err := runtime.EnabledActions(ctx)
		if err != nil {
			return controlexperiment.ScenarioExecution{}, err
		}
		snapshot := runtime.Snapshot()
		for _, action := range actions {
			if action.Kind != control.ActionDropMessage {
				continue
			}
			item, ok := omnipaxosClosureSnapshotItem(snapshot, action.Item)
			if ok && item.Value.Message != nil {
				seenMessageTypes[item.Value.Message.TypeHint]++
			}
			if ok && item.Value.Message != nil &&
				omnipaxosClosureReplicationLeaf(item.Value.Message.TypeHint) {
				if drop.ID == "" {
					drop = action
				}
			}
		}
		if drop.ID != "" {
			break
		}
		selected, ok := firstOmnipaxosScenarioProgress(actions)
		if !ok {
			return controlexperiment.ScenarioExecution{}, fmt.Errorf(
				"OMNIPAXOS_CLOSURE_PREFIX_QUIESCENT: decision=%d actions=%v", decision, actions,
			)
		}
		if _, err := runtime.Select(ctx, selected.ID); err != nil {
			return controlexperiment.ScenarioExecution{}, err
		}
	}
	if drop.ID == "" {
		return controlexperiment.ScenarioExecution{}, fmt.Errorf(
			"OMNIPAXOS_CLOSURE_PREFIX_BUDGET_EXHAUSTED: seen=%v", seenMessageTypes,
		)
	}
	prefix, err := runtime.Trace()
	if err != nil {
		return controlexperiment.ScenarioExecution{}, err
	}
	if err := runtime.Close(); err != nil {
		return controlexperiment.ScenarioExecution{}, err
	}
	closed = true
	prefixRisk, err := projector.Project("omnipaxos-closure-prefix-risk", spec, prefix)
	if err != nil {
		return controlexperiment.ScenarioExecution{}, err
	}
	plan := controlexperiment.ScenarioPlan{
		ID: "omnipaxos-drop-accept-decide",
		Steps: []controlexperiment.ScenarioStep{{
			ID:       "drop-accept-decide",
			Selector: controlexperiment.FrontierActionSelector{ActionID: drop.ID},
		}},
	}
	return controlexperiment.ExecuteSemanticBoundedScenarioPlan(
		ctx, "omnipaxos-intervention", plan, 1, 1, spec, prefixRisk,
		prefix, runtimeConfig, faultEnvelope, adapterFactory, projector,
		semanticProjector, nil, 0,
	)
}

func omnipaxosClosureSnapshotItem(
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
