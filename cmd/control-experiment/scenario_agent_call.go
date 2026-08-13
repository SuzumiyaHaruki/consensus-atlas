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

const scenarioAgentPromptVersion = "scenario-agent-plan-v1"

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
		PromptVersion string                              `json:"prompt_version"`
		PlanTemplate  controlexperiment.ScenarioPlan      `json:"plan_template"`
		AgentView     controlexperiment.ScenarioAgentView `json:"agent_view"`
	}{scenarioAgentPromptVersion, template, view}
	encoded, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return "", "", err
	}
	system := "Return exactly one ScenarioPlan JSON object and no prose. Use only id, steps, and selector fields shown by " +
		"plan_template. Use an exact action_id only for an Action currently listed in root_frontier or prior feedback; " +
		"use semantic selector fields for future steps. Never add budgets, faults, assertions, verdicts, or digests."
	user := "Create a short plan of at most max_steps that advances the supplied hypothesis. A trusted concretizer requires " +
		"each selector to match exactly one current admissible Action. If prior_feedback exists, return a complete revised plan " +
		"from the same root and repair its mechanical reason. Frozen input JSON:\n" + string(encoded)
	return system, user, nil
}
