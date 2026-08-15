package controlexperiment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	RiskAgentMaxCalls     = 4
	RiskCandidateMaxBytes = 64 << 10
	RiskCandidateMaxSteps = 6

	RiskAgentAccepted = "accepted"
	RiskAgentStopped  = "stopped"

	RiskAgentReasonJSON        = "risk-candidate-json-invalid"
	RiskAgentReasonCandidate   = "risk-candidate-invalid"
	RiskAgentReasonBinding     = "risk-candidate-single-use-binding"
	RiskAgentReasonProperty    = "risk-candidate-property-unknown"
	RiskAgentReasonInspiration = "risk-candidate-inspiration-unknown"
	RiskAgentReasonDuplicate   = "risk-candidate-existing-risk"
	RiskAgentReasonUnqualified = "risk-candidate-unqualified"
	RiskAgentReasonTokenBudget = "risk-agent-token-budget-exceeded"
)

var (
	errRiskCandidateJSON      = errors.New("EXPERIMENT_RISK_CANDIDATE_JSON_INVALID")
	errRiskCandidateDuplicate = errors.New("EXPERIMENT_RISK_CANDIDATE_EXISTING_RISK")
	errRiskCandidateBinding   = errors.New("EXPERIMENT_RISK_CANDIDATE_BINDING_INVALID")
	errRiskCandidateProperty  = errors.New("EXPERIMENT_RISK_CANDIDATE_PROPERTY_UNKNOWN")
	errRiskCandidatePattern   = errors.New("EXPERIMENT_RISK_CANDIDATE_INSPIRATION_UNKNOWN")
	errRiskCandidateBridge    = errors.New("EXPERIMENT_RISK_CANDIDATE_SCENARIO_BRIDGE_INVALID")
)

// RiskCandidate is deliberately only a semantic hypothesis. Family, order
// edges, capabilities, execution controls and verdicts remain trusted inputs.
type RiskCandidate struct {
	ID                 string                          `json:"id"`
	PropertyRef        string                          `json:"property_ref,omitempty"`
	InspirationRef     string                          `json:"inspiration_ref,omitempty"`
	Summary            string                          `json:"summary"`
	SuspectedMechanism string                          `json:"suspected_mechanism,omitempty"`
	Predicates         []semantic.ObservationPredicate `json:"predicates"`
}

type RiskAgentBudget struct {
	MaxCalls  int `json:"max_calls"`
	MaxTokens int `json:"max_tokens"`
}

type RiskAgentFeedback struct {
	Outcome     string                            `json:"outcome"`
	ReasonCode  string                            `json:"reason_code,omitempty"`
	CandidateID string                            `json:"candidate_id,omitempty"`
	Issues      []semantic.RiskQualificationIssue `json:"issues,omitempty"`
}

type RiskAgentView struct {
	Knowledge               ProtocolKnowledgePack            `json:"knowledge"`
	ObservationCapabilities []semantic.ObservationCapability `json:"observation_capabilities"`
	Actions                 []control.ActionKind             `json:"actions"`
	MaxMilestones           int                              `json:"max_milestones"`
	Prior                   *RiskAgentFeedback               `json:"prior_feedback,omitempty"`
}

type RiskCandidateAssessment struct {
	Candidate     RiskCandidate              `json:"candidate"`
	Spec          semantic.RiskWitnessSpec   `json:"spec"`
	Qualification semantic.RiskQualification `json:"qualification"`
}

type RiskAgentAttempt struct {
	Ordinal       int                      `json:"ordinal"`
	ResponseBytes []byte                   `json:"response_bytes"`
	Assessment    *RiskCandidateAssessment `json:"assessment,omitempty"`
	Feedback      RiskAgentFeedback        `json:"feedback"`
	ModelWork     ModelWork                `json:"model_work"`
}

type RiskAgentResult struct {
	Status    string                   `json:"status"`
	Attempts  []RiskAgentAttempt       `json:"attempts"`
	Accepted  *RiskCandidateAssessment `json:"accepted,omitempty"`
	ModelWork ModelWork                `json:"model_work"`
}

type RiskPlanner func(context.Context, RiskAgentView) ([]byte, ModelWork, error)

func (budget RiskAgentBudget) Validate() error {
	if budget.MaxCalls <= 0 || budget.MaxCalls > RiskAgentMaxCalls || budget.MaxTokens <= 0 {
		return errors.New("EXPERIMENT_RISK_AGENT_BUDGET_INVALID")
	}
	return nil
}

func (view RiskAgentView) Validate() error {
	if view.Knowledge.ValidateAgentMaterials() != nil ||
		semantic.ValidateObservationCapabilities(view.ObservationCapabilities) != nil ||
		!validRiskAgentActions(view.Actions) || view.MaxMilestones != RiskCandidateMaxSteps {
		return errors.New("EXPERIMENT_RISK_AGENT_VIEW_INVALID")
	}
	if view.Prior != nil && (view.Prior.Outcome != RiskAgentStopped || view.Prior.ReasonCode == "") {
		return errors.New("EXPERIMENT_RISK_AGENT_FEEDBACK_INVALID")
	}
	return nil
}

func DiscoverRiskWithPlanner(
	ctx context.Context,
	budget RiskAgentBudget,
	knowledge ProtocolKnowledgePack,
	capabilities []semantic.ObservationCapability,
	actions []control.ActionKind,
	planner RiskPlanner,
) (RiskAgentResult, error) {
	if budget.Validate() != nil ||
		knowledge.ValidateAgentMaterials() != nil || semantic.ValidateObservationCapabilities(capabilities) != nil ||
		!validRiskAgentActions(actions) || planner == nil {
		return RiskAgentResult{}, errors.New("EXPERIMENT_RISK_AGENT_INPUT_INVALID")
	}
	result := RiskAgentResult{Status: RiskAgentStopped}
	var prior *RiskAgentFeedback
	for ordinal := 1; ordinal <= budget.MaxCalls; ordinal++ {
		view := RiskAgentView{
			Knowledge: knowledge, ObservationCapabilities: cloneObservationCapabilities(capabilities),
			Actions: append([]control.ActionKind(nil), actions...), MaxMilestones: RiskCandidateMaxSteps,
			Prior: cloneRiskAgentFeedback(prior),
		}
		response, work, err := planner(ctx, view)
		if err != nil {
			return result, err
		}
		if !validStatelessPlannerWork(work) {
			return result, errors.New("EXPERIMENT_RISK_AGENT_MODEL_WORK_INVALID")
		}
		addModelWork(&result.ModelWork, work)
		attempt := RiskAgentAttempt{
			Ordinal: ordinal, ResponseBytes: append([]byte(nil), response...), ModelWork: work,
		}
		if result.ModelWork.TotalTokens > budget.MaxTokens {
			attempt.Feedback = RiskAgentFeedback{Outcome: RiskAgentStopped, ReasonCode: RiskAgentReasonTokenBudget}
			result.Attempts = append(result.Attempts, attempt)
			return result, nil
		}
		candidate, err := ParseRiskCandidate(response)
		if err != nil {
			reason := RiskAgentReasonCandidate
			if errors.Is(err, errRiskCandidateJSON) {
				reason = RiskAgentReasonJSON
			} else if errors.Is(err, errRiskCandidateBinding) {
				reason = RiskAgentReasonBinding
			}
			attempt.Feedback = RiskAgentFeedback{Outcome: RiskAgentStopped, ReasonCode: reason}
			result.Attempts = append(result.Attempts, attempt)
			prior = &result.Attempts[len(result.Attempts)-1].Feedback
			continue
		}
		assessment, err := AssessRiskCandidate(knowledge, candidate, capabilities, actions)
		if err != nil {
			reason := RiskAgentReasonCandidate
			switch {
			case errors.Is(err, errRiskCandidateDuplicate):
				reason = RiskAgentReasonDuplicate
			case errors.Is(err, errRiskCandidateProperty):
				reason = RiskAgentReasonProperty
			case errors.Is(err, errRiskCandidatePattern):
				reason = RiskAgentReasonInspiration
			}
			attempt.Feedback = RiskAgentFeedback{
				Outcome: RiskAgentStopped, ReasonCode: reason, CandidateID: candidate.ID,
			}
			result.Attempts = append(result.Attempts, attempt)
			prior = &result.Attempts[len(result.Attempts)-1].Feedback
			continue
		}
		attempt.Assessment = &assessment
		attempt.Feedback = RiskAgentFeedback{
			Outcome: RiskAgentAccepted, CandidateID: candidate.ID,
			Issues: append([]semantic.RiskQualificationIssue(nil), assessment.Qualification.Issues...),
		}
		if !assessment.Qualification.Qualified {
			attempt.Feedback.Outcome = RiskAgentStopped
			attempt.Feedback.ReasonCode = RiskAgentReasonUnqualified
			result.Attempts = append(result.Attempts, attempt)
			prior = &result.Attempts[len(result.Attempts)-1].Feedback
			continue
		}
		result.Attempts = append(result.Attempts, attempt)
		accepted := assessment
		result.Accepted = &accepted
		result.Status = RiskAgentAccepted
		return result, nil
	}
	return result, nil
}

func ParseRiskCandidate(data []byte) (RiskCandidate, error) {
	if len(data) == 0 || len(data) > RiskCandidateMaxBytes {
		return RiskCandidate{}, errRiskCandidateJSON
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var candidate RiskCandidate
	if err := decoder.Decode(&candidate); err != nil {
		return RiskCandidate{}, errRiskCandidateJSON
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return RiskCandidate{}, errRiskCandidateJSON
	}
	if err := candidate.ValidateAgentDraft(); err != nil {
		return RiskCandidate{}, err
	}
	return candidate, nil
}

func (candidate RiskCandidate) Validate() error {
	if !validMethodToken(candidate.ID) || len(candidate.ID) > 128 ||
		candidate.Summary == "" || len(candidate.Summary) > 2048 ||
		strings.TrimSpace(candidate.Summary) != candidate.Summary || len(candidate.Predicates) < 2 ||
		len(candidate.Predicates) > RiskCandidateMaxSteps {
		return errors.New("EXPERIMENT_RISK_CANDIDATE_INVALID")
	}
	seen := make(map[string]bool, len(candidate.Predicates))
	bindings := make(map[string]int)
	for _, predicate := range candidate.Predicates {
		if !validMethodToken(predicate.MilestoneID) || len(predicate.MilestoneID) > 128 || seen[predicate.MilestoneID] {
			return errors.New("EXPERIMENT_RISK_CANDIDATE_MILESTONE_INVALID")
		}
		seen[predicate.MilestoneID] = true
		for _, constraint := range predicate.Constraints {
			if constraint.BindAs != "" {
				bindings[constraint.BindAs]++
			}
		}
	}
	for _, count := range bindings {
		if count < 2 {
			return errRiskCandidateBinding
		}
	}
	requirements, err := semantic.CompileRiskRequirements(candidate.Predicates)
	if err != nil || len(requirements.Actions) == 0 {
		return errors.New("EXPERIMENT_RISK_CANDIDATE_PREDICATE_INVALID")
	}
	return nil
}

// ValidateAgentDraft requires the richer A9e authoring fields. Validate stays
// backward compatible so previously published A9d artifacts remain readable.
func (candidate RiskCandidate) ValidateAgentDraft() error {
	if err := candidate.Validate(); err != nil {
		return err
	}
	if !validMethodToken(candidate.PropertyRef) || len(candidate.PropertyRef) > agentMaterialReferenceMaxBytes ||
		(candidate.InspirationRef != "original" && (!validMethodToken(candidate.InspirationRef) ||
			len(candidate.InspirationRef) > agentMaterialReferenceMaxBytes)) ||
		candidate.SuspectedMechanism == "" || len(candidate.SuspectedMechanism) > 2048 ||
		strings.TrimSpace(candidate.SuspectedMechanism) != candidate.SuspectedMechanism {
		return errors.New("EXPERIMENT_RISK_CANDIDATE_AGENT_FIELDS_INVALID")
	}
	if _, err := scenarioRiskCandidateKnowledge(candidate); err != nil {
		return err
	}
	return nil
}

func AssessRiskCandidate(
	knowledge ProtocolKnowledgePack,
	candidate RiskCandidate,
	capabilities []semantic.ObservationCapability,
	actions []control.ActionKind,
) (RiskCandidateAssessment, error) {
	if knowledge.Validate() != nil || candidate.ValidateAgentDraft() != nil {
		return RiskCandidateAssessment{}, errors.New("EXPERIMENT_RISK_CANDIDATE_INPUT_INVALID")
	}
	propertyFound := false
	for _, property := range knowledge.Properties {
		if property.ID == candidate.PropertyRef {
			propertyFound = true
			break
		}
	}
	if !propertyFound {
		return RiskCandidateAssessment{}, errRiskCandidateProperty
	}
	if candidate.InspirationRef != "original" {
		patternFound := false
		for _, pattern := range knowledge.IssuePatterns {
			if pattern.ID == candidate.InspirationRef {
				patternFound = true
				break
			}
		}
		if !patternFound {
			return RiskCandidateAssessment{}, errRiskCandidatePattern
		}
	}
	for _, existing := range knowledge.Risks {
		if existing.ID == candidate.ID {
			return RiskCandidateAssessment{}, errRiskCandidateDuplicate
		}
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
		candidate.ID+"-witness", knowledge.Family, candidate.ID, milestones, orders,
	)
	if err != nil {
		return RiskCandidateAssessment{}, err
	}
	qualification, err := semantic.QualifyRisk(candidate.Predicates, capabilities, actions)
	if err != nil {
		return RiskCandidateAssessment{}, err
	}
	return RiskCandidateAssessment{Candidate: candidate, Spec: spec, Qualification: qualification}, nil
}

func validRiskAgentActions(actions []control.ActionKind) bool {
	seen := make(map[control.ActionKind]bool, len(actions))
	for _, action := range actions {
		if action.Validate() != nil || seen[action] {
			return false
		}
		seen[action] = true
	}
	return len(actions) > 0
}

func cloneObservationCapabilities(
	values []semantic.ObservationCapability,
) []semantic.ObservationCapability {
	result := append([]semantic.ObservationCapability(nil), values...)
	for index := range result {
		result[index].Fields = append([]semantic.ObservationField(nil), values[index].Fields...)
		if values[index].Values != nil {
			result[index].Values = make(map[semantic.ObservationField][]string, len(values[index].Values))
			for field, allowed := range values[index].Values {
				result[index].Values[field] = append([]string(nil), allowed...)
			}
		}
	}
	return result
}

func cloneRiskAgentFeedback(value *RiskAgentFeedback) *RiskAgentFeedback {
	if value == nil {
		return nil
	}
	result := *value
	result.Issues = append([]semantic.RiskQualificationIssue(nil), value.Issues...)
	return &result
}
