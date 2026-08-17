package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	agenticMethodSpecFile        = "method-spec.json"
	riskAgentPromptVersion       = "risk-agent-navigation-v4"
	etcdraftSemanticInputSchema  = "etcdraft-agentic-input-v1"
	omnipaxosSemanticInputSchema = "omnipaxos-agentic-input-v1"
)

func buildAgenticMethodSpec(
	options controlExperimentOptions,
	target agenticEpisodeTarget,
	budget agenticEpisodeBudget,
	client agentIntentTransport,
	scenarioClient agentIntentTransport,
	semanticInputSchema string,
	semanticExposure controlexperiment.ScenarioSemanticExposureMode,
	sessionWallClockMS int64,
	mounts []controlexperiment.KnowledgeSourceMount,
	feedbackProbe *controlexperiment.AgenticCapabilityFeedbackProbe,
) (controlexperiment.AgenticMethodSpec, error) {
	if target.validate() != nil || budget.validate() != nil || budget.Logical == nil ||
		options.InvestigationEpisodes <= 0 || semanticInputSchema == "" || sessionWallClockMS <= 0 ||
		client == nil || scenarioClient == nil ||
		semanticExposure.Validate() != nil {
		return controlexperiment.AgenticMethodSpec{}, errors.New("AGENTIC_METHOD_SPEC_INPUT_INVALID")
	}
	semanticBytes, err := os.ReadFile(options.SemanticInput)
	if err != nil || len(semanticBytes) == 0 || len(semanticBytes) > etcdraftSemanticInputLimit {
		return controlexperiment.AgenticMethodSpec{}, errors.New("AGENTIC_METHOD_SPEC_SEMANTIC_INPUT_INVALID")
	}
	totalBudget, err := controlexperiment.ScaleAgenticLogicalBudget(
		*budget.Logical, options.InvestigationEpisodes,
	)
	if err != nil {
		return controlexperiment.AgenticMethodSpec{}, err
	}
	source, err := agenticSourceExposureSpec(target.Knowledge, mounts)
	if err != nil {
		return controlexperiment.AgenticMethodSpec{}, err
	}
	scenarioTransport := scenarioClient.freeze()
	spec, err := controlexperiment.NewAgenticMethodSpec(controlexperiment.AgenticMethodSpec{
		TargetID: target.ID, Transport: client.freeze(), ScenarioTransport: &scenarioTransport,
		RiskPromptVersion: riskAgentPromptVersion, ScenarioPromptVersion: scenarioAgentPromptVersion,
		SemanticInputSchema:      semanticInputSchema,
		SemanticInputDigest:      controlexperiment.AgentInvocationDigest(semanticBytes),
		ScenarioSemanticExposure: semanticExposure, SourceExposure: source,
		CapabilityFeedbackMode: controlexperiment.AgenticCapabilityFeedbackMode(
			normalizedCapabilityFeedbackMode(options.CapabilityFeedbackMode),
		),
		CapabilityFeedbackProbe: feedbackProbe,
		EpisodeLimits: controlexperiment.AgenticEpisodeLimits{
			MaxRiskCalls: budget.MaxRiskCalls, MaxScenarioCalls: budget.MaxScenarioCalls,
			MaxTotalCalls: budget.MaxTotalCalls, MaxObservedTokens: budget.MaxObservedTokens,
			MaxScenarioPlanSteps: budget.MaxScenarioPlanSteps,
			MaxRuntimeDecisions:  budget.MaxRuntimeDecisions,
			SessionWallClockMS:   sessionWallClockMS,
		},
		InvestigationEpisodes: options.InvestigationEpisodes,
		EpisodeBudget:         *budget.Logical, InvestigationBudget: totalBudget,
	})
	if err != nil {
		return controlexperiment.AgenticMethodSpec{}, err
	}
	if options.MethodSpecDigest != "" && options.MethodSpecDigest != spec.Digest {
		return controlexperiment.AgenticMethodSpec{}, errors.New("AGENTIC_METHOD_SPEC_EXPECTED_DIGEST_MISMATCH")
	}
	return spec, nil
}

func normalizedCapabilityFeedbackMode(value string) string {
	if value == "" {
		return controlexperiment.AgenticCapabilityFeedbackStructuredGaps
	}
	return value
}

func agenticSourceExposureSpec(
	knowledge controlexperiment.ProtocolKnowledgePack,
	mounts []controlexperiment.KnowledgeSourceMount,
) (controlexperiment.AgenticSourceExposureSpec, error) {
	if len(mounts) == 0 {
		return controlexperiment.AgenticSourceExposureSpec{
			Mode: controlexperiment.AgenticSourceExposureNone,
		}, nil
	}
	if controlexperiment.ValidateKnowledgeSourceMounts(mounts) != nil {
		return controlexperiment.AgenticSourceExposureSpec{}, errors.New("AGENTIC_METHOD_SPEC_SOURCE_EXPOSURE_INVALID")
	}
	catalog, err := controlexperiment.KnowledgeSourceCatalog(knowledge)
	if err != nil {
		return controlexperiment.AgenticSourceExposureSpec{}, err
	}
	catalogDigest, err := control.CanonicalDigest(catalog)
	if err != nil {
		return controlexperiment.AgenticSourceExposureSpec{}, err
	}
	prefixes := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		prefix := mount.ReferencePrefix
		if prefix == "" {
			prefix = "repo"
		}
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)
	return controlexperiment.AgenticSourceExposureSpec{
		Mode:              controlexperiment.AgenticSourceExposureDossierV2,
		ReferencePrefixes: prefixes, CatalogDigest: catalogDigest,
	}, nil
}

func bindAgenticMethodSpec(
	directory string,
	resume bool,
	spec controlexperiment.AgenticMethodSpec,
) error {
	if spec.Validate() != nil {
		return errors.New("AGENTIC_METHOD_SPEC_INVALID")
	}
	path := filepath.Join(directory, agenticMethodSpecFile)
	if resume {
		var stored controlexperiment.AgenticMethodSpec
		if readStrictJSONFile(path, 128<<10, &stored) != nil || stored.Validate() != nil ||
			!reflect.DeepEqual(stored, spec) {
			return errors.New("AGENTIC_METHOD_SPEC_RECOVERY_DRIFT")
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(directory), 0o700); err != nil {
		return err
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		return err
	}
	return writeStatelessAgentJSON(directory, agenticMethodSpecFile, spec)
}

func readAgenticMethodSpec(
	directory string,
	expectedDigest string,
) (controlexperiment.AgenticMethodSpec, error) {
	var spec controlexperiment.AgenticMethodSpec
	if readStrictJSONFile(
		filepath.Join(directory, agenticMethodSpecFile), 128<<10, &spec,
	) != nil || spec.Validate() != nil || spec.Digest != expectedDigest {
		return controlexperiment.AgenticMethodSpec{}, errors.New("AGENTIC_METHOD_SPEC_ARTIFACT_INVALID")
	}
	return spec, nil
}
