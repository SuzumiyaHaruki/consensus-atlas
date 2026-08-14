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
	Testing       *scenarioTestingResult                      `json:"testing,omitempty"`
}

type scenarioEpisodeCoreInputs struct {
	RootID            string
	Knowledge         controlexperiment.ProtocolKnowledgePack
	Hypothesis        controlexperiment.TestHypothesis
	RiskSpec          semantic.RiskWitnessSpec
	Root              controlruntime.Trace
	Runtime           controlexperiment.RuntimeConfig
	FaultEnvelope     *controlexperiment.FaultEnvelope
	SemanticExposure  controlexperiment.ScenarioSemanticExposureMode
	NewAdapter        controlexperiment.AdapterFactory
	RiskProjector     controlexperiment.SemanticPrefixProjector
	SemanticProjector controlexperiment.ScenarioSemanticProjector
}

func runScenarioAgentEpisodeCore(
	ctx context.Context,
	inputs scenarioEpisodeCoreInputs,
	journal *scenarioAgentCallJournal,
	maxCalls int,
	maxPlanSteps int,
	maxDecisions int,
	activateKey func() error,
) (scenarioAgentEpisodeResult, error) {
	if journal == nil || journal.core == nil || inputs.RootID == "" ||
		inputs.Knowledge.Validate() != nil || inputs.RiskSpec.Validate() != nil ||
		inputs.Hypothesis.Validate(
			inputs.Knowledge, inputs.RiskSpec, controlexperiment.ScenarioPlanningBackendID,
		) != nil || inputs.Root.Validate() != nil || inputs.SemanticExposure.Validate() != nil ||
		inputs.NewAdapter == nil || inputs.RiskProjector == nil || inputs.SemanticProjector == nil ||
		maxCalls <= 0 || maxCalls > controlexperiment.ScenarioAgentMaxCalls ||
		maxPlanSteps <= 0 || maxPlanSteps > controlexperiment.ScenarioPlanMaxSteps ||
		maxDecisions <= 0 || maxDecisions > controlexperiment.ScenarioAgentMaxDecisions ||
		activateKey == nil || journal.SetRoot(inputs.RootID) != nil {
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
		return scenarioAgentEpisodeResult{}, err
	}
	semantics, err := inputs.SemanticProjector(inputs.Root, frontier, snapshot)
	if err != nil {
		return scenarioAgentEpisodeResult{}, err
	}
	agent, err := controlexperiment.ExploreScenarioWithPlanner(
		ctx, maxCalls, maxPlanSteps, maxDecisions,
		inputs.Knowledge, inputs.Hypothesis, inputs.RiskSpec, frontier, semantics,
		rootRisk, inputs.Root, inputs.Runtime, inputs.FaultEnvelope, inputs.NewAdapter,
		inputs.RiskProjector, inputs.SemanticProjector,
		func(ctx context.Context, view controlexperiment.ScenarioAgentView) (
			[]byte, controlexperiment.ModelWork, error,
		) {
			content, work, callErr := journal.Planner(ctx, inputs.RiskSpec, view)
			if !errors.Is(callErr, errStatelessAgentCallKeyRequired) {
				return content, work, callErr
			}
			if err := activateKey(); err != nil {
				return nil, work, err
			}
			return journal.Planner(ctx, inputs.RiskSpec, view)
		},
	)
	audits, auditErr := journal.Audits()
	result := scenarioAgentEpisodeResult{
		Agent: agent, ProviderCalls: audits, FrontierWork: frontierWork,
	}
	if auditErr != nil {
		return result, auditErr
	}
	if err != nil {
		return result, err
	}
	if len(audits) != len(agent.Attempts) {
		return scenarioAgentEpisodeResult{}, errors.New("SCENARIO_EPISODE_AUDIT_INVALID")
	}
	return result, nil
}
