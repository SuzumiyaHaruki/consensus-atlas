package main

import (
	"encoding/hex"
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const omnipaxosSemanticInputLimit = 64 << 10

type omnipaxosScenarioAuthoringSource struct {
	Knowledge  controlexperiment.ProtocolKnowledgePack `json:"protocol_knowledge"`
	Hypothesis controlexperiment.TestHypothesis        `json:"test_hypothesis"`
	Workload   omnipaxosWorkloadAuthoringSource        `json:"workload"`
	Experiment omnipaxosScenarioExperimentConfig       `json:"experiment"`
}

// omnipaxosAgenticAuthoringSource deliberately excludes TestHypothesis so
// strict decoding rejects every pre-seeded hypothesis, including an empty
// JSON object. Curated hypotheses remain available through the legacy source.
type omnipaxosAgenticAuthoringSource struct {
	Knowledge  controlexperiment.ProtocolKnowledgePack `json:"protocol_knowledge"`
	Workload   omnipaxosWorkloadAuthoringSource        `json:"workload"`
	Experiment omnipaxosScenarioExperimentConfig       `json:"experiment"`
}

type omnipaxosWorkloadAuthoringSource struct {
	ID             string                                 `json:"id"`
	TargetSelector string                                 `json:"target_selector"`
	Invocations    []omnipaxosWorkloadInvocationAuthoring `json:"invocations"`
}

type omnipaxosWorkloadInvocationAuthoring struct {
	ID             string `json:"id"`
	Value          string `json:"value"`
	ExpectedStatus string `json:"expected_status"`
}

func (source omnipaxosWorkloadAuthoringSource) build() (controlexperiment.WorkloadPlan, error) {
	workload := controlexperiment.WorkloadPlan{
		SchemaVersion:  controlexperiment.WorkloadPlanVersion,
		ID:             source.ID,
		TargetSelector: source.TargetSelector,
		Invocations:    make([]controlexperiment.WorkloadInvocation, 0, len(source.Invocations)),
	}
	for _, invocation := range source.Invocations {
		payload, err := omnipaxosv2.InputPayload(omnipaxosv2.Input{
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

// omnipaxosScenarioExperimentConfig contains only values consumed by the A7
// Scenario path. Worker location and provider configuration remain runtime
// inputs rather than protocol knowledge.
type omnipaxosScenarioExperimentConfig struct {
	Runtime                  controlexperiment.RuntimeConfig                `json:"runtime"`
	FaultEnvelope            controlexperiment.FaultEnvelope                `json:"fault_envelope"`
	ScenarioMaxCalls         int                                            `json:"scenario_max_calls"`
	ScenarioMaxSteps         int                                            `json:"scenario_max_steps"`
	ScenarioMaxDecisions     int                                            `json:"scenario_max_decisions"`
	ScenarioSemanticExposure controlexperiment.ScenarioSemanticExposureMode `json:"scenario_semantic_exposure"`
	SessionBudget            controlexperiment.CampaignLogicalBudget        `json:"session_budget"`
	SessionWallClockMS       int64                                          `json:"session_wall_clock_ms"`
}

func (config omnipaxosScenarioExperimentConfig) validate() error {
	seed, seedErr := hex.DecodeString(config.Runtime.SeedHex)
	if seedErr != nil || len(seed) == 0 || config.Runtime.ClockError != 0 ||
		config.FaultEnvelope.Validate() != nil || config.FaultEnvelope.MaxMessageDrops <= 0 ||
		config.FaultEnvelope.MaxCrashes != 0 || config.FaultEnvelope.MaxConcurrentCrashes != 0 ||
		config.FaultEnvelope.MaxMessageDuplicates != 0 || config.FaultEnvelope.MaxPartitions != 0 ||
		config.FaultEnvelope.MaxActivePartitions != 0 || config.ScenarioMaxCalls <= 0 ||
		config.ScenarioMaxCalls > controlexperiment.ScenarioAgentMaxCalls ||
		config.ScenarioMaxSteps <= 0 || config.ScenarioMaxSteps > controlexperiment.ScenarioPlanMaxSteps ||
		config.ScenarioMaxDecisions <= 0 ||
		config.ScenarioMaxDecisions > controlexperiment.ScenarioAgentMaxDecisions ||
		config.ScenarioSemanticExposure.Validate() != nil ||
		!validScenarioSessionBudget(config.SessionBudget, config.SessionWallClockMS) {
		return errors.New("OMNIPAXOS_SCENARIO_EXPERIMENT_CONFIG_INVALID")
	}
	return nil
}

func (config omnipaxosScenarioExperimentConfig) faultEnvelope() *controlexperiment.FaultEnvelope {
	envelope := config.FaultEnvelope
	return &envelope
}

func loadOmnipaxosScenarioAuthoringSource(
	path string,
	riskSpec semantic.RiskWitnessSpec,
) (
	controlexperiment.ProtocolKnowledgePack,
	controlexperiment.TestHypothesis,
	omnipaxosScenarioExperimentConfig,
	controlexperiment.WorkloadPlan,
	error,
) {
	var source omnipaxosScenarioAuthoringSource
	if path == "" || riskSpec.Validate() != nil ||
		readStrictJSONFile(path, omnipaxosSemanticInputLimit, &source) != nil {
		return controlexperiment.ProtocolKnowledgePack{}, controlexperiment.TestHypothesis{},
			omnipaxosScenarioExperimentConfig{}, controlexperiment.WorkloadPlan{},
			errors.New("OMNIPAXOS_SCENARIO_INPUT_FILE_INVALID")
	}
	if source.Experiment.validate() != nil {
		return controlexperiment.ProtocolKnowledgePack{}, controlexperiment.TestHypothesis{},
			omnipaxosScenarioExperimentConfig{}, controlexperiment.WorkloadPlan{},
			errors.New("OMNIPAXOS_SCENARIO_INPUT_EXPERIMENT_INVALID")
	}
	workload, err := source.Workload.build()
	if err != nil {
		return controlexperiment.ProtocolKnowledgePack{}, controlexperiment.TestHypothesis{},
			omnipaxosScenarioExperimentConfig{}, controlexperiment.WorkloadPlan{},
			errors.New("OMNIPAXOS_SCENARIO_INPUT_WORKLOAD_INVALID")
	}
	knowledge, hypothesis, err := buildSemanticAuthoring(
		source.Knowledge, source.Hypothesis, riskSpec, "omnipaxos",
		controlexperiment.ScenarioPlanningBackendID, controlexperiment.ScenarioPlanningBackendID,
	)
	if err != nil {
		return controlexperiment.ProtocolKnowledgePack{}, controlexperiment.TestHypothesis{},
			omnipaxosScenarioExperimentConfig{}, controlexperiment.WorkloadPlan{}, err
	}
	return knowledge, hypothesis, source.Experiment, workload, nil
}

func loadOmnipaxosAgenticAuthoringSource(
	path string,
) (
	controlexperiment.ProtocolKnowledgePack,
	omnipaxosScenarioExperimentConfig,
	controlexperiment.WorkloadPlan,
	error,
) {
	var source omnipaxosAgenticAuthoringSource
	if path == "" || readStrictJSONFile(path, omnipaxosSemanticInputLimit, &source) != nil {
		return controlexperiment.ProtocolKnowledgePack{}, omnipaxosScenarioExperimentConfig{},
			controlexperiment.WorkloadPlan{}, errors.New("OMNIPAXOS_AGENTIC_INPUT_FILE_INVALID")
	}
	if source.Experiment.validate() != nil {
		return controlexperiment.ProtocolKnowledgePack{}, omnipaxosScenarioExperimentConfig{},
			controlexperiment.WorkloadPlan{}, errors.New("OMNIPAXOS_AGENTIC_INPUT_EXPERIMENT_INVALID")
	}
	workload, err := source.Workload.build()
	if err != nil {
		return controlexperiment.ProtocolKnowledgePack{}, omnipaxosScenarioExperimentConfig{},
			controlexperiment.WorkloadPlan{}, errors.New("OMNIPAXOS_AGENTIC_INPUT_WORKLOAD_INVALID")
	}
	knowledge, err := buildAgenticKnowledgeAuthoring(source.Knowledge, "omnipaxos")
	if err != nil || knowledge.Family != "paxos" {
		return controlexperiment.ProtocolKnowledgePack{}, omnipaxosScenarioExperimentConfig{},
			controlexperiment.WorkloadPlan{}, errors.New("OMNIPAXOS_AGENTIC_INPUT_MATERIALS_INVALID")
	}
	return knowledge, source.Experiment, workload, nil
}
