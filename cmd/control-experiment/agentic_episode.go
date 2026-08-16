package main

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	agenticEpisodeCompleted       = "completed"
	agenticEpisodeRiskStopped     = "risk-agent-stopped"
	agenticEpisodeScenarioStopped = "scenario-agent-stopped"
	agenticEpisodeTokenStopped    = "model-token-threshold-reached"
	agenticEpisodeExecutionFailed = "execution-failed"
)

var errAgenticEpisodeTokenThreshold = errors.New("AGENTIC_EPISODE_MODEL_TOKEN_THRESHOLD_REACHED")

type agenticEpisodeBudget struct {
	MaxRiskCalls         int `json:"max_risk_calls"`
	MaxScenarioCalls     int `json:"max_scenario_calls"`
	MaxTotalCalls        int `json:"max_total_calls"`
	MaxObservedTokens    int `json:"max_observed_tokens"`
	MaxScenarioPlanSteps int `json:"max_scenario_plan_steps"`
	MaxRuntimeDecisions  int `json:"max_runtime_decisions"`
}

func (budget agenticEpisodeBudget) validate() error {
	if budget.MaxRiskCalls <= 0 || budget.MaxRiskCalls > controlexperiment.RiskAgentMaxCalls ||
		budget.MaxScenarioCalls <= 0 || budget.MaxScenarioCalls > controlexperiment.ScenarioAgentMaxCalls ||
		budget.MaxTotalCalls <= 0 || budget.MaxRiskCalls+budget.MaxScenarioCalls > budget.MaxTotalCalls ||
		budget.MaxObservedTokens <= 0 || budget.MaxScenarioPlanSteps <= 0 ||
		budget.MaxScenarioPlanSteps > controlexperiment.ScenarioPlanMaxSteps ||
		budget.MaxRuntimeDecisions <= 0 || budget.MaxRuntimeDecisions > controlexperiment.ScenarioAgentMaxDecisions {
		return errors.New("AGENTIC_EPISODE_BUDGET_INVALID")
	}
	return nil
}

type agenticEpisodeMetrics struct {
	CandidateAccepted bool `json:"candidate_accepted"`
	RiskReached       bool `json:"risk_reached"`
	CorePSSSamples    int  `json:"core_pss_samples"`
	UniquePSSStates   int  `json:"unique_pss_states"`
	ProtocolPSSStates int  `json:"protocol_pss_states,omitempty"`
	ControlPSSStates  int  `json:"control_pss_states,omitempty"`
	OracleFindings    int  `json:"oracle_findings"`
}

type agenticEpisodeWork struct {
	Model              controlexperiment.ModelWork        `json:"model"`
	ScenarioFrontier   controlexperiment.PhaseWork        `json:"scenario_frontier"`
	ScenarioSearch     controlexperiment.StatelessDFSWork `json:"scenario_search"`
	QualifiedExecution controlexperiment.WorkLedger       `json:"qualified_execution"`
}

type agenticEpisodeResult struct {
	Status                string                                      `json:"status"`
	RiskAgent             controlexperiment.RiskAgentResult           `json:"risk_agent"`
	RiskProviderCalls     []controlexperiment.StatelessAgentCallAudit `json:"risk_provider_calls"`
	Scenario              *scenarioAgentEpisodeResult                 `json:"scenario,omitempty"`
	ScenarioProviderCalls []controlexperiment.StatelessAgentCallAudit `json:"scenario_provider_calls,omitempty"`
	Failure               *controlexperiment.MethodFailure            `json:"failure,omitempty"`
	Testing               *scenarioTestingResult                      `json:"testing,omitempty"`
	Metrics               agenticEpisodeMetrics                       `json:"metrics"`
	Work                  agenticEpisodeWork                          `json:"work"`
}

// agenticEpisodeObservationProjector is the only semantic surface the common
// coordinator asks a target to expose. The target remains responsible for
// translating its own evidence into these observations.
type agenticEpisodeObservationProjector interface {
	controlexperiment.ObservationHistoryProjector
	Capabilities() []semantic.ObservationCapability
}

// agenticEpisodeTarget is a thin composition boundary, not a protocol model.
// The coordinator owns Agent calls, budgets and phase order. A target supplies
// its runtime composition and qualified testing callback.
type agenticEpisodeTarget struct {
	ID                   string
	Knowledge            controlexperiment.ProtocolKnowledgePack
	Surface              controlexperiment.AgentTargetSurface
	Actions              []control.ActionKind
	ObservationProjector agenticEpisodeObservationProjector
	ScenarioInputs       func(
		controlexperiment.ScenarioRiskHypothesis,
		controlexperiment.SemanticPrefixProjector,
	) (scenarioEpisodeCoreInputs, error)
	Execute func(
		context.Context,
		controlexperiment.ScenarioRiskHypothesis,
		controlexperiment.SemanticPrefixProjector,
		controlexperiment.ScenarioExecution,
	) (scenarioTestingResult, error)
}

func (target agenticEpisodeTarget) validate() error {
	if strings.TrimSpace(target.ID) == "" || strings.ContainsAny(target.ID, " /\\") ||
		target.Knowledge.ValidateAgentMaterials() != nil || target.ObservationProjector == nil ||
		target.Surface.Validate() != nil || target.Surface.TargetID != target.ID ||
		reflect.ValueOf(target.ObservationProjector).Kind() == reflect.Pointer &&
			reflect.ValueOf(target.ObservationProjector).IsNil() ||
		semantic.ValidateObservationCapabilities(target.ObservationProjector.Capabilities()) != nil ||
		len(target.Actions) == 0 || target.ScenarioInputs == nil || target.Execute == nil {
		return errors.New("AGENTIC_EPISODE_TARGET_INVALID")
	}
	seen := make(map[control.ActionKind]bool, len(target.Actions))
	for _, action := range target.Actions {
		if action.Validate() != nil || seen[action] {
			return errors.New("AGENTIC_EPISODE_TARGET_ACTIONS_INVALID")
		}
		seen[action] = true
	}
	return nil
}

func runAgenticEpisode(
	ctx context.Context,
	target agenticEpisodeTarget,
	riskJournal *statelessAgentCallJournal,
	scenarioJournal *scenarioAgentCallJournal,
	budget agenticEpisodeBudget,
	memory []controlexperiment.RiskExplorationMemoryEntry,
	knowledgeReader controlexperiment.RiskKnowledgeReader,
	activateRiskKey func() error,
	activateScenarioKey func() error,
) (agenticEpisodeResult, error) {
	result := agenticEpisodeResult{Status: agenticEpisodeRiskStopped}
	if target.validate() != nil || riskJournal == nil || scenarioJournal == nil ||
		scenarioJournal.core == nil || riskJournal == scenarioJournal.core || budget.validate() != nil ||
		activateRiskKey == nil || activateScenarioKey == nil ||
		riskJournal.SetRoot(target.ID+"-risk-agent") != nil {
		return result, errors.New("AGENTIC_EPISODE_INPUT_INVALID")
	}
	capabilities := target.ObservationProjector.Capabilities()
	risk, runErr := controlexperiment.DiscoverRiskWithPlanner(
		ctx, controlexperiment.RiskAgentBudget{
			MaxCalls: budget.MaxRiskCalls, MaxTokens: budget.MaxObservedTokens,
		}, target.Knowledge, capabilities, target.Actions, memory, &target.Surface, knowledgeReader,
		func(ctx context.Context, view controlexperiment.RiskAgentView) (
			[]byte, controlexperiment.ModelWork, error,
		) {
			content, work, err := planRiskCandidate(ctx, riskJournal, view)
			if !errors.Is(err, errStatelessAgentCallKeyRequired) {
				return content, work, err
			}
			if err := activateRiskKey(); err != nil {
				return nil, work, err
			}
			return planRiskCandidate(ctx, riskJournal, view)
		},
	)
	result.RiskAgent = risk
	riskAudits, auditErr := riskJournal.Audits()
	if auditErr != nil {
		return result, auditErr
	}
	result.RiskProviderCalls = riskAudits
	result.Work.Model = modelWorkFromAgentAudits(result.RiskProviderCalls)
	if runErr != nil {
		return result, runErr
	}
	if risk.Accepted == nil {
		return result, nil
	}
	result.Metrics.CandidateAccepted = true
	if result.Work.Model.TotalTokens >= budget.MaxObservedTokens {
		result.Status = agenticEpisodeTokenStopped
		return result, nil
	}
	scenarioRisk, err := controlexperiment.BuildScenarioRiskHypothesis(
		target.Knowledge, *risk.Accepted, capabilities, target.Actions,
	)
	if err != nil {
		return result, err
	}
	projector, err := controlexperiment.NewLinearObservationRiskProjector(
		scenarioRisk.Spec, scenarioRisk.Predicates, target.ObservationProjector,
	)
	if err != nil {
		return result, err
	}
	coreInputs, err := target.ScenarioInputs(scenarioRisk, projector)
	if err != nil || !reflect.DeepEqual(coreInputs.Knowledge, scenarioRisk.Knowledge) ||
		!reflect.DeepEqual(coreInputs.Hypothesis, scenarioRisk.Hypothesis) ||
		!reflect.DeepEqual(coreInputs.RiskSpec, scenarioRisk.Spec) || coreInputs.RiskProjector == nil ||
		coreInputs.RiskProjector.ID() != projector.ID() {
		return result, errors.New("AGENTIC_EPISODE_TARGET_SCENARIO_INVALID")
	}
	coreInputs.TargetSurface = &target.Surface
	rootID := target.ID + "-agentic-" + risk.Accepted.Candidate.ID
	coreInputs.RootID = rootID
	if scenarioJournal.SetRoot(rootID) != nil {
		return result, errors.New("AGENTIC_EPISODE_SCENARIO_ROOT_INVALID")
	}
	scenarioObservedTokens := 0
	scenario, scenarioErr := runScenarioEpisodeCore(
		ctx, coreInputs, budget.MaxScenarioCalls, budget.MaxScenarioPlanSteps,
		budget.MaxRuntimeDecisions,
		func(ctx context.Context, view controlexperiment.ScenarioAgentView) (
			[]byte, controlexperiment.ModelWork, error,
		) {
			content, work, err := scenarioJournal.Planner(ctx, scenarioRisk.Spec, view)
			if errors.Is(err, errStatelessAgentCallKeyRequired) {
				if err := activateScenarioKey(); err != nil {
					return nil, work, err
				}
				content, work, err = scenarioJournal.Planner(ctx, scenarioRisk.Spec, view)
			}
			if err == nil {
				scenarioObservedTokens += work.TotalTokens
				if result.Work.Model.TotalTokens+scenarioObservedTokens > budget.MaxObservedTokens {
					return nil, work, errAgenticEpisodeTokenThreshold
				}
			}
			return content, work, err
		},
	)
	result.Scenario = &scenario
	result.ScenarioProviderCalls, err = scenarioJournal.Audits()
	if err != nil {
		return result, err
	}
	result.Work.Model = modelWorkFromAgentAudits(result.RiskProviderCalls)
	addAgentModelWork(&result.Work.Model, modelWorkFromAgentAudits(result.ScenarioProviderCalls))
	result.Work.ScenarioFrontier = scenario.FrontierWork
	result.Work.ScenarioSearch = scenario.Agent.ExecutionWork
	if errors.Is(scenarioErr, errAgenticEpisodeTokenThreshold) {
		result.Status = agenticEpisodeTokenStopped
		return result, nil
	}
	if scenarioErr != nil {
		if scenario.Failure != nil {
			result.Status = agenticEpisodeExecutionFailed
			result.Failure = cloneAgenticEpisodeFailure(scenario.Failure)
			return result, nil
		}
		return result, scenarioErr
	}
	if scenario.Agent.Execution == nil {
		result.Status = agenticEpisodeScenarioStopped
		return result, nil
	}
	testing, err := target.Execute(ctx, scenarioRisk, projector, *scenario.Agent.Execution)
	if err != nil {
		return result, err
	}
	result.Testing = &testing
	result.Status = agenticEpisodeCompleted
	result.Metrics, err = testingAgenticEpisodeMetrics(testing)
	if err != nil {
		return result, err
	}
	result.Work.QualifiedExecution = testing.Bundle.Work
	return result, nil
}

func cloneAgenticEpisodeFailure(failure *controlexperiment.MethodFailure) *controlexperiment.MethodFailure {
	if failure == nil {
		return nil
	}
	cloned := *failure
	if failure.Terminal != nil {
		terminal := *failure.Terminal
		terminal.AttemptedAction.Parameters = append(
			[]byte(nil), failure.Terminal.AttemptedAction.Parameters...,
		)
		cloned.Terminal = &terminal
	}
	return &cloned
}

func modelWorkFromAgentAudits(audits []controlexperiment.StatelessAgentCallAudit) controlexperiment.ModelWork {
	var result controlexperiment.ModelWork
	for _, audit := range audits {
		addAgentModelWork(&result, audit.Work)
	}
	return result
}
