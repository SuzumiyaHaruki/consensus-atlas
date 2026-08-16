package main

import (
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

// buildAgenticKnowledgeAuthoring is the seed-free Hypothesis-Agent input.
func buildAgenticKnowledgeAuthoring(
	knowledgeSource controlexperiment.ProtocolKnowledgePack,
	protocol string,
) (controlexperiment.ProtocolKnowledgePack, error) {
	if knowledgeSource.SchemaVersion != "" || knowledgeSource.Digest != "" || len(knowledgeSource.Risks) != 0 {
		return controlexperiment.ProtocolKnowledgePack{}, errors.New("AGENTIC_INPUT_AUTHORING_FIELDS_INVALID")
	}
	knowledge, err := controlexperiment.NewProtocolKnowledgePack(knowledgeSource)
	if err != nil || knowledge.Protocol != protocol || knowledge.ValidateAgentMaterials() != nil {
		return controlexperiment.ProtocolKnowledgePack{}, errors.New("AGENTIC_INPUT_MATERIALS_INVALID")
	}
	return knowledge, nil
}
