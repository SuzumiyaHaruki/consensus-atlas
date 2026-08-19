package controlexperiment

import (
	"errors"
	"sort"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const (
	AgenticMethodSpecSchemaVersion                                  = "consensus-atlas/agentic-method-spec/v1"
	AgenticMethodExecutorID                                         = "consensus-atlas/agentic-episode-cli/v1"
	AgenticMethodStrategyID                                         = "agentic-episode-v1"
	AgenticMethodImplementationID                                   = "consensus-atlas/agentic-method/m4l7-risk-input-closure-handoff-v1"
	agenticMethodClosureLegacyImplementationID                      = "consensus-atlas/agentic-method/m4l5-closure-v1"
	agenticMethodLegacyImplementationID                             = "consensus-atlas/agentic-method/m4d-v1"
	AgenticSourceExposureNone                                       = "none"
	AgenticSourceExposureDossierV1                                  = "dossier-declared-readonly-v1"
	AgenticSourceExposureDossierV2                                  = "dossier-declared-readonly-navigation-v2"
	AgenticCapabilityFeedbackReasonCodes                            = "reason-codes"
	AgenticCapabilityFeedbackStructuredGaps                         = "structured-gaps"
	AgenticClosureModePublicFixed                                   = "public-fixed"
	AgenticClosureModeTargetLocal                                   = "target-local"
	AgenticRiskInputAgentGenerated             AgenticRiskInputMode = "agent-generated"
	AgenticRiskInputExistingCandidate          AgenticRiskInputMode = "existing-candidate"
	agenticRiskInputLegacyAgentDiscovery       AgenticRiskInputMode = "agent-discovery"
	agenticRiskInputLegacyFixedAccepted        AgenticRiskInputMode = "fixed-accepted"
	agenticMethodMaxInvestigation                                   = 1_000
)

// AgenticSourceExposureSpec identifies the source surface available to the
// Risk Agent without recording machine-local directory paths. Exact excerpts
// remain bound by the durable provider-call journal.
type AgenticSourceExposureSpec struct {
	Mode              string   `json:"mode"`
	ReferencePrefixes []string `json:"reference_prefixes,omitempty"`
	CatalogDigest     string   `json:"catalog_digest,omitempty"`
}

// AgenticEpisodeLimits binds execution-shaping limits that are more specific
// than the logical budget. Different Risk/Scenario splits or plan depths are
// different methods even when their total allowance is equal.
type AgenticEpisodeLimits struct {
	MaxRiskCalls         int   `json:"max_risk_calls"`
	MaxScenarioCalls     int   `json:"max_scenario_calls"`
	MaxTotalCalls        int   `json:"max_total_calls"`
	MaxObservedTokens    int   `json:"max_observed_tokens"`
	MaxScenarioPlanSteps int   `json:"max_scenario_plan_steps"`
	MaxRuntimeDecisions  int   `json:"max_runtime_decisions"`
	SessionWallClockMS   int64 `json:"session_wall_clock_ms"`
}

type AgenticCapabilityFeedbackMode string

type AgenticClosureMode string

type AgenticRiskInputMode string

func (mode AgenticClosureMode) Validate() error {
	switch mode {
	case AgenticClosureModePublicFixed, AgenticClosureModeTargetLocal:
		return nil
	default:
		return errors.New("EXPERIMENT_AGENTIC_CLOSURE_MODE_INVALID")
	}
}

// Empty is the legacy projection used by historical MethodSpec artifacts.
// New CLI runs always resolve to one of the two explicit modes.
func (mode AgenticCapabilityFeedbackMode) Validate() error {
	switch mode {
	case "", AgenticCapabilityFeedbackReasonCodes, AgenticCapabilityFeedbackStructuredGaps:
		return nil
	default:
		return errors.New("EXPERIMENT_AGENTIC_CAPABILITY_FEEDBACK_MODE_INVALID")
	}
}

// AgenticCapabilityFeedbackProbe is a public calibration input. Trusted code
// preflights the plan against the real TargetSurface and exposes only the
// resulting mechanical gap through exploration Memory; the plan is never
// executed as Runtime work.
type AgenticCapabilityFeedbackProbe struct {
	ID               string       `json:"id"`
	Plan             ScenarioPlan `json:"plan"`
	MaxScenarioCalls int          `json:"max_scenario_calls"`
}

func (probe AgenticCapabilityFeedbackProbe) Validate() error {
	if !validMethodToken(probe.ID) || probe.Plan.Validate() != nil ||
		probe.MaxScenarioCalls != 1 {
		return errors.New("EXPERIMENT_AGENTIC_CAPABILITY_FEEDBACK_PROBE_INVALID")
	}
	return nil
}

// AgenticMethodSpec is the typed, protocol-neutral projection of the actual
// Agent invocation configuration. SUT/build identity remains in the formal
// benchmark contract and qualified Bundle; this spec binds the method knobs
// that can vary for the same SUT.
type AgenticMethodSpec struct {
	SchemaVersion            string                          `json:"schema_version"`
	ExecutorID               string                          `json:"executor_id"`
	Strategy                 string                          `json:"strategy"`
	ImplementationID         string                          `json:"implementation_id"`
	TargetID                 string                          `json:"target_id"`
	Transport                AgentTransportFreeze            `json:"transport"`
	ScenarioTransport        *AgentTransportFreeze           `json:"scenario_transport,omitempty"`
	RiskPromptVersion        string                          `json:"risk_prompt_version"`
	ScenarioPromptVersion    string                          `json:"scenario_prompt_version"`
	SemanticInputSchema      string                          `json:"semantic_input_schema"`
	SemanticInputDigest      string                          `json:"semantic_input_digest"`
	ScenarioSemanticExposure ScenarioSemanticExposureMode    `json:"scenario_semantic_exposure"`
	ClosureMode              AgenticClosureMode              `json:"closure_mode,omitempty"`
	RiskInputMode            AgenticRiskInputMode            `json:"risk_input_mode,omitempty"`
	RiskInputDigest          string                          `json:"risk_input_digest,omitempty"`
	CapabilityFeedbackMode   AgenticCapabilityFeedbackMode   `json:"capability_feedback_mode,omitempty"`
	CapabilityFeedbackProbe  *AgenticCapabilityFeedbackProbe `json:"capability_feedback_probe,omitempty"`
	SourceExposure           AgenticSourceExposureSpec       `json:"source_exposure"`
	EpisodeLimits            AgenticEpisodeLimits            `json:"episode_limits"`
	InvestigationEpisodes    int                             `json:"investigation_episodes"`
	EpisodeBudget            AgenticLogicalBudget            `json:"episode_budget"`
	InvestigationBudget      AgenticLogicalBudget            `json:"investigation_budget"`
	Digest                   string                          `json:"digest"`
}

func NewAgenticMethodSpec(spec AgenticMethodSpec) (AgenticMethodSpec, error) {
	spec.SchemaVersion = AgenticMethodSpecSchemaVersion
	spec.ExecutorID = AgenticMethodExecutorID
	spec.Strategy = AgenticMethodStrategyID
	spec.ImplementationID = AgenticMethodImplementationID
	spec.SourceExposure.ReferencePrefixes = append(
		[]string(nil), spec.SourceExposure.ReferencePrefixes...,
	)
	if spec.ScenarioTransport != nil {
		transport := *spec.ScenarioTransport
		spec.ScenarioTransport = &transport
	}
	if spec.CapabilityFeedbackProbe != nil {
		probe := *spec.CapabilityFeedbackProbe
		probe.Plan.Steps = append([]ScenarioStep(nil), probe.Plan.Steps...)
		spec.CapabilityFeedbackProbe = &probe
	}
	sort.Strings(spec.SourceExposure.ReferencePrefixes)
	spec.Digest = ""
	digest, err := control.CanonicalDigest(spec)
	if err != nil {
		return AgenticMethodSpec{}, err
	}
	spec.Digest = digest
	if spec.Validate() != nil {
		return AgenticMethodSpec{}, errors.New("EXPERIMENT_AGENTIC_METHOD_SPEC_INVALID")
	}
	return spec, nil
}

func (spec AgenticMethodSpec) Validate() error {
	closureModeValid := spec.ClosureMode.Validate() == nil
	if spec.ImplementationID == agenticMethodLegacyImplementationID {
		closureModeValid = spec.ClosureMode == ""
	}
	riskInputValid := spec.RiskInputMode == AgenticRiskInputAgentGenerated &&
		spec.RiskInputDigest == "" || spec.RiskInputMode == AgenticRiskInputExistingCandidate &&
		validSHA256(spec.RiskInputDigest)
	if spec.ImplementationID == agenticMethodClosureLegacyImplementationID {
		riskInputValid = spec.RiskInputMode == "" && spec.RiskInputDigest == "" ||
			spec.RiskInputMode == agenticRiskInputLegacyAgentDiscovery && spec.RiskInputDigest == "" ||
			spec.RiskInputMode == agenticRiskInputLegacyFixedAccepted && validSHA256(spec.RiskInputDigest)
	} else if spec.ImplementationID == agenticMethodLegacyImplementationID {
		riskInputValid = spec.RiskInputMode == "" && spec.RiskInputDigest == ""
	}
	if spec.SchemaVersion != AgenticMethodSpecSchemaVersion ||
		spec.ExecutorID != AgenticMethodExecutorID || spec.Strategy != AgenticMethodStrategyID ||
		!validAgenticMethodImplementationID(spec.ImplementationID) || !validMethodToken(spec.TargetID) ||
		spec.Transport.Validate() != nil ||
		spec.ScenarioTransport != nil && spec.ScenarioTransport.Validate() != nil ||
		!validMethodToken(spec.RiskPromptVersion) ||
		!validMethodToken(spec.ScenarioPromptVersion) || !validMethodToken(spec.SemanticInputSchema) ||
		!validSHA256(spec.SemanticInputDigest) || spec.ScenarioSemanticExposure.Validate() != nil ||
		!closureModeValid || !riskInputValid ||
		spec.CapabilityFeedbackMode.Validate() != nil ||
		spec.CapabilityFeedbackProbe != nil && spec.CapabilityFeedbackProbe.Validate() != nil ||
		spec.CapabilityFeedbackProbe != nil && (spec.InvestigationEpisodes != 1 ||
			spec.EpisodeLimits.MaxScenarioCalls != spec.CapabilityFeedbackProbe.MaxScenarioCalls) ||
		spec.InvestigationEpisodes <= 0 || spec.InvestigationEpisodes > agenticMethodMaxInvestigation ||
		spec.EpisodeBudget.Validate() != nil || spec.InvestigationBudget.Validate() != nil ||
		!validAgenticSourceExposure(spec.SourceExposure) ||
		!validAgenticEpisodeLimits(spec.EpisodeLimits, spec.EpisodeBudget) {
		return errors.New("EXPERIMENT_AGENTIC_METHOD_SPEC_INVALID")
	}
	wantBudget, err := ScaleAgenticLogicalBudget(
		spec.EpisodeBudget, spec.InvestigationEpisodes,
	)
	if err != nil || wantBudget != spec.InvestigationBudget {
		return errors.New("EXPERIMENT_AGENTIC_METHOD_SPEC_BUDGET_INVALID")
	}
	want := spec
	want.Digest = ""
	digest, err := control.CanonicalDigest(want)
	if err != nil || !validSHA256(spec.Digest) || digest != spec.Digest {
		return errors.New("EXPERIMENT_AGENTIC_METHOD_SPEC_DIGEST_MISMATCH")
	}
	return nil
}

func validAgenticMethodImplementationID(id string) bool {
	return id == AgenticMethodImplementationID ||
		id == agenticMethodClosureLegacyImplementationID ||
		id == agenticMethodLegacyImplementationID
}

func validAgenticEpisodeLimits(limits AgenticEpisodeLimits, budget AgenticLogicalBudget) bool {
	return limits.MaxRiskCalls > 0 && limits.MaxScenarioCalls > 0 && limits.MaxTotalCalls > 0 &&
		limits.MaxRiskCalls+limits.MaxScenarioCalls <= limits.MaxTotalCalls &&
		limits.MaxTotalCalls == budget.MaxModelCalls &&
		limits.MaxObservedTokens == budget.MaxModelTokens && limits.MaxScenarioPlanSteps > 0 &&
		limits.MaxRuntimeDecisions > 0 && limits.SessionWallClockMS > 0 &&
		limits.MaxRuntimeDecisions <= budget.MaxPrimarySchedulerDecisions
}

func ScaleAgenticLogicalBudget(
	budget AgenticLogicalBudget,
	multiplier int,
) (AgenticLogicalBudget, error) {
	if budget.Validate() != nil || multiplier <= 0 || multiplier > agenticMethodMaxInvestigation {
		return AgenticLogicalBudget{}, errors.New("EXPERIMENT_AGENTIC_BUDGET_SCALE_INVALID")
	}
	scale := func(value int) (int, bool) {
		maxInt := int(^uint(0) >> 1)
		return value * multiplier, value <= maxInt/multiplier
	}
	result := AgenticLogicalBudget{}
	var ok bool
	if result.MaxAttempts, ok = scale(budget.MaxAttempts); !ok {
		return AgenticLogicalBudget{}, errors.New("EXPERIMENT_AGENTIC_BUDGET_SCALE_OVERFLOW")
	}
	if result.MaxPrimarySchedulerDecisions, ok = scale(budget.MaxPrimarySchedulerDecisions); !ok {
		return AgenticLogicalBudget{}, errors.New("EXPERIMENT_AGENTIC_BUDGET_SCALE_OVERFLOW")
	}
	if result.MaxPrimaryWorkUnits, ok = scale(budget.MaxPrimaryWorkUnits); !ok {
		return AgenticLogicalBudget{}, errors.New("EXPERIMENT_AGENTIC_BUDGET_SCALE_OVERFLOW")
	}
	if result.MaxReplayWorkUnits, ok = scale(budget.MaxReplayWorkUnits); !ok {
		return AgenticLogicalBudget{}, errors.New("EXPERIMENT_AGENTIC_BUDGET_SCALE_OVERFLOW")
	}
	if result.MaxModelCalls, ok = scale(budget.MaxModelCalls); !ok {
		return AgenticLogicalBudget{}, errors.New("EXPERIMENT_AGENTIC_BUDGET_SCALE_OVERFLOW")
	}
	if result.MaxModelTokens, ok = scale(budget.MaxModelTokens); !ok {
		return AgenticLogicalBudget{}, errors.New("EXPERIMENT_AGENTIC_BUDGET_SCALE_OVERFLOW")
	}
	return result, result.Validate()
}

func validAgenticSourceExposure(source AgenticSourceExposureSpec) bool {
	switch source.Mode {
	case AgenticSourceExposureNone:
		return len(source.ReferencePrefixes) == 0 && source.CatalogDigest == ""
	case AgenticSourceExposureDossierV1, AgenticSourceExposureDossierV2:
		if len(source.ReferencePrefixes) == 0 || !validSHA256(source.CatalogDigest) {
			return false
		}
	default:
		return false
	}
	for index, prefix := range source.ReferencePrefixes {
		if strings.TrimSpace(prefix) == "" || strings.ContainsAny(prefix, " \\\r\n\x00") ||
			(index > 0 && source.ReferencePrefixes[index-1] >= prefix) {
			return false
		}
	}
	return true
}
