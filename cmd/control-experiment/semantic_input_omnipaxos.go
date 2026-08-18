package main

import (
	"encoding/hex"
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const omnipaxosSemanticInputLimit = 64 << 10

// omnipaxosAgenticAuthoringSource excludes TestHypothesis so strict decoding
// rejects every pre-seeded hypothesis. The Risk Agent owns authoring.
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

// omnipaxosScenarioExperimentConfig contains target execution and Agent
// budget inputs. Worker location remains a runtime input.
type omnipaxosScenarioExperimentConfig struct {
	Runtime                  controlexperiment.RuntimeConfig                `json:"runtime"`
	FaultEnvelope            controlexperiment.FaultEnvelope                `json:"fault_envelope"`
	ScenarioMaxCalls         int                                            `json:"scenario_max_calls"`
	ScenarioMaxSteps         int                                            `json:"scenario_max_steps"`
	ScenarioMaxDecisions     int                                            `json:"scenario_max_decisions"`
	ScenarioSemanticExposure controlexperiment.ScenarioSemanticExposureMode `json:"scenario_semantic_exposure"`
	SessionBudget            controlexperiment.AgenticLogicalBudget         `json:"session_budget"`
	SessionWallClockMS       int64                                          `json:"session_wall_clock_ms"`
}

func (config omnipaxosScenarioExperimentConfig) validate() error {
	seed, seedErr := hex.DecodeString(config.Runtime.SeedHex)
	if seedErr != nil || len(seed) == 0 || config.Runtime.ClockError != 0 ||
		config.FaultEnvelope.Validate() != nil || config.FaultEnvelope.MaxMessageDrops <= 0 ||
		config.FaultEnvelope.MaxCrashes != 0 || config.FaultEnvelope.MaxConcurrentCrashes != 0 ||
		config.FaultEnvelope.MaxMessageDuplicates != 0 ||
		config.FaultEnvelope.MaxPartitions != 0 ||
		config.FaultEnvelope.MaxActivePartitions != 0 || config.ScenarioMaxCalls <= 0 ||
		config.ScenarioMaxCalls > controlexperiment.ScenarioAgentMaxCalls ||
		config.ScenarioMaxSteps <= 0 || config.ScenarioMaxSteps > controlexperiment.ScenarioPlanMaxSteps ||
		config.ScenarioMaxDecisions <= 0 ||
		config.ScenarioMaxDecisions > controlexperiment.ScenarioAgentMaxDecisions ||
		config.ScenarioSemanticExposure.Validate() != nil ||
		!validAgenticBudget(config.SessionBudget, config.SessionWallClockMS) {
		return errors.New("OMNIPAXOS_SCENARIO_EXPERIMENT_CONFIG_INVALID")
	}
	return nil
}

func (config omnipaxosScenarioExperimentConfig) faultEnvelope() *controlexperiment.FaultEnvelope {
	envelope := config.FaultEnvelope
	return &envelope
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
