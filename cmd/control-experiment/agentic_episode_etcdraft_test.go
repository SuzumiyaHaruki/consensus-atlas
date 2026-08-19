package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
	"github.com/SuzumiyaHaruki/consensus-atlas/targetoracles"
)

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
		len(target.Surface.Capabilities.OracleCapabilities) != 4 ||
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
		composition.Target.ClosureFactory == nil ||
		composition.MethodSpec.Validate() != nil ||
		composition.Target.MethodSpecDigest != composition.MethodSpec.Digest ||
		composition.MethodSpec.InvestigationEpisodes != 1 ||
		composition.MethodSpec.Transport.Model != openRouterFixtureModel ||
		composition.MethodSpec.SemanticInputSchema != etcdraftSemanticInputSchema ||
		composition.MethodSpec.ClosureMode != controlexperiment.AgenticClosureModeTargetLocal ||
		composition.MethodSpec.CapabilityFeedbackMode != controlexperiment.AgenticCapabilityFeedbackStructuredGaps ||
		composition.CapabilityFeedbackMode != controlexperiment.AgenticCapabilityFeedbackStructuredGaps ||
		composition.MethodSpec.EpisodeLimits.MaxRiskCalls != composition.Budget.MaxRiskCalls ||
		composition.MethodSpec.EpisodeLimits.MaxScenarioCalls != composition.Budget.MaxScenarioCalls ||
		composition.MethodSpec.EpisodeLimits.MaxScenarioPlanSteps != composition.Budget.MaxScenarioPlanSteps ||
		composition.Budget.Logical == nil ||
		*composition.Budget.Logical != inputs.experiment.SessionBudget ||
		composition.Budget.MaxRiskCalls != 3 || composition.Budget.MaxScenarioCalls != 3 ||
		composition.Budget.MaxScenarioPlanSteps != 5 ||
		composition.Budget.MaxTotalCalls != 6 || composition.Budget.MaxObservedTokens != 50000 ||
		transport.Model != openRouterFixtureModel || transport.Thinking != "low" ||
		transport.MaxOutputTokens != 32000 || transport.MaxRetries != inputs.experiment.ModelMaxRetries {
		t.Fatalf("etcd/raft registry composition drifted: %#v err=%v", composition, err)
	}
	publicOptions := controlExperimentOptions{
		Target: "etcdraft-v2", InvestigationEpisodes: 1,
		SemanticInput: "../../plans/agent/etcdraft-agentic-calibration-v1.json",
		AgentKeyFile:  "fixture-key.txt", AgentModel: openRouterFixtureModel,
		ClosureMode: controlexperiment.AgenticClosureModePublicFixed,
	}
	publicComposition, err := prepareAgenticEpisodeComposition(ctx, publicOptions)
	if err != nil || publicComposition.Target.ClosureFactory != nil ||
		publicComposition.MethodSpec.ClosureMode != controlexperiment.AgenticClosureModePublicFixed ||
		publicComposition.MethodSpec.Digest == composition.MethodSpec.Digest {
		t.Fatalf("public-fixed arm was not mechanically bound: %#v/%v",
			publicComposition.MethodSpec, err)
	}
	tamperedClosure := composition
	tamperedClosure.Target.ClosureFactory = nil
	_, err = runAgenticEpisodeDirectory(ctx, agenticEpisodeDirectoryOptions{
		Directory:    filepath.Join(t.TempDir(), "closure-mode-mismatch"),
		AgentKeyFile: "fixture-key.txt",
		ReadKey:      func(string) (string, error) { return "fixture-key", nil },
		Recovery:     etcdraftAgenticEpisodeRecoveryBinding(),
		Prepare: func(context.Context) (agenticEpisodeComposition, error) {
			return tamperedClosure, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "COMPOSITION_INVALID") {
		t.Fatalf("composition accepted MethodSpec/factory mismatch: %v", err)
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
	investigation, err := agenticInvestigationBudgetFromEpisode(3, composition.Budget)
	if err != nil || investigation.MaxEpisodes != 3 ||
		investigation.MaxModelCalls != 3*composition.Budget.MaxTotalCalls ||
		investigation.MaxModelTokens != 3*composition.Budget.MaxObservedTokens ||
		investigation.MaxRuntimeDecisionAllowance != 3*composition.Budget.MaxRuntimeDecisions {
		t.Fatalf("Investigation budget did not scale the existing episode contract: %#v/%v",
			investigation, err)
	}
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
	if result.Status != agenticEvidenceRiskUnverified ||
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
		DecisionsUsed: 512, SelectedPathDecisions: 112,
		BranchExplorationDecisions: 400,
		Execution:                  &controlexperiment.ScenarioExecution{},
	}
	result := assessTestingEvidence(assessment, testing, scenario)
	if result.Status != agenticEvidenceBudgetExhausted ||
		result.ReasonCode != "runtime-decision-budget-exhausted" ||
		result.FirstMissingMilestone != "ordered-intervention" {
		t.Fatalf("global branch cost was inferred from the shorter selected trace: %#v", result)
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

func TestUnselectedReplayStableBranchRunsIndependentOracle(t *testing.T) {
	calls := 0
	target := agenticEpisodeTarget{Execute: func(
		context.Context,
		controlexperiment.ScenarioRiskHypothesis,
		controlexperiment.SemanticPrefixProjector,
		controlexperiment.ScenarioExecution,
		string,
	) (scenarioTestingResult, error) {
		calls++
		return scenarioTestingResult{Oracle: oracle.Result{
			Checked:    []string{"agreement"},
			Violations: []oracle.Violation{{Monitor: "agreement", Step: 1, Message: "candidate violation"}},
		}}, nil
	}}
	scenario := controlexperiment.ScenarioAgentResult{
		StopReason: controlexperiment.ScenarioAgentStopDecisionBudget,
		CandidateExecutions: []controlexperiment.ScenarioCandidateExecution{{
			BranchID: "final-treatment", Intent: controlexperiment.ScenarioIntentBranch,
			Execution: controlexperiment.ScenarioExecution{
				FinalTrace: controlruntime.Trace{Digest: "candidate-trace"},
			},
		}},
	}
	branches, err := executeAgenticBranchCandidates(
		context.Background(), target, controlexperiment.ScenarioRiskHypothesis{}, nil, scenario,
	)
	assessment := assessUnselectedBranchEvidence(
		agenticEvidenceAssessment{Status: agenticEvidenceInconclusive}, branches, scenario,
	)
	if err != nil || calls != 1 || len(branches) != 1 ||
		assessment.Status != agenticEvidenceOracleFinding || assessment.ReasonCode != "agreement" {
		t.Fatalf("unselected replay-stable branch bypassed the independent Oracle: %#v/%v", branches, err)
	}
}
