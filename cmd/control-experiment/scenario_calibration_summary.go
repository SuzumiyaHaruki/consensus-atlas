package main

import (
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	scenarioPlannerAgent         = "agent"
	scenarioPlannerDeterministic = "deterministic"
)

type scenarioCalibrationSummary struct {
	Classification string                                      `json:"classification"`
	Planner        string                                      `json:"planner,omitempty"`
	Transport      controlexperiment.AgentTransportFreeze      `json:"transport"`
	AgentStatus    string                                      `json:"agent_status"`
	Attempts       int                                         `json:"attempts"`
	Feedback       []controlexperiment.ScenarioAgentFeedback   `json:"feedback"`
	FinalPlan      *controlexperiment.ScenarioPlan             `json:"final_plan,omitempty"`
	ProviderCalls  []controlexperiment.StatelessAgentCallAudit `json:"provider_calls"`
	ModelWork      controlexperiment.ModelWork                 `json:"model_work"`
	Testing        *scenarioCalibrationTestingSummary          `json:"testing,omitempty"`
}

type scenarioCalibrationTestingSummary struct {
	Outcome             string   `json:"outcome"`
	TraceDigest         string   `json:"trace_digest"`
	BundleDigest        string   `json:"bundle_digest"`
	ReplayStable        bool     `json:"replay_stable"`
	CorePSSSamples      int      `json:"core_pss_samples"`
	UniqueCorePSSStates int      `json:"unique_core_pss_states"`
	RiskStatus          string   `json:"risk_status"`
	SatisfiedMilestones []string `json:"satisfied_milestones"`
	FirstMissing        string   `json:"first_missing_milestone,omitempty"`
	OracleViolations    int      `json:"oracle_violations"`
}

func summarizeScenarioCalibration(
	classification string,
	client openRouterIntentClient,
	result scenarioAgentEpisodeResult,
) scenarioCalibrationSummary {
	summary := scenarioCalibrationSummary{
		Classification: classification,
		Planner:        scenarioPlannerAgent,
		Transport:      openRouterTransportFreeze(client),
		AgentStatus:    result.Agent.Status,
		Attempts:       len(result.Agent.Attempts),
		Feedback:       make([]controlexperiment.ScenarioAgentFeedback, 0, len(result.Agent.Attempts)),
		ProviderCalls:  append([]controlexperiment.StatelessAgentCallAudit(nil), result.ProviderCalls...),
		ModelWork:      result.Agent.ModelWork,
	}
	for _, attempt := range result.Agent.Attempts {
		summary.Feedback = append(summary.Feedback, attempt.Feedback)
	}
	if result.Agent.Execution != nil {
		for index := len(result.Agent.Attempts) - 1; index >= 0; index-- {
			if result.Agent.Attempts[index].Plan != nil {
				plan := *result.Agent.Attempts[index].Plan
				plan.Steps = append([]controlexperiment.ScenarioStep(nil), plan.Steps...)
				summary.FinalPlan = &plan
				break
			}
		}
	}
	if result.Testing != nil {
		firstMissing := ""
		if len(result.Testing.Risk.MissingMilestones) > 0 {
			firstMissing = result.Testing.Risk.MissingMilestones[0]
		}
		summary.Testing = &scenarioCalibrationTestingSummary{
			Outcome: result.Testing.Outcome, TraceDigest: result.Testing.Bundle.Trace.Digest,
			BundleDigest: result.Testing.Bundle.Digest, ReplayStable: result.Testing.Replay.Stable,
			CorePSSSamples:      result.Testing.CorePSSSamples,
			UniqueCorePSSStates: result.Testing.UniqueCorePSSStates,
			RiskStatus:          result.Testing.Risk.Status,
			SatisfiedMilestones: append([]string(nil), result.Testing.Risk.SatisfiedMilestones...),
			FirstMissing:        firstMissing, OracleViolations: len(result.Testing.Oracle.Violations),
		}
	}
	return summary
}

func summarizeDeterministicScenario(
	classification string,
	result scenarioAgentEpisodeResult,
) scenarioCalibrationSummary {
	summary := summarizeScenarioCalibration(classification, openRouterIntentClient{}, result)
	summary.Planner = scenarioPlannerDeterministic
	summary.Transport = controlexperiment.AgentTransportFreeze{}
	return summary
}
