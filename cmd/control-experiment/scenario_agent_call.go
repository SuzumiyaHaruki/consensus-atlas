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

const scenarioAgentPromptVersion = "scenario-agent-receding-horizon-v3"

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
	prepared, err := journal.core.client.prepare(system, user)
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

func scenarioAgentPrompt(
	spec semantic.RiskWitnessSpec,
	view controlexperiment.ScenarioAgentView,
) (string, string, error) {
	if spec.Validate() != nil || view.Knowledge.Validate() != nil ||
		view.Hypothesis.Validate(
			view.Knowledge, spec, controlexperiment.ScenarioPlanningBackendID,
		) != nil || view.Frontier.Validate(spec) != nil || view.MaxSteps <= 0 ||
		view.Semantics.Validate(view.Frontier) != nil ||
		view.MaxSteps > controlexperiment.ScenarioPlanMaxSteps || len(view.Frontier.Actions) == 0 {
		return "", "", errors.New("SCENARIO_AGENT_PROMPT_VIEW_INVALID")
	}
	template := controlexperiment.ScenarioPlan{
		ID: "scenario-plan", Steps: []controlexperiment.ScenarioStep{{
			ID: "step-1", Selector: controlexperiment.FrontierActionSelector{
				ActionID: view.Frontier.Actions[0].ActionID,
			},
		}},
	}
	input := struct {
		PromptVersion  string                              `json:"prompt_version"`
		PlanTemplate   controlexperiment.ScenarioPlan      `json:"plan_template"`
		SelectorFields []string                            `json:"selector_fields"`
		AgentView      controlexperiment.ScenarioAgentView `json:"agent_view"`
	}{
		PromptVersion: scenarioAgentPromptVersion,
		PlanTemplate:  template,
		SelectorFields: []string{
			"action_id", "kind", "node", "item_kind", "owner", "message_source",
			"message_target", "temporal_kind", "effect_kind", "durability",
		},
		AgentView: view,
	}
	encoded, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return "", "", err
	}
	system := "Return exactly one ScenarioPlan JSON object and no prose. Use only id, steps, and selector_fields listed in " +
		"the frozen input. action_id is valid only for an Action in the supplied current root_frontier. For every step after " +
		"the first, omit action_id and use stable semantic selector fields because executing an earlier step rebuilds the " +
		"frontier and may invalidate every current ActionID. Never copy an ActionID from prior_feedback. " +
		"action_semantics only describes the bound current Actions and grants " +
		"no authority to invent Actions or facts. Never add budgets, faults, assertions, verdicts, or digests."
	user := "Create a short plan of at most max_steps that advances the supplied hypothesis. A trusted concretizer requires " +
		"each selector to match exactly one current admissible Action. A completed prior_feedback means the trusted root has " +
		"advanced and this plan must continue from the supplied current frontier. A stopped prior_feedback means return a " +
		"complete revised plan from the current root and repair its mechanical reason. plan_template demonstrates only a " +
		"first-step exact ID; later steps must use selector_fields without action_id. Frozen input JSON:\n" + string(encoded)
	return system, user, nil
}
