package main

import (
	"errors"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func loadAgenticCapabilityFeedbackProbe(
	path string,
) (*controlexperiment.AgenticCapabilityFeedbackProbe, error) {
	if path == "" {
		return nil, nil
	}
	var probe controlexperiment.AgenticCapabilityFeedbackProbe
	if readStrictJSONFile(path, controlexperiment.ScenarioPlanMaxBytes, &probe) != nil ||
		probe.Validate() != nil {
		return nil, errors.New("AGENTIC_CAPABILITY_FEEDBACK_PROBE_INVALID")
	}
	return &probe, nil
}

func agenticCapabilityFeedbackProbeMemory(
	surface controlexperiment.AgentTargetSurface,
	probe *controlexperiment.AgenticCapabilityFeedbackProbe,
) ([]controlexperiment.RiskExplorationMemoryEntry, error) {
	if probe == nil {
		return nil, nil
	}
	if surface.Validate() != nil || probe.Validate() != nil {
		return nil, errors.New("AGENTIC_CAPABILITY_FEEDBACK_PROBE_INPUT_INVALID")
	}
	gaps, err := surface.ScenarioCapabilityGaps(probe.Plan, nil)
	if err != nil || len(gaps) == 0 {
		return nil, errors.New("AGENTIC_CAPABILITY_FEEDBACK_PROBE_NOT_REJECTED")
	}
	reasonSet := make(map[string]bool, len(gaps))
	for _, gap := range gaps {
		if gap.Code != controlexperiment.AgentCapabilityGapMissingAction &&
			gap.Code != controlexperiment.AgentCapabilityGapMissingControl {
			return nil, errors.New("AGENTIC_CAPABILITY_FEEDBACK_PROBE_GAP_INVALID")
		}
		reasonSet[gap.Code] = true
	}
	reasons := make([]string, 0, len(reasonSet))
	for reason := range reasonSet {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	return []controlexperiment.RiskExplorationMemoryEntry{{
		Episode:               1,
		Summary:               "A trusted mechanical capability probe was rejected before Runtime execution.",
		EpisodeOutcome:        controlexperiment.RiskMemoryOutcomePlanningStopped,
		MechanicalReasonCodes: reasons,
		CapabilityGaps:        append([]controlexperiment.AgentCapabilityGap(nil), gaps...),
	}}, nil
}

func agenticEpisodeBudgetForCapabilityProbe(
	budget agenticEpisodeBudget,
	probe *controlexperiment.AgenticCapabilityFeedbackProbe,
) (agenticEpisodeBudget, error) {
	if probe == nil {
		return budget, nil
	}
	if budget.validate() != nil || budget.Logical == nil || probe.Validate() != nil ||
		probe.MaxScenarioCalls > budget.MaxScenarioCalls {
		return agenticEpisodeBudget{}, errors.New("AGENTIC_CAPABILITY_FEEDBACK_PROBE_BUDGET_INVALID")
	}
	return agenticEpisodeBudgetFromExperiment(
		probe.MaxScenarioCalls, budget.MaxScenarioPlanSteps,
		budget.MaxRuntimeDecisions, *budget.Logical,
	)
}
