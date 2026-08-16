package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
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
	methodSpecDigest := formalTestStringDigest("agentic-etcdraft-method")
	composition, err := prepareAgenticEpisodeComposition(ctx, controlExperimentOptions{
		Target:        "etcdraft-v2",
		SemanticInput: "../../plans/agent/etcdraft-agentic-calibration-v1.json",
		AgentKeyFile:  "fixture-key.txt", AgentModel: openRouterFixtureModel,
		MethodSpecDigest: methodSpecDigest,
	})
	if err != nil || composition.Target.ID != "etcdraft-v2" ||
		composition.Target.MethodSpecDigest != methodSpecDigest || composition.Budget.Logical == nil ||
		*composition.Budget.Logical != inputs.experiment.SessionBudget ||
		composition.Budget.MaxRiskCalls != 3 || composition.Budget.MaxScenarioCalls != 3 ||
		composition.Budget.MaxScenarioPlanSteps != 4 ||
		composition.Budget.MaxTotalCalls != 6 || composition.Budget.MaxObservedTokens != 50000 ||
		composition.Client.Model != openRouterFixtureModel ||
		composition.Client.ReasoningEffort != "low" || composition.Client.MaxOutputTokens != 32000 ||
		composition.Client.MaxRetries != inputs.experiment.ModelMaxRetries {
		t.Fatalf("etcd/raft registry composition drifted: %#v err=%v", composition, err)
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
		OracleIDs: []string{etcdraftClientApplicationBindingMonitorID},
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
