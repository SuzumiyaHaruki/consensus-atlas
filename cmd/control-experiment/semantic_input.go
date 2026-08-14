package main

import (
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func buildSemanticAuthoring(
	knowledgeSource controlexperiment.ProtocolKnowledgePack,
	hypothesisSource controlexperiment.TestHypothesis,
	riskSpec semantic.RiskWitnessSpec,
	protocol string,
	constructionBackend string,
	consumerBackend string,
) (controlexperiment.ProtocolKnowledgePack, controlexperiment.TestHypothesis, error) {
	if knowledgeSource.SchemaVersion != "" || knowledgeSource.Digest != "" ||
		hypothesisSource.SchemaVersion != "" || hypothesisSource.KnowledgeDigest != "" ||
		hypothesisSource.RiskSpecDigest != "" || hypothesisSource.Digest != "" ||
		hypothesisSource.RiskID != riskSpec.RiskID {
		return controlexperiment.ProtocolKnowledgePack{}, controlexperiment.TestHypothesis{},
			errors.New("SEMANTIC_INPUT_AUTHORING_FIELDS_INVALID")
	}
	knowledge, err := controlexperiment.NewProtocolKnowledgePack(knowledgeSource)
	if err != nil || knowledge.Family != riskSpec.FamilyID || knowledge.Protocol != protocol {
		return controlexperiment.ProtocolKnowledgePack{}, controlexperiment.TestHypothesis{},
			errors.New("SEMANTIC_INPUT_KNOWLEDGE_INVALID")
	}
	hypothesis, err := controlexperiment.NewTestHypothesis(
		hypothesisSource.ID, knowledge, riskSpec, hypothesisSource.Rationale, constructionBackend,
	)
	if err != nil || hypothesis.Validate(knowledge, riskSpec, consumerBackend) != nil {
		return controlexperiment.ProtocolKnowledgePack{}, controlexperiment.TestHypothesis{},
			errors.New("SEMANTIC_INPUT_HYPOTHESIS_INVALID")
	}
	return knowledge, hypothesis, nil
}
