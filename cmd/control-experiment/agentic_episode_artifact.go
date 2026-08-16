package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	agenticEpisodeSummaryFile = "summary.json"
	agenticEpisodeBundleFile  = "bundle.json"
)

// agenticEpisodeArtifact is deliberately compact. Exact prompts and provider
// responses remain in the two journals; the full Trace remains in one
// ExecutionBundle. This summary keeps only navigation and accounting facts.
type agenticEpisodeArtifact struct {
	TargetID              string                                      `json:"target_id"`
	Status                string                                      `json:"status"`
	Budget                agenticEpisodeBudget                        `json:"budget"`
	Accepted              *controlexperiment.RiskCandidateAssessment  `json:"accepted_risk,omitempty"`
	RiskAttempts          int                                         `json:"risk_attempts"`
	RiskFeedback          *controlexperiment.RiskAgentFeedback        `json:"risk_feedback,omitempty"`
	ScenarioStatus        string                                      `json:"scenario_status,omitempty"`
	ScenarioAttempts      int                                         `json:"scenario_attempts"`
	RiskProviderCalls     []controlexperiment.StatelessAgentCallAudit `json:"risk_provider_calls"`
	ScenarioProviderCalls []controlexperiment.StatelessAgentCallAudit `json:"scenario_provider_calls,omitempty"`
	Failure               *controlexperiment.MethodFailure            `json:"failure,omitempty"`
	PlanID                string                                      `json:"plan_id,omitempty"`
	RiskResultID          string                                      `json:"risk_result_id,omitempty"`
	Metrics               agenticEpisodeMetrics                       `json:"metrics"`
	Work                  agenticEpisodeWork                          `json:"work"`
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
	Summary agenticEpisodeArtifact
	Testing *scenarioTestingResult
}

func newAgenticEpisodeArtifact(
	targetID string,
	budget agenticEpisodeBudget,
	result agenticEpisodeResult,
) (agenticEpisodeArtifact, error) {
	artifact := agenticEpisodeArtifact{
		TargetID: targetID, Status: result.Status, Budget: budget,
		RiskAttempts: len(result.RiskAgent.Attempts),
		RiskProviderCalls: append([]controlexperiment.StatelessAgentCallAudit(nil),
			result.RiskProviderCalls...),
		ScenarioProviderCalls: append([]controlexperiment.StatelessAgentCallAudit(nil),
			result.ScenarioProviderCalls...),
		Failure: cloneAgenticEpisodeFailure(result.Failure), Metrics: result.Metrics, Work: result.Work,
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
		artifact.ScenarioAttempts = len(result.Scenario.Agent.Attempts)
	}
	if result.Testing != nil {
		artifact.PlanID = result.Testing.PlanID
		artifact.RiskResultID = result.Testing.Risk.ID
	}
	if err := artifact.validateCompact(); err != nil {
		return agenticEpisodeArtifact{}, err
	}
	if result.Status == agenticEpisodeCompleted {
		var metrics agenticEpisodeMetrics
		var metricsErr error
		if result.Testing != nil {
			metrics, metricsErr = testingAgenticEpisodeMetrics(*result.Testing)
		}
		if result.Testing == nil || artifact.Accepted == nil ||
			result.Testing.validateExecutionStructure() != nil ||
			metricsErr != nil || artifact.Metrics != metrics ||
			!reflect.DeepEqual(artifact.Work.QualifiedExecution, result.Testing.Bundle.Work) {
			return agenticEpisodeArtifact{}, errors.New("AGENTIC_EPISODE_ARTIFACT_TESTING_INVALID")
		}
	} else if result.Testing != nil {
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
	if artifact.Status != agenticEpisodeCompleted {
		if _, err := os.Lstat(filepath.Join(clean, agenticEpisodeBundleFile)); !os.IsNotExist(err) {
			return recoveredAgenticEpisode{}, false, errors.New("AGENTIC_EPISODE_RECOVERY_UNEXPECTED_BUNDLE")
		}
		return recovered, true, nil
	}
	var bundle controlexperiment.ExecutionBundle
	if err := readStrictJSONFile(filepath.Join(clean, agenticEpisodeBundleFile), 64<<20, &bundle); err != nil ||
		bundle.Validate() != nil {
		return recoveredAgenticEpisode{}, false, errors.New("AGENTIC_EPISODE_RECOVERY_BUNDLE_INVALID")
	}
	assessment, projector, err := recoverAgenticEpisodeAssessment(artifact, binding, bundle)
	if err != nil {
		return recoveredAgenticEpisode{}, false, err
	}
	risk, err := projector.Project(artifact.RiskResultID, assessment.Spec, bundle.Trace)
	if err != nil {
		return recoveredAgenticEpisode{}, false, err
	}
	testing, err := binding.Testing(artifact.PlanID, bundle, risk, assessment.Spec, projector)
	metrics, metricsErr := testingAgenticEpisodeMetrics(testing)
	if err != nil || metricsErr != nil || metrics != artifact.Metrics ||
		!reflect.DeepEqual(artifact.Work.QualifiedExecution, bundle.Work) {
		return recoveredAgenticEpisode{}, false, errors.New("AGENTIC_EPISODE_RECOVERY_EVIDENCE_DRIFT")
	}
	recovered.Testing = &testing
	return recovered, true, nil
}

func (artifact agenticEpisodeArtifact) validateCompact() error {
	if artifact.TargetID == "" || artifact.Budget.validate() != nil ||
		artifact.RiskAttempts < 0 || artifact.ScenarioAttempts < 0 ||
		artifact.RiskAttempts != len(artifact.RiskProviderCalls) ||
		artifact.ScenarioAttempts != len(artifact.ScenarioProviderCalls) ||
		artifact.Work.Model != modelWorkFromAgentAudits(append(
			append([]controlexperiment.StatelessAgentCallAudit(nil), artifact.RiskProviderCalls...),
			artifact.ScenarioProviderCalls...,
		)) {
		return errors.New("AGENTIC_EPISODE_ARTIFACT_ACCOUNTING_INVALID")
	}
	for _, audit := range append(
		append([]controlexperiment.StatelessAgentCallAudit(nil), artifact.RiskProviderCalls...),
		artifact.ScenarioProviderCalls...,
	) {
		if audit.Validate() != nil {
			return errors.New("AGENTIC_EPISODE_ARTIFACT_AUDIT_INVALID")
		}
	}
	if artifact.Accepted != nil && validateAgenticEpisodeAssessment(*artifact.Accepted) != nil {
		return errors.New("AGENTIC_EPISODE_ARTIFACT_ASSESSMENT_INVALID")
	}
	switch artifact.Status {
	case agenticEpisodeRiskStopped:
		if artifact.Failure != nil || artifact.Accepted != nil || artifact.Metrics.CandidateAccepted ||
			artifact.ScenarioStatus != "" || artifact.PlanID != "" || artifact.RiskResultID != "" {
			return errors.New("AGENTIC_EPISODE_ARTIFACT_RISK_STOP_INVALID")
		}
	case agenticEpisodeScenarioStopped, agenticEpisodeTokenStopped:
		if artifact.Failure != nil || artifact.Metrics.CandidateAccepted != (artifact.Accepted != nil) ||
			artifact.PlanID != "" || artifact.RiskResultID != "" {
			return errors.New("AGENTIC_EPISODE_ARTIFACT_STOP_INVALID")
		}
	case agenticEpisodeExecutionFailed:
		if artifact.Accepted == nil || !artifact.Metrics.CandidateAccepted || artifact.Failure == nil ||
			artifact.Failure.Phase == "" || artifact.Failure.Code == "" || artifact.Failure.Decision <= 0 ||
			artifact.Failure.Terminal == nil || artifact.Failure.Terminal.Validate() != nil ||
			artifact.PlanID != "" || artifact.RiskResultID != "" {
			return errors.New("AGENTIC_EPISODE_ARTIFACT_EXECUTION_FAILURE_INVALID")
		}
	case agenticEpisodeCompleted:
		if artifact.Failure != nil || artifact.Accepted == nil || !artifact.Metrics.CandidateAccepted ||
			artifact.ScenarioStatus != controlexperiment.ScenarioAgentCompleted ||
			artifact.PlanID == "" || artifact.RiskResultID == "" {
			return errors.New("AGENTIC_EPISODE_ARTIFACT_COMPLETED_INVALID")
		}
	default:
		return errors.New("AGENTIC_EPISODE_ARTIFACT_STATUS_INVALID")
	}
	return nil
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
		!assessment.Qualification.Qualified || len(assessment.Qualification.Issues) != 0 {
		return errors.New("AGENTIC_EPISODE_ASSESSMENT_INVALID")
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

func testingAgenticEpisodeMetrics(testing scenarioTestingResult) (agenticEpisodeMetrics, error) {
	metrics := agenticEpisodeMetrics{
		CandidateAccepted: true,
		RiskReached:       testing.Risk.Status == semantic.RiskWitnessReached,
		CorePSSSamples:    testing.CorePSSSamples,
		UniquePSSStates:   testing.UniqueCorePSSStates,
		OracleFindings:    len(testing.Oracle.Violations),
	}
	states := make([]psscore.State, len(testing.Bundle.CorePSS))
	for index, sample := range testing.Bundle.CorePSS {
		states[index] = sample.State
	}
	views, err := psscore.SummarizeStates(states)
	if err != nil || views.Validate() != nil || views.Samples != testing.CorePSSSamples ||
		views.JointStates != testing.UniqueCorePSSStates {
		return agenticEpisodeMetrics{}, errors.New("AGENTIC_EPISODE_PSS_VIEWS_INVALID")
	}
	metrics.ProtocolPSSStates = views.ProtocolStates
	metrics.ControlPSSStates = views.ControlStates
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
			(episode.Summary.Status == agenticEpisodeCompleted) != (episode.Testing != nil) {
			return nil, errors.New("AGENTIC_EXPLORATION_MEMORY_EPISODE_INVALID")
		}
		entry := controlexperiment.RiskExplorationMemoryEntry{
			Episode: index + 1, EpisodeOutcome: episode.Summary.Status,
			ModelCalls:  episode.Summary.Work.Model.Calls,
			ModelTokens: episode.Summary.Work.Model.TotalTokens,
			SearchWorkUnits: episode.Summary.Work.ScenarioFrontier.WorkUnits +
				episode.Summary.Work.ScenarioSearch.TotalWorkUnits,
			ExecutionWorkUnits: episode.Summary.Work.QualifiedExecution.Primary.WorkUnits +
				episode.Summary.Work.QualifiedExecution.Replay.WorkUnits,
			MechanicalReasonCodes: agenticExplorationReasonCodes(episode.Summary.RiskFeedback),
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
		if episode.Testing != nil {
			metrics, err := testingAgenticEpisodeMetrics(*episode.Testing)
			if err != nil || metrics != episode.Summary.Metrics ||
				episode.Testing.validateExecutionStructure() != nil {
				return nil, errors.New("AGENTIC_EXPLORATION_MEMORY_EVIDENCE_INVALID")
			}
			entry.RiskStatus = episode.Testing.Risk.Status
			entry.SatisfiedMilestones = append(
				[]string(nil), episode.Testing.Risk.SatisfiedMilestones...,
			)
			if len(episode.Testing.Risk.MissingMilestones) > 0 {
				entry.FirstMissingMilestone = episode.Testing.Risk.MissingMilestones[0]
			}
			entry.OracleFindings = len(episode.Testing.Oracle.Violations)
			localProtocolStates := make(map[string]bool, len(episode.Testing.Bundle.CorePSS))
			for _, sample := range episode.Testing.Bundle.CorePSS {
				keys, err := psscore.Keys(sample.State)
				if err != nil {
					return nil, errors.New("AGENTIC_EXPLORATION_MEMORY_PSS_INVALID")
				}
				localProtocolStates[keys.Protocol] = true
			}
			entry.ProtocolPSSStates = len(localProtocolStates)
			for key := range localProtocolStates {
				if !seenProtocolStates[key] {
					entry.NewProtocolPSSStates++
					seenProtocolStates[key] = true
				}
			}
		}
		memory = append(memory, entry)
	}
	if len(memory) > controlexperiment.RiskExplorationMemoryMax {
		memory = memory[len(memory)-controlexperiment.RiskExplorationMemoryMax:]
	}
	return memory, nil
}

func agenticExplorationReasonCodes(
	feedback *controlexperiment.RiskAgentFeedback,
) []string {
	if feedback == nil {
		return nil
	}
	seen := make(map[string]bool)
	result := make([]string, 0, len(feedback.Reviews)+1)
	appendReason := func(reason string) {
		if reason != "" && !seen[reason] {
			seen[reason] = true
			result = append(result, reason)
		}
	}
	appendReason(feedback.ReasonCode)
	for _, review := range feedback.Reviews {
		appendReason(review.ReasonCode)
	}
	return result
}
