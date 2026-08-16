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

const (
	scenarioAgentPromptVersion                = "scenario-agent-investigation-v9"
	scenarioInvestigationStructuredOutputName = "scenario_investigation_v3"
)

type scenarioAgentCallJournal struct {
	core *statelessAgentCallJournal
}

func newScenarioAgentCallJournal(
	directory string,
	client openRouterIntentClient,
	key string,
) (*scenarioAgentCallJournal, error) {
	core, err := newStatelessAgentCallJournal(directory, client, key)
	if err != nil {
		return nil, err
	}
	return &scenarioAgentCallJournal{core: core}, nil
}

func recoverScenarioAgentCallJournal(
	directory string,
	client openRouterIntentClient,
) (*scenarioAgentCallJournal, error) {
	core, err := recoverStatelessAgentCallJournal(directory, client)
	if err != nil {
		return nil, err
	}
	return &scenarioAgentCallJournal{core: core}, nil
}

func (journal *scenarioAgentCallJournal) SetRoot(rootID string) error {
	if journal == nil || journal.core == nil {
		return errors.New("SCENARIO_AGENT_CALL_JOURNAL_INVALID")
	}
	return journal.core.SetRoot(rootID)
}

func (journal *scenarioAgentCallJournal) ActivateKey(key string) error {
	if journal == nil || journal.core == nil {
		return errors.New("SCENARIO_AGENT_CALL_JOURNAL_INVALID")
	}
	return journal.core.ActivateKey(key)
}

func (journal *scenarioAgentCallJournal) Audits() (
	[]controlexperiment.StatelessAgentCallAudit,
	error,
) {
	if journal == nil || journal.core == nil {
		return nil, errors.New("SCENARIO_AGENT_CALL_JOURNAL_INVALID")
	}
	return journal.core.Audits()
}

func (journal *scenarioAgentCallJournal) Planner(
	ctx context.Context,
	spec semantic.RiskWitnessSpec,
	view controlexperiment.ScenarioAgentView,
) ([]byte, controlexperiment.ModelWork, error) {
	if journal == nil || journal.core == nil {
		return nil, controlexperiment.ModelWork{}, errors.New("SCENARIO_AGENT_CALL_JOURNAL_INVALID")
	}
	system, user, err := scenarioAgentPrompt(spec, view)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	output, err := scenarioInvestigationStructuredOutput(view)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	prepared, err := journal.core.client.prepare(system, user, output)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	requestDigest, err := control.CanonicalDigest(view)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	ordinal := journal.core.next + 1
	return journal.core.planningCall(ctx, planningAgentCallPlan{
		intentID:      fmt.Sprintf("scenario-agent-call-%d", ordinal),
		requestDigest: requestDigest, prepared: prepared, contentReady: true,
	})
}

func scenarioInvestigationStructuredOutput(view controlexperiment.ScenarioAgentView) (openRouterStructuredOutput, error) {
	if scenarioSelectionOnlyView(view) {
		stringField := map[string]any{"type": "string", "minLength": 1}
		schema := map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"intent":         map[string]any{"type": "string", "enum": []string{controlexperiment.ScenarioIntentSelect}},
				"from_branch_id": stringField,
			},
			"required": []string{"intent", "from_branch_id"},
		}
		encoded, err := json.Marshal(schema)
		if err != nil {
			return openRouterStructuredOutput{}, err
		}
		return openRouterStructuredOutput{Name: scenarioInvestigationStructuredOutputName, Schema: encoded}, nil
	}
	if view.MaxSteps <= 0 || view.MaxSteps > controlexperiment.ScenarioPlanMaxSteps ||
		len(view.AvailableIntents) == 0 {
		return openRouterStructuredOutput{}, errors.New("SCENARIO_AGENT_OUTPUT_SCHEMA_INVALID")
	}
	stringField := map[string]any{"type": "string", "minLength": 1}
	selector := map[string]any{
		"oneOf": []any{
			map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{"action_id": stringField},
				"required":   []string{"action_id"},
			},
			map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"kind": stringField, "node": stringField, "item_kind": stringField,
					"owner": stringField, "message_source": stringField,
					"message_target": stringField, "temporal_kind": stringField,
					"effect_kind": stringField, "durability": stringField,
				},
				"required": []string{"kind"},
			},
		},
	}
	plan := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"id": stringField,
			"steps": map[string]any{
				"type": "array", "minItems": 1, "maxItems": view.MaxSteps,
				"items": map[string]any{
					"type": "object", "additionalProperties": false,
					"properties": map[string]any{
						"id": stringField, "after_milestone": stringField, "selector": selector,
					},
					"required": []string{"id", "selector"},
				},
			},
		},
		"required": []string{"id", "steps"},
	}
	schema := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"intent":    map[string]any{"type": "string", "enum": view.AvailableIntents},
			"branch_id": stringField, "from_branch_id": stringField,
			"reference_branch_id": stringField,
			"omitted_step_ids": map[string]any{
				"type": "array", "minItems": 1, "items": stringField,
			},
			"plan": plan,
		},
		"required": []string{"intent", "plan"},
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return openRouterStructuredOutput{}, err
	}
	return openRouterStructuredOutput{Name: scenarioInvestigationStructuredOutputName, Schema: encoded}, nil
}

func scenarioAgentPrompt(
	spec semantic.RiskWitnessSpec,
	view controlexperiment.ScenarioAgentView,
) (string, string, error) {
	selectionOnly := scenarioSelectionOnlyView(view)
	if spec.Validate() != nil || view.Knowledge.Validate() != nil ||
		view.TargetSurface != nil && view.TargetSurface.Validate() != nil ||
		view.Hypothesis.Validate(
			view.Knowledge, spec, controlexperiment.ScenarioPlanningBackendID,
		) != nil || view.AcceptedHypothesis != nil &&
		view.AcceptedHypothesis.Validate(view.Knowledge, view.Hypothesis, spec) != nil ||
		view.Frontier.Validate(spec) != nil ||
		!scenarioPromptMilestonesMatch(spec, view.OrderedMilestones) ||
		view.Semantics.Validate(view.Frontier) != nil ||
		selectionOnly && (view.MaxSteps != 0 || view.DecisionAllowance != 0 ||
			view.RemainingDecisions < 0 || len(view.Branches) == 0) ||
		!selectionOnly && (view.MaxSteps <= 0 || view.MaxSteps > controlexperiment.ScenarioPlanMaxSteps ||
			view.DecisionAllowance < view.MaxSteps || view.RemainingDecisions < view.DecisionAllowance ||
			len(view.Frontier.Actions) == 0) {
		return "", "", errors.New("SCENARIO_AGENT_PROMPT_VIEW_INVALID")
	}
	promptView := view
	if view.Prior != nil {
		prior := *view.Prior
		prior.NaturalProgress = nil
		promptView.Prior = &prior
	}
	agentView := any(promptView)
	implementationContext := "Use target_dossier as implementation context: preserve stated public contracts and treat blind spots " +
		"as unavailable evidence rather than permission to invent an internal transition. "
	if promptView.AcceptedHypothesis != nil {
		agentView = struct {
			AcceptedHypothesis *controlexperiment.AcceptedHypothesisContext    `json:"accepted_hypothesis"`
			TargetSurface      *controlexperiment.AgentTargetSurface           `json:"target_surface,omitempty"`
			OrderedMilestones  []string                                        `json:"ordered_milestones"`
			Frontier           controlexperiment.RiskFrontierView              `json:"root_frontier"`
			Semantics          controlexperiment.ScenarioSemanticExposure      `json:"action_semantics"`
			MaxSteps           int                                             `json:"max_steps"`
			DecisionAllowance  int                                             `json:"decision_allowance"`
			RemainingDecisions int                                             `json:"remaining_decisions"`
			AvailableIntents   []string                                        `json:"available_intents"`
			Branches           []controlexperiment.ScenarioInvestigationBranch `json:"branches,omitempty"`
			Prior              *controlexperiment.ScenarioAgentFeedback        `json:"prior_feedback,omitempty"`
		}{
			AcceptedHypothesis: promptView.AcceptedHypothesis,
			TargetSurface:      promptView.TargetSurface, OrderedMilestones: promptView.OrderedMilestones,
			Frontier: promptView.Frontier, Semantics: promptView.Semantics,
			MaxSteps: promptView.MaxSteps, DecisionAllowance: promptView.DecisionAllowance,
			RemainingDecisions: promptView.RemainingDecisions,
			AvailableIntents:   promptView.AvailableIntents,
			Branches:           promptView.Branches, Prior: promptView.Prior,
		}
		implementationContext = "Use accepted_hypothesis as the investigated mechanism and executable witness. It is an " +
			"Agent proposal accepted for execution, not a protocol fact or verdict. "
	}
	input := struct {
		PromptVersion  string   `json:"prompt_version"`
		SelectorFields []string `json:"selector_fields"`
		AgentView      any      `json:"agent_view"`
	}{
		PromptVersion: scenarioAgentPromptVersion,
		SelectorFields: []string{
			"action_id", "kind", "node", "item_kind", "owner", "message_source",
			"message_target", "temporal_kind", "effect_kind", "durability",
		},
		AgentView: agentView,
	}
	encoded, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return "", "", err
	}
	if selectionOnly {
		system := "Return exactly one ScenarioInvestigationProposal JSON object and no prose. " +
			"This is a final zero-Action selection call: provide exactly intent=select and one from_branch_id listed in branches. " +
			"Do not provide plan, Action, assertion, verdict, budget, or any other field."
		user := "Choose the stored branch that best represents the investigation result. This call executes no Runtime Action, " +
			"but its model usage is still charged. Frozen input JSON:\n" + string(encoded)
		return system, user, nil
	}
	system := "Return exactly one ScenarioInvestigationProposal JSON object and no prose. Choose intent only from " +
		"available_intents. Except for select, the nested plan may use only id, steps, and selector_fields listed in the input. " +
		"select is a zero-Action final choice: provide only intent=select and from_branch_id, and omit plan. " +
		"action_id is valid only for an Action in the supplied current root_frontier or a branch's available_actions when " +
		"continuing from that branch. control and ablate execute from an earlier root checkpoint and must use semantic selectors. " +
		"action_semantics only describes the bound current Actions and grants no authority to invent Actions or facts. " +
		"Never copy an ActionID from prior_feedback. Never add budgets, faults, assertions, verdicts, or digests."
	user := "Create one complete but bounded investigation proposal of at most max_steps that advances the supplied hypothesis. " +
		"Use target_surface as the authoritative current topology, workload, runtime and fault allowance. " +
		implementationContext +
		"A later step may use after_milestone only with an ID listed in ordered_milestones; the trusted " +
		"executor will advance ordinary effects, messages, and naturally due timers until that milestone is observed. " +
		"A trusted concretizer requires " +
		"each selector to match exactly one current admissible Action. A completed prior_feedback means the trusted root has " +
		"advanced and this plan must continue from the supplied current frontier. A stopped prior_feedback includes the complete " +
		"previous_proposal and failed_step; use revise with a complete repaired plan from the current frontier. " +
		"Use branch with a new branch_id to retain an intervention result. Use control with a new branch_id and an existing " +
		"reference_branch_id; it will execute from the referenced branch's root checkpoint. Use ablate only when available, name " +
		"the omitted_step_ids from the reference branch's applied_interventions, and supply a shorter plan. Branch metadata reports " +
		"the strategic Actions actually applied, not merely proposed plan text. Use select with from_branch_id to finalize a stored path " +
		"without executing another Action; use from_branch_id with continue to promote and extend a path, or with branch to fork " +
		"another candidate. revise repairs only the current selected path. minimize is intentionally " +
		"unavailable until a trusted finding exists. " +
		"When present, prior_feedback.progress_delta is the compact trusted account of decisions, new milestones, the first missing " +
		"milestone, transition novelty, repeated scheduling-pattern depth, and recent Action kinds. Use it to continue, revise, or " +
		"change the intervention; repetition is search feedback and is not itself a protocol verdict. " +
		"decision_allowance bounds this proposal plus its deterministic natural-progress slice; remaining_decisions is the " +
		"episode-wide successful Action budget still available. " +
		"Frozen input JSON:\n" + string(encoded)
	if view.MaxSteps == 1 {
		system += " For every non-select intent, the nested plan must contain exactly one step."
	} else {
		system += " For every non-select intent and every step after the first, omit action_id and use stable semantic selector " +
			"fields because executing an " +
			"earlier step rebuilds the frontier and may invalidate every current ActionID."
	}
	return system, user, nil
}

func scenarioSelectionOnlyView(view controlexperiment.ScenarioAgentView) bool {
	return len(view.AvailableIntents) == 1 &&
		view.AvailableIntents[0] == controlexperiment.ScenarioIntentSelect
}

func scenarioPromptMilestonesMatch(spec semantic.RiskWitnessSpec, values []string) bool {
	if len(values) != len(spec.Milestones) {
		return false
	}
	for index, milestone := range spec.Milestones {
		if values[index] != milestone.ID {
			return false
		}
	}
	return true
}
