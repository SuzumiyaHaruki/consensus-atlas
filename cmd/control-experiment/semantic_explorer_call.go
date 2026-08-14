package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const semanticExplorerPromptVersion = "semantic-explorer-permutation-v1"

// semanticExplorerCallJournal is intentionally a narrow facade over the
// existing durable provider-call journal. It adds no second dispatch,
// recovery, response-identity or model-work implementation.
type semanticExplorerCallJournal struct {
	core *statelessAgentCallJournal
}

func newSemanticExplorerCallJournal(
	directory string,
	client openRouterIntentClient,
	key string,
) (*semanticExplorerCallJournal, error) {
	core, err := newStatelessAgentCallJournal(directory, client, key)
	if err != nil {
		return nil, err
	}
	return &semanticExplorerCallJournal{core: core}, nil
}

func recoverSemanticExplorerCallJournal(
	directory string,
	client openRouterIntentClient,
) (*semanticExplorerCallJournal, error) {
	core, err := recoverStatelessAgentCallJournal(directory, client)
	if err != nil {
		return nil, err
	}
	return &semanticExplorerCallJournal{core: core}, nil
}

func (journal *semanticExplorerCallJournal) ActivateKey(key string) error {
	if journal == nil {
		return errors.New("SEMANTIC_EXPLORER_CALL_JOURNAL_INVALID")
	}
	return journal.core.ActivateKey(key)
}

func (journal *semanticExplorerCallJournal) ClearKey() {
	if journal != nil {
		journal.core.ClearKey()
	}
}

func (journal *semanticExplorerCallJournal) SetRoot(rootID string) error {
	if journal == nil {
		return errors.New("SEMANTIC_EXPLORER_CALL_JOURNAL_INVALID")
	}
	return journal.core.SetRoot(rootID)
}

func (journal *semanticExplorerCallJournal) Calls() int {
	if journal == nil {
		return 0
	}
	return journal.core.Calls()
}

func (journal *semanticExplorerCallJournal) Audits() (
	[]controlexperiment.StatelessAgentCallAudit,
	error,
) {
	if journal == nil {
		return nil, errors.New("SEMANTIC_EXPLORER_CALL_JOURNAL_INVALID")
	}
	return journal.core.Audits()
}

func (journal *semanticExplorerCallJournal) Planner(
	ctx context.Context,
	view controlexperiment.SemanticExplorerAgentView,
) ([]byte, controlexperiment.ModelWork, error) {
	if journal == nil || journal.core == nil {
		return nil, controlexperiment.ModelWork{}, errors.New("SEMANTIC_EXPLORER_CALL_JOURNAL_INVALID")
	}
	system, user, err := semanticExplorerPrompt(view)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	output, err := semanticExplorerStructuredOutput(view)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	prepared, err := journal.core.client.prepare(system, user, output)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	ordinal := journal.core.next + 1
	return journal.core.planningCall(ctx, planningAgentCallPlan{
		intentID:      fmt.Sprintf("semantic-explorer-call-%d", ordinal),
		requestDigest: view.Request.Digest, prepared: prepared, contentReady: true,
	})
}

func semanticExplorerStructuredOutput(
	view controlexperiment.SemanticExplorerAgentView,
) (openRouterStructuredOutput, error) {
	candidates := make([]string, len(view.Request.Queue.Candidates))
	for index, candidate := range view.Request.Queue.Candidates {
		candidates[index] = candidate.CandidateID
	}
	if len(candidates) == 0 {
		return openRouterStructuredOutput{}, errors.New("SEMANTIC_EXPLORER_OUTPUT_SCHEMA_INVALID")
	}
	schema := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"schema_version": map[string]any{"type": "string", "const": controlexperiment.SemanticExplorerProposalVersion},
			"id":             map[string]any{"type": "string", "const": view.Request.ID},
			"request_digest": map[string]any{"type": "string", "const": view.Request.Digest},
			"queue_digest":   map[string]any{"type": "string", "const": view.Request.Queue.Digest},
			"ordered_candidate_ids": map[string]any{
				"type": "array", "minItems": len(candidates), "maxItems": len(candidates),
				"uniqueItems": true, "items": map[string]any{"type": "string", "enum": candidates},
			},
			"digest": map[string]any{"type": "string", "const": ""},
		},
		"required": []string{
			"schema_version", "id", "request_digest", "queue_digest", "ordered_candidate_ids", "digest",
		},
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return openRouterStructuredOutput{}, err
	}
	return openRouterStructuredOutput{Name: "semantic_explorer_proposal_v1", Schema: encoded}, nil
}

func semanticExplorerPrompt(
	view controlexperiment.SemanticExplorerAgentView,
) (string, string, error) {
	if view.Knowledge.Validate() != nil || view.Hypothesis.ValidateIdentity() != nil ||
		view.Request.Validate() != nil ||
		view.Request.KnowledgeDigest != view.Knowledge.Digest ||
		view.Request.HypothesisDigest != view.Hypothesis.Digest ||
		view.Hypothesis.KnowledgeDigest != view.Knowledge.Digest ||
		view.Hypothesis.RiskSpecDigest != view.Request.Queue.RiskSpecDigest ||
		!semanticExplorerKnowledgeAllows(view) {
		return "", "", errors.New("SEMANTIC_EXPLORER_PROMPT_VIEW_INVALID")
	}
	candidateIDs := make([]string, 0, len(view.Request.Queue.Candidates))
	for _, candidate := range view.Request.Queue.Candidates {
		candidateIDs = append(candidateIDs, candidate.CandidateID)
	}
	template := controlexperiment.SemanticExplorerProposal{
		SchemaVersion: controlexperiment.SemanticExplorerProposalVersion,
		ID:            view.Request.ID, RequestDigest: view.Request.Digest,
		QueueDigest: view.Request.Queue.Digest, OrderedCandidateIDs: candidateIDs,
	}
	input := struct {
		PromptVersion    string                                      `json:"prompt_version"`
		ProposalTemplate controlexperiment.SemanticExplorerProposal  `json:"proposal_template"`
		AgentView        controlexperiment.SemanticExplorerAgentView `json:"agent_view"`
	}{semanticExplorerPromptVersion, template, view}
	encoded, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return "", "", err
	}
	system := "Return exactly one JSON object and no prose. Copy proposal_template exactly, changing only ordered_candidate_ids. " +
		"ordered_candidate_ids must contain every supplied Candidate ID exactly once, but may be reordered. " +
		"Leave digest empty. Do not add, remove, invent, or edit any field or Candidate ID."
	user := "Order the frozen semantic queue using only the supplied public protocol knowledge, hypothesis, queue summaries, " +
		"and mechanical prior feedback. You cannot create actions, change the risk, budget, backend, or execute the system. " +
		"A trusted validator will reject authority expansion. Frozen input JSON:\n" + string(encoded)
	return system, user, nil
}

func semanticExplorerKnowledgeAllows(view controlexperiment.SemanticExplorerAgentView) bool {
	for _, risk := range view.Knowledge.Risks {
		if risk.ID != view.Hypothesis.RiskID {
			continue
		}
		for _, backendID := range risk.AllowedBackendIDs {
			if backendID == controlexperiment.SemanticBestFirstAlgorithmID {
				return true
			}
		}
		return false
	}
	return false
}
