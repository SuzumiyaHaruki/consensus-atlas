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
	Knowledge     ProtocolKnowledgePack
	Hypothesis    TestHypothesis
	Spec          semantic.RiskWitnessSpec
	Predicates    []semantic.ObservationPredicate
	Qualification semantic.RiskQualification
}

func BuildScenarioRiskHypothesis(
	base ProtocolKnowledgePack,
	assessment RiskCandidateAssessment,
	capabilities []semantic.ObservationCapability,
	actions []control.ActionKind,
) (ScenarioRiskHypothesis, error) {
	recomputed, err := AssessRiskCandidate(base, assessment.Candidate, capabilities, actions)
	if err != nil || !reflect.DeepEqual(recomputed, assessment) || !assessment.Qualification.Qualified {
		return ScenarioRiskHypothesis{}, errors.New("EXPERIMENT_SCENARIO_RISK_ASSESSMENT_INVALID")
	}
	definition, err := json.Marshal(assessment.Candidate.Predicates)
	if err != nil || len(definition) > 2000 {
		return ScenarioRiskHypothesis{}, errors.New("EXPERIMENT_SCENARIO_RISK_DEFINITION_INVALID")
	}
	knowledge := ProtocolKnowledgePack{
		ID:     base.ID + "-with-" + assessment.Candidate.ID,
		Family: base.Family, Protocol: base.Protocol,
		Knowledge: append(append([]KnowledgeStatement(nil), base.Knowledge...), KnowledgeStatement{
			ID:   assessment.Candidate.ID + "-ordered-predicates",
			Text: "Trusted ordered predicates: " + string(definition),
		}),
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
	return ScenarioRiskHypothesis{
		Knowledge: knowledge, Hypothesis: hypothesis, Spec: assessment.Spec,
		Predicates:    cloneObservationPredicates(assessment.Candidate.Predicates),
		Qualification: assessment.Qualification,
	}, nil
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
