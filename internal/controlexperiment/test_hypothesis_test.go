package controlexperiment

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

func TestHypothesisRequiresExplicitAllowedBackend(t *testing.T) {
	riskSpec, err := semantic.NewRiskWitnessSpec(
		"hypothesis-risk", "fixture-cft", "backend-selection",
		[]string{"started", "completed"},
		[]semantic.RiskWitnessOrder{{Before: "started", After: "completed"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	knowledge, err := NewProtocolKnowledgePack(ProtocolKnowledgePack{
		ID: "hypothesis-knowledge", Family: riskSpec.FamilyID, Protocol: "fixture-consensus",
		Knowledge: []KnowledgeStatement{{ID: "backend", Text: "Use the explicitly allowed backend."}},
		Risks: []ProtocolRisk{{
			ID: riskSpec.RiskID, Summary: "Exercise explicit backend binding.",
			RequiredCapabilities: []string{"natural-time"},
			RequiredActions:      []control.ActionKind{control.ActionFireTemporal},
			AllowedBackendIDs:    []string{ScenarioPlanningBackendID},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := NewTestHypothesis(
		"explicit-backend-hypothesis", knowledge, riskSpec,
		"Validate against the backend selected by the active consumer.", ScenarioPlanningBackendID,
	)
	if err != nil || hypothesis.Validate(knowledge, riskSpec, ScenarioPlanningBackendID) != nil {
		t.Fatalf("allowed backend rejected: %#v/%v", hypothesis, err)
	}
	const retiredBackend = "retired-bounded-dfs-v1"
	if hypothesis.Validate(knowledge, riskSpec, retiredBackend) == nil {
		t.Fatal("implicit DFS compatibility was accepted")
	}
	if _, err := NewTestHypothesis(
		"unsupported-backend-hypothesis", knowledge, riskSpec,
		"Reject a backend that the knowledge pack does not allow.", retiredBackend,
	); err == nil {
		t.Fatal("constructor accepted an unsupported backend")
	}
}
