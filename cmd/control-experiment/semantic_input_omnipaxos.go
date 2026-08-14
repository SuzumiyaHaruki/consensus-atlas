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
		config.ScenarioSemanticExposure.Validate() != nil {
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
	if source.Knowledge.SchemaVersion != "" || source.Knowledge.Digest != "" ||
		source.Hypothesis.SchemaVersion != "" || source.Hypothesis.KnowledgeDigest != "" ||
		source.Hypothesis.RiskSpecDigest != "" || source.Hypothesis.Digest != "" ||
		source.Hypothesis.RiskID != riskSpec.RiskID {
		return controlexperiment.ProtocolKnowledgePack{}, controlexperiment.TestHypothesis{},
			omnipaxosScenarioExperimentConfig{}, controlexperiment.WorkloadPlan{},
			errors.New("OMNIPAXOS_SCENARIO_INPUT_AUTHORING_FIELDS_INVALID")
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
	knowledge, err := controlexperiment.NewProtocolKnowledgePack(source.Knowledge)
	if err != nil || knowledge.Family != riskSpec.FamilyID || knowledge.Protocol != "omnipaxos" {
		return controlexperiment.ProtocolKnowledgePack{}, controlexperiment.TestHypothesis{},
			omnipaxosScenarioExperimentConfig{}, controlexperiment.WorkloadPlan{},
			errors.New("OMNIPAXOS_SCENARIO_INPUT_KNOWLEDGE_INVALID")
	}
	hypothesis, err := controlexperiment.NewTestHypothesis(
		source.Hypothesis.ID, knowledge, riskSpec, source.Hypothesis.Rationale,
		controlexperiment.ScenarioPlanningBackendID,
	)
	if err != nil || hypothesis.Validate(
		knowledge, riskSpec, controlexperiment.ScenarioPlanningBackendID,
	) != nil {
		return controlexperiment.ProtocolKnowledgePack{}, controlexperiment.TestHypothesis{},
			omnipaxosScenarioExperimentConfig{}, controlexperiment.WorkloadPlan{},
			errors.New("OMNIPAXOS_SCENARIO_INPUT_HYPOTHESIS_INVALID")
	}
	return knowledge, hypothesis, source.Experiment, workload, nil
}
