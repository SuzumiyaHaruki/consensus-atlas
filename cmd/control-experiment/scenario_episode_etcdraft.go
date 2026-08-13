package main

import (
	"context"
	"errors"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

type etcdraftScenarioEpisodeResult struct {
	Agent         controlexperiment.ScenarioAgentResult       `json:"agent"`
	ProviderCalls []controlexperiment.StatelessAgentCallAudit `json:"provider_calls"`
	FrontierWork  controlexperiment.PhaseWork                 `json:"frontier_work"`
	Testing       *etcdraftScenarioTestingResult              `json:"testing,omitempty"`
}

type etcdraftScenarioTestingResult struct {
	PlanID              string                            `json:"plan_id"`
	Bundle              controlexperiment.ExecutionBundle `json:"execution_bundle"`
	Risk                semantic.RiskWitnessResult        `json:"risk"`
	CorePSSSamples      int                               `json:"core_pss_samples"`
	UniqueCorePSSStates int                               `json:"unique_core_pss_states"`
	Replay              controlexperiment.ReplayResult    `json:"replay"`
	Oracle              oracle.Result                     `json:"oracle"`
	Outcome             string                            `json:"outcome"`
}

func runEtcdraftScenarioAgentEpisode(
	ctx context.Context,
	inputs etcdraftSemanticCalibrationInputs,
	journal *scenarioAgentCallJournal,
	initialFeedback *controlexperiment.ScenarioAgentFeedback,
	maxAttempts int,
	maxSteps int,
	activateKey func() error,
) (etcdraftScenarioEpisodeResult, error) {
	if journal == nil || journal.core == nil || maxAttempts <= 0 ||
		maxAttempts > controlexperiment.ScenarioAgentMaxAttempts || maxSteps <= 0 ||
		maxSteps > controlexperiment.ScenarioPlanMaxSteps ||
		activateKey == nil || !validEtcdraftScenarioInitialFeedback(initialFeedback, maxSteps) ||
		journal.SetRoot("invoked-scenario") != nil {
		return etcdraftScenarioEpisodeResult{}, errors.New("ETCDRAFT_SCENARIO_EPISODE_INPUT_INVALID")
	}
	projector := etcdraftSemanticPrefixProjector{}
	rootRisk, err := projector.Project("etcdraft-scenario-root-risk", inputs.riskSpec, inputs.root)
	if err != nil {
		return etcdraftScenarioEpisodeResult{}, err
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(inputs.experiment.AdapterConfig)
	}
	frontier, frontierWork, err := controlexperiment.ReconstructRiskFrontierView(
		ctx, "etcdraft-scenario-root-frontier", inputs.riskSpec, rootRisk,
		inputs.root, len(inputs.root.Records), inputs.experiment.Runtime,
		inputs.experiment.faultEnvelope(), factory,
	)
	if err != nil {
		return etcdraftScenarioEpisodeResult{}, err
	}
	plannerCalls := 0
	agent, err := controlexperiment.ExploreScenarioWithPlanner(
		ctx, maxAttempts, maxSteps,
		inputs.knowledge, inputs.hypothesis, inputs.riskSpec, frontier, rootRisk, inputs.root,
		inputs.experiment.Runtime, inputs.experiment.faultEnvelope(), factory, projector,
		func(ctx context.Context, view controlexperiment.ScenarioAgentView) (
			[]byte, controlexperiment.ModelWork, error,
		) {
			if plannerCalls == 0 && initialFeedback != nil && view.Prior == nil {
				prior := *initialFeedback
				view.Prior = &prior
			}
			plannerCalls++
			content, work, callErr := journal.Planner(ctx, inputs.riskSpec, view)
			if !errors.Is(callErr, errStatelessAgentCallKeyRequired) {
				return content, work, callErr
			}
			if err := activateKey(); err != nil {
				return nil, work, err
			}
			return journal.Planner(ctx, inputs.riskSpec, view)
		},
	)
	if err != nil {
		return etcdraftScenarioEpisodeResult{}, err
	}
	audits, err := journal.Audits()
	if err != nil || len(audits) != len(agent.Attempts) {
		return etcdraftScenarioEpisodeResult{}, errors.New("ETCDRAFT_SCENARIO_EPISODE_AUDIT_INVALID")
	}
	result := etcdraftScenarioEpisodeResult{
		Agent: agent, ProviderCalls: audits, FrontierWork: frontierWork,
	}
	if agent.Status == controlexperiment.ScenarioAgentCompleted {
		testing, err := executeEtcdraftScenarioQualified(ctx, inputs, *agent.Execution)
		if err != nil {
			return etcdraftScenarioEpisodeResult{}, err
		}
		result.Testing = &testing
	}
	return result, nil
}

func validEtcdraftScenarioInitialFeedback(
	feedback *controlexperiment.ScenarioAgentFeedback,
	maxSteps int,
) bool {
	if feedback == nil {
		return true
	}
	return feedback.Attempt > 0 && len(feedback.Steps) <= maxSteps &&
		(feedback.Outcome == controlexperiment.ScenarioAgentCompleted ||
			feedback.Outcome == controlexperiment.ScenarioAgentStopped)
}

func executeEtcdraftScenarioQualified(
	ctx context.Context,
	inputs etcdraftSemanticCalibrationInputs,
	execution controlexperiment.ScenarioExecution,
) (etcdraftScenarioTestingResult, error) {
	policy, err := controlexperiment.CompileScenarioPolicy(
		"etcdraft-scenario-qualified-policy", inputs.root, execution,
		[]control.ActionKind{
			control.ActionInvoke, control.ActionCompleteEffect,
			control.ActionDeliverMessage, control.ActionFireTemporal,
		},
	)
	if err != nil {
		return etcdraftScenarioTestingResult{}, err
	}
	config := controlexperiment.Config{
		SchemaVersion:    controlexperiment.SchemaVersionV2,
		ID:               "etcdraft-scenario-qualified-testing",
		PSSID:            etcdraftv2.CorePSSMappingID,
		Runtime:          inputs.experiment.Runtime,
		Admission:        &inputs.campaign.admission,
		FaultEnvelope:    inputs.experiment.faultEnvelope(),
		WorkloadRouterID: etcdraftv2.WorkloadRouterID,
		DecisionsPerRun:  len(execution.FinalTrace.Records), RequireReplay: true,
		Runs: []controlexperiment.RunPlan{{Run: 1, Policy: policy, Workload: &inputs.campaign.workload}},
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(inputs.experiment.AdapterConfig)
	}
	_, bundle, err := controlexperiment.ExecuteQualifiedBundle(
		ctx, config, inputs.campaign.qualification, factory, etcdraftv2.CorePSSMapper{},
		etcdraftv2.DecisionProjector{}, etcdraftv2.WorkloadRouter{},
	)
	if err != nil {
		return etcdraftScenarioTestingResult{}, err
	}
	risk, err := (etcdraftSemanticPrefixProjector{}).Project(
		execution.FinalRisk.ID, inputs.riskSpec, bundle.Trace,
	)
	if err != nil || bundle.Trace.Digest != execution.FinalTrace.Digest ||
		!reflect.DeepEqual(risk, execution.FinalRisk) {
		return etcdraftScenarioTestingResult{}, errors.New("ETCDRAFT_SCENARIO_QUALIFIED_TRACE_MISMATCH")
	}
	verdict := oracle.CheckBundle(bundle, oracle.BundleTraceIntegrity{}, oracle.BundleAgreement{})
	outcome := etcdraftSemanticTestingPassed
	if len(verdict.Violations) > 0 {
		outcome = etcdraftSemanticTestingViolation
	}
	return etcdraftScenarioTestingResult{
		PlanID: execution.PlanID, Bundle: bundle, Risk: risk,
		CorePSSSamples: bundle.Run.CorePSSSamples, UniqueCorePSSStates: bundle.Run.UniqueCoreStates,
		Replay: bundle.Run.Replay, Oracle: verdict, Outcome: outcome,
	}, nil
}
