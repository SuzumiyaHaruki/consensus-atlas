package controlexperiment

import (
	"encoding/json"
	"errors"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

// ScenarioRiskHypothesis is an ephemeral bridge into the existing Scenario
// Agent contract. Agent-authored content stays limited to the candidate;
// trusted code derives the backend and capability metadata below.
type ScenarioRiskHypothesis struct {
	Knowledge          ProtocolKnowledgePack
	Hypothesis         TestHypothesis
	AcceptedHypothesis AcceptedHypothesisContext
	Spec               semantic.RiskWitnessSpec
	Predicates         []semantic.ObservationPredicate
	Qualification      semantic.RiskQualification
}

// AcceptedHypothesisContext keeps Agent-authored investigation intent
// separate from curated protocol and target knowledge. It is the compact
// context exposed to the Scenario Agent; trusted validation still retains the
// original ProtocolKnowledgePack internally.
type AcceptedHypothesisContext struct {
	Candidate RiskCandidate    `json:"candidate"`
	Property  ProtocolProperty `json:"property"`
}

func (context AcceptedHypothesisContext) Validate(
	knowledge ProtocolKnowledgePack,
	hypothesis TestHypothesis,
	spec semantic.RiskWitnessSpec,
) error {
	if context.Candidate.Validate() != nil || context.Property.ID == "" ||
		context.Candidate.PropertyRef != context.Property.ID ||
		hypothesis.RiskID != context.Candidate.ID || spec.RiskID != context.Candidate.ID ||
		hypothesis.Rationale != context.Candidate.Summary {
		return errors.New("EXPERIMENT_ACCEPTED_HYPOTHESIS_CONTEXT_INVALID")
	}
	found := false
	for _, property := range knowledge.Properties {
		if property.ID == context.Property.ID {
			found = reflect.DeepEqual(property, context.Property)
			break
		}
	}
	if !found {
		return errors.New("EXPERIMENT_ACCEPTED_HYPOTHESIS_CONTEXT_PROPERTY_INVALID")
	}
	wantSpec, err := riskCandidateWitnessSpec(knowledge.Family, context.Candidate)
	if err != nil || !reflect.DeepEqual(wantSpec, spec) {
		return errors.New("EXPERIMENT_ACCEPTED_HYPOTHESIS_CONTEXT_WITNESS_INVALID")
	}
	return nil
}

func BuildScenarioRiskHypothesis(
	base ProtocolKnowledgePack,
	assessment RiskCandidateAssessment,
	capabilities []semantic.ObservationCapability,
	actions []control.ActionKind,
	targetSurfaces ...*AgentTargetSurface,
) (ScenarioRiskHypothesis, error) {
	if len(targetSurfaces) > 1 || len(targetSurfaces) == 1 && targetSurfaces[0] == nil {
		return ScenarioRiskHypothesis{}, errors.New("EXPERIMENT_SCENARIO_RISK_TARGET_INVALID")
	}
	var recomputed RiskCandidateAssessment
	var err error
	if len(targetSurfaces) == 1 {
		recomputed, err = AssessRiskCandidateForTarget(
			base, assessment.Candidate, capabilities, actions, targetSurfaces[0],
		)
	} else {
		recomputed, err = AssessRiskCandidate(base, assessment.Candidate, capabilities, actions)
	}
	if err != nil || !reflect.DeepEqual(recomputed, assessment) || !assessment.Qualification.Qualified {
		return ScenarioRiskHypothesis{}, errors.New("EXPERIMENT_SCENARIO_RISK_ASSESSMENT_INVALID")
	}
	candidateKnowledge, err := scenarioRiskCandidateKnowledge(assessment.Candidate)
	if err != nil {
		return ScenarioRiskHypothesis{}, err
	}
	knowledgeStatements := append([]KnowledgeStatement(nil), base.Knowledge...)
	knowledgeStatements = append(knowledgeStatements, candidateKnowledge...)
	knowledge := ProtocolKnowledgePack{
		ID:     base.ID + "-with-" + assessment.Candidate.ID,
		Family: base.Family, Protocol: base.Protocol,
		Knowledge:     knowledgeStatements,
		Properties:    append([]ProtocolProperty(nil), base.Properties...),
		IssuePatterns: append([]HistoricalIssuePattern(nil), base.IssuePatterns...),
		TargetDossier: cloneTargetDossier(base.TargetDossier),
		Risks: append(cloneProtocolKnowledgeRisks(base.Risks), ProtocolRisk{
			ID: assessment.Candidate.ID, Summary: assessment.Candidate.Summary,
			RequiredCapabilities: scenarioRiskObservationRequirements(assessment.Qualification.Requirements),
			RequiredActions:      append([]control.ActionKind(nil), assessment.Qualification.Requirements.Actions...),
			AllowedBackendIDs:    []string{ScenarioPlanningBackendID},
		}),
	}
	knowledge, err = NewProtocolKnowledgePack(knowledge)
	if err != nil {
		return ScenarioRiskHypothesis{}, err
	}
	hypothesis, err := NewTestHypothesis(
		assessment.Candidate.ID+"-hypothesis", knowledge, assessment.Spec,
		assessment.Candidate.Summary, ScenarioPlanningBackendID,
	)
	if err != nil {
		return ScenarioRiskHypothesis{}, err
	}
	property, ok := protocolPropertyByID(base.Properties, assessment.Candidate.PropertyRef)
	if !ok {
		return ScenarioRiskHypothesis{}, errors.New("EXPERIMENT_SCENARIO_RISK_PROPERTY_INVALID")
	}
	accepted := AcceptedHypothesisContext{
		Candidate: cloneAcceptedRiskCandidate(assessment.Candidate), Property: property,
	}
	if accepted.Validate(knowledge, hypothesis, assessment.Spec) != nil {
		return ScenarioRiskHypothesis{}, errors.New("EXPERIMENT_SCENARIO_RISK_CONTEXT_INVALID")
	}
	return ScenarioRiskHypothesis{
		Knowledge: knowledge, Hypothesis: hypothesis, AcceptedHypothesis: accepted, Spec: assessment.Spec,
		Predicates:    cloneObservationPredicates(assessment.Candidate.Predicates),
		Qualification: assessment.Qualification,
	}, nil
}

func protocolPropertyByID(properties []ProtocolProperty, id string) (ProtocolProperty, bool) {
	for _, property := range properties {
		if property.ID == id {
			return property, true
		}
	}
	return ProtocolProperty{}, false
}

func cloneAcceptedHypothesisContext(context *AcceptedHypothesisContext) *AcceptedHypothesisContext {
	if context == nil {
		return nil
	}
	cloned := *context
	cloned.Candidate = cloneAcceptedRiskCandidate(context.Candidate)
	return &cloned
}

func cloneAcceptedRiskCandidate(candidate RiskCandidate) RiskCandidate {
	candidate.RequiredFidelity = append([]string(nil), candidate.RequiredFidelity...)
	candidate.MechanismSteps = append([]RiskMechanismStep(nil), candidate.MechanismSteps...)
	for index := range candidate.MechanismSteps {
		candidate.MechanismSteps[index].SupportRefs = append(
			[]string(nil), candidate.MechanismSteps[index].SupportRefs...,
		)
	}
	candidate.Predicates = cloneObservationPredicates(candidate.Predicates)
	return candidate
}

// scenarioRiskCandidateKnowledge is shared by candidate validation and the
// Scenario bridge so an accepted candidate cannot later exceed the existing
// ProtocolKnowledge statement boundary after trusted prefixes are added.
func scenarioRiskCandidateKnowledge(candidate RiskCandidate) ([]KnowledgeStatement, error) {
	definition, err := json.Marshal(candidate.Predicates)
	if err != nil {
		return nil, errRiskCandidateBridge
	}
	statements := []KnowledgeStatement{
		{
			ID: candidate.ID + "-property",
			Text: "Investigated property " + candidate.PropertyRef +
				"; suspected mechanism: " + candidate.SuspectedMechanism,
		},
		{
			ID:   candidate.ID + "-ordered-predicates",
			Text: "Trusted ordered predicates: " + string(definition),
		},
	}
	for _, statement := range statements {
		if len(statement.Text) > protocolKnowledgeTextMaxBytes {
			return nil, errRiskCandidateBridge
		}
	}
	return statements, nil
}

func scenarioRiskObservationRequirements(requirements semantic.RiskRequirements) []string {
	result := make([]string, 0, len(requirements.Observations)*2)
	for _, observation := range requirements.Observations {
		result = append(result, "observation-"+string(observation.Kind))
		for _, field := range observation.Fields {
			result = append(result, "observation-"+string(observation.Kind)+"-"+string(field))
		}
	}
	return result
}

func cloneObservationPredicates(values []semantic.ObservationPredicate) []semantic.ObservationPredicate {
	result := append([]semantic.ObservationPredicate(nil), values...)
	for index := range result {
		result[index].Constraints = append([]semantic.ObservationConstraint(nil), values[index].Constraints...)
	}
	return result
}
