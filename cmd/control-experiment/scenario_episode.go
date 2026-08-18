package main

import (
	"context"
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

type scenarioAgentEpisodeResult struct {
	Agent         controlexperiment.ScenarioAgentResult       `json:"agent"`
	ProviderCalls []controlexperiment.StatelessAgentCallAudit `json:"provider_calls"`
	FrontierWork  controlexperiment.PhaseWork                 `json:"frontier_work"`
	Failure       *controlexperiment.MethodFailure            `json:"failure,omitempty"`
	Testing       *scenarioTestingResult                      `json:"testing,omitempty"`
}

type scenarioEpisodeCoreInputs struct {
	RootID             string
	Knowledge          controlexperiment.ProtocolKnowledgePack
	Hypothesis         controlexperiment.TestHypothesis
	AcceptedHypothesis *controlexperiment.AcceptedHypothesisContext
	RiskSpec           semantic.RiskWitnessSpec
	Root               controlruntime.Trace
	Runtime            controlexperiment.RuntimeConfig
	FaultEnvelope      *controlexperiment.FaultEnvelope
	TargetSurface      *controlexperiment.AgentTargetSurface
	SemanticExposure   controlexperiment.ScenarioSemanticExposureMode
	NewAdapter         controlexperiment.AdapterFactory
	ActionPreparer     controlexperiment.ScenarioActionPreparer
	RiskProjector      controlexperiment.SemanticPrefixProjector
	SemanticProjector  controlexperiment.ScenarioSemanticProjector
}

// runScenarioEpisodeCore is the shared trusted execution substrate used by
// active Agentic target compositions. A planner supplies only an investigation
// proposal with a bounded ScenarioPlan and model accounting; branch selection,
// frontier reconstruction, concretization, natural
// progress, Replay and qualification remain trusted.
func runScenarioEpisodeCore(
	ctx context.Context,
	inputs scenarioEpisodeCoreInputs,
	maxCalls int,
	maxPlanSteps int,
	maxDecisions int,
	planner controlexperiment.ScenarioPlanner,
) (scenarioAgentEpisodeResult, error) {
	if inputs.RootID == "" ||
		inputs.Knowledge.Validate() != nil || inputs.RiskSpec.Validate() != nil ||
		inputs.Hypothesis.Validate(
			inputs.Knowledge, inputs.RiskSpec, controlexperiment.ScenarioPlanningBackendID,
		) != nil || inputs.Root.Validate() != nil || inputs.SemanticExposure.Validate() != nil ||
		inputs.NewAdapter == nil || inputs.RiskProjector == nil || inputs.SemanticProjector == nil ||
		maxCalls <= 0 || maxCalls > controlexperiment.ScenarioAgentMaxCalls ||
		maxPlanSteps <= 0 || maxPlanSteps > controlexperiment.ScenarioPlanMaxSteps ||
		maxDecisions <= 0 || maxDecisions > controlexperiment.ScenarioAgentMaxDecisions ||
		planner == nil {
		return scenarioAgentEpisodeResult{}, errors.New("SCENARIO_EPISODE_INPUT_INVALID")
	}
	rootRisk, err := inputs.RiskProjector.Project(
		inputs.RootID+"-risk", inputs.RiskSpec, inputs.Root,
	)
	if err != nil {
		return scenarioAgentEpisodeResult{}, err
	}
	frontier, snapshot, frontierWork, err := controlexperiment.ReconstructRiskFrontierState(
		ctx, inputs.RootID+"-frontier", inputs.RiskSpec, rootRisk,
		inputs.Root, len(inputs.Root.Records), inputs.Runtime,
		inputs.FaultEnvelope, inputs.NewAdapter,
	)
	if err != nil {
		return scenarioAgentEpisodeResult{
			FrontierWork: frontierWork, Failure: scenarioTerminalFailure(err),
		}, err
	}
	semantics, err := inputs.SemanticProjector(inputs.Root, frontier, snapshot)
	if err != nil {
		return scenarioAgentEpisodeResult{}, err
	}
	var preparers []controlexperiment.ScenarioActionPreparer
	if inputs.ActionPreparer != nil {
		preparers = append(preparers, inputs.ActionPreparer)
	}
	agent, err := controlexperiment.ExploreScenarioWithPlanner(
		ctx, maxCalls, maxPlanSteps, maxDecisions,
		inputs.Knowledge, inputs.Hypothesis, inputs.RiskSpec, frontier, semantics,
		rootRisk, inputs.Root, inputs.Runtime, inputs.FaultEnvelope, inputs.TargetSurface,
		inputs.AcceptedHypothesis,
		inputs.NewAdapter,
		inputs.RiskProjector, inputs.SemanticProjector, planner, preparers...,
	)
	result := scenarioAgentEpisodeResult{
		Agent: agent, FrontierWork: frontierWork,
	}
	if err != nil {
		result.Failure = scenarioTerminalFailure(err)
		return result, err
	}
	return result, nil
}

func scenarioTerminalFailure(err error) *controlexperiment.MethodFailure {
	var terminalError *controlruntime.TerminalExecutionError
	if !errors.As(err, &terminalError) || terminalError.Terminal.Validate() != nil {
		return nil
	}
	terminal := terminalError.Terminal
	terminal.AttemptedAction.Parameters = append(
		[]byte(nil), terminalError.Terminal.AttemptedAction.Parameters...,
	)
	return &controlexperiment.MethodFailure{
		Phase: "scenario-action", Code: terminal.Code, Decision: int(terminal.Decision),
		Terminal: &terminal,
	}
}
