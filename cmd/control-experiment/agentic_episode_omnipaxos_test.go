package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
	"github.com/SuzumiyaHaruki/consensus-atlas/targetoracles"
)

type failAfterInitialYieldAdapter struct {
	control.Adapter
	runCalls int
}

func investigationScenarioDecisions(episodes []recoveredAgenticEpisode) int {
	total := 0
	for _, episode := range episodes {
		total += episode.Summary.ScenarioDecisionsUsed
	}
	return total
}

func TestOmnipaxosCapabilityFeedbackProbeUsesRealTargetSurface(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(180*time.Second))
	defer cancel()
	workerPath := buildOmnipaxosScenarioWorker(t)
	options := controlExperimentOptions{
		Target: "omnipaxos-v2", WorkerPath: workerPath, InvestigationEpisodes: 1,
		SemanticInput: "../../plans/agent/omnipaxos-agentic-calibration-v1.json",
		AgentKeyFile:  "fixture-key.txt", AgentModel: openRouterFixtureModel,
		CapabilityFeedbackMode:  controlexperiment.AgenticCapabilityFeedbackStructuredGaps,
		CapabilityFeedbackProbe: "../../plans/agent/omnipaxos-capability-feedback-probe-v1.json",
	}
	composition, err := prepareAgenticEpisodeComposition(ctx, options)
	if err != nil || composition.CapabilityFeedbackProbe == nil ||
		composition.CapabilityFeedbackProbe.ID != "omnipaxos-missing-crash-probe-v1" ||
		composition.MethodSpec.CapabilityFeedbackProbe == nil ||
		composition.MethodSpec.CapabilityFeedbackProbe.ID != composition.CapabilityFeedbackProbe.ID ||
		composition.Budget.MaxScenarioCalls != 1 || len(composition.Memory) != 1 ||
		len(composition.Memory[0].CapabilityGaps) != 1 ||
		composition.Memory[0].CapabilityGaps[0].Code != controlexperiment.AgentCapabilityGapMissingAction ||
		!strings.Contains(composition.Memory[0].CapabilityGaps[0].Summary, "crash") {
		t.Fatalf("OmniPaxos capability probe did not produce bound mechanical Memory: %#v/%v",
			composition, err)
	}
	reasonOnly := projectAgenticCapabilityFeedbackMemory(
		composition.Memory, controlexperiment.AgenticCapabilityFeedbackReasonCodes,
	)
	structured := projectAgenticCapabilityFeedbackMemory(
		composition.Memory, controlexperiment.AgenticCapabilityFeedbackStructuredGaps,
	)
	if len(reasonOnly[0].CapabilityGaps) != 0 ||
		len(reasonOnly[0].MechanicalReasonCodes) != 1 ||
		len(structured[0].CapabilityGaps) != 1 {
		t.Fatalf("capability probe arms did not differ only in structured gap exposure: %#v/%#v",
			reasonOnly, structured)
	}
	tampered := composition
	tampered.Memory = append([]controlexperiment.RiskExplorationMemoryEntry(nil), composition.Memory...)
	tampered.Memory[0].CapabilityGaps = append(
		[]controlexperiment.AgentCapabilityGap(nil), composition.Memory[0].CapabilityGaps...,
	)
	tampered.Memory[0].CapabilityGaps[0].Summary = "caller replaced the trusted probe result"
	_, err = runAgenticEpisodeDirectory(ctx, agenticEpisodeDirectoryOptions{
		Directory: filepath.Join(t.TempDir(), "tampered-probe"), AgentKeyFile: "fixture-key.txt",
		ReadKey:  func(string) (string, error) { return "fixture-key", nil },
		Recovery: omnipaxosAgenticEpisodeRecoveryBinding(),
		Prepare:  func(context.Context) (agenticEpisodeComposition, error) { return tampered, nil },
	})
	if err == nil || !strings.Contains(err.Error(), "COMPOSITION_INVALID") {
		t.Fatalf("method-bound probe Memory was replaceable: %v", err)
	}
}

func TestAgenticArtifactAccountsChargedScenarioCallRejectedByTokenThreshold(t *testing.T) {
	artifact := agenticEpisodeArtifact{
		Status:            agenticEpisodeTokenStopped,
		Budget:            agenticEpisodeBudget{MaxObservedTokens: 10},
		RiskAttempts:      1,
		RiskProviderCalls: []controlexperiment.StatelessAgentCallAudit{{}},
		ScenarioAttempts:  0,
		ScenarioProviderCalls: []controlexperiment.StatelessAgentCallAudit{{
			Status: controlexperiment.StatelessAgentCallContentReady,
			Work:   controlexperiment.ModelWork{Calls: 1, InputTokens: 4, OutputTokens: 3, TotalTokens: 7},
		}},
		Work: agenticEpisodeWork{Model: controlexperiment.ModelWork{
			Calls: 2, InputTokens: 8, OutputTokens: 6, TotalTokens: 14,
		}},
		Assessment: agenticEvidenceAssessment{ReasonCode: "model-token-threshold-reached"},
	}
	if !agenticProviderAttemptAccountingValid(artifact) {
		t.Fatal("charged provider response rejected at the token boundary was lost")
	}
	artifact.Status = agenticEpisodeScenarioStopped
	if agenticProviderAttemptAccountingValid(artifact) {
		t.Fatal("non-token terminal state acquired an unmatched provider call")
	}
}

func TestAgenticArtifactDoesNotTreatUnknownProviderUsageAsZeroCost(t *testing.T) {
	observed := []controlexperiment.StatelessAgentCallAudit{{
		Work:                controlexperiment.ModelWork{Calls: 1, InputTokens: 4, OutputTokens: 3, TotalTokens: 7},
		ProviderUsageStatus: agentProviderUsageObserved,
	}}
	if !agenticProviderUsageReconciled(observed) {
		t.Fatal("observed provider usage was rejected")
	}
	observed[0].Work = controlexperiment.ModelWork{Calls: 1}
	observed[0].ProviderUsageStatus = agentProviderUsageUnknown
	if agenticProviderUsageReconciled(observed) {
		t.Fatal("unknown provider usage was accepted as zero-token formal cost")
	}
}

func TestScenarioDecisionProvenanceUsesFinalMergedExecution(t *testing.T) {
	execution := &controlexperiment.ScenarioExecution{
		Steps: []controlexperiment.ScenarioStepFeedback{
			{Outcome: controlexperiment.ScenarioStepApplied, Choice: &controlexperiment.FrontierChoice{}},
			{Outcome: controlexperiment.ScenarioStepRejected, Choice: &controlexperiment.FrontierChoice{}},
			{Outcome: controlexperiment.ScenarioStepApplied},
		},
		AutomaticProgress: []controlexperiment.ScenarioStepFeedback{{}, {}},
	}
	got := scenarioDecisionProvenance(execution)
	want := (agenticDecisionProvenance{AgentSelected: 1, PublicProgress: 2})
	if got != want || got.Validate(3) != nil {
		t.Fatalf("final merged execution provenance drifted: got=%#v want=%#v", got, want)
	}
	if scenarioDecisionProvenance(nil) != (agenticDecisionProvenance{}) {
		t.Fatal("nil final execution produced decision provenance")
	}
}

func TestAgenticArtifactRejectsZeroProvenanceForExecutedDecisions(t *testing.T) {
	artifact := agenticEpisodeArtifact{
		TargetID: "provenance-fixture",
		Status:   agenticEpisodeRiskStopped,
		Budget: agenticEpisodeBudget{
			MaxRiskCalls: 1, MaxScenarioCalls: 1, MaxTotalCalls: 2,
			MaxObservedTokens: 10, MaxScenarioPlanSteps: 1, MaxRuntimeDecisions: 2,
		},
		ScenarioDecisionsUsed: 1,
	}
	if artifact.validateCompact() == nil {
		t.Fatal("executed decision acquired the historical zero-provenance escape")
	}
	artifact.DecisionProvenance.PublicProgress = 1
	if err := artifact.validateCompact(); err != nil {
		t.Fatalf("complete decision provenance was rejected: %v", err)
	}
}

func TestCurrentMethodRecoveryRequiresOracleAttribution(t *testing.T) {
	current := &controlexperiment.AgenticMethodSpec{
		ImplementationID: controlexperiment.AgenticMethodImplementationID,
	}
	legacy := &controlexperiment.AgenticMethodSpec{
		ImplementationID: "consensus-atlas/agentic-method/m4n10-deep-candidate-investigation-v1",
	}
	m4n11 := &controlexperiment.AgenticMethodSpec{
		ImplementationID: "consensus-atlas/agentic-method/m4n11-provider-recovery-and-quorum-oracle-v1",
	}
	if err := requireRecoveredScenarioOracleAttribution(current, true, nil); err == nil {
		t.Fatal("current executed path recovered without Oracle attribution")
	}
	if err := requireRecoveredScenarioOracleAttribution(m4n11, true, nil); err == nil {
		t.Fatal("M4n11 executed path recovered without Oracle attribution")
	}
	if err := requireRecoveredScenarioOracleAttribution(
		current, true, &scenarioOracleAttribution{RootDecisions: 1},
	); err != nil {
		t.Fatalf("current attributed path was rejected: %v", err)
	}
	if err := requireRecoveredScenarioOracleAttribution(legacy, true, nil); err != nil {
		t.Fatalf("legacy artifact lost read-only compatibility: %v", err)
	}
	if err := requireRecoveredScenarioOracleAttribution(current, false, nil); err != nil {
		t.Fatalf("current non-executed episode incorrectly required attribution: %v", err)
	}
}

func (adapter *failAfterInitialYieldAdapter) RunUntilYield(
	ctx context.Context,
) (control.Yield, error) {
	adapter.runCalls++
	if adapter.runCalls > 1 {
		return control.Yield{}, errors.New("fixture action execution failed")
	}
	return adapter.Adapter.RunUntilYield(ctx)
}

func (adapter *failAfterInitialYieldAdapter) Close() error {
	if closer, ok := adapter.Adapter.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

func TestOmnipaxosAgenticEpisodeBoundsAccountsAndRecoversBothAgents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(180*time.Second))
	defer cancel()
	workerPath := buildOmnipaxosScenarioWorker(t)
	inputs, err := prepareOmnipaxosAgenticEpisode(
		ctx, workerPath, "../../plans/agent/omnipaxos-agentic-calibration-v1.json",
	)
	if err != nil {
		t.Fatal(err)
	}
	if recovery := agenticKnowledgeText(inputs.Knowledge, "omnipaxos-recovery-adoption"); !strings.Contains(recovery, "prepare quorum") || !strings.Contains(recovery, "accepted log") ||
		strings.Contains(strings.ToLower(recovery), "raft") {
		t.Fatalf("OmniPaxos knowledge lost protocol-local recovery semantics: %q", recovery)
	}
	configuredBudget, err := agenticEpisodeBudgetFromExperiment(
		inputs.Experiment.ScenarioMaxCalls, inputs.Experiment.ScenarioMaxSteps,
		inputs.Experiment.ScenarioMaxDecisions, inputs.Experiment.SessionBudget,
	)
	if err != nil || configuredBudget.MaxRiskCalls != 4 ||
		configuredBudget.MaxScenarioCalls != 8 || configuredBudget.MaxScenarioPlanSteps != 1 ||
		configuredBudget.MaxRuntimeDecisions != 128 || configuredBudget.MaxTotalCalls != 12 ||
		configuredBudget.MaxObservedTokens != 240000 {
		t.Fatalf("OmniPaxos A9e1 budget does not permit Risk repair: %#v/%v", configuredBudget, err)
	}
	candidateBytes, err := json.Marshal(omnipaxosDiscoveredRiskPortfolio())
	if err != nil {
		t.Fatal(err)
	}
	providerCalls := 0
	memoryRiskRequests := 0
	scenarioFeedbackViews := 0
	client := fixtureOpenRouterIntentClient()
	client.HTTP = agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
		providerCalls++
		var payload openRouterChatRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		var content []byte
		switch payload.ResponseFormat.JSONSchema.Name {
		case "risk_candidate_portfolio":
			view := riskAgentViewFromPayload(t, payload)
			if view.TargetSurface == nil || view.TargetSurface.TargetID != "omnipaxos-v2" ||
				len(view.TargetSurface.Nodes) != 3 ||
				view.TargetSurface.FaultAllowance.MaxMessageDrops != 1 ||
				len(view.TargetSurface.Capabilities.ComposableActions) != 4 ||
				len(view.TargetSurface.Capabilities.ObservationCapabilities) == 0 ||
				len(view.TargetSurface.Capabilities.OracleCapabilities) != 3 ||
				len(view.TargetSurface.Capabilities.FidelityBoundaries) != 1 {
				t.Fatalf("Risk Agent did not receive the active target surface: %#v", view.TargetSurface)
			}
			if len(view.ExplorationMemory) > 0 {
				memoryRiskRequests++
				if view.ExplorationMemory[len(view.ExplorationMemory)-1].CandidateID !=
					omnipaxosDiscoveredRiskCandidate().ID {
					t.Fatalf("Risk Agent received unrelated memory: %#v", view.ExplorationMemory)
				}
			}
			content = candidateBytes
		case scenarioInvestigationStructuredOutputName:
			view := a4bScenarioViewFromPayload(t, payload)
			promptContent := payload.Messages[1].Content
			wantIntents := []string{controlexperiment.ScenarioIntentContinue}
			if view.Prior != nil && view.Prior.Outcome == controlexperiment.ScenarioAgentStopped {
				wantIntents = []string{controlexperiment.ScenarioIntentRevise}
			}
			if scenarioPromptFeedbackAllowsAbandon(view.Prior) {
				wantIntents = append(wantIntents, controlexperiment.ScenarioIntentAbandon)
			}
			planRequired := len(wantIntents) == 1
			if view.AcceptedHypothesis == nil ||
				view.AcceptedHypothesis.Candidate.ID != omnipaxosDiscoveredRiskCandidate().ID ||
				view.TargetSurface == nil || view.TargetSurface.TargetID != "omnipaxos-v2" ||
				len(view.TargetSurface.Workload.Invocations) != 1 ||
				!reflect.DeepEqual(view.AvailableIntents, wantIntents) ||
				planRequired != bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte(`"required":["intent","plan"]`)) ||
				!bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte(`"plan"`)) ||
				!bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte(`"message_class"`)) ||
				!bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte(`"replication"`)) ||
				bytes.Contains(payload.ResponseFormat.JSONSchema.Schema, []byte(`"branch_id"`)) ||
				!strings.Contains(promptContent, "selector_trace") ||
				!strings.Contains(promptContent, "milestone-stalled") ||
				!strings.Contains(promptContent, "logical-clock") ||
				!strings.Contains(promptContent, "fault allowance/usage/remaining") ||
				!strings.Contains(promptContent, "already restricted to strategic Actions") ||
				!strings.Contains(promptContent, "kind=invoke") ||
				!strings.Contains(promptContent, `"action_frontier"`) ||
				!strings.Contains(promptContent, `"coordination"`) ||
				strings.Contains(promptContent, "kind=Invoke") ||
				strings.Contains(promptContent, `"current_strategic_candidates"`) ||
				strings.Contains(promptContent, `"action_semantics"`) ||
				strings.Contains(promptContent, `"root_frontier"`) {
				t.Fatalf("Scenario Agent did not receive compact accepted context: %#v", view)
			}
			if strings.Contains(promptContent, `"knowledge":`) ||
				strings.Contains(promptContent, `"hypothesis":`) {
				t.Fatal("Scenario provider prompt retained the full knowledge/hypothesis contracts")
			}
			if view.Prior != nil {
				scenarioFeedbackViews++
				if view.Prior.PreviousProposal == nil || len(view.Prior.Steps) == 0 {
					t.Fatalf("stateless Scenario feedback lost the preceding strategic action: %#v", view.Prior)
				}
			}
			var step *controlexperiment.ScenarioStep
			for _, action := range view.Frontier.Actions {
				if action.Kind == control.ActionDropMessage &&
					(action.MessageTypeHint == "sequence-paxos/accept-sync" ||
						action.MessageTypeHint == "sequence-paxos/accept-decide") {
					value := controlexperiment.ScenarioStep{ID: "drop-replication", Selector: controlexperiment.FrontierActionSelector{
						Kind: control.ActionDropMessage, MessageSource: action.MessageSource.Node,
						MessageTarget: action.MessageTarget, MessageTypeHint: action.MessageTypeHint,
					}}
					step = &value
					break
				}
			}
			if step == nil {
				for _, action := range view.Frontier.Actions {
					if action.Kind == control.ActionDeliverMessage && action.MessageTypeHint == "sequence-paxos/promise" {
						value := controlexperiment.ScenarioStep{ID: "advance-replication", Selector: controlexperiment.FrontierActionSelector{
							Kind: action.Kind, MessageSource: action.MessageSource.Node,
							MessageTarget: action.MessageTarget, MessageTypeHint: action.MessageTypeHint,
						}}
						step = &value
						break
					}
				}
			}
			if step == nil {
				for _, action := range view.Frontier.Actions {
					if action.Kind == control.ActionDeliverMessage &&
						(action.MessageTypeHint == "sequence-paxos/prepare" ||
							action.MessageTypeHint == "sequence-paxos/prepare-req") {
						value := controlexperiment.ScenarioStep{ID: "deliver-prepare", Selector: controlexperiment.FrontierActionSelector{
							Kind: control.ActionDeliverMessage, MessageSource: action.MessageSource.Node,
							MessageTarget: action.MessageTarget, MessageTypeHint: action.MessageTypeHint,
						}}
						step = &value
						break
					}
				}
			}
			if step == nil {
				for _, action := range view.Frontier.Actions {
					if action.Kind != control.ActionDeliverMessage &&
						action.Kind != control.ActionFireTemporal {
						continue
					}
					value := controlexperiment.ScenarioStep{
						ID: "advance-bootstrap",
						Selector: controlexperiment.FrontierActionSelector{
							ActionID: action.ActionID,
						},
					}
					step = &value
					break
				}
			}
			if step != nil {
				content, err = json.Marshal(controlexperiment.ScenarioInvestigationProposal{
					Intent: view.AvailableIntents[0],
					Plan: controlexperiment.ScenarioPlan{
						ID: "agentic-episode-scenario", Steps: []controlexperiment.ScenarioStep{*step},
					},
				})
			}
			if len(content) == 0 {
				t.Fatalf("Scenario Agent prompt had no replication message: %#v", view.Frontier.Actions)
			}
		default:
			t.Fatalf("unexpected structured output %q", payload.ResponseFormat.JSONSchema.Name)
		}
		if err != nil {
			t.Fatal(err)
		}
		response := fixtureOpenRouterResponse(t, providerCalls, content)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(response))}, nil
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
	keyActivations := 0
	activateRisk := func() error {
		keyActivations++
		return riskJournal.ActivateKey("fixture-key")
	}
	activateScenario := func() error {
		keyActivations++
		return scenarioJournal.ActivateKey("fixture-key")
	}
	budget := agenticEpisodeBudget{
		MaxRiskCalls: 1, MaxScenarioCalls: 5, MaxTotalCalls: 6,
		MaxObservedTokens: 45, MaxScenarioPlanSteps: 4,
		MaxRuntimeDecisions: inputs.Experiment.ScenarioMaxDecisions,
	}
	result, err := runOmnipaxosAgenticEpisode(
		ctx, inputs, riskJournal, scenarioJournal, budget, activateRisk, activateScenario,
	)
	if err != nil || result.Status != agenticEpisodeCompleted || result.Testing == nil ||
		!result.Metrics.CandidateAccepted || !result.Metrics.WitnessInstantiated ||
		result.Scenario == nil || result.Scenario.Agent.DecisionsUsed == 0 ||
		result.Metrics.OracleFindings != 0 || !result.Testing.Replay.Stable ||
		result.Scenario.Agent.SetupWork.TotalWorkUnits == 0 ||
		result.Work.Model != (controlexperiment.ModelWork{
			Calls: 2, InputTokens: 8, OutputTokens: 6, TotalTokens: 14,
		}) || result.Work.ScenarioSearch.TotalWorkUnits == 0 ||
		result.Work.QualifiedExecution.Primary.WorkUnits == 0 ||
		result.Work.QualifiedExecution.Replay.WorkUnits == 0 ||
		len(result.RiskProviderCalls) != 1 || len(result.ScenarioProviderCalls) != 1 ||
		providerCalls != 2 || keyActivations != 2 || scenarioFeedbackViews != 0 ||
		result.Assessment.Status != agenticEvidenceWitnessInstantiated ||
		result.Assessment.ReasonCode != "property-oracle-clean" ||
		result.Assessment.PropertyID != "client-operation-continuity" ||
		result.Assessment.EvidenceLevel != controlexperiment.PropertyEvidenceOracleBacked ||
		!reflect.DeepEqual(result.Assessment.OracleIDs, []string{
			targetoracles.OmnipaxosClientDecisionBindingMonitorID,
		}) {
		t.Fatalf("bounded agentic episode incomplete: %#v calls=%d keys=%d err=%v",
			result, providerCalls, keyActivations, err)
	}
	// Automatic coordinator setup commits Invoke before the first planner call.
	// It remains in the final merged execution but is not owned by an attempt.
	// Attempts are process evidence and must not be the source of final path
	// accounting.
	finalAutomaticInvoke := false
	for _, progress := range result.Scenario.Agent.Execution.AutomaticProgress {
		if progress.Choice != nil && progress.Choice.Action.Kind == control.ActionInvoke {
			finalAutomaticInvoke = true
			break
		}
	}
	attemptAutomaticInvoke := false
	for _, attempt := range result.Scenario.Agent.Attempts {
		if attempt.Execution == nil {
			continue
		}
		for _, progress := range attempt.Execution.AutomaticProgress {
			if progress.Choice == nil || progress.Choice.Action.Kind != control.ActionInvoke {
				continue
			}
			attemptAutomaticInvoke = true
			break
		}
		if attemptAutomaticInvoke {
			break
		}
	}
	if !finalAutomaticInvoke || attemptAutomaticInvoke {
		t.Fatalf("automatic setup provenance drifted: final_invoke=%t attempt_invoke=%t",
			finalAutomaticInvoke, attemptAutomaticInvoke)
	}
	artifactDirectory := t.TempDir()
	artifact, err := persistAgenticEpisodeArtifacts(
		artifactDirectory, "omnipaxos-v2", budget, result,
	)
	var recordedAttemptWork []controlexperiment.ScenarioExecutionWork
	for _, attempt := range result.Scenario.Agent.Attempts {
		if attempt.Execution != nil {
			recordedAttemptWork = append(recordedAttemptWork, attempt.Execution.Work)
		}
	}
	wantProvenance := scenarioDecisionProvenance(result.Scenario.Agent.Execution)
	attemptPublicProgress := 0
	for _, attempt := range result.Scenario.Agent.Attempts {
		if attempt.Execution != nil {
			attemptPublicProgress += len(attempt.Execution.AutomaticProgress)
		}
	}
	if err != nil || len(artifact.ScenarioAttemptFeedback) != artifact.ScenarioAttempts ||
		len(artifact.ScenarioAttemptFeedback) != 1 ||
		artifact.DecisionProvenance != wantProvenance ||
		artifact.ScenarioSetupWork != result.Scenario.Agent.SetupWork ||
		artifact.ScenarioTrustedProgressWork != result.Scenario.Agent.TrustedProgressWork ||
		artifact.DecisionProvenance.Validate(artifact.ScenarioDecisionsUsed) != nil ||
		wantProvenance.PublicProgress <= attemptPublicProgress ||
		!artifact.ScenarioAttemptFeedback[0].EnteredExecution ||
		artifact.ScenarioAttemptFeedback[0].Outcome != controlexperiment.ScenarioStatusCompleted {
		t.Fatalf("compact Scenario attempt evidence missing: %#v setup=%#v trusted=%#v search=%#v attempt_work=%#v err=%v",
			artifact.ScenarioAttemptFeedback, result.Scenario.Agent.SetupWork,
			result.Scenario.Agent.TrustedProgressWork, result.Scenario.Agent.ExecutionWork,
			recordedAttemptWork, err)
	}
	recoveredArtifact, terminal, err := recoverAgenticEpisodeArtifacts(
		artifactDirectory, omnipaxosAgenticEpisodeRecoveryBinding(),
	)
	if err != nil || !terminal || recoveredArtifact.Testing == nil ||
		recoveredArtifact.Summary.Metrics != artifact.Metrics ||
		recoveredArtifact.Summary.DecisionProvenance != artifact.DecisionProvenance ||
		!reflect.DeepEqual(recoveredArtifact.Summary.ScenarioAttemptFeedback, artifact.ScenarioAttemptFeedback) ||
		recoveredArtifact.Testing.Bundle.Trace.Digest != result.Testing.Bundle.Trace.Digest ||
		recoveredArtifact.Testing.Risk.Digest != result.Testing.Risk.Digest ||
		!reflect.DeepEqual(recoveredArtifact.Testing.Oracle, result.Testing.Oracle) {
		t.Fatalf("terminal artifact recovery drifted: %#v terminal=%t err=%v",
			recoveredArtifact, terminal, err)
	}
	memory, err := deriveAgenticExplorationMemory([]recoveredAgenticEpisode{
		recoveredArtifact, recoveredArtifact,
	})
	if err != nil || len(memory) != 2 || memory[0].CandidateID != result.RiskAgent.Accepted.Candidate.ID ||
		memory[0].PropertyRef != result.RiskAgent.Accepted.Candidate.PropertyRef ||
		memory[0].EvidenceLevel != controlexperiment.PropertyEvidenceOracleBacked ||
		memory[0].RepeatedCandidate || memory[0].RiskStatus != semantic.RiskWitnessReached ||
		memory[0].EpisodeOutcome != controlexperiment.RiskMemoryOutcomeExecutionCompleted ||
		len(memory[0].SatisfiedMilestones) != len(result.Testing.Risk.SatisfiedMilestones) ||
		memory[0].ModelTokens != result.Work.Model.TotalTokens || memory[0].SearchWorkUnits == 0 ||
		len(memory[0].MechanicalReasonCodes) != 1 ||
		memory[0].MechanicalReasonCodes[0] != controlexperiment.RiskAgentReasonBinding ||
		!memory[1].RepeatedCandidate {
		t.Fatalf("exploration memory did not derive prior evidence: %#v/%v", memory, err)
	}
	oracleLabel := recoveredArtifact.Summary
	oracleLabel.Assessment.Status = agenticEvidenceOracleFinding
	mechanicalLabel := recoveredArtifact.Summary
	mechanicalLabel.Assessment.Status = agenticEvidenceInconclusive
	if agenticMemoryEpisodeOutcome(oracleLabel, memory[0].RiskStatus) !=
		agenticMemoryEpisodeOutcome(mechanicalLabel, memory[0].RiskStatus) {
		t.Fatal("Oracle-derived assessment changed Agent-facing episode outcome")
	}
	abandonedEpisode := recoveredArtifact
	abandonedEpisode.Summary.ScenarioStopReason = controlexperiment.ScenarioAgentStopHypothesisAbandoned
	abandonedMemory, err := deriveAgenticExplorationMemory([]recoveredAgenticEpisode{abandonedEpisode})
	if err != nil || len(abandonedMemory) != 1 ||
		abandonedMemory[0].EpisodeOutcome != controlexperiment.RiskMemoryOutcomeHypothesisAbandoned ||
		len(abandonedMemory[0].MechanicalReasonCodes) != 2 ||
		abandonedMemory[0].MechanicalReasonCodes[1] != controlexperiment.ScenarioAgentStopHypothesisAbandoned {
		t.Fatalf("hypothesis abandonment was not exposed as mechanical Memory: %#v/%v", abandonedMemory, err)
	}
	if _, terminal, err := recoverAgenticEpisodeArtifacts(
		t.TempDir(), omnipaxosAgenticEpisodeRecoveryBinding(),
	); err != nil || terminal {
		t.Fatalf("empty partial directory was not resumable: terminal=%t err=%v", terminal, err)
	}
	summaryBytes, err := os.ReadFile(filepath.Join(artifactDirectory, agenticEpisodeSummaryFile))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(summaryBytes, []byte("\"trace\"")) ||
		bytes.Contains(summaryBytes, []byte("\"execution_bundle\"")) {
		t.Fatalf("compact summary duplicated execution evidence: %s", summaryBytes)
	}
	tamperedDirectory := t.TempDir()
	if _, err := persistAgenticEpisodeArtifacts(
		tamperedDirectory, "omnipaxos-v2", budget, result,
	); err != nil {
		t.Fatal(err)
	}
	var tampered agenticEpisodeArtifact
	if err := readStrictJSONFile(
		filepath.Join(tamperedDirectory, agenticEpisodeSummaryFile), 512<<10, &tampered,
	); err != nil {
		t.Fatal(err)
	}
	tampered.Metrics.WitnessInstantiated = false
	encoded, err := json.MarshalIndent(tampered, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(tamperedDirectory, agenticEpisodeSummaryFile), append(encoded, '\n'), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, terminal, err := recoverAgenticEpisodeArtifacts(
		tamperedDirectory, omnipaxosAgenticEpisodeRecoveryBinding(),
	); err == nil || terminal {
		t.Fatal("tampered terminal summary was accepted")
	}
	recoveredRisk, err := recoverStatelessAgentCallJournal(filepath.Join(directory, "risk"), client)
	if err != nil {
		t.Fatal(err)
	}
	recoveredScenario, err := recoverScenarioAgentCallJournal(filepath.Join(directory, "scenario"), client)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := runOmnipaxosAgenticEpisode(
		ctx, inputs, recoveredRisk, recoveredScenario, budget,
		func() error {
			keyActivations++
			return recoveredRisk.ActivateKey("unexpected-key")
		},
		func() error {
			keyActivations++
			return recoveredScenario.ActivateKey("unexpected-key")
		},
	)
	if err != nil || recovered.Status != result.Status || recovered.Metrics != result.Metrics ||
		recovered.Work.Model != result.Work.Model || recovered.Testing == nil ||
		recovered.Testing.Bundle.Trace.Digest != result.Testing.Bundle.Trace.Digest ||
		providerCalls != 2 || keyActivations != 2 {
		t.Fatalf("dual-Agent recovery repeated a model call or drifted: %#v calls=%d keys=%d err=%v",
			recovered, providerCalls, keyActivations, err)
	}
	limitedRisk, err := newStatelessAgentCallJournal(filepath.Join(directory, "limited-risk"), client, "")
	if err != nil {
		t.Fatal(err)
	}
	limitedScenario, err := newScenarioAgentCallJournal(filepath.Join(directory, "limited-scenario"), client, "")
	if err != nil {
		t.Fatal(err)
	}
	limitedBudget := budget
	limitedBudget.MaxObservedTokens = 13
	limited, err := runOmnipaxosAgenticEpisode(
		ctx, inputs, limitedRisk, limitedScenario, limitedBudget,
		func() error {
			keyActivations++
			return limitedRisk.ActivateKey("fixture-key")
		},
		func() error {
			keyActivations++
			return limitedScenario.ActivateKey("fixture-key")
		},
	)
	if err != nil || limited.Status != agenticEpisodeTokenStopped || limited.Testing == nil ||
		!limited.Testing.Replay.Stable ||
		!limited.Metrics.CandidateAccepted || limited.Metrics.WitnessInstantiated ||
		limited.Work.Model.TotalTokens != 14 || providerCalls != 4 || keyActivations != 4 ||
		limited.Assessment.Status != agenticEvidenceBudgetExhausted || !limited.Metrics.OracleEvaluated {
		t.Fatalf("token threshold did not preserve automatic setup evidence: %#v calls=%d keys=%d err=%v",
			limited, providerCalls, keyActivations, err)
	}
	sealedRisk, err := newStatelessAgentCallJournal(filepath.Join(directory, "sealed-risk"), client, "")
	if err != nil {
		t.Fatal(err)
	}
	sealedScenario, err := newScenarioAgentCallJournal(filepath.Join(directory, "sealed-scenario"), client, "")
	if err != nil {
		t.Fatal(err)
	}
	sealedBudget := budget
	sealedBudget.MaxObservedTokens = 15
	sealed, err := runOmnipaxosAgenticEpisode(
		ctx, inputs, sealedRisk, sealedScenario, sealedBudget,
		func() error {
			keyActivations++
			return sealedRisk.ActivateKey("fixture-key")
		},
		func() error {
			keyActivations++
			return sealedScenario.ActivateKey("fixture-key")
		},
	)
	if err != nil || sealed.Status != agenticEpisodeCompleted || sealed.Testing == nil ||
		!sealed.Testing.Replay.Stable || !sealed.Metrics.OracleEvaluated ||
		sealed.Work.Model.TotalTokens != 14 || sealed.Assessment.Status != agenticEvidenceWitnessInstantiated ||
		len(sealed.ScenarioProviderCalls) != 1 {
		t.Fatalf("one strategic call did not seal the completed execution evidence: %#v err=%v", sealed, err)
	}
	sealedDirectory := t.TempDir()
	if _, err := persistAgenticEpisodeArtifacts(
		sealedDirectory, "omnipaxos-v2", sealedBudget, sealed,
	); err != nil {
		t.Fatal(err)
	}
	recoveredSealed, terminal, err := recoverAgenticEpisodeArtifacts(
		sealedDirectory, omnipaxosAgenticEpisodeRecoveryBinding(),
	)
	if err != nil || !terminal || recoveredSealed.Testing == nil ||
		!recoveredSealed.Summary.Metrics.OracleEvaluated ||
		recoveredSealed.Summary.Status != agenticEpisodeCompleted {
		t.Fatalf("sealed token-stop evidence was not durably recoverable: %#v terminal=%t err=%v",
			recoveredSealed, terminal, err)
	}
	tokenInvestigationDirectory := t.TempDir()
	tokenEpisodeDirectory := filepath.Join(tokenInvestigationDirectory, "episode-0001")
	if err := os.Mkdir(tokenEpisodeDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	limitedArtifact, err := persistAgenticEpisodeArtifacts(
		tokenEpisodeDirectory, "omnipaxos-v2", limitedBudget, limited,
	)
	if err != nil || limitedArtifact.ScenarioAttempts != 0 ||
		len(limitedArtifact.ScenarioProviderCalls) != 1 {
		t.Fatalf("charged token-boundary call was not persisted: %#v err=%v", limitedArtifact, err)
	}
	queuedAfterTokenStop, err := nextAgenticPortfolioRisk([]recoveredAgenticEpisode{{
		Summary: limitedArtifact,
	}})
	if err != nil || queuedAfterTokenStop == nil ||
		queuedAfterTokenStop.Candidate.ID != limitedArtifact.Accepted.Candidate.ID {
		t.Fatalf("accepted candidate with no Scenario attempt was skipped: %#v err=%v",
			queuedAfterTokenStop, err)
	}
	runnerTarget, err := newOmnipaxosAgenticEpisodeTarget(inputs)
	if err != nil {
		t.Fatal(err)
	}
	tokenPrepareCalls := 0
	tokenKeyReads := 0
	tokenStoppedInvestigation, err := runAgenticInvestigation(ctx, agenticInvestigationOptions{
		Directory: tokenInvestigationDirectory, Resume: true,
		AgentKeyFile: "fixture-key.txt", ReadKey: func(string) (string, error) {
			tokenKeyReads++
			return "fixture-key", nil
		},
		Recovery: omnipaxosAgenticEpisodeRecoveryBinding(),
		Budget: agenticInvestigationBudget{
			MaxEpisodes: 2, MaxModelCalls: 8, MaxModelTokens: 40,
			MaxRuntimeDecisionAllowance: 2 * limitedBudget.MaxRuntimeDecisions,
		},
		Prepare: func(context.Context) (agenticEpisodeComposition, error) {
			tokenPrepareCalls++
			return agenticEpisodeComposition{
				Target: runnerTarget, Budget: limitedBudget, Client: client,
				ScenarioClient: client, SessionWallClockMS: 60_000,
			}, nil
		},
	})
	if err != nil || tokenStoppedInvestigation.StopReason != agenticInvestigationEpisodeLimit ||
		len(tokenStoppedInvestigation.Episodes) != 2 || tokenPrepareCalls != 1 || tokenKeyReads == 0 ||
		tokenStoppedInvestigation.Episodes[1].Summary.Accepted == nil ||
		tokenStoppedInvestigation.Episodes[1].Summary.Accepted.Candidate.ID !=
			limitedArtifact.Accepted.Candidate.ID ||
		tokenStoppedInvestigation.Episodes[1].Summary.ScenarioAttempts == 0 {
		t.Fatalf("token-stopped candidate was not continued: %#v prepare=%d keys=%d err=%v",
			tokenStoppedInvestigation, tokenPrepareCalls, tokenKeyReads, err)
	}
	runnerDirectory := filepath.Join(t.TempDir(), "agentic-episode")
	keyReads := 0
	prepareCalls := 0
	providerCallsBeforeRunner := providerCalls
	runner, err := runAgenticEpisodeDirectory(ctx, agenticEpisodeDirectoryOptions{
		Directory: runnerDirectory, AgentKeyFile: "fixture-key.txt",
		ReadKey: func(string) (string, error) {
			keyReads++
			return "fixture-key", nil
		},
		Recovery: omnipaxosAgenticEpisodeRecoveryBinding(),
		Prepare: func(context.Context) (agenticEpisodeComposition, error) {
			prepareCalls++
			return agenticEpisodeComposition{
				Target: runnerTarget, Budget: budget, Client: client, SessionWallClockMS: 60_000,
			}, nil
		},
	})
	if err != nil || runner.Summary.Status != agenticEpisodeCompleted || runner.Testing == nil ||
		providerCalls != providerCallsBeforeRunner+2 || keyReads != 2 || prepareCalls != 1 {
		t.Fatalf("directory runner incomplete: %#v calls=%d keys=%d prepare=%d err=%v",
			runner, providerCalls, keyReads, prepareCalls, err)
	}
	terminalRunner, err := runAgenticEpisodeDirectory(ctx, agenticEpisodeDirectoryOptions{
		Directory: runnerDirectory, Resume: true,
		Recovery: omnipaxosAgenticEpisodeRecoveryBinding(),
	})
	if err != nil || terminalRunner.Testing == nil || terminalRunner.Summary.Status != agenticEpisodeCompleted ||
		providerCalls != providerCallsBeforeRunner+2 || keyReads != 2 || prepareCalls != 1 {
		t.Fatalf("terminal runner accessed active inputs: %#v calls=%d keys=%d prepare=%d err=%v",
			terminalRunner, providerCalls, keyReads, prepareCalls, err)
	}
	var terminalOutput bytes.Buffer
	if err := run(ctx, []string{
		"-strategy", agenticEpisodeStrategy,
		"-target", "omnipaxos-v2",
		"-campaign-dir", runnerDirectory,
		"-campaign-resume",
	}, &terminalOutput); err != nil ||
		!bytes.Contains(terminalOutput.Bytes(), []byte("target=omnipaxos-v2 status=completed")) ||
		providerCalls != providerCallsBeforeRunner+2 || keyReads != 2 || prepareCalls != 1 {
		t.Fatalf("terminal CLI recovery failed or accessed active inputs: %q err=%v",
			terminalOutput.String(), err)
	}
	investigationPrepareCalls := 0
	investigationDirectory := filepath.Join(t.TempDir(), "agentic-investigation")
	investigation, err := runAgenticInvestigation(ctx, agenticInvestigationOptions{
		Directory:    investigationDirectory,
		AgentKeyFile: "fixture-key.txt",
		ReadKey: func(string) (string, error) {
			keyReads++
			return "fixture-key", nil
		},
		Recovery: omnipaxosAgenticEpisodeRecoveryBinding(),
		Budget: agenticInvestigationBudget{
			MaxEpisodes: 2, MaxModelCalls: 2 * budget.MaxTotalCalls,
			MaxModelTokens:              2*budget.MaxObservedTokens + 1,
			MaxRuntimeDecisionAllowance: 2 * budget.MaxRuntimeDecisions,
		},
		Prepare: func(context.Context) (agenticEpisodeComposition, error) {
			investigationPrepareCalls++
			return agenticEpisodeComposition{
				Target: runnerTarget, Budget: budget, Client: client, SessionWallClockMS: 60_000,
			}, nil
		},
	})
	if err != nil || investigation.StopReason != agenticInvestigationEpisodeLimit ||
		len(investigation.Episodes) != 2 || len(investigation.ExplorationMemory) != 2 ||
		investigation.ModelWork.Calls != 4 || investigation.ModelWork.TotalTokens != 28 ||
		investigation.ReservedDecisionAllowance != 2*budget.MaxRuntimeDecisions ||
		investigation.ConsumedScenarioDecisions != investigationScenarioDecisions(investigation.Episodes) ||
		!investigation.ExplorationMemory[1].RepeatedCandidate ||
		memoryRiskRequests != 1 || providerCalls != providerCallsBeforeRunner+6 || keyReads != 6 || investigationPrepareCalls != 1 {
		t.Fatalf("two-round Investigation did not pass recovered Memory: %#v calls=%d keys=%d memory=%d prepare=%d err=%v",
			investigation, providerCalls, keyReads, memoryRiskRequests, investigationPrepareCalls, err)
	}
	resumePrepareCalls := 0
	resumedInvestigation, err := runAgenticInvestigation(ctx, agenticInvestigationOptions{
		Directory: investigationDirectory, Resume: true,
		AgentKeyFile: "fixture-key.txt",
		ReadKey: func(string) (string, error) {
			keyReads++
			return "fixture-key", nil
		},
		Recovery: omnipaxosAgenticEpisodeRecoveryBinding(),
		Budget: agenticInvestigationBudget{
			MaxEpisodes: 3, MaxModelCalls: 3 * budget.MaxTotalCalls,
			MaxModelTokens:              3*budget.MaxObservedTokens + 1,
			MaxRuntimeDecisionAllowance: 3 * budget.MaxRuntimeDecisions,
		},
		Prepare: func(context.Context) (agenticEpisodeComposition, error) {
			resumePrepareCalls++
			return agenticEpisodeComposition{
				Target: runnerTarget, Budget: budget, Client: client, SessionWallClockMS: 60_000,
			}, nil
		},
	})
	if err != nil || resumedInvestigation.StopReason != agenticInvestigationEpisodeLimit ||
		len(resumedInvestigation.Episodes) != 3 || len(resumedInvestigation.ExplorationMemory) != 3 ||
		resumedInvestigation.ModelWork.Calls != 6 || resumedInvestigation.ModelWork.TotalTokens != 42 ||
		resumedInvestigation.ReservedDecisionAllowance != 3*budget.MaxRuntimeDecisions ||
		resumedInvestigation.ConsumedScenarioDecisions != investigationScenarioDecisions(resumedInvestigation.Episodes) ||
		!resumedInvestigation.ExplorationMemory[2].RepeatedCandidate ||
		memoryRiskRequests != 2 || providerCalls != providerCallsBeforeRunner+8 || keyReads != 8 || resumePrepareCalls != 1 {
		t.Fatalf("Investigation resume repeated old episodes or lost Memory: %#v calls=%d keys=%d memory=%d prepare=%d err=%v",
			resumedInvestigation, providerCalls, keyReads, memoryRiskRequests, resumePrepareCalls, err)
	}
	budgetStopped, err := runAgenticInvestigation(ctx, agenticInvestigationOptions{
		Directory:    filepath.Join(t.TempDir(), "budget-stopped-investigation"),
		AgentKeyFile: "fixture-key.txt", ReadKey: func(string) (string, error) {
			keyReads++
			return "fixture-key", nil
		},
		Recovery: omnipaxosAgenticEpisodeRecoveryBinding(),
		Budget: agenticInvestigationBudget{
			MaxEpisodes: 2, MaxModelCalls: budget.MaxTotalCalls - 1,
			MaxModelTokens:              budget.MaxObservedTokens,
			MaxRuntimeDecisionAllowance: budget.MaxRuntimeDecisions,
		},
		Prepare: func(context.Context) (agenticEpisodeComposition, error) {
			return agenticEpisodeComposition{
				Target: runnerTarget, Budget: budget, Client: client, SessionWallClockMS: 60_000,
			}, nil
		},
	})
	if err != nil || budgetStopped.StopReason != agenticInvestigationCallLimit ||
		len(budgetStopped.Episodes) != 0 || providerCalls != providerCallsBeforeRunner+8 || keyReads != 8 {
		t.Fatalf("Investigation started an episode without its declared call allowance: %#v err=%v",
			budgetStopped, err)
	}
}

func scenarioPromptFeedbackAllowsAbandon(prior *controlexperiment.ScenarioAgentFeedback) bool {
	if prior == nil || prior.ProgressDelta == nil ||
		prior.ProgressDelta.MilestoneProgress == controlexperiment.ScenarioMilestoneProgressAdvanced ||
		prior.ProgressDelta.MilestoneProgress == controlexperiment.ScenarioMilestoneProgressInstantiated {
		return false
	}
	return prior.ProgressDelta.NaturalProgressStop != controlexperiment.ScenarioProgressSlice &&
		prior.ProgressDelta.NaturalProgressStop != controlexperiment.ScenarioProgressBudget
}

func TestA9e3aAgenticEpisodePersistsTerminalExecutionOutcome(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(180*time.Second))
	defer cancel()
	workerPath := buildOmnipaxosScenarioWorker(t)
	inputs, err := prepareOmnipaxosAgenticEpisode(
		ctx, workerPath, "../../plans/agent/omnipaxos-agentic-calibration-v1.json",
	)
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) {
		return omnipaxosv2.New(inputs.Experiment.adapterConfig(workerPath))
	}
	workloadRoot, err := buildWorkloadReadyRootForTest(
		ctx, inputs.Root, inputs.Experiment.Runtime, factory,
		omnipaxosv2.WorkloadRouter{}, inputs.Workload, firstOmnipaxosScenarioProgress,
	)
	if err != nil {
		t.Fatal(err)
	}
	target, err := newOmnipaxosAgenticEpisodeTarget(inputs)
	if err != nil {
		t.Fatal(err)
	}
	originalScenarioInputs := target.ScenarioInputs
	target.ScenarioInputs = func(
		risk controlexperiment.ScenarioRiskHypothesis,
		projector controlexperiment.SemanticPrefixProjector,
	) (scenarioEpisodeCoreInputs, error) {
		core, err := originalScenarioInputs(risk, projector)
		if err != nil {
			return scenarioEpisodeCoreInputs{}, err
		}
		core.Root = workloadRoot
		baseFactory := core.NewAdapter
		core.NewAdapter = func() (control.Adapter, error) {
			base, err := baseFactory()
			if err != nil {
				return nil, err
			}
			return &failAfterInitialYieldAdapter{Adapter: base}, nil
		}
		return core, nil
	}
	candidateBytes, err := json.Marshal(omnipaxosDiscoveredRiskPortfolio())
	if err != nil {
		t.Fatal(err)
	}
	providerCalls := 0
	client := fixtureOpenRouterIntentClient()
	client.HTTP = agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
		providerCalls++
		var payload openRouterChatRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.ResponseFormat.JSONSchema.Name != "risk_candidate_portfolio" {
			t.Fatal("Scenario Agent was called after terminal reconstruction failure")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(bytes.NewReader(fixtureOpenRouterResponse(
				t, providerCalls, candidateBytes,
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
	budget := agenticEpisodeBudget{
		MaxRiskCalls: 1, MaxScenarioCalls: 1, MaxTotalCalls: 2, MaxObservedTokens: 20,
		MaxScenarioPlanSteps: 1, MaxRuntimeDecisions: inputs.Experiment.ScenarioMaxDecisions,
	}
	result, err := runAgenticEpisode(
		ctx, target, riskJournal, scenarioJournal, budget, nil, nil, nil,
		func() error { return riskJournal.ActivateKey("fixture-key") },
		func() error { return scenarioJournal.ActivateKey("fixture-key") },
	)
	if err != nil || result.Status != agenticEpisodeExecutionFailed || result.Failure == nil ||
		result.Failure.Terminal == nil || result.Failure.Terminal.Validate() != nil ||
		result.Failure.Code != "ADAPTER_ACTION_FAILED" || result.Failure.Decision <= 0 ||
		result.Testing != nil || providerCalls != 1 || len(result.RiskProviderCalls) != 1 ||
		len(result.ScenarioProviderCalls) != 0 || result.Work.ScenarioFrontier.WorkUnits == 0 {
		t.Fatalf("terminal Agent episode was not preserved: %#v calls=%d err=%v", result, providerCalls, err)
	}
	artifactDirectory := t.TempDir()
	artifact, err := persistAgenticEpisodeArtifacts(
		artifactDirectory, target.ID, budget, result,
	)
	if err != nil {
		t.Fatal(err)
	}
	recovered, terminal, err := recoverAgenticEpisodeArtifacts(
		artifactDirectory, omnipaxosAgenticEpisodeRecoveryBinding(),
	)
	if err != nil || !terminal || recovered.Testing != nil || artifact.Failure == nil ||
		recovered.Summary.Failure == nil ||
		recovered.Summary.Failure.Terminal.Digest != result.Failure.Terminal.Digest {
		t.Fatalf("terminal Agent artifact did not recover: %#v terminal=%t err=%v",
			recovered, terminal, err)
	}
}

func a4bScenarioViewFromPayload(
	t *testing.T,
	payload openRouterChatRequest,
) controlexperiment.ScenarioAgentView {
	t.Helper()
	var prompt struct {
		AgentView struct {
			AcceptedHypothesis *controlexperiment.AcceptedHypothesisContext `json:"accepted_hypothesis"`
			TargetSurface      *controlexperiment.AgentTargetSurface        `json:"target_surface"`
			AvailableIntents   []string                                     `json:"available_intents"`
			Prior              *controlexperiment.ScenarioAgentFeedback     `json:"prior_feedback,omitempty"`
			ActionFrontier     scenarioPromptFrontier                       `json:"action_frontier"`
		} `json:"agent_view"`
	}
	if len(payload.Messages) != 2 {
		t.Fatal("Scenario prompt has unexpected message count")
	}
	marker := "Frozen input JSON:\n"
	index := bytes.Index([]byte(payload.Messages[1].Content), []byte(marker))
	if index < 0 {
		t.Fatal("Scenario prompt has no frozen input marker")
	}
	if err := json.Unmarshal([]byte(payload.Messages[1].Content)[index+len(marker):], &prompt); err != nil {
		t.Fatal(err)
	}
	if prompt.AgentView.ActionFrontier.ID == "" {
		t.Fatalf("Scenario prompt projection omitted action_frontier: %s", payload.Messages[1].Content)
	}
	frontier := controlexperiment.RiskFrontierView{
		SchemaVersion:        prompt.AgentView.ActionFrontier.SchemaVersion,
		ID:                   prompt.AgentView.ActionFrontier.ID,
		Progress:             prompt.AgentView.ActionFrontier.Progress,
		PrefixDecisions:      prompt.AgentView.ActionFrontier.PrefixDecisions,
		NextDecision:         prompt.AgentView.ActionFrontier.NextDecision,
		PrefixTraceDigest:    prompt.AgentView.ActionFrontier.PrefixTraceDigest,
		SnapshotDigest:       prompt.AgentView.ActionFrontier.SnapshotDigest,
		RuntimeEnabledDigest: prompt.AgentView.ActionFrontier.RuntimeEnabledDigest,
		AdmissibleDigest:     prompt.AgentView.ActionFrontier.AdmissibleDigest,
		RuntimeActionCount:   prompt.AgentView.ActionFrontier.RuntimeActionCount,
		Digest:               prompt.AgentView.ActionFrontier.Digest,
	}
	for _, action := range prompt.AgentView.ActionFrontier.Actions {
		frontier.Actions = append(frontier.Actions, action.FrontierActionRef)
	}
	return controlexperiment.ScenarioAgentView{
		AcceptedHypothesis: prompt.AgentView.AcceptedHypothesis,
		TargetSurface:      prompt.AgentView.TargetSurface,
		AvailableIntents:   append([]string(nil), prompt.AgentView.AvailableIntents...),
		Prior:              prompt.AgentView.Prior,
		Frontier:           frontier,
	}
}

func riskAgentViewFromPayload(
	t *testing.T,
	payload openRouterChatRequest,
) controlexperiment.RiskAgentView {
	t.Helper()
	if len(payload.Messages) != 2 {
		t.Fatal("Risk prompt has unexpected message count")
	}
	marker := []byte("Input JSON:\n")
	content := []byte(payload.Messages[1].Content)
	index := bytes.Index(content, marker)
	if index < 0 {
		t.Fatal("Risk prompt has no input marker")
	}
	var view controlexperiment.RiskAgentView
	if err := json.Unmarshal(content[index+len(marker):], &view); err != nil {
		t.Fatal(err)
	}
	return view
}
