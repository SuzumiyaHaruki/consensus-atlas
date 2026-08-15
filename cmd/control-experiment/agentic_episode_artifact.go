package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
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
		Metrics: result.Metrics, Work: result.Work,
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
		if result.Testing == nil || artifact.Accepted == nil ||
			result.Testing.validateExecutionStructure() != nil ||
			artifact.Metrics != testingAgenticEpisodeMetrics(*result.Testing) ||
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
	if err != nil || testingAgenticEpisodeMetrics(testing) != artifact.Metrics ||
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
		if artifact.Accepted != nil || artifact.Metrics.CandidateAccepted ||
			artifact.ScenarioStatus != "" || artifact.PlanID != "" || artifact.RiskResultID != "" {
			return errors.New("AGENTIC_EPISODE_ARTIFACT_RISK_STOP_INVALID")
		}
	case agenticEpisodeScenarioStopped, agenticEpisodeTokenStopped:
		if artifact.Metrics.CandidateAccepted != (artifact.Accepted != nil) ||
			artifact.PlanID != "" || artifact.RiskResultID != "" {
			return errors.New("AGENTIC_EPISODE_ARTIFACT_STOP_INVALID")
		}
	case agenticEpisodeCompleted:
		if artifact.Accepted == nil || !artifact.Metrics.CandidateAccepted ||
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

func testingAgenticEpisodeMetrics(testing scenarioTestingResult) agenticEpisodeMetrics {
	return agenticEpisodeMetrics{
		CandidateAccepted: true,
		RiskReached:       testing.Risk.Status == semantic.RiskWitnessReached,
		CorePSSSamples:    testing.CorePSSSamples,
		UniquePSSStates:   testing.UniqueCorePSSStates,
		OracleFindings:    len(testing.Oracle.Violations),
	}
}
