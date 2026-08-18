package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	agenticEpisodeSummaryFile        = "summary.json"
	agenticEpisodeBundleFile         = "bundle.json"
	agenticEpisodeBranchEvidenceFile = "branch-evidence.json"
)

type agenticBranchEvidenceArtifact struct {
	BranchID          string                       `json:"branch_id"`
	Intent            string                       `json:"intent"`
	ReferenceBranchID string                       `json:"reference_branch_id,omitempty"`
	PlanID            string                       `json:"plan_id"`
	RiskResultID      string                       `json:"risk_result_id"`
	TraceDigest       string                       `json:"trace_digest"`
	OracleFindings    int                          `json:"oracle_findings"`
	Work              controlexperiment.WorkLedger `json:"work"`
}

type agenticScenarioAttemptArtifact struct {
	Ordinal          int                                                 `json:"ordinal"`
	Intent           string                                              `json:"intent,omitempty"`
	Outcome          string                                              `json:"outcome"`
	ReasonCode       string                                              `json:"reason_code,omitempty"`
	ValidationIssues []controlexperiment.ScenarioProposalValidationIssue `json:"validation_issues,omitempty"`
	CapabilityGaps   []controlexperiment.AgentCapabilityGap              `json:"capability_gaps,omitempty"`
	AllowedIntents   []string                                            `json:"allowed_intents,omitempty"`
	FailedStepID     string                                              `json:"failed_step_id,omitempty"`
	MatchCount       int                                                 `json:"match_count,omitempty"`
	SelectorTrace    []controlexperiment.ScenarioSelectorFilter          `json:"selector_trace,omitempty"`
	EnteredExecution bool                                                `json:"entered_execution"`
	ProgressDelta    *controlexperiment.ScenarioProgressDelta            `json:"progress_delta,omitempty"`
}

// agenticEpisodeArtifact is deliberately compact. Exact prompts and provider
// responses remain in the two journals; full Traces remain in the selected
// Bundle and optional branch evidence. This summary keeps only navigation and
// accounting facts.
type agenticEpisodeArtifact struct {
	TargetID                   string                                      `json:"target_id"`
	MethodSpecDigest           string                                      `json:"method_spec_digest,omitempty"`
	Status                     string                                      `json:"status"`
	Budget                     agenticEpisodeBudget                        `json:"budget"`
	Accepted                   *controlexperiment.RiskCandidateAssessment  `json:"accepted_risk,omitempty"`
	RiskAttempts               int                                         `json:"risk_attempts"`
	RiskFeedback               *controlexperiment.RiskAgentFeedback        `json:"risk_feedback,omitempty"`
	ScenarioStatus             string                                      `json:"scenario_status,omitempty"`
	ScenarioStopReason         string                                      `json:"scenario_stop_reason,omitempty"`
	ScenarioAttempts           int                                         `json:"scenario_attempts"`
	ScenarioAttemptFeedback    []agenticScenarioAttemptArtifact            `json:"scenario_attempt_feedback,omitempty"`
	ScenarioDecisionsUsed      int                                         `json:"scenario_decisions_used"`
	SelectedPathDecisions      int                                         `json:"selected_path_decisions"`
	BranchExplorationDecisions int                                         `json:"branch_exploration_decisions"`
	BranchEvidence             []agenticBranchEvidenceArtifact             `json:"branch_evidence,omitempty"`
	RiskProviderCalls          []controlexperiment.StatelessAgentCallAudit `json:"risk_provider_calls"`
	ScenarioProviderCalls      []controlexperiment.StatelessAgentCallAudit `json:"scenario_provider_calls,omitempty"`
	Failure                    *controlexperiment.MethodFailure            `json:"failure,omitempty"`
	PlanID                     string                                      `json:"plan_id,omitempty"`
	RiskResultID               string                                      `json:"risk_result_id,omitempty"`
	TraceDigest                string                                      `json:"trace_digest,omitempty"`
	Metrics                    agenticEpisodeMetrics                       `json:"metrics"`
	Work                       agenticEpisodeWork                          `json:"work"`
	Assessment                 agenticEvidenceAssessment                   `json:"evidence_assessment,omitempty"`
}

type agenticEpisodeRecoveryBinding struct {
	TargetID             string
	ObservationProjector agenticEpisodeObservationProjector
	Testing              func(
		string,
		controlexperiment.ExecutionBundle,
		semantic.RiskWitnessResult,
		semantic.RiskWitnessSpec,
		controlexperiment.SemanticPrefixProjector,
	) (scenarioTestingResult, error)
}

type recoveredAgenticEpisode struct {
	Summary                agenticEpisodeArtifact
	MethodSpec             *controlexperiment.AgenticMethodSpec
	Testing                *scenarioTestingResult
	BranchTesting          []agenticBranchTestingResult
	UnreconciledModelCalls int
}

func newAgenticEpisodeArtifact(
	targetID string,
	budget agenticEpisodeBudget,
	result agenticEpisodeResult,
) (agenticEpisodeArtifact, error) {
	artifact := agenticEpisodeArtifact{
		TargetID: targetID, MethodSpecDigest: result.MethodSpecDigest,
		Status: result.Status, Budget: budget,
		RiskAttempts: len(result.RiskAgent.Attempts),
		RiskProviderCalls: append([]controlexperiment.StatelessAgentCallAudit(nil),
			result.RiskProviderCalls...),
		ScenarioProviderCalls: append([]controlexperiment.StatelessAgentCallAudit(nil),
			result.ScenarioProviderCalls...),
		Failure: cloneAgenticEpisodeFailure(result.Failure), Metrics: result.Metrics, Work: result.Work,
		Assessment: result.Assessment,
	}
	if result.RiskAgent.Accepted != nil {
		accepted := *result.RiskAgent.Accepted
		artifact.Accepted = &accepted
	}
	if len(result.RiskAgent.Attempts) > 0 {
		feedback := result.RiskAgent.Attempts[len(result.RiskAgent.Attempts)-1].Feedback
		artifact.RiskFeedback = &feedback
	}
	if result.Scenario != nil {
		artifact.ScenarioStatus = result.Scenario.Agent.Status
		artifact.ScenarioStopReason = result.Scenario.Agent.StopReason
		artifact.ScenarioAttempts = len(result.Scenario.Agent.Attempts)
		artifact.ScenarioDecisionsUsed = result.Scenario.Agent.DecisionsUsed
		artifact.SelectedPathDecisions = result.Scenario.Agent.SelectedPathDecisions
		artifact.BranchExplorationDecisions = result.Scenario.Agent.BranchExplorationDecisions
		for _, attempt := range result.Scenario.Agent.Attempts {
			compact := agenticScenarioAttemptArtifact{
				Ordinal: attempt.Ordinal, Intent: attempt.Feedback.Intent,
				Outcome: attempt.Feedback.Outcome, ReasonCode: attempt.Feedback.ReasonCode,
				ValidationIssues: append(
					[]controlexperiment.ScenarioProposalValidationIssue(nil),
					attempt.Feedback.ValidationIssues...,
				),
				CapabilityGaps: append(
					[]controlexperiment.AgentCapabilityGap(nil),
					attempt.Feedback.CapabilityGaps...,
				),
				AllowedIntents:   append([]string(nil), attempt.Feedback.AllowedIntents...),
				EnteredExecution: attempt.Execution != nil,
				ProgressDelta:    attempt.Feedback.ProgressDelta,
			}
			if attempt.Feedback.FailedStep != nil {
				compact.FailedStepID = attempt.Feedback.FailedStep.ID
			}
			for index := len(attempt.Feedback.Steps) - 1; index >= 0; index-- {
				step := attempt.Feedback.Steps[index]
				if step.Outcome != controlexperiment.ScenarioStepRejected {
					continue
				}
				compact.FailedStepID = step.StepID
				compact.MatchCount = step.MatchCount
				compact.SelectorTrace = append(
					[]controlexperiment.ScenarioSelectorFilter(nil), step.SelectorTrace...,
				)
				break
			}
			artifact.ScenarioAttemptFeedback = append(artifact.ScenarioAttemptFeedback, compact)
		}
		applyAgenticCapabilityAdaptationMetrics(
			&artifact.Metrics, artifact.ScenarioAttemptFeedback,
		)
	}
	if result.Testing != nil {
		artifact.PlanID = result.Testing.PlanID
		artifact.RiskResultID = result.Testing.Risk.ID
		artifact.TraceDigest = result.Testing.Bundle.Trace.Digest
	}
	for _, branch := range result.BranchTesting {
		artifact.BranchEvidence = append(artifact.BranchEvidence, agenticBranchEvidenceArtifact{
			BranchID: branch.BranchID, Intent: branch.Intent,
			ReferenceBranchID: branch.ReferenceBranchID,
			PlanID:            branch.Testing.PlanID, RiskResultID: branch.Testing.Risk.ID,
			TraceDigest:    branch.Testing.Bundle.Trace.Digest,
			OracleFindings: len(branch.Testing.Oracle.Violations), Work: branch.Testing.Bundle.Work,
		})
	}
	if err := artifact.validateCompact(); err != nil {
		return agenticEpisodeArtifact{}, err
	}
	if result.Status == agenticEpisodeCompleted {
		metrics, metricsErr := agenticEpisodeMetricsFromEvidence(result.Testing, result.BranchTesting)
		primaryValid := result.Testing == nil && artifact.PlanID == "" && artifact.RiskResultID == "" ||
			result.Testing != nil && result.Testing.validateExecutionStructure() == nil &&
				reflect.DeepEqual(artifact.Work.QualifiedExecution, result.Testing.Bundle.Work)
		if artifact.Accepted == nil || len(result.BranchTesting) != len(artifact.BranchEvidence) ||
			result.Testing == nil && len(result.BranchTesting) == 0 || !primaryValid ||
			metricsErr != nil || artifact.Metrics != metrics ||
			!branchExecutionWorkMatches(artifact.Work.BranchQualifiedExecutions, result.BranchTesting) ||
			!agenticEvidenceMethodMatches(
				artifact.MethodSpecDigest, result.Testing, result.BranchTesting,
			) {
			return agenticEpisodeArtifact{}, errors.New("AGENTIC_EPISODE_ARTIFACT_TESTING_INVALID")
		}
	} else if result.Testing != nil || len(result.BranchTesting) != 0 {
		return agenticEpisodeArtifact{}, errors.New("AGENTIC_EPISODE_ARTIFACT_UNEXPECTED_TESTING")
	}
	return artifact, nil
}

func persistAgenticEpisodeArtifacts(
	directory string,
	targetID string,
	budget agenticEpisodeBudget,
	result agenticEpisodeResult,
) (agenticEpisodeArtifact, error) {
	artifact, err := newAgenticEpisodeArtifact(targetID, budget, result)
	if err != nil {
		return agenticEpisodeArtifact{}, err
	}
	if result.Testing != nil {
		if err := writeStatelessAgentJSON(directory, agenticEpisodeBundleFile, result.Testing.Bundle); err != nil {
			return agenticEpisodeArtifact{}, err
		}
	}
	if len(result.BranchTesting) > 0 {
		if err := writeStatelessAgentJSON(
			directory, agenticEpisodeBranchEvidenceFile, result.BranchTesting,
		); err != nil {
			return agenticEpisodeArtifact{}, err
		}
	}
	if err := writeStatelessAgentJSON(directory, agenticEpisodeSummaryFile, artifact); err != nil {
		return agenticEpisodeArtifact{}, err
	}
	return artifact, nil
}

func recoverAgenticEpisodeArtifacts(
	directory string,
	binding agenticEpisodeRecoveryBinding,
) (recoveredAgenticEpisode, bool, error) {
	clean := filepath.Clean(directory)
	if directory == "" || clean == "." || clean == string(filepath.Separator) ||
		binding.validate() != nil {
		return recoveredAgenticEpisode{}, false, errors.New("AGENTIC_EPISODE_RECOVERY_INPUT_INVALID")
	}
	info, err := os.Lstat(clean)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return recoveredAgenticEpisode{}, false, errors.New("AGENTIC_EPISODE_RECOVERY_DIRECTORY_INVALID")
	}
	var artifact agenticEpisodeArtifact
	summaryPath := filepath.Join(clean, agenticEpisodeSummaryFile)
	if _, err := os.Lstat(summaryPath); os.IsNotExist(err) {
		return recoveredAgenticEpisode{}, false, nil
	} else if err != nil {
		return recoveredAgenticEpisode{}, false, errors.New("AGENTIC_EPISODE_RECOVERY_SUMMARY_INVALID")
	}
	err = readStrictJSONFile(summaryPath, 512<<10, &artifact)
	if err != nil || artifact.validateCompact() != nil || artifact.TargetID != binding.TargetID {
		return recoveredAgenticEpisode{}, false, errors.New("AGENTIC_EPISODE_RECOVERY_SUMMARY_INVALID")
	}
	recovered := recoveredAgenticEpisode{Summary: artifact}
	if artifact.MethodSpecDigest != "" {
		spec, err := readAgenticMethodSpec(clean, artifact.MethodSpecDigest)
		if err != nil {
			return recoveredAgenticEpisode{}, false, err
		}
		recovered.MethodSpec = &spec
	} else if _, err := os.Lstat(filepath.Join(clean, agenticMethodSpecFile)); !os.IsNotExist(err) {
		return recoveredAgenticEpisode{}, false, errors.New("AGENTIC_EPISODE_RECOVERY_UNEXPECTED_METHOD_SPEC")
	}
	if artifact.Status != agenticEpisodeCompleted {
		if _, err := os.Lstat(filepath.Join(clean, agenticEpisodeBundleFile)); !os.IsNotExist(err) {
			return recoveredAgenticEpisode{}, false, errors.New("AGENTIC_EPISODE_RECOVERY_UNEXPECTED_BUNDLE")
		}
		if _, err := os.Lstat(filepath.Join(clean, agenticEpisodeBranchEvidenceFile)); !os.IsNotExist(err) {
			return recoveredAgenticEpisode{}, false, errors.New("AGENTIC_EPISODE_RECOVERY_UNEXPECTED_BRANCH_EVIDENCE")
		}
		return recovered, true, nil
	}
	if artifact.PlanID != "" {
		var bundle controlexperiment.ExecutionBundle
		if err := readStrictJSONFile(filepath.Join(clean, agenticEpisodeBundleFile), 64<<20, &bundle); err != nil ||
			bundle.Validate() != nil || bundle.Trace.Digest != artifact.TraceDigest {
			return recoveredAgenticEpisode{}, false, errors.New("AGENTIC_EPISODE_RECOVERY_BUNDLE_INVALID")
		}
		testing, err := recoverAgenticTesting(
			artifact, binding, artifact.PlanID, artifact.RiskResultID, bundle,
		)
		if err != nil {
			return recoveredAgenticEpisode{}, false, err
		}
		recovered.Testing = &testing
	} else if _, err := os.Lstat(filepath.Join(clean, agenticEpisodeBundleFile)); !os.IsNotExist(err) {
		return recoveredAgenticEpisode{}, false, errors.New("AGENTIC_EPISODE_RECOVERY_UNEXPECTED_BUNDLE")
	}
	if len(artifact.BranchEvidence) > 0 {
		var stored []agenticBranchTestingResult
		if err := readStrictJSONFile(
			filepath.Join(clean, agenticEpisodeBranchEvidenceFile), 256<<20, &stored,
		); err != nil || len(stored) != len(artifact.BranchEvidence) {
			return recoveredAgenticEpisode{}, false, errors.New("AGENTIC_EPISODE_RECOVERY_BRANCH_EVIDENCE_INVALID")
		}
		for index, branch := range stored {
			summary := artifact.BranchEvidence[index]
			if branch.BranchID != summary.BranchID || branch.Intent != summary.Intent ||
				branch.ReferenceBranchID != summary.ReferenceBranchID ||
				branch.Testing.PlanID != summary.PlanID || branch.Testing.Risk.ID != summary.RiskResultID ||
				branch.Testing.Bundle.Trace.Digest != summary.TraceDigest {
				return recoveredAgenticEpisode{}, false, errors.New("AGENTIC_EPISODE_RECOVERY_BRANCH_EVIDENCE_DRIFT")
			}
			testing, err := recoverAgenticTesting(
				artifact, binding, summary.PlanID, summary.RiskResultID, branch.Testing.Bundle,
			)
			if err != nil || !reflect.DeepEqual(testing, branch.Testing) ||
				len(testing.Oracle.Violations) != summary.OracleFindings ||
				!reflect.DeepEqual(testing.Bundle.Work, summary.Work) {
				return recoveredAgenticEpisode{}, false, errors.New("AGENTIC_EPISODE_RECOVERY_BRANCH_EVIDENCE_DRIFT")
			}
			branch.Testing = testing
			recovered.BranchTesting = append(recovered.BranchTesting, branch)
		}
	} else if _, err := os.Lstat(filepath.Join(clean, agenticEpisodeBranchEvidenceFile)); !os.IsNotExist(err) {
		return recoveredAgenticEpisode{}, false, errors.New("AGENTIC_EPISODE_RECOVERY_UNEXPECTED_BRANCH_EVIDENCE")
	}
	metrics, metricsErr := agenticEpisodeMetricsFromEvidence(recovered.Testing, recovered.BranchTesting)
	applyAgenticCapabilityAdaptationMetrics(&metrics, artifact.ScenarioAttemptFeedback)
	if metricsErr != nil || !agenticMetricsMatchArtifact(
		metrics, artifact.Metrics, artifact.PlanID != "",
	) ||
		recovered.Testing != nil && !reflect.DeepEqual(
			artifact.Work.QualifiedExecution, recovered.Testing.Bundle.Work,
		) || !branchExecutionWorkMatches(artifact.Work.BranchQualifiedExecutions, recovered.BranchTesting) ||
		!agenticEvidenceMethodMatches(
			artifact.MethodSpecDigest, recovered.Testing, recovered.BranchTesting,
		) {
		return recoveredAgenticEpisode{}, false, errors.New("AGENTIC_EPISODE_RECOVERY_EVIDENCE_DRIFT")
	}
	return recovered, true, nil
}

func recoverAgenticTesting(
	artifact agenticEpisodeArtifact,
	binding agenticEpisodeRecoveryBinding,
	planID string,
	riskResultID string,
	bundle controlexperiment.ExecutionBundle,
) (scenarioTestingResult, error) {
	if bundle.Validate() != nil {
		return scenarioTestingResult{}, errors.New("AGENTIC_EPISODE_RECOVERY_BUNDLE_INVALID")
	}
	assessment, projector, err := recoverAgenticEpisodeAssessment(artifact, binding, bundle)
	if err != nil {
		return scenarioTestingResult{}, err
	}
	risk, err := projector.Project(riskResultID, assessment.Spec, bundle.Trace)
	if err != nil {
		return scenarioTestingResult{}, err
	}
	return binding.Testing(planID, bundle, risk, assessment.Spec, projector)
}

func branchExecutionWorkMatches(
	work []agenticBranchExecutionWork,
	branches []agenticBranchTestingResult,
) bool {
	if len(work) != len(branches) {
		return false
	}
	for index := range branches {
		if work[index].BranchID != branches[index].BranchID ||
			!reflect.DeepEqual(work[index].Work, branches[index].Testing.Bundle.Work) {
			return false
		}
	}
	return true
}

func agenticEvidenceMethodMatches(
	methodSpecDigest string,
	selected *scenarioTestingResult,
	branches []agenticBranchTestingResult,
) bool {
	if methodSpecDigest != "" && !validAgenticSHA256(methodSpecDigest) {
		return false
	}
	bundles := make([]controlexperiment.ExecutionBundle, 0, len(branches)+1)
	if selected != nil {
		bundles = append(bundles, selected.Bundle)
	}
	for _, branch := range branches {
		bundles = append(bundles, branch.Testing.Bundle)
	}
	for _, bundle := range bundles {
		if bundle.Identity.MethodSpecDigest != methodSpecDigest ||
			(methodSpecDigest != "" && bundle.SchemaVersion != controlexperiment.ExecutionBundleSchemaVersionV3) {
			return false
		}
	}
	return true
}

func (artifact agenticEpisodeArtifact) validateCompact() error {
	if artifact.TargetID == "" || artifact.Budget.validate() != nil ||
		artifact.MethodSpecDigest != "" && !validAgenticSHA256(artifact.MethodSpecDigest) ||
		artifact.RiskAttempts < 0 || artifact.ScenarioAttempts < 0 ||
		artifact.RiskAttempts > artifact.Budget.MaxRiskCalls ||
		artifact.ScenarioAttempts > artifact.Budget.MaxScenarioCalls ||
		artifact.ScenarioDecisionsUsed < 0 ||
		artifact.ScenarioDecisionsUsed > artifact.Budget.MaxRuntimeDecisions ||
		artifact.SelectedPathDecisions < 0 ||
		artifact.SelectedPathDecisions > artifact.ScenarioDecisionsUsed ||
		artifact.BranchExplorationDecisions < 0 ||
		artifact.BranchExplorationDecisions > artifact.ScenarioDecisionsUsed ||
		!agenticProviderAttemptAccountingValid(artifact) ||
		!agenticProviderUsageReconciled(append(
			append([]controlexperiment.StatelessAgentCallAudit(nil), artifact.RiskProviderCalls...),
			artifact.ScenarioProviderCalls...,
		)) ||
		artifact.Work.Model != modelWorkFromAgentAudits(append(
			append([]controlexperiment.StatelessAgentCallAudit(nil), artifact.RiskProviderCalls...),
			artifact.ScenarioProviderCalls...,
		)) {
		return errors.New("AGENTIC_EPISODE_ARTIFACT_ACCOUNTING_INVALID")
	}
	expectedCandidates := len(artifact.BranchEvidence) + boolInt(artifact.PlanID != "")
	legacyCandidateAccounting := len(artifact.BranchEvidence) == 0 && artifact.PlanID != "" &&
		artifact.Metrics.ExecutedCandidates == 0 && artifact.Metrics.BranchCandidates == 0
	if artifact.Metrics.BranchCandidates != len(artifact.BranchEvidence) ||
		artifact.Metrics.ExecutedCandidates != expectedCandidates && !legacyCandidateAccounting ||
		len(artifact.Work.BranchQualifiedExecutions) != len(artifact.BranchEvidence) ||
		len(artifact.BranchEvidence) > artifact.ScenarioAttempts ||
		len(artifact.BranchEvidence) > controlexperiment.ScenarioAgentMaxCalls {
		return errors.New("AGENTIC_EPISODE_ARTIFACT_BRANCH_ACCOUNTING_INVALID")
	}
	if len(artifact.ScenarioAttemptFeedback) != 0 &&
		len(artifact.ScenarioAttemptFeedback) != artifact.ScenarioAttempts {
		return errors.New("AGENTIC_EPISODE_ARTIFACT_SCENARIO_FEEDBACK_INVALID")
	}
	for index, attempt := range artifact.ScenarioAttemptFeedback {
		if attempt.Ordinal != index+1 || attempt.Outcome == "" ||
			attempt.MatchCount < 0 ||
			!validAgenticScenarioAttemptIntents(attempt.AllowedIntents) {
			return errors.New("AGENTIC_EPISODE_ARTIFACT_SCENARIO_FEEDBACK_INVALID")
		}
		previous := int(^uint(0) >> 1)
		for _, filter := range attempt.SelectorTrace {
			if filter.Field == "" || filter.Requested == "" || filter.CandidateCount < 0 ||
				filter.CandidateCount > previous {
				return errors.New("AGENTIC_EPISODE_ARTIFACT_SCENARIO_FEEDBACK_INVALID")
			}
			previous = filter.CandidateCount
		}
		for _, issue := range attempt.ValidationIssues {
			if issue.Validate() != nil {
				return errors.New("AGENTIC_EPISODE_ARTIFACT_SCENARIO_FEEDBACK_INVALID")
			}
		}
		for _, gap := range attempt.CapabilityGaps {
			if (gap.Code != controlexperiment.AgentCapabilityGapMissingAction &&
				gap.Code != controlexperiment.AgentCapabilityGapMissingControl) ||
				gap.Reference == "" || gap.Summary == "" {
				return errors.New("AGENTIC_EPISODE_ARTIFACT_SCENARIO_FEEDBACK_INVALID")
			}
		}
		if attempt.ReasonCode == controlexperiment.ScenarioAgentProposalInvalid &&
			(len(attempt.ValidationIssues) == 0 || len(attempt.AllowedIntents) == 0) {
			return errors.New("AGENTIC_EPISODE_ARTIFACT_SCENARIO_FEEDBACK_INVALID")
		}
		if attempt.ProgressDelta != nil && !validAgenticScenarioProgress(*attempt.ProgressDelta) {
			return errors.New("AGENTIC_EPISODE_ARTIFACT_SCENARIO_FEEDBACK_INVALID")
		}
	}
	expectedAdaptation := agenticCapabilityAdaptationFromAttempts(artifact.ScenarioAttemptFeedback)
	if artifact.Metrics.CapabilityGapAttempts != expectedAdaptation.GapAttempts ||
		artifact.Metrics.RepeatedCapabilityGapAttempts != expectedAdaptation.RepeatedGapAttempts ||
		artifact.Metrics.CapabilityRepairAttempts != expectedAdaptation.RepairAttempts ||
		artifact.Metrics.CapabilityRepairExecutions != expectedAdaptation.RepairExecutions {
		return errors.New("AGENTIC_EPISODE_ARTIFACT_CAPABILITY_METRICS_INVALID")
	}
	seenBranches := make(map[string]bool, len(artifact.BranchEvidence))
	for index, branch := range artifact.BranchEvidence {
		if branch.BranchID == "" || strings.ContainsAny(branch.BranchID, " /\\") ||
			branch.Intent == "" || branch.PlanID == "" || branch.RiskResultID == "" ||
			len(branch.TraceDigest) != 64 || branch.OracleFindings < 0 || seenBranches[branch.BranchID] ||
			artifact.Work.BranchQualifiedExecutions[index].BranchID != branch.BranchID ||
			!reflect.DeepEqual(artifact.Work.BranchQualifiedExecutions[index].Work, branch.Work) {
			return errors.New("AGENTIC_EPISODE_ARTIFACT_BRANCH_EVIDENCE_INVALID")
		}
		seenBranches[branch.BranchID] = true
	}
	for _, audit := range append(
		append([]controlexperiment.StatelessAgentCallAudit(nil), artifact.RiskProviderCalls...),
		artifact.ScenarioProviderCalls...,
	) {
		if audit.Validate() != nil {
			return errors.New("AGENTIC_EPISODE_ARTIFACT_AUDIT_INVALID")
		}
	}
	if !agenticAssessmentMatchesSummary(artifact) {
		return errors.New("AGENTIC_EPISODE_ARTIFACT_ASSESSMENT_SUMMARY_INVALID")
	}
	if artifact.Accepted != nil {
		if err := validateAgenticEpisodeAssessment(*artifact.Accepted); err != nil {
			return errors.Join(errors.New("AGENTIC_EPISODE_ARTIFACT_ASSESSMENT_CANDIDATE_INVALID"), err)
		}
	}
	switch artifact.Status {
	case agenticEpisodeRiskStopped:
		if artifact.Failure != nil || artifact.Accepted != nil || artifact.Metrics.CandidateAccepted ||
			artifact.ScenarioStatus != "" || artifact.PlanID != "" || artifact.RiskResultID != "" ||
			artifact.TraceDigest != "" ||
			len(artifact.BranchEvidence) != 0 {
			return errors.New("AGENTIC_EPISODE_ARTIFACT_RISK_STOP_INVALID")
		}
	case agenticEpisodeScenarioStopped, agenticEpisodeTokenStopped:
		if artifact.Failure != nil || artifact.Metrics.CandidateAccepted != (artifact.Accepted != nil) ||
			artifact.PlanID != "" || artifact.RiskResultID != "" || artifact.TraceDigest != "" ||
			len(artifact.BranchEvidence) != 0 {
			return errors.New("AGENTIC_EPISODE_ARTIFACT_STOP_INVALID")
		}
	case agenticEpisodeExecutionFailed:
		if artifact.Accepted == nil || !artifact.Metrics.CandidateAccepted || artifact.Failure == nil ||
			artifact.Failure.Phase == "" || artifact.Failure.Code == "" || artifact.Failure.Decision <= 0 ||
			artifact.Failure.Terminal == nil || artifact.Failure.Terminal.Validate() != nil ||
			artifact.PlanID != "" || artifact.RiskResultID != "" || artifact.TraceDigest != "" ||
			len(artifact.BranchEvidence) != 0 {
			return errors.New("AGENTIC_EPISODE_ARTIFACT_EXECUTION_FAILURE_INVALID")
		}
	case agenticEpisodeCompleted:
		hasPrimary := artifact.PlanID != "" && artifact.RiskResultID != ""
		if artifact.Failure != nil || artifact.Accepted == nil || !artifact.Metrics.CandidateAccepted ||
			artifact.PlanID == "" != (artifact.RiskResultID == "") ||
			hasPrimary != (artifact.TraceDigest != "") ||
			artifact.TraceDigest != "" && !validAgenticSHA256(artifact.TraceDigest) ||
			!hasPrimary && len(artifact.BranchEvidence) == 0 ||
			artifact.ScenarioStatus != controlexperiment.ScenarioAgentCompleted &&
				!(artifact.ScenarioStatus == controlexperiment.ScenarioAgentStopped && !hasPrimary) {
			return errors.New("AGENTIC_EPISODE_ARTIFACT_COMPLETED_INVALID")
		}
	default:
		return errors.New("AGENTIC_EPISODE_ARTIFACT_STATUS_INVALID")
	}
	return nil
}

func validAgenticScenarioProgress(delta controlexperiment.ScenarioProgressDelta) bool {
	if delta.Decisions < 0 || delta.UniqueStateTransitions < 0 ||
		delta.RepeatedStateTransitions < 0 || delta.RepeatedPatternDepth < 0 ||
		delta.TemporalCallbacks < 0 || delta.LogicalClockAdvances < 0 ||
		delta.LogicalClockAdvances > delta.TemporalCallbacks {
		return false
	}
	switch delta.MilestoneProgress {
	case controlexperiment.ScenarioMilestoneProgressUnchanged,
		controlexperiment.ScenarioMilestoneProgressAdvanced,
		controlexperiment.ScenarioMilestoneProgressRepeated,
		controlexperiment.ScenarioMilestoneProgressStalled,
		controlexperiment.ScenarioMilestoneProgressReached:
	default:
		return false
	}
	seenCounts := make(map[control.ActionKind]bool, len(delta.ActionCounts))
	for _, count := range delta.ActionCounts {
		if count.Kind.Validate() != nil || count.Count <= 0 || seenCounts[count.Kind] {
			return false
		}
		seenCounts[count.Kind] = true
	}
	for _, kind := range delta.AvailableInterventions {
		if kind.Validate() != nil {
			return false
		}
	}
	return true
}

func validAgenticScenarioAttemptIntents(values []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		switch value {
		case controlexperiment.ScenarioIntentContinue,
			controlexperiment.ScenarioIntentRevise,
			controlexperiment.ScenarioIntentBranch,
			controlexperiment.ScenarioIntentControl,
			controlexperiment.ScenarioIntentAblate,
			controlexperiment.ScenarioIntentSelect,
			controlexperiment.ScenarioIntentAbandon,
			controlexperiment.ScenarioIntentMinimize:
		default:
			return false
		}
		if seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func agenticProviderUsageReconciled(
	audits []controlexperiment.StatelessAgentCallAudit,
) bool {
	for _, audit := range audits {
		if audit.Work.Calls == 1 && audit.ProviderUsageStatus != agentProviderUsageObserved {
			return false
		}
	}
	return true
}

// A provider response that crosses the observed-token threshold is already
// charged and durable, but it is deliberately withheld from the typed
// Scenario planner. Consequently it has an audit entry without a
// ScenarioAgentAttempt. No other terminal state may use that exception.
func agenticProviderAttemptAccountingValid(artifact agenticEpisodeArtifact) bool {
	if artifact.RiskAttempts != len(artifact.RiskProviderCalls) {
		return false
	}
	if artifact.ScenarioAttempts == len(artifact.ScenarioProviderCalls) {
		return true
	}
	if artifact.Status != agenticEpisodeTokenStopped ||
		artifact.Assessment.ReasonCode != "model-token-threshold-reached" ||
		len(artifact.ScenarioProviderCalls) != artifact.ScenarioAttempts+1 ||
		artifact.Work.Model.TotalTokens <= artifact.Budget.MaxObservedTokens {
		return false
	}
	last := artifact.ScenarioProviderCalls[len(artifact.ScenarioProviderCalls)-1]
	return last.Status == controlexperiment.StatelessAgentCallContentReady && last.Work.Calls == 1
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func agenticMetricsMatchArtifact(
	actual agenticEpisodeMetrics,
	stored agenticEpisodeMetrics,
	hasPrimary bool,
) bool {
	if actual == stored {
		return true
	}
	if hasPrimary && stored.ExecutedCandidates == 0 && stored.BranchCandidates == 0 &&
		actual.ExecutedCandidates == 1 && actual.BranchCandidates == 0 {
		actual.ExecutedCandidates = 0
		return actual == stored
	}
	return false
}

func agenticAssessmentMatchesSummary(artifact agenticEpisodeArtifact) bool {
	assessment := artifact.Assessment
	// Summaries created before the evidence-assessment field remain readable;
	// new summaries always populate it in newAgenticEpisodeArtifact.
	if assessment.Status == "" {
		return true
	}
	if assessment.ReasonCode == "" ||
		(assessment.EvidenceLevel != "" &&
			assessment.EvidenceLevel != controlexperiment.PropertyEvidenceHypothesis &&
			assessment.EvidenceLevel != controlexperiment.PropertyEvidenceObservable &&
			assessment.EvidenceLevel != controlexperiment.PropertyEvidenceOracleBacked) {
		return false
	}
	for index, oracleID := range assessment.OracleIDs {
		if oracleID == "" || (index > 0 && assessment.OracleIDs[index-1] >= oracleID) {
			return false
		}
	}
	if assessment.FidelityAssessment != "" &&
		assessment.FidelityAssessment != controlexperiment.AgentFidelityNotApplicable &&
		assessment.FidelityAssessment != controlexperiment.AgentFidelityUnassessed {
		return false
	}
	for index, boundaryID := range assessment.FidelityBoundaryIDs {
		if boundaryID == "" || index > 0 && assessment.FidelityBoundaryIDs[index-1] >= boundaryID {
			return false
		}
	}
	if artifact.Accepted == nil {
		if assessment.PropertyID != "" || assessment.EvidenceLevel != "" || len(assessment.OracleIDs) != 0 ||
			assessment.FidelityAssessment != "" || len(assessment.FidelityBoundaryIDs) != 0 {
			return false
		}
	} else if assessment.PropertyID != artifact.Accepted.Candidate.PropertyRef ||
		assessment.EvidenceLevel == "" {
		return false
	} else if assessment.FidelityAssessment != "" {
		if assessment.FidelityAssessment != artifact.Accepted.FidelityAssessment {
			return false
		}
		wantBoundaries := make([]string, len(artifact.Accepted.FidelityNotices))
		for index, notice := range artifact.Accepted.FidelityNotices {
			wantBoundaries[index] = notice.Reference
		}
		if len(assessment.FidelityBoundaryIDs) != len(wantBoundaries) {
			return false
		}
		for index := range wantBoundaries {
			if assessment.FidelityBoundaryIDs[index] != wantBoundaries[index] {
				return false
			}
		}
	}
	switch artifact.Status {
	case agenticEpisodeRiskStopped:
		return assessment.Status == agenticEvidencePlanningFailed ||
			assessment.Status == agenticEvidenceCapabilityGap
	case agenticEpisodeScenarioStopped:
		return assessment.Status == agenticEvidencePlanningFailed ||
			assessment.Status == agenticEvidenceCapabilityGap
	case agenticEpisodeTokenStopped:
		return assessment.Status == agenticEvidenceBudgetExhausted
	case agenticEpisodeExecutionFailed:
		return assessment.Status == agenticEvidenceExecutionFailed
	case agenticEpisodeCompleted:
		if artifact.Metrics.OracleFindings > 0 {
			return assessment.Status == agenticEvidenceOracleFinding
		}
		if artifact.Metrics.RiskReached {
			return assessment.Status == agenticEvidenceRiskReached ||
				assessment.Status == agenticEvidenceRiskUnverified
		}
		return assessment.Status == agenticEvidenceInconclusive ||
			assessment.Status == agenticEvidenceBudgetExhausted
	default:
		return false
	}
}

func (binding agenticEpisodeRecoveryBinding) validate() error {
	if binding.TargetID == "" || binding.ObservationProjector == nil || binding.Testing == nil ||
		semantic.ValidateObservationCapabilities(binding.ObservationProjector.Capabilities()) != nil {
		return errors.New("AGENTIC_EPISODE_RECOVERY_BINDING_INVALID")
	}
	return nil
}

func recoverAgenticEpisodeAssessment(
	artifact agenticEpisodeArtifact,
	binding agenticEpisodeRecoveryBinding,
	bundle controlexperiment.ExecutionBundle,
) (controlexperiment.RiskCandidateAssessment, controlexperiment.LinearObservationRiskProjector, error) {
	if artifact.Accepted == nil || validateAgenticEpisodeAssessment(*artifact.Accepted) != nil {
		return controlexperiment.RiskCandidateAssessment{}, controlexperiment.LinearObservationRiskProjector{},
			errors.New("AGENTIC_EPISODE_RECOVERY_CANDIDATE_INVALID")
	}
	candidate := artifact.Accepted.Candidate
	spec := artifact.Accepted.Spec
	qualification, err := semantic.QualifyRisk(
		candidate.Predicates, binding.ObservationProjector.Capabilities(),
		bundle.Qualification.Manifest.Capabilities.Actions,
	)
	if err != nil {
		return controlexperiment.RiskCandidateAssessment{}, controlexperiment.LinearObservationRiskProjector{},
			errors.New("AGENTIC_EPISODE_RECOVERY_QUALIFICATION_RECOMPUTE_FAILED")
	}
	if !qualification.Qualified {
		return controlexperiment.RiskCandidateAssessment{}, controlexperiment.LinearObservationRiskProjector{},
			errors.New("AGENTIC_EPISODE_RECOVERY_QUALIFICATION_UNSUPPORTED")
	}
	if !jsonEquivalent(qualification, artifact.Accepted.Qualification) {
		return controlexperiment.RiskCandidateAssessment{}, controlexperiment.LinearObservationRiskProjector{},
			errors.New("AGENTIC_EPISODE_RECOVERY_QUALIFICATION_DRIFT")
	}
	projector, err := controlexperiment.NewLinearObservationRiskProjector(
		spec, candidate.Predicates, binding.ObservationProjector,
	)
	return *artifact.Accepted, projector, err
}

func validateAgenticEpisodeAssessment(
	assessment controlexperiment.RiskCandidateAssessment,
) error {
	candidate := assessment.Candidate
	if candidate.Validate() != nil || assessment.Spec.Validate() != nil ||
		!assessment.Qualification.Qualified || len(assessment.Qualification.Issues) != 0 ||
		len(assessment.CapabilityGaps) != 0 || len(candidate.RequiredFidelity) != 0 {
		return errors.New("AGENTIC_EPISODE_ASSESSMENT_INVALID")
	}
	if assessment.FidelityAssessment != "" &&
		assessment.FidelityAssessment != controlexperiment.AgentFidelityNotApplicable &&
		assessment.FidelityAssessment != controlexperiment.AgentFidelityUnassessed {
		return errors.New("AGENTIC_EPISODE_ASSESSMENT_FIDELITY_INVALID")
	}
	for index, notice := range assessment.FidelityNotices {
		if notice.Code != controlexperiment.AgentCapabilityNoticeFidelityUnassessed ||
			notice.Reference == "" || notice.Summary == "" ||
			index > 0 && assessment.FidelityNotices[index-1].Reference >= notice.Reference {
			return errors.New("AGENTIC_EPISODE_ASSESSMENT_FIDELITY_INVALID")
		}
	}
	if assessment.FidelityAssessment == controlexperiment.AgentFidelityUnassessed &&
		len(assessment.FidelityNotices) == 0 ||
		assessment.FidelityAssessment == controlexperiment.AgentFidelityNotApplicable &&
			len(assessment.FidelityNotices) != 0 {
		return errors.New("AGENTIC_EPISODE_ASSESSMENT_FIDELITY_INVALID")
	}
	milestones := make([]string, len(candidate.Predicates))
	orders := make([]semantic.RiskWitnessOrder, 0, len(candidate.Predicates)-1)
	for index, predicate := range candidate.Predicates {
		milestones[index] = predicate.MilestoneID
		if index > 0 {
			orders = append(orders, semantic.RiskWitnessOrder{
				Before: candidate.Predicates[index-1].MilestoneID, After: predicate.MilestoneID,
			})
		}
	}
	spec, err := semantic.NewRiskWitnessSpec(
		candidate.ID+"-witness", assessment.Spec.FamilyID, candidate.ID, milestones, orders,
	)
	requirements, requirementsErr := semantic.CompileRiskRequirements(candidate.Predicates)
	if err != nil || requirementsErr != nil || !reflect.DeepEqual(spec, assessment.Spec) ||
		!jsonEquivalent(requirements, assessment.Qualification.Requirements) {
		return errors.New("AGENTIC_EPISODE_ASSESSMENT_DERIVATION_INVALID")
	}
	return nil
}

func jsonEquivalent(left any, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func agenticEpisodeMetricsFromEvidence(
	selected *scenarioTestingResult,
	branches []agenticBranchTestingResult,
) (agenticEpisodeMetrics, error) {
	metrics := agenticEpisodeMetrics{CandidateAccepted: true, BranchCandidates: len(branches)}
	protocolStates := make(map[string]bool)
	controlStates := make(map[string]bool)
	jointStates := make(map[string]bool)
	results := make([]scenarioTestingResult, 0, len(branches)+1)
	if selected != nil {
		results = append(results, *selected)
	}
	for _, branch := range branches {
		results = append(results, branch.Testing)
	}
	metrics.ExecutedCandidates = len(results)
	if len(results) == 0 {
		return metrics, nil
	}
	for _, testing := range results {
		if testing.validateExecutionStructure() != nil {
			return agenticEpisodeMetrics{}, errors.New("AGENTIC_EPISODE_CANDIDATE_EVIDENCE_INVALID")
		}
		metrics.RiskReached = metrics.RiskReached ||
			testing.Risk.Status == semantic.RiskWitnessReached
		metrics.CorePSSSamples += testing.CorePSSSamples
		metrics.OracleFindings += len(testing.Oracle.Violations)
		for _, sample := range testing.Bundle.CorePSS {
			keys, err := psscore.Keys(sample.State)
			if err != nil {
				return agenticEpisodeMetrics{}, errors.New("AGENTIC_EPISODE_PSS_VIEWS_INVALID")
			}
			protocolStates[keys.Protocol] = true
			controlStates[keys.Control] = true
			jointStates[keys.Joint] = true
		}
	}
	metrics.ProtocolPSSStates = len(protocolStates)
	metrics.ControlPSSStates = len(controlStates)
	metrics.UniquePSSStates = len(jointStates)
	return metrics, nil
}

// deriveAgenticExplorationMemory rebuilds Agent context from recovered
// episode artifacts. It creates no additional durable state: the summaries
// and Bundles remain the source of truth.
func deriveAgenticExplorationMemory(
	episodes []recoveredAgenticEpisode,
) ([]controlexperiment.RiskExplorationMemoryEntry, error) {
	memory := make([]controlexperiment.RiskExplorationMemoryEntry, 0, len(episodes))
	seenCandidates := make(map[string]bool)
	seenProtocolStates := make(map[string]bool)
	for index, episode := range episodes {
		if episode.Summary.validateCompact() != nil ||
			(episode.Summary.Status == agenticEpisodeCompleted) !=
				(episode.Testing != nil || len(episode.BranchTesting) > 0) {
			return nil, errors.New("AGENTIC_EXPLORATION_MEMORY_EPISODE_INVALID")
		}
		reasons := agenticExplorationReasonCodes(
			episode.Summary.RiskFeedback, episode.Summary.ScenarioAttemptFeedback,
		)
		capabilityGaps := agenticExplorationCapabilityGaps(episode.Summary.ScenarioAttemptFeedback)
		if episode.Summary.ScenarioStopReason == controlexperiment.ScenarioAgentStopHypothesisAbandoned {
			reasons = append(reasons, controlexperiment.ScenarioAgentStopHypothesisAbandoned)
		}
		entry := controlexperiment.RiskExplorationMemoryEntry{
			Episode:     index + 1,
			ModelCalls:  episode.Summary.Work.Model.Calls,
			ModelTokens: episode.Summary.Work.Model.TotalTokens,
			SearchWorkUnits: episode.Summary.Work.ScenarioFrontier.WorkUnits +
				episode.Summary.Work.ScenarioSearch.TotalWorkUnits,
			ExecutionWorkUnits:    agenticEvidenceExecutionWorkUnits(episode.Summary.Work),
			MechanicalReasonCodes: reasons,
			CapabilityGaps:        capabilityGaps,
		}
		if episode.Summary.Accepted != nil {
			candidate := episode.Summary.Accepted.Candidate
			entry.CandidateID = candidate.ID
			entry.Summary = candidate.Summary
			entry.SuspectedMechanism = candidate.SuspectedMechanism
			entry.RepeatedCandidate = seenCandidates[candidate.ID]
			seenCandidates[candidate.ID] = true
		} else if episode.Summary.RiskFeedback != nil {
			entry.CandidateID = episode.Summary.RiskFeedback.CandidateID
			entry.RepeatedCandidate = entry.CandidateID != "" && seenCandidates[entry.CandidateID]
			if entry.CandidateID != "" {
				seenCandidates[entry.CandidateID] = true
			}
		}
		if episode.Testing != nil || len(episode.BranchTesting) > 0 {
			metrics, err := agenticEpisodeMetricsFromEvidence(episode.Testing, episode.BranchTesting)
			applyAgenticCapabilityAdaptationMetrics(
				&metrics, episode.Summary.ScenarioAttemptFeedback,
			)
			if err != nil || !agenticMetricsMatchArtifact(
				metrics, episode.Summary.Metrics, episode.Testing != nil,
			) {
				return nil, errors.New("AGENTIC_EXPLORATION_MEMORY_EVIDENCE_INVALID")
			}
			representative := representativeAgenticTesting(episode.Testing, episode.BranchTesting)
			entry.RiskStatus = representative.Risk.Status
			entry.SatisfiedMilestones = append(
				[]string(nil), representative.Risk.SatisfiedMilestones...,
			)
			if len(representative.Risk.MissingMilestones) > 0 {
				entry.FirstMissingMilestone = representative.Risk.MissingMilestones[0]
			}
			localProtocolStates := make(map[string]bool)
			allTesting := make([]scenarioTestingResult, 0, len(episode.BranchTesting)+1)
			if episode.Testing != nil {
				allTesting = append(allTesting, *episode.Testing)
			}
			for _, branch := range episode.BranchTesting {
				allTesting = append(allTesting, branch.Testing)
			}
			for _, testing := range allTesting {
				for _, sample := range testing.Bundle.CorePSS {
					keys, err := psscore.Keys(sample.State)
					if err != nil {
						return nil, errors.New("AGENTIC_EXPLORATION_MEMORY_PSS_INVALID")
					}
					localProtocolStates[keys.Protocol] = true
				}
			}
			entry.ProtocolPSSStates = len(localProtocolStates)
			for key := range localProtocolStates {
				if !seenProtocolStates[key] {
					entry.NewProtocolPSSStates++
					seenProtocolStates[key] = true
				}
			}
		}
		entry.EpisodeOutcome = agenticMemoryEpisodeOutcome(episode.Summary, entry.RiskStatus)
		memory = append(memory, entry)
	}
	if len(memory) > controlexperiment.RiskExplorationMemoryMax {
		memory = memory[len(memory)-controlexperiment.RiskExplorationMemoryMax:]
	}
	return memory, nil
}

func representativeAgenticTesting(
	selected *scenarioTestingResult,
	branches []agenticBranchTestingResult,
) *scenarioTestingResult {
	if selected != nil && selected.Risk.Status == semantic.RiskWitnessReached {
		return selected
	}
	for index := range branches {
		if branches[index].Testing.Risk.Status == semantic.RiskWitnessReached {
			return &branches[index].Testing
		}
	}
	if selected != nil {
		return selected
	}
	return &branches[0].Testing
}

func agenticMemoryEpisodeOutcome(
	summary agenticEpisodeArtifact,
	riskStatus string,
) string {
	if summary.Status == agenticEpisodeExecutionFailed {
		return controlexperiment.RiskMemoryOutcomeExecutionFailed
	}
	if summary.ScenarioStopReason == controlexperiment.ScenarioAgentStopHypothesisAbandoned {
		return controlexperiment.RiskMemoryOutcomeHypothesisAbandoned
	}
	if summary.Status == agenticEpisodeTokenStopped ||
		summary.ScenarioDecisionsUsed >= summary.Budget.MaxRuntimeDecisions ||
		summary.ScenarioStopReason == controlexperiment.ScenarioAgentStopDecisionBudget ||
		summary.ScenarioStopReason == controlexperiment.ScenarioAgentStopCallBudget {
		return controlexperiment.RiskMemoryOutcomeBudgetExhausted
	}
	if summary.Status == agenticEpisodeCompleted {
		if riskStatus == semantic.RiskWitnessNotReached {
			return controlexperiment.RiskMemoryOutcomeRiskNearMiss
		}
		return controlexperiment.RiskMemoryOutcomeExecutionCompleted
	}
	return controlexperiment.RiskMemoryOutcomePlanningStopped
}

func agenticEvidenceExecutionWorkUnits(work agenticEpisodeWork) int {
	result := work.QualifiedExecution.Primary.WorkUnits + work.QualifiedExecution.Replay.WorkUnits
	for _, branch := range work.BranchQualifiedExecutions {
		result += branch.Work.Primary.WorkUnits + branch.Work.Replay.WorkUnits
	}
	return result
}

func agenticExplorationReasonCodes(
	feedback *controlexperiment.RiskAgentFeedback,
	scenario []agenticScenarioAttemptArtifact,
) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(scenario)+1)
	appendReason := func(reason string) {
		if reason != "" && !seen[reason] {
			seen[reason] = true
			result = append(result, reason)
		}
	}
	if feedback != nil {
		appendReason(feedback.ReasonCode)
		for _, review := range feedback.Reviews {
			appendReason(review.ReasonCode)
		}
	}
	for _, attempt := range scenario {
		for _, gap := range attempt.CapabilityGaps {
			appendReason(gap.Code)
		}
	}
	return result
}

func agenticExplorationCapabilityGaps(
	scenario []agenticScenarioAttemptArtifact,
) []controlexperiment.AgentCapabilityGap {
	seen := make(map[string]bool)
	var result []controlexperiment.AgentCapabilityGap
	for _, attempt := range scenario {
		for _, gap := range attempt.CapabilityGaps {
			key := gap.Code + "\x00" + gap.Reference + "\x00" + gap.Summary
			if !seen[key] {
				seen[key] = true
				result = append(result, gap)
			}
		}
	}
	return result
}

type agenticCapabilityAdaptationMetrics struct {
	GapAttempts         int
	RepeatedGapAttempts int
	RepairAttempts      int
	RepairExecutions    int
}

// capability adaptation is computed only from durable Scenario attempt
// evidence. A repeated signature ignores the step ID because a revised plan
// may rename the step while requesting the same unavailable control.
func agenticCapabilityAdaptationFromAttempts(
	attempts []agenticScenarioAttemptArtifact,
) agenticCapabilityAdaptationMetrics {
	var metrics agenticCapabilityAdaptationMetrics
	seen := make(map[string]bool)
	for index, attempt := range attempts {
		if len(attempt.CapabilityGaps) == 0 {
			continue
		}
		metrics.GapAttempts++
		current := make(map[string]bool, len(attempt.CapabilityGaps))
		for _, gap := range attempt.CapabilityGaps {
			signature := gap.Code + "\x00" + gap.Summary
			current[signature] = true
		}
		repeated := false
		for signature := range current {
			repeated = repeated || seen[signature]
		}
		if repeated {
			metrics.RepeatedGapAttempts++
		}
		for signature := range current {
			seen[signature] = true
		}
		if index+1 < len(attempts) {
			metrics.RepairAttempts++
			next := attempts[index+1]
			if next.EnteredExecution && len(next.CapabilityGaps) == 0 {
				metrics.RepairExecutions++
			}
		}
	}
	return metrics
}

func applyAgenticCapabilityAdaptationMetrics(
	metrics *agenticEpisodeMetrics,
	attempts []agenticScenarioAttemptArtifact,
) {
	adaptation := agenticCapabilityAdaptationFromAttempts(attempts)
	metrics.CapabilityGapAttempts = adaptation.GapAttempts
	metrics.RepeatedCapabilityGapAttempts = adaptation.RepeatedGapAttempts
	metrics.CapabilityRepairAttempts = adaptation.RepairAttempts
	metrics.CapabilityRepairExecutions = adaptation.RepairExecutions
}
