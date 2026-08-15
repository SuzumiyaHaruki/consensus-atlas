package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

// planRiskCandidate reuses the single durable provider journal. Risk-specific
// code owns only the public prompt and structured response schema.
func planRiskCandidate(
	ctx context.Context,
	journal *statelessAgentCallJournal,
	view controlexperiment.RiskAgentView,
) ([]byte, controlexperiment.ModelWork, error) {
	if journal == nil || journal.client.HTTP == nil || view.Validate() != nil {
		return nil, controlexperiment.ModelWork{}, errors.New("RISK_AGENT_CALL_INPUT_INVALID")
	}
	system, user, err := riskAgentPrompt(view)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	output, err := riskAgentStructuredOutput(view)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	prepared, err := journal.client.prepare(system, user, output)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	requestDigest, err := control.CanonicalDigest(view)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	ordinal := journal.next + 1
	return journal.planningCall(ctx, planningAgentCallPlan{
		intentID: fmt.Sprintf("risk-agent-call-%d", ordinal), requestDigest: requestDigest,
		prepared: prepared, contentReady: true,
	})
}

func riskAgentPrompt(view controlexperiment.RiskAgentView) (string, string, error) {
	if view.Validate() != nil {
		return "", "", errors.New("RISK_AGENT_PROMPT_VIEW_INVALID")
	}
	encoded, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return "", "", err
	}
	system := "Return exactly one RiskCandidate JSON object and no prose. The candidate contains id, property_ref, " +
		"inspiration_ref, summary, suspected_mechanism, and ordered predicates. Use only supplied property, issue-pattern, " +
		"observation-kind, and observation-field references. Do not provide actions, capabilities, " +
		"budgets, execution facts, assertions, scores, or verdicts. Predicate order is temporal order."
	user := "Propose a falsifiable trigger hypothesis for one supplied property, using two to max_milestones ordered " +
		"semantic milestones. Use an issue pattern only as structural inspiration, or set inspiration_ref to original. " +
		"Explain the suspected mechanism without claiming a verdict. Reuse a bind_as token in at least two constraints when an entity must remain " +
		"the same across milestones. Every bind_as token must occur in at least two constraints; omit one-off field " +
		"constraints instead of binding them. A bind_as token uses lowercase letters, digits, and hyphens only. " +
		"If prior_feedback reports missing requirements, revise the candidate using only the " +
		"available surface. Trusted code will compile and qualify it before any execution. Input JSON:\n" + string(encoded)
	return system, user, nil
}

func riskAgentStructuredOutput(
	view controlexperiment.RiskAgentView,
) (openRouterStructuredOutput, error) {
	if view.Validate() != nil {
		return openRouterStructuredOutput{}, errors.New("RISK_AGENT_OUTPUT_SCHEMA_INVALID")
	}
	token := map[string]any{
		"type": "string", "minLength": 1, "maxLength": 128,
		"pattern": "^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$",
	}
	predicateVariants := make([]any, 0, len(view.ObservationCapabilities))
	for _, capability := range view.ObservationCapabilities {
		constraintVariants := make([]any, 0, len(capability.Fields)*2)
		for _, field := range capability.Fields {
			constraintVariants = append(constraintVariants, riskConstraintSchema(field, "bind_as", nil))
			if allowed := capability.Values[field]; len(allowed) > 0 {
				constraintVariants = append(constraintVariants, riskConstraintSchema(field, "equals", allowed))
			}
		}
		items := any(false)
		if len(constraintVariants) > 0 {
			items = map[string]any{"oneOf": constraintVariants}
		}
		predicateVariants = append(predicateVariants, map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"milestone_id": token,
				"kind":         map[string]any{"type": "string", "const": capability.Kind},
				"constraints": map[string]any{
					"type": "array", "maxItems": len(capability.Fields), "items": items,
				},
			},
			"required": []string{"milestone_id", "kind", "constraints"},
		})
	}
	schema := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"id": token,
			"property_ref": map[string]any{
				"type": "string", "enum": protocolPropertyIDs(view.Knowledge.Properties),
			},
			"inspiration_ref": map[string]any{
				"type": "string", "enum": issuePatternIDs(view.Knowledge.IssuePatterns),
			},
			"summary": map[string]any{
				"type": "string", "minLength": 1, "maxLength": 2048,
			},
			"suspected_mechanism": map[string]any{
				"type": "string", "minLength": 1, "maxLength": 2048,
			},
			"predicates": map[string]any{
				"type": "array", "minItems": 2, "maxItems": view.MaxMilestones,
				"items": map[string]any{"oneOf": predicateVariants},
			},
		},
		"required": []string{
			"id", "property_ref", "inspiration_ref", "summary", "suspected_mechanism", "predicates",
		},
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return openRouterStructuredOutput{}, err
	}
	return openRouterStructuredOutput{Name: "risk_candidate", Schema: encoded}, nil
}

func protocolPropertyIDs(properties []controlexperiment.ProtocolProperty) []string {
	result := make([]string, 0, len(properties))
	for _, property := range properties {
		result = append(result, property.ID)
	}
	return result
}

func issuePatternIDs(patterns []controlexperiment.HistoricalIssuePattern) []string {
	result := make([]string, 0, len(patterns)+1)
	result = append(result, "original")
	for _, pattern := range patterns {
		result = append(result, pattern.ID)
	}
	return result
}

func riskConstraintSchema(
	field semantic.ObservationField,
	valueName string,
	allowed []string,
) map[string]any {
	value := map[string]any{"type": "string", "minLength": 1, "maxLength": 128}
	if valueName == "bind_as" {
		value["pattern"] = "^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$"
	}
	if len(allowed) > 0 {
		value = map[string]any{"type": "string", "enum": allowed}
	}
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"field":   map[string]any{"type": "string", "const": field},
			valueName: value,
		},
		"required": []string{"field", valueName},
	}
}
