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
	scenarioAgentPromptVersion       = "scenario-agent-complete-intent-v8"
	scenarioPlanStructuredOutputName = "scenario_plan_v2"
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
	output, err := scenarioPlanStructuredOutput(view.MaxSteps)
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

func scenarioPlanStructuredOutput(maxSteps int) (openRouterStructuredOutput, error) {
	if maxSteps <= 0 || maxSteps > controlexperiment.ScenarioPlanMaxSteps {
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
	schema := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"id": stringField,
			"steps": map[string]any{
				"type": "array", "minItems": 1, "maxItems": maxSteps,
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
	encoded, err := json.Marshal(schema)
	if err != nil {
		return openRouterStructuredOutput{}, err
	}
	return openRouterStructuredOutput{Name: scenarioPlanStructuredOutputName, Schema: encoded}, nil
}

func scenarioAgentPrompt(
	spec semantic.RiskWitnessSpec,
	view controlexperiment.ScenarioAgentView,
) (string, string, error) {
	if spec.Validate() != nil || view.Knowledge.Validate() != nil ||
		view.TargetSurface != nil && view.TargetSurface.Validate() != nil ||
		view.Hypothesis.Validate(
			view.Knowledge, spec, controlexperiment.ScenarioPlanningBackendID,
		) != nil || view.AcceptedHypothesis != nil &&
		view.AcceptedHypothesis.Validate(view.Knowledge, view.Hypothesis, spec) != nil ||
		view.Frontier.Validate(spec) != nil || view.MaxSteps <= 0 ||
		!scenarioPromptMilestonesMatch(spec, view.OrderedMilestones) ||
		view.Semantics.Validate(view.Frontier) != nil ||
		view.MaxSteps > controlexperiment.ScenarioPlanMaxSteps || len(view.Frontier.Actions) == 0 {
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
			AcceptedHypothesis *controlexperiment.AcceptedHypothesisContext `json:"accepted_hypothesis"`
			TargetSurface      *controlexperiment.AgentTargetSurface        `json:"target_surface,omitempty"`
			OrderedMilestones  []string                                     `json:"ordered_milestones"`
			Frontier           controlexperiment.RiskFrontierView           `json:"root_frontier"`
			Semantics          controlexperiment.ScenarioSemanticExposure   `json:"action_semantics"`
			MaxSteps           int                                          `json:"max_steps"`
			Prior              *controlexperiment.ScenarioAgentFeedback     `json:"prior_feedback,omitempty"`
		}{
			AcceptedHypothesis: promptView.AcceptedHypothesis,
			TargetSurface:      promptView.TargetSurface, OrderedMilestones: promptView.OrderedMilestones,
			Frontier: promptView.Frontier, Semantics: promptView.Semantics,
			MaxSteps: promptView.MaxSteps, Prior: promptView.Prior,
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
	system := "Return exactly one ScenarioPlan JSON object and no prose. Use only id, steps, and selector_fields listed in " +
		"the frozen input. action_id is valid only for an Action in the supplied current root_frontier. " +
		"action_semantics only describes the bound current Actions and grants no authority to invent Actions or facts. " +
		"Never copy an ActionID from prior_feedback. Never add budgets, faults, assertions, verdicts, or digests."
	user := "Create one complete but bounded test intent of at most max_steps that advances the supplied hypothesis. " +
		"Use target_surface as the authoritative current topology, workload, runtime and fault allowance. " +
		implementationContext +
		"A later step may use after_milestone only with an ID listed in ordered_milestones; the trusted " +
		"executor will advance ordinary effects, messages, and naturally due timers until that milestone is observed. " +
		"A trusted concretizer requires " +
		"each selector to match exactly one current admissible Action. A completed prior_feedback means the trusted root has " +
		"advanced and this plan must continue from the supplied current frontier. A stopped prior_feedback includes the complete " +
		"previous_plan and failed_step; return a complete revised plan from the current frontier and repair its mechanical reason. " +
		"When present, prior_feedback.progress_delta is the compact trusted account of decisions, new milestones, the first missing " +
		"milestone, transition novelty, repeated scheduling-pattern depth, and recent Action kinds. Use it to continue, revise, or " +
		"change the intervention; repetition is search feedback and is not itself a protocol verdict. " +
		"Frozen input JSON:\n" + string(encoded)
	if view.MaxSteps == 1 {
		system += " Return exactly one step using the exact action_id of one current Action."
		user = "Choose one current strategic intervention that advances the hypothesis. After it executes, a deterministic " +
			"trusted closure handles ordinary complete-effect, deliver-message, and fire-temporal-event progress until the temporary " +
			"single-step compatibility checkpoint, the client operation terminates, natural progress is quiescent, or the total " +
			"decision budget ends. prior_feedback " +
			"contains the previous strategic intervention and the closure stop reason; routine closure steps remain in the audit rather " +
			"than this prompt. Treat the supplied current frontier and Risk progress as authoritative. Frozen input JSON:\n" +
			string(encoded)
	} else {
		system += " For every step after the first, omit action_id and use stable semantic selector fields because executing an " +
			"earlier step rebuilds the frontier and may invalidate every current ActionID."
	}
	return system, user, nil
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
