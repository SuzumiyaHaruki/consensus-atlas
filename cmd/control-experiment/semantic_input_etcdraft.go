package main

import (
	"encoding/hex"
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const etcdraftSemanticInputLimit = 256 << 10

// etcdraftAgenticAuthoringSource deliberately has no TestHypothesis field.
// Strict decoding rejects pre-seeded hypotheses instead of silently ignoring
// them; the Risk Agent owns hypothesis authoring.
type etcdraftAgenticAuthoringSource struct {
	Knowledge  controlexperiment.ProtocolKnowledgePack `json:"protocol_knowledge"`
	Workload   etcdraftWorkloadAuthoringSource         `json:"workload"`
	Experiment etcdraftAgentExperimentConfig           `json:"experiment"`
}

type etcdraftWorkloadAuthoringSource struct {
	ID             string                                `json:"id"`
	TargetSelector string                                `json:"target_selector"`
	Invocations    []etcdraftWorkloadInvocationAuthoring `json:"invocations"`
}

type etcdraftWorkloadInvocationAuthoring struct {
	ID             string `json:"id"`
	Operation      string `json:"operation"`
	Value          string `json:"value"`
	ExpectedStatus string `json:"expected_status"`
}

func (source etcdraftWorkloadAuthoringSource) build() (controlexperiment.WorkloadPlan, error) {
	workload := controlexperiment.WorkloadPlan{
		SchemaVersion: controlexperiment.WorkloadPlanVersion,
		ID:            source.ID, TargetSelector: source.TargetSelector,
		Invocations: make([]controlexperiment.WorkloadInvocation, 0, len(source.Invocations)),
	}
	for _, invocation := range source.Invocations {
		payload, err := etcdraftv2.InputPayload(etcdraftv2.Input{
			Operation: invocation.Operation,
			RequestID: invocation.ID,
			Value:     []byte(invocation.Value),
		})
		if err != nil {
			return controlexperiment.WorkloadPlan{}, err
		}
		workload.Invocations = append(workload.Invocations, controlexperiment.WorkloadInvocation{
			ID: invocation.ID, Input: payload, ExpectedStatus: invocation.ExpectedStatus,
		})
	}
	if err := workload.Validate(); err != nil {
		return controlexperiment.WorkloadPlan{}, err
	}
	return workload, nil
}

type etcdraftAgentExperimentConfig struct {
	AdapterConfig            etcdraftv2.Config                              `json:"adapter_config"`
	Runtime                  controlexperiment.RuntimeConfig                `json:"runtime"`
	FaultEnvelope            controlexperiment.FaultEnvelope                `json:"fault_envelope"`
	ScenarioMaxCalls         int                                            `json:"scenario_max_calls"`
	ScenarioMaxSteps         int                                            `json:"scenario_max_steps"`
	ScenarioMaxDecisions     int                                            `json:"scenario_max_decisions"`
	ScenarioSemanticExposure controlexperiment.ScenarioSemanticExposureMode `json:"scenario_semantic_exposure"`
	ModelReasoningEffort     string                                         `json:"model_reasoning_effort"`
	ModelExcludeReasoning    bool                                           `json:"model_exclude_reasoning"`
	ModelMaxOutputTokens     int                                            `json:"model_max_output_tokens"`
	ModelMaxRetries          int                                            `json:"model_max_retries"`
	SessionBudget            controlexperiment.AgenticLogicalBudget         `json:"session_budget"`
	SessionWallClockMS       int64                                          `json:"session_wall_clock_ms"`
}

func (config etcdraftAgentExperimentConfig) validateAgentic() error {
	seed, seedErr := hex.DecodeString(config.Runtime.SeedHex)
	_, adapterErr := etcdraftv2.NewWithConfig(config.AdapterConfig)
	if adapterErr != nil || seedErr != nil || len(seed) == 0 || config.Runtime.ClockError != 0 ||
		config.FaultEnvelope.Validate() != nil || config.ScenarioMaxCalls <= 0 ||
		config.ScenarioMaxCalls > controlexperiment.ScenarioAgentMaxCalls ||
		config.ScenarioMaxSteps <= 0 || config.ScenarioMaxSteps > controlexperiment.ScenarioPlanMaxSteps ||
		config.ScenarioMaxDecisions <= 0 ||
		config.ScenarioMaxDecisions > controlexperiment.ScenarioAgentMaxDecisions ||
		config.ScenarioSemanticExposure.Validate() != nil ||
		!validOpenRouterReasoningEffort(config.ModelReasoningEffort) ||
		config.ModelMaxOutputTokens <= 0 || config.ModelMaxOutputTokens > openRouterMaxOutputTokens ||
		config.ModelMaxRetries != 0 ||
		!validAgenticBudget(config.SessionBudget, config.SessionWallClockMS) {
		return errors.New("ETCDRAFT_AGENTIC_EXPERIMENT_CONFIG_INVALID")
	}
	return nil
}

func validAgenticBudget(budget controlexperiment.AgenticLogicalBudget, wallClockMS int64) bool {
	return budget.Validate() == nil && wallClockMS > 0
}

func (config etcdraftAgentExperimentConfig) faultEnvelope() *controlexperiment.FaultEnvelope {
	envelope := config.FaultEnvelope
	return &envelope
}

func loadEtcdraftAgenticAuthoringSource(
	path string,
) (
	controlexperiment.ProtocolKnowledgePack,
	etcdraftAgentExperimentConfig,
	controlexperiment.WorkloadPlan,
	error,
) {
	var source etcdraftAgenticAuthoringSource
	if path == "" || readStrictJSONFile(path, etcdraftSemanticInputLimit, &source) != nil {
		return controlexperiment.ProtocolKnowledgePack{}, etcdraftAgentExperimentConfig{},
			controlexperiment.WorkloadPlan{}, errors.New("ETCDRAFT_AGENTIC_INPUT_FILE_INVALID")
	}
	if source.Experiment.validateAgentic() != nil {
		return controlexperiment.ProtocolKnowledgePack{}, etcdraftAgentExperimentConfig{},
			controlexperiment.WorkloadPlan{}, errors.New("ETCDRAFT_AGENTIC_INPUT_EXPERIMENT_INVALID")
	}
	workload, err := source.Workload.build()
	if err != nil {
		return controlexperiment.ProtocolKnowledgePack{}, etcdraftAgentExperimentConfig{},
			controlexperiment.WorkloadPlan{}, errors.New("ETCDRAFT_AGENTIC_INPUT_WORKLOAD_INVALID")
	}
	knowledge, err := buildAgenticKnowledgeAuthoring(source.Knowledge, "etcdraft")
	if err != nil || knowledge.Family != "raft" {
		return controlexperiment.ProtocolKnowledgePack{}, etcdraftAgentExperimentConfig{},
			controlexperiment.WorkloadPlan{}, errors.New("ETCDRAFT_AGENTIC_INPUT_MATERIALS_INVALID")
	}
	return knowledge, source.Experiment, workload, nil
}
