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
	"strings"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
	"github.com/SuzumiyaHaruki/consensus-atlas/targetoracles"
)

func TestFixedScenarioCallBudgetDoesNotDependOnRiskUsage(t *testing.T) {
	logical := controlexperiment.AgenticLogicalBudget{
		MaxAttempts: 1, MaxPrimarySchedulerDecisions: 16384,
		MaxPrimaryWorkUnits: 32768, MaxReplayWorkUnits: 8192,
		MaxModelCalls: 12, MaxModelTokens: 240000,
	}
	budget, err := agenticEpisodeBudgetFromExperiment(8, 1, 64, logical)
	if err != nil || budget.MaxRiskCalls != 4 || budget.MaxScenarioCalls != 8 ||
		budget.MaxTotalCalls != 12 || budget.MaxObservedTokens != 240000 {
		t.Fatalf("fixed deep-investigation budget drifted: %#v/%v", budget, err)
	}
	for _, riskCalls := range []int{0, 1, 2, 3, 4} {
		allowance, allowanceErr := fixedScenarioCallAllowance(budget, riskCalls)
		if allowanceErr != nil || allowance != 8 {
			t.Fatalf("Risk usage %d changed the executed Scenario allowance: %d/%v",
				riskCalls, allowance, allowanceErr)
		}
	}
	if _, err := fixedScenarioCallAllowance(budget, 5); err == nil {
		t.Fatal("Risk work above its own allowance reached Scenario execution")
	}
}

func TestAgenticAssessmentSeparatesRootAndAgentOracleFindings(t *testing.T) {
	rootViolation := oracle.Violation{
		Monitor: targetoracles.ElectionSafetyMonitorID, Step: 12,
		Message: "leader was already invalid in the deterministic root",
	}
	agentViolation := oracle.Violation{
		Monitor: targetoracles.ElectionSafetyMonitorID, Step: 14,
		Message: "leader became invalid after the Agent boundary",
	}
	base := agenticEvidenceAssessment{
		Status: agenticEvidencePlanningFailed, ReasonCode: "scenario-not-executed",
	}
	rootOnly := scenarioTestingResult{
		Risk: semantic.RiskWitnessResult{
			Status: semantic.RiskWitnessNotReached, MissingMilestones: []string{"post-root"},
		},
		Oracle:            oracle.Result{Violations: []oracle.Violation{rootViolation}},
		OracleAttribution: &scenarioOracleAttribution{RootDecisions: 12},
	}
	rootAssessment := assessTestingEvidence(base, rootOnly, controlexperiment.ScenarioAgentResult{})
	if rootAssessment.Status == agenticEvidenceOracleFinding ||
		rootAssessment.RootPrefixFindings != 1 ||
		rootAssessment.ReasonCode != agenticEvidenceRootPrefixFinding {
		t.Fatalf("root finding was attributed to the Agent: %#v", rootAssessment)
	}
	providerAssessment := assessTestingEvidence(base, rootOnly, controlexperiment.ScenarioAgentResult{
		StopReason: controlexperiment.ScenarioAgentStopProviderResponse,
	})
	if providerAssessment.Status != agenticEvidenceInconclusive ||
		providerAssessment.ReasonCode != controlexperiment.ScenarioAgentStopProviderResponse ||
		providerAssessment.RootPrefixFindings != 1 {
		t.Fatalf("provider failure did not outrank hypothesis non-reach: %#v", providerAssessment)
	}
	witness := rootOnly
	witness.Risk.Status = semantic.RiskWitnessReached
	witness.Risk.MissingMilestones = nil
	witnessAssessment := assessTestingEvidence(base, witness, controlexperiment.ScenarioAgentResult{
		StopReason: controlexperiment.ScenarioAgentStopProviderResponse,
	})
	if witnessAssessment.Status != agenticEvidenceWitnessUnverified ||
		witnessAssessment.ReasonCode != "missing-property-oracle" {
		t.Fatalf("provider failure hid an instantiated witness: %#v", witnessAssessment)
	}
	agentPath := rootOnly
	agentPath.Oracle.Violations = append(agentPath.Oracle.Violations, agentViolation)
	agentPath.OracleAttribution = &scenarioOracleAttribution{RootDecisions: 12}
	agentAssessment := assessTestingEvidence(base, agentPath, controlexperiment.ScenarioAgentResult{
		StopReason: controlexperiment.ScenarioAgentStopProviderResponse,
	})
	if agentAssessment.Status != agenticEvidenceOracleFinding ||
		agentAssessment.ReasonCode != targetoracles.ElectionSafetyMonitorID ||
		agentAssessment.RootPrefixFindings != 1 {
		t.Fatalf("post-root finding did not outrank provider failure: %#v", agentAssessment)
	}
}

func TestScenarioResponseFailureKeepsClassificationAtTokenBoundary(t *testing.T) {
	work := controlexperiment.ModelWork{Calls: 1, InputTokens: 8, OutputTokens: 5, TotalTokens: 13}
	content, keptWork, err := agenticScenarioTokenBoundary(
		101, 100, nil, work, &controlexperiment.ScenarioPlannerResponseFailure{
			Code: controlexperiment.ScenarioAgentReasonResponseFinishLength, Repairable: true,
		},
	)
	var failure *controlexperiment.ScenarioPlannerResponseFailure
	if len(content) != 0 || keptWork != work || !errors.As(err, &failure) ||
		failure.Code != controlexperiment.ScenarioAgentReasonResponseFinishLength || failure.Repairable {
		t.Fatalf("token threshold replaced the charged response failure: %q/%#v/%v", content, keptWork, err)
	}
	if _, _, err := agenticScenarioTokenBoundary(101, 100, []byte(`{}`), work, nil); !errors.Is(err, errAgenticEpisodeTokenThreshold) {
		t.Fatalf("successful over-budget response did not retain the ordinary threshold stop: %v", err)
	}
}

func TestAgenticEpisodeKeepsUnknownProviderUsageOutsideSealedSummary(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(180*time.Second))
	defer cancel()
	composition, err := prepareAgenticEpisodeComposition(ctx, controlExperimentOptions{
		Target: "etcdraft-v2", InvestigationEpisodes: 1,
		SemanticInput: "../../plans/agent/etcdraft-agentic-calibration-v1.json",
		AgentKeyFile:  "fixture-key.txt", AgentProvider: deepSeekProvider,
		AgentModel: deepSeekDefaultModel,
	})
	if err != nil {
		t.Fatal(err)
	}
	client := newDeepSeekIntentClient(deepSeekDefaultModel)
	client.HTTP = agentHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	composition.Client = client
	directory := filepath.Join(t.TempDir(), "unknown-provider-usage")
	recovered, err := runAgenticEpisodeDirectory(ctx, agenticEpisodeDirectoryOptions{
		Directory: directory, AgentKeyFile: "fixture-key.txt",
		ReadKey:  func(string) (string, error) { return "fixture-key", nil },
		Recovery: etcdraftAgenticEpisodeRecoveryBinding(),
		Prepare:  func(context.Context) (agenticEpisodeComposition, error) { return composition, nil },
	})
	if err == nil || err.Error() != "AGENTIC_EPISODE_PROVIDER_USAGE_UNRECONCILED" ||
		recovered.UnreconciledModelCalls != 1 || recovered.Summary.TargetID != "" {
		t.Fatalf("unknown provider usage entered a sealed Episode: %#v/%v", recovered, err)
	}
	if _, statErr := os.Stat(filepath.Join(directory, agenticEpisodeSummaryFile)); !os.IsNotExist(statErr) {
		t.Fatalf("unreconciled call produced a trusted summary: %v", statErr)
	}
}

func TestEtcdraftBindingUsesCommonAgenticEpisodeContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(180*time.Second))
	defer cancel()
	inputs, err := prepareEtcdraftAgenticEpisode(
		ctx, "", "../../plans/agent/etcdraft-agentic-calibration-v1.json",
		fixtureOpenRouterIntentClient(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if majority := agenticKnowledgeText(inputs.knowledge, "raft-majority-and-voting"); !strings.Contains(majority, "Q=floor(n/2)+1") || strings.Contains(majority, "majority is 3") {
		t.Fatalf("Raft knowledge lost topology-derived quorum semantics: %q", majority)
	}
	if faultModel := agenticKnowledgeText(inputs.knowledge, "cft-fault-model"); !strings.Contains(faultModel, "asymmetric loss") || !strings.Contains(faultModel, "may not forge") {
		t.Fatalf("Raft knowledge lost CFT fault boundary: %q", faultModel)
	}
	target, err := newEtcdraftAgenticEpisodeTarget(inputs)
	if err != nil || target.validate() != nil || target.ID != "etcdraft-v2" ||
		target.ObservationProjector.ID() != etcdraftv2.ObservationProjectionID ||
		len(target.ObservationProjector.Capabilities()) == 0 ||
		len(target.Surface.Capabilities.ComposableActions) == 0 ||
		target.ScenarioInputs == nil || target.Execute == nil {
		t.Fatalf("etcd/raft did not satisfy common Agentic Episode contract: %#v err=%v", target, err)
	}
	if len(target.Surface.Nodes) != 3 || len(target.Surface.Workload.Invocations) != 1 ||
		!bytes.Contains(target.Surface.Workload.Invocations[0].InputJSON, []byte("propose")) ||
		target.Surface.FaultAllowance.MaxCrashes != 1 ||
		len(target.Surface.TemporalKinds) == 0 || !target.Surface.Runtime.StrictReplay ||
		len(target.Surface.Capabilities.DeclaredActions) != 10 ||
		len(target.Surface.Capabilities.ComposableActions) != 10 ||
		len(target.Surface.Capabilities.ObservationCapabilities) == 0 ||
		len(target.Surface.Capabilities.OracleCapabilities) != 5 ||
		len(target.Surface.Capabilities.FidelityBoundaries) != 1 {
		t.Fatalf("etcd/raft dynamic Agent surface was not derived from active inputs: %#v", target.Surface)
	}
	composition, err := prepareAgenticEpisodeComposition(ctx, controlExperimentOptions{
		Target: "etcdraft-v2", InvestigationEpisodes: 1,
		SemanticInput: "../../plans/agent/etcdraft-agentic-calibration-v1.json",
		AgentKeyFile:  "fixture-key.txt", AgentModel: openRouterFixtureModel,
	})
	if err != nil {
		t.Fatal(err)
	}
	transport := composition.Client.freeze()
	if composition.Target.ID != "etcdraft-v2" ||
		composition.MethodSpec.Validate() != nil ||
		composition.Target.MethodSpecDigest != composition.MethodSpec.Digest ||
		composition.MethodSpec.InvestigationEpisodes != 1 ||
		composition.MethodSpec.Transport.Model != openRouterFixtureModel ||
		composition.MethodSpec.SemanticInputSchema != etcdraftSemanticInputSchema ||
		composition.MethodSpec.ClosureMode != controlexperiment.AgenticClosureModePublicFixed ||
		composition.MethodSpec.RiskInputMode != controlexperiment.AgenticRiskInputAgentGenerated ||
		composition.MethodSpec.RiskInputDigest != "" || composition.ExistingRisk != nil ||
		composition.MethodSpec.CapabilityFeedbackMode != controlexperiment.AgenticCapabilityFeedbackStructuredGaps ||
		composition.CapabilityFeedbackMode != controlexperiment.AgenticCapabilityFeedbackStructuredGaps ||
		composition.MethodSpec.EpisodeLimits.MaxRiskCalls != composition.Budget.MaxRiskCalls ||
		composition.MethodSpec.EpisodeLimits.MaxScenarioCalls != composition.Budget.MaxScenarioCalls ||
		composition.MethodSpec.EpisodeLimits.MaxScenarioPlanSteps != composition.Budget.MaxScenarioPlanSteps ||
		composition.Budget.Logical == nil ||
		*composition.Budget.Logical != inputs.experiment.SessionBudget ||
		composition.Budget.MaxRiskCalls != 4 || composition.Budget.MaxScenarioCalls != 8 ||
		composition.Budget.MaxScenarioPlanSteps != 1 ||
		composition.Budget.MaxRuntimeDecisions != 64 ||
		composition.Budget.MaxTotalCalls != 12 || composition.Budget.MaxObservedTokens != 240000 ||
		transport.Model != openRouterFixtureModel || transport.Thinking != "high" ||
		transport.MaxOutputTokens != 64000 || transport.MaxRetries != inputs.experiment.ModelMaxRetries {
		t.Fatalf("etcd/raft registry composition drifted: %#v err=%v", composition, err)
	}
	existingOptions := controlExperimentOptions{
		Target: "etcdraft-v2", InvestigationEpisodes: 1,
		SemanticInput: "../../plans/agent/etcdraft-agentic-calibration-v1.json",
		AgentKeyFile:  "fixture-key.txt", AgentModel: openRouterFixtureModel,
		RiskInput: "../../plans/agent/etcdraft-alternate-quorum-risk-v1.json",
	}
	existingPublic, err := prepareAgenticEpisodeComposition(ctx, existingOptions)
	if err != nil || existingPublic.ExistingRisk == nil ||
		existingPublic.ExistingRisk.Candidate.ID != "append-response-loss-with-alternate-quorum" ||
		existingPublic.MethodSpec.RiskInputMode != controlexperiment.AgenticRiskInputExistingCandidate ||
		!validAgenticSHA256(existingPublic.MethodSpec.RiskInputDigest) ||
		existingPublic.MethodSpec.RiskInputDigest != existingPublic.RiskInputDigest {
		t.Fatalf("existing Risk input was not mechanically bound: %#v/%v",
			existingPublic.MethodSpec, err)
	}
	multiEpisodeOptions := existingOptions
	multiEpisodeOptions.InvestigationEpisodes = 2
	multiEpisode, err := prepareAgenticEpisodeComposition(ctx, multiEpisodeOptions)
	if err != nil || multiEpisode.MethodSpec.InvestigationEpisodes != 2 ||
		multiEpisode.MethodSpec.RiskInputMode != controlexperiment.AgenticRiskInputExistingCandidate ||
		multiEpisode.RiskInputDigest != existingPublic.RiskInputDigest {
		t.Fatalf("existing Risk input did not support a multi-Episode Investigation: %#v/%v",
			multiEpisode.MethodSpec, err)
	}
	tamperedRiskInput := existingPublic
	tamperedRiskInput.RiskInputDigest = formalTestStringDigest("different-existing-risk")
	_, err = runAgenticEpisodeDirectory(ctx, agenticEpisodeDirectoryOptions{
		Directory:    filepath.Join(t.TempDir(), "fixed-risk-mismatch"),
		AgentKeyFile: "fixture-key.txt",
		ReadKey:      func(string) (string, error) { return "fixture-key", nil },
		Recovery:     etcdraftAgenticEpisodeRecoveryBinding(),
		Prepare: func(context.Context) (agenticEpisodeComposition, error) {
			return tamperedRiskInput, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "COMPOSITION_INVALID") {
		t.Fatalf("composition accepted existing Risk identity mismatch: %v", err)
	}
	tamperedRiskContent := existingPublic
	changedRisk := *existingPublic.ExistingRisk
	changedRisk.Candidate.Summary += " changed after loading"
	tamperedRiskContent.ExistingRisk = &changedRisk
	_, err = runAgenticEpisodeDirectory(ctx, agenticEpisodeDirectoryOptions{
		Directory:    filepath.Join(t.TempDir(), "fixed-risk-content-mismatch"),
		AgentKeyFile: "fixture-key.txt",
		ReadKey:      func(string) (string, error) { return "fixture-key", nil },
		Recovery:     etcdraftAgenticEpisodeRecoveryBinding(),
		Prepare: func(context.Context) (agenticEpisodeComposition, error) {
			return tamperedRiskContent, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "COMPOSITION_INVALID") {
		t.Fatalf("composition accepted substituted existing Risk content: %v", err)
	}
	mismatchOptions := controlExperimentOptions{
		Target: "etcdraft-v2", InvestigationEpisodes: 1,
		SemanticInput: "../../plans/agent/etcdraft-agentic-calibration-v1.json",
		AgentKeyFile:  "fixture-key.txt", AgentModel: openRouterFixtureModel,
		MethodSpecDigest: formalTestStringDigest("wrong-agentic-method"),
	}
	if _, err := prepareAgenticEpisodeComposition(ctx, mismatchOptions); err == nil {
		t.Fatal("caller-supplied method digest replaced the derived method identity")
	}
	reasonOptions := mismatchOptions
	reasonOptions.MethodSpecDigest = ""
	reasonOptions.CapabilityFeedbackMode = controlexperiment.AgenticCapabilityFeedbackReasonCodes
	reasonComposition, err := prepareAgenticEpisodeComposition(ctx, reasonOptions)
	if err != nil || reasonComposition.MethodSpec.CapabilityFeedbackMode !=
		controlexperiment.AgenticCapabilityFeedbackReasonCodes ||
		reasonComposition.MethodSpec.Digest == composition.MethodSpec.Digest {
		t.Fatalf("feedback mode was not bound to actual method identity: %#v/%v", reasonComposition.MethodSpec, err)
	}
	methodDirectory := filepath.Join(t.TempDir(), "episode")
	if err := bindAgenticMethodSpec(methodDirectory, false, composition.MethodSpec); err != nil {
		t.Fatal(err)
	}
	stored, err := readAgenticMethodSpec(methodDirectory, composition.MethodSpec.Digest)
	if err != nil || stored.Digest != composition.MethodSpec.Digest {
		t.Fatalf("persisted method identity = %#v/%v", stored, err)
	}
	tampered := composition.MethodSpec
	tampered.EpisodeLimits.MaxScenarioPlanSteps++
	if err := bindAgenticMethodSpec(methodDirectory, true, tampered); err == nil {
		t.Fatal("resume accepted changed method limits")
	}
	investigation, err := agenticInvestigationBudgetFromEpisode(6, composition.Budget)
	if err != nil || investigation.MaxEpisodes != 6 ||
		investigation.MaxModelCalls != 72 || investigation.MaxModelTokens != 1_440_000 ||
		investigation.MaxRuntimeDecisionAllowance != 384 {
		t.Fatalf("Investigation budget did not scale the existing episode contract: %#v/%v",
			investigation, err)
	}
}

func agenticKnowledgeText(
	knowledge controlexperiment.ProtocolKnowledgePack,
	id string,
) string {
	for _, statement := range knowledge.Knowledge {
		if statement.ID == id {
			return statement.Text
		}
	}
	return ""
}

func TestEtcdraftReadyAdvanceHypothesisReportsTargetFidelityGap(t *testing.T) {
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
	candidate := controlexperiment.RiskCandidate{
		ID:          "ready-visibility-before-durability",
		PropertyRef: "client-operation-continuity", InspirationRef: "visibility-before-durability",
		Summary:          "Investigate a crash between client-visible progress and durable Ready completion.",
		RequiredFidelity: []string{"rawnode-host-persistence-atomicity"},
		MechanismSteps: []controlexperiment.RiskMechanismStep{
			{MilestoneID: "operation-started", Kind: semantic.ObservationWorkloadInvoked, Rationale: "Create an operation whose host persistence relation matters.", SupportRefs: []string{"primer/persistence-and-visibility"}},
			{MilestoneID: "host-crashed", Kind: semantic.ObservationNodeCrashed, Rationale: "Place a crash inside the suspected visibility and durability interval.", SupportRefs: []string{"primer/persistence-and-visibility"}},
			{MilestoneID: "later-decision", Kind: semantic.ObservationDecisionAdvanced, Rationale: "Observe whether later recovery remains related to the original operation.", SupportRefs: []string{"primer/persistence-and-visibility"}},
		},
		Predicates: []semantic.ObservationPredicate{
			{MilestoneID: "operation-started", Kind: semantic.ObservationWorkloadInvoked},
			{MilestoneID: "host-crashed", Kind: semantic.ObservationNodeCrashed},
			{MilestoneID: "later-decision", Kind: semantic.ObservationDecisionAdvanced},
		},
	}
	response, err := json.Marshal(controlexperiment.RiskCandidatePortfolio{
		Candidates: []controlexperiment.RiskCandidate{candidate},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := controlexperiment.DiscoverRiskWithPlanner(
		ctx, controlexperiment.RiskAgentBudget{MaxCalls: 1, MaxTokens: 100},
		target.Knowledge, target.ObservationProjector.Capabilities(),
		target.Surface.Capabilities.ComposableActions, nil,
		&target.Surface, nil,
		func(context.Context, controlexperiment.RiskAgentView) ([]byte, controlexperiment.ModelWork, error) {
			return response, controlexperiment.ModelWork{
				Calls: 1, InputTokens: 3, OutputTokens: 2, TotalTokens: 5,
			}, nil
		},
	)
	assessment := assessRiskAgentStop(result)
	if err != nil || result.Accepted != nil || len(result.Attempts) != 1 ||
		result.Attempts[0].Assessment == nil ||
		!result.Attempts[0].Assessment.Qualification.Qualified ||
		result.Attempts[0].Feedback.ReasonCode != controlexperiment.RiskAgentReasonFidelity ||
		len(result.Attempts[0].Feedback.CapabilityGaps) != 1 ||
		result.Attempts[0].Feedback.CapabilityGaps[0].Reference != "rawnode-host-persistence-atomicity" ||
		assessment.Status != agenticEvidenceCapabilityGap ||
		assessment.ReasonCode != controlexperiment.AgentCapabilityGapTargetFidelity {
		t.Fatalf("Ready/Advance blind spot was not reported as a fidelity gap: %#v/%#v/%v",
			result, assessment, err)
	}
}

func TestEtcdraftFidelityNoticeDoesNotBecomeAnImplicitGate(t *testing.T) {
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
	candidate := controlexperiment.RiskCandidate{
		ID: "continuity-without-fidelity-claim", PropertyRef: "client-operation-continuity",
		InspirationRef: "original", Summary: "Exercise observable continuity without claiming a persistence window.",
		SuspectedMechanism: "An invoked operation is followed by observable decision progress.",
		Predicates: []semantic.ObservationPredicate{
			{MilestoneID: "invoked", Kind: semantic.ObservationWorkloadInvoked},
			{MilestoneID: "advanced", Kind: semantic.ObservationDecisionAdvanced},
		},
	}
	assessment, err := controlexperiment.AssessRiskCandidateForTarget(
		target.Knowledge, candidate, target.ObservationProjector.Capabilities(),
		target.Surface.Capabilities.ComposableActions, &target.Surface,
	)
	if err != nil || !assessment.Qualification.Qualified || len(assessment.CapabilityGaps) != 0 ||
		assessment.FidelityAssessment != controlexperiment.AgentFidelityUnassessed ||
		len(assessment.FidelityNotices) != 1 ||
		assessment.FidelityNotices[0].Reference != "rawnode-host-persistence-atomicity" {
		t.Fatalf("undeclared fidelity relation was hidden or turned into a gate: %#v/%v", assessment, err)
	}
}

func TestAgenticOracleRegistryRejectsMetadataExecutionDrift(t *testing.T) {
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
	if err != nil || target.validate() != nil {
		t.Fatalf("valid target registry rejected: %#v/%v", target, err)
	}
	drifted := target
	drifted.Surface.Capabilities.OracleCapabilities = append(
		[]controlexperiment.AgentOracleCapability(nil),
		target.Surface.Capabilities.OracleCapabilities...,
	)
	drifted.Surface.Capabilities.OracleCapabilities[0].ID = "declared-but-not-executed"
	if drifted.validate() == nil {
		t.Fatal("surface Oracle metadata drifted away from the executable registry")
	}
	assessment := agenticEvidenceAssessment{
		Status: agenticEvidencePlanningFailed, PropertyID: "client-operation-continuity",
		OracleIDs: []string{targetoracles.ClientApplicationBindingMonitorID},
	}
	testing := scenarioTestingResult{
		Risk:   semantic.RiskWitnessResult{Status: semantic.RiskWitnessReached},
		Oracle: oracle.Result{Checked: []string{"agreement", "trace-integrity"}},
	}
	result := assessTestingEvidence(
		assessment, testing, controlexperiment.ScenarioAgentResult{},
	)
	if result.Status != agenticEvidenceWitnessUnverified ||
		result.ReasonCode != "property-oracle-not-executed" {
		t.Fatalf("missing registered property monitor was accepted: %#v", result)
	}
}

func TestAgenticEvidenceUsesGlobalScenarioDecisionAccounting(t *testing.T) {
	assessment := agenticEvidenceAssessment{
		Status: agenticEvidencePlanningFailed, PropertyID: "agreement",
	}
	testing := scenarioTestingResult{
		Risk: semantic.RiskWitnessResult{
			Status:            semantic.RiskWitnessNotReached,
			MissingMilestones: []string{"ordered-intervention"},
		},
	}
	scenario := controlexperiment.ScenarioAgentResult{
		Status:        controlexperiment.ScenarioAgentCompleted,
		StopReason:    controlexperiment.ScenarioAgentStopDecisionBudget,
		DecisionsUsed: 512, SelectedPathDecisions: 512,
		Execution: &controlexperiment.ScenarioExecution{},
	}
	result := assessTestingEvidence(assessment, testing, scenario)
	if result.Status != agenticEvidenceBudgetExhausted ||
		result.ReasonCode != "runtime-decision-budget-exhausted" ||
		result.FirstMissingMilestone != "ordered-intervention" {
		t.Fatalf("global Scenario cost was not used at the budget boundary: %#v", result)
	}
	scenario.StopReason = controlexperiment.ScenarioAgentStopCallBudget
	result = assessTestingEvidence(assessment, testing, scenario)
	if result.Status != agenticEvidenceBudgetExhausted ||
		result.ReasonCode != "scenario-call-budget-exhausted" {
		t.Fatalf("an executed investigation that exhausted planner calls was misclassified: %#v", result)
	}
	scenario.StopReason = controlexperiment.ScenarioAgentStopHypothesisAbandoned
	result = assessTestingEvidence(assessment, testing, scenario)
	if result.Status != agenticEvidenceInconclusive ||
		result.ReasonCode != agenticEvidenceHypothesisAbandoned {
		t.Fatalf("Agent-directed hypothesis abandonment became a verdict: %#v", result)
	}
}

func TestScenarioCapabilityGapRemainsMechanicalEpisodeEvidence(t *testing.T) {
	scenario := controlexperiment.ScenarioAgentResult{Attempts: []controlexperiment.ScenarioAgentAttempt{{
		Feedback: controlexperiment.ScenarioAgentFeedback{CapabilityGaps: []controlexperiment.AgentCapabilityGap{{
			Code:      controlexperiment.AgentCapabilityGapMissingControl,
			Reference: "effect-step",
			Summary:   "requested effect outcome is unavailable",
		}}},
	}}}
	if got := lastScenarioCapabilityGapCode(scenario); got != controlexperiment.AgentCapabilityGapMissingControl {
		t.Fatalf("terminal capability gap reason was lost: %q", got)
	}
	reasons := agenticExplorationReasonCodes(nil, []agenticScenarioAttemptArtifact{{
		CapabilityGaps: scenario.Attempts[0].Feedback.CapabilityGaps,
	}})
	if len(reasons) != 1 || reasons[0] != controlexperiment.AgentCapabilityGapMissingControl {
		t.Fatalf("capability gap did not reach mechanical Memory: %#v", reasons)
	}
	gaps := agenticExplorationCapabilityGaps([]agenticScenarioAttemptArtifact{{
		CapabilityGaps: scenario.Attempts[0].Feedback.CapabilityGaps,
	}})
	if len(gaps) != 1 || gaps[0].Reference != "effect-step" ||
		gaps[0].Summary != "requested effect outcome is unavailable" {
		t.Fatalf("structured capability gap did not reach Agent Memory: %#v", gaps)
	}
	summary := agenticEpisodeArtifact{
		Status:             agenticEpisodeScenarioStopped,
		ScenarioStopReason: controlexperiment.ScenarioAgentStopCapabilityGap,
		Assessment: agenticEvidenceAssessment{
			Status: agenticEvidenceCapabilityGap, ReasonCode: controlexperiment.AgentCapabilityGapMissingControl,
		},
	}
	if !agenticAssessmentMatchesSummary(summary) {
		t.Fatal("Scenario capability-gap assessment was rejected as planning failure")
	}
}

func TestCapabilityFeedbackProjectionMatchesMethodMode(t *testing.T) {
	memory := []controlexperiment.RiskExplorationMemoryEntry{{
		Episode: 1, EpisodeOutcome: controlexperiment.RiskMemoryOutcomePlanningStopped,
		CapabilityGaps: []controlexperiment.AgentCapabilityGap{{
			Code: controlexperiment.AgentCapabilityGapMissingAction, Reference: "failure-step",
			Summary: "fail-effect is unavailable",
		}},
	}}
	structured := projectAgenticCapabilityFeedbackMemory(
		memory, controlexperiment.AgenticCapabilityFeedbackStructuredGaps,
	)
	reasonOnly := projectAgenticCapabilityFeedbackMemory(
		memory, controlexperiment.AgenticCapabilityFeedbackReasonCodes,
	)
	legacy := projectAgenticCapabilityFeedbackMemory(memory, "")
	if len(structured[0].CapabilityGaps) != 1 || len(reasonOnly[0].CapabilityGaps) != 0 ||
		len(legacy[0].CapabilityGaps) != 0 || len(memory[0].CapabilityGaps) != 1 {
		t.Fatalf("feedback mode projection drifted: structured=%#v reason=%#v legacy=%#v input=%#v",
			structured, reasonOnly, legacy, memory)
	}
	structured[0].CapabilityGaps[0].Summary = "mutated"
	if memory[0].CapabilityGaps[0].Summary != "fail-effect is unavailable" {
		t.Fatal("feedback projection mutated durable Memory")
	}
}

func TestAgenticCapabilityAdaptationMetricsUseDurableAttemptOrder(t *testing.T) {
	gap := controlexperiment.AgentCapabilityGap{
		Code:      controlexperiment.AgentCapabilityGapMissingControl,
		Reference: "first-step",
		Summary:   "the requested persist failure is unavailable",
	}
	repeated := gap
	repeated.Reference = "renamed-step"
	attempts := []agenticScenarioAttemptArtifact{
		{CapabilityGaps: []controlexperiment.AgentCapabilityGap{gap}},
		{CapabilityGaps: []controlexperiment.AgentCapabilityGap{repeated}},
		{EnteredExecution: true},
	}
	metrics := agenticCapabilityAdaptationFromAttempts(attempts)
	if metrics.GapAttempts != 2 || metrics.RepeatedGapAttempts != 1 ||
		metrics.RepairAttempts != 2 || metrics.RepairExecutions != 1 {
		t.Fatalf("capability adaptation metrics drifted: %#v", metrics)
	}
	baseline := agenticCapabilityAdaptationFromAttempts(attempts[:2])
	treatment := agenticCapabilityAdaptationFromAttempts([]agenticScenarioAttemptArtifact{
		attempts[0], attempts[2],
	})
	if baseline.GapAttempts != 2 || baseline.RepeatedGapAttempts != 1 ||
		baseline.RepairAttempts != 1 || baseline.RepairExecutions != 0 ||
		treatment.GapAttempts != 1 || treatment.RepeatedGapAttempts != 0 ||
		treatment.RepairAttempts != 1 || treatment.RepairExecutions != 1 {
		t.Fatalf("scripted feedback ablation was not separated: baseline=%#v treatment=%#v", baseline, treatment)
	}
	episodes := []recoveredAgenticEpisode{
		{Summary: agenticEpisodeArtifact{ScenarioAttemptFeedback: attempts[:1]}},
		{Summary: agenticEpisodeArtifact{ScenarioAttemptFeedback: attempts[1:]}},
	}
	if investigation := agenticInvestigationCapabilityAdaptation(episodes); investigation != metrics {
		t.Fatalf("Investigation metrics lost cross-Episode repair: %#v/%#v", investigation, metrics)
	}
}

func TestInvestigationSelectsEveryDistinctExecutablePortfolioRiskInOrder(t *testing.T) {
	first := controlexperiment.RiskCandidateAssessment{Candidate: controlexperiment.RiskCandidate{
		ID: "first-risk", Summary: "Invoke an operation, then drop one message.",
		Predicates: []semantic.ObservationPredicate{
			{MilestoneID: "invoke", Kind: semantic.ObservationWorkloadInvoked},
			{MilestoneID: "drop", Kind: semantic.ObservationMessageDropped},
		},
	}}
	second := controlexperiment.RiskCandidateAssessment{Candidate: controlexperiment.RiskCandidate{
		ID: "second-risk", Summary: "Invoke an operation, then observe a decision.",
		Predicates: []semantic.ObservationPredicate{
			{MilestoneID: "invoke", Kind: semantic.ObservationWorkloadInvoked},
			{MilestoneID: "decision", Kind: semantic.ObservationDecisionAdvanced},
		},
	}}
	duplicate := first
	duplicate.Candidate.ID = "same-semantics-different-display-id"
	tokenStopped := agenticEpisodeArtifact{
		Status: agenticEpisodeTokenStopped, Accepted: &first,
		ExecutableRisks:  []controlexperiment.RiskCandidateAssessment{first, second},
		ScenarioAttempts: 0,
	}
	if agenticEpisodeRiskEnteredScenario(tokenStopped) {
		t.Fatal("an accepted candidate with no Scenario attempt was marked investigated")
	}
	selected, err := selectNextAgenticPortfolioRisk(tokenStopped.ExecutableRisks, nil)
	if err != nil || selected == nil || selected.Candidate.ID != first.Candidate.ID {
		t.Fatalf("token-stopped candidate did not remain first in the queue: %#v/%v", selected, err)
	}
	tokenStopped.ScenarioAttempts = 1
	if !agenticEpisodeRiskEnteredScenario(tokenStopped) {
		t.Fatal("a candidate with a real Scenario attempt remained uninvestigated")
	}

	selected, err = selectNextAgenticPortfolioRisk(
		[]controlexperiment.RiskCandidateAssessment{first, duplicate, second},
		[]controlexperiment.RiskCandidateAssessment{first},
	)
	if err != nil || selected == nil || selected.Candidate.ID != second.Candidate.ID {
		t.Fatalf("portfolio queue did not skip an investigated semantic duplicate: %#v/%v", selected, err)
	}
	selected, err = selectNextAgenticPortfolioRisk(
		[]controlexperiment.RiskCandidateAssessment{first, second},
		[]controlexperiment.RiskCandidateAssessment{first, second},
	)
	if err != nil || selected != nil {
		t.Fatalf("exhausted portfolio produced another investigation: %#v/%v", selected, err)
	}
}

func TestInvestigationCountsRiskGenerationEpisodesWithExecutableCandidates(t *testing.T) {
	episodes := []recoveredAgenticEpisode{
		{Summary: agenticEpisodeArtifact{RiskAttempts: 4, ExecutableRisks: []controlexperiment.RiskCandidateAssessment{{}}}},
		{Summary: agenticEpisodeArtifact{RiskAttempts: 0, ScenarioAttempts: 8}},
		{Summary: agenticEpisodeArtifact{RiskAttempts: 3}},
		{Summary: agenticEpisodeArtifact{RiskAttempts: 3, ExecutableRisks: []controlexperiment.RiskCandidateAssessment{{}}}},
	}
	if got := riskGenerationEpisodeCountWithExecutableCandidates(episodes); got != 2 {
		t.Fatalf("Risk generation Episode count with executable candidates = %d, want 2", got)
	}
}

func TestAgenticEpisodeCLIAllowsExplicitRepositoryRootAndSourceMounts(t *testing.T) {
	options := controlExperimentOptions{
		Target: "etcdraft-v2", MethodSpecDigest: strings.Repeat("a", 64),
		InvestigationEpisodes: 6,
		KnowledgeSourceMounts: []string{
			"repo=/tmp/consensus-atlas", "go.etcd.io/raft/v3@v3.6.0/=/tmp/raft",
		},
		RiskInput:      "/tmp/risk.json",
		RepositoryRoot: "/tmp/consensus-atlas",
		Decisions:      96, PolicySeed: 1,
	}
	if projected := agenticEpisodeNonSessionProjection(options); projected.hasNonSessionFlags() {
		t.Fatalf("valid Agentic session flags leaked into generic CLI validation: %#v", projected)
	}
}
