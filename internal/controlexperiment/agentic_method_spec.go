package controlexperiment

import (
	"errors"
	"sort"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const (
	AgenticMethodSpecSchemaVersion                                         = "consensus-atlas/agentic-method-spec/v1"
	AgenticMethodExecutorID                                                = "consensus-atlas/agentic-episode-cli/v1"
	AgenticMethodStrategyID                                                = "agentic-episode-v1"
	AgenticMethodImplementationID                                          = "consensus-atlas/agentic-method/m4n20-bootstrap-root-only-v1"
	agenticMethodImplementationM4n19                                       = "consensus-atlas/agentic-method/m4n19-public-progress-only-v1"
	agenticMethodImplementationM4n18                                       = "consensus-atlas/agentic-method/m4n18-closure-disabled-progress-v1"
	agenticMethodImplementationM4n17                                       = "consensus-atlas/agentic-method/m4n17-strategic-bootstrap-v1"
	agenticMethodImplementationM4n16                                       = "consensus-atlas/agentic-method/m4n16-recorded-schedule-execution-v1"
	agenticMethodImplementationM4n15                                       = "consensus-atlas/agentic-method/m4n15-single-path-artifact-v1"
	agenticMethodImplementationM4n14                                       = "consensus-atlas/agentic-method/m4n14-recorded-invoke-and-risk-length-repair-v1"
	agenticMethodImplementationM4n13                                       = "consensus-atlas/agentic-method/m4n13-causal-bootstrap-and-sealed-token-stop-v1"
	agenticMethodImplementationM4n12                                       = "consensus-atlas/agentic-method/m4n12-bootstrap-root-and-action-coherence-v1"
	agenticMethodImplementationM4n11                                       = "consensus-atlas/agentic-method/m4n11-provider-recovery-and-quorum-oracle-v1"
	agenticMethodM4n10LegacyID                                             = "consensus-atlas/agentic-method/m4n10-deep-candidate-investigation-v1"
	agenticMethodM4n8LegacyID                                              = "consensus-atlas/agentic-method/m4n8-agent-semantics-portfolio-search-v1"
	agenticMethodM4n7LegacyID                                              = "consensus-atlas/agentic-method/m4n7-qualification-cost-cargo-replay-v1"
	agenticMethodM4n6LegacyID                                              = "consensus-atlas/agentic-method/m4n6-causal-closure-build-evidence-v1"
	agenticMethodM4n5LegacyID                                              = "consensus-atlas/agentic-method/m4n5-multinode-closure-v1"
	agenticMethodM4n2LegacyID                                              = "consensus-atlas/agentic-method/m4n2-preparation-replay-attribution-v1"
	agenticMethodM4n1LegacyID                                              = "consensus-atlas/agentic-method/m4n1-local-sut-source-binding-v1"
	agenticMethodRiskFidelityLegacyID                                      = "consensus-atlas/agentic-method/m4m4-risk-fidelity-v1"
	agenticMethodM4m1LegacyID                                              = "consensus-atlas/agentic-method/m4m1-closure-ownership-v1"
	agenticMethodClosureHandoffLegacyImplementationID                      = "consensus-atlas/agentic-method/m4l7-risk-input-closure-handoff-v1"
	agenticMethodClosureLegacyImplementationID                             = "consensus-atlas/agentic-method/m4l5-closure-v1"
	agenticMethodLegacyImplementationID                                    = "consensus-atlas/agentic-method/m4d-v1"
	AgenticSourceExposureNone                                              = "none"
	AgenticSourceExposureDossierV1                                         = "dossier-declared-readonly-v1"
	AgenticSourceExposureDossierV2                                         = "dossier-declared-readonly-navigation-v2"
	AgenticSourceExposureRepositorySearchV3                                = "mounted-repository-search-readonly-v3"
	AgenticCapabilityFeedbackReasonCodes                                   = "reason-codes"
	AgenticCapabilityFeedbackStructuredGaps                                = "structured-gaps"
	AgenticClosureModePublicFixed                                          = "public-fixed"
	AgenticClosureModeTargetLocal                                          = "target-local"
	AgenticRiskInputAgentGenerated                    AgenticRiskInputMode = "agent-generated"
	AgenticRiskInputExistingCandidate                 AgenticRiskInputMode = "existing-candidate"
	agenticRiskInputLegacyAgentDiscovery              AgenticRiskInputMode = "agent-discovery"
	agenticRiskInputLegacyFixedAccepted               AgenticRiskInputMode = "fixed-accepted"
	agenticMethodMaxInvestigation                                          = 1_000
)

// AgenticSourceExposureSpec identifies the source surface available to the
// Risk Agent without recording machine-local directory paths. Exact excerpts
// remain bound by the durable provider-call journal.
type AgenticSourceExposureSpec struct {
	Mode              string                    `json:"mode"`
	ReferencePrefixes []string                  `json:"reference_prefixes,omitempty"`
	CatalogDigest     string                    `json:"catalog_digest,omitempty"`
	SUTBindings       []AgenticSUTSourceBinding `json:"sut_bindings,omitempty"`
}

// AgenticSUTSourceBinding ties an Agent-visible module reference prefix to
// the exact local source-tree identity used for this method run. Machine-local
// directories stay outside MethodSpec; the runtime mount retains them.
type AgenticSUTSourceBinding struct {
	ReferencePrefix string `json:"reference_prefix"`
	ModulePath      string `json:"module_path"`
	ModuleVersion   string `json:"module_version"`
	ContentDigest   string `json:"content_digest"`
}

func (binding AgenticSUTSourceBinding) Validate() error {
	if strings.TrimSpace(binding.ReferencePrefix) == "" ||
		strings.ContainsAny(binding.ReferencePrefix, " \\\r\n\x00") ||
		!strings.HasSuffix(binding.ReferencePrefix, "/") ||
		strings.TrimSpace(binding.ModulePath) == "" ||
		strings.ContainsAny(binding.ModulePath, " \\\r\n\x00") ||
		strings.TrimSpace(binding.ModuleVersion) == "" ||
		strings.ContainsAny(binding.ModuleVersion, " \\\r\n\x00") ||
		!validSHA256(binding.ContentDigest) {
		return errors.New("EXPERIMENT_AGENTIC_SUT_SOURCE_BINDING_INVALID")
	}
	return nil
}

// AgenticEpisodeLimits binds execution-shaping limits that are more specific
// than the logical budget. Different Risk/Scenario splits or plan depths are
// different methods even when their total allowance is equal.
type AgenticEpisodeLimits struct {
	MaxRiskCalls           int   `json:"max_risk_calls"`
	MaxScenarioCalls       int   `json:"max_scenario_calls"`
	MaxTotalCalls          int   `json:"max_total_calls"`
	MaxObservedTokens      int   `json:"max_observed_tokens"`
	MaxScenarioPlanSteps   int   `json:"max_scenario_plan_steps"`
	MaxRuntimeDecisions    int   `json:"max_runtime_decisions"`
	SessionWallClockMS     int64 `json:"session_wall_clock_ms"`
	PreparationWallClockMS int64 `json:"preparation_wall_clock_ms,omitempty"`
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
	if spec.EpisodeLimits.PreparationWallClockMS == 0 {
		spec.EpisodeLimits.PreparationWallClockMS = 1_200_000
	}
	spec.SourceExposure.ReferencePrefixes = append(
		[]string(nil), spec.SourceExposure.ReferencePrefixes...,
	)
	spec.SourceExposure.SUTBindings = append(
		[]AgenticSUTSourceBinding(nil), spec.SourceExposure.SUTBindings...,
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
	sort.Slice(spec.SourceExposure.SUTBindings, func(i, j int) bool {
		return spec.SourceExposure.SUTBindings[i].ReferencePrefix <
			spec.SourceExposure.SUTBindings[j].ReferencePrefix
	})
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
	preparationLimitValid := spec.EpisodeLimits.PreparationWallClockMS > 0
	if spec.ImplementationID != AgenticMethodImplementationID {
		preparationLimitValid = spec.EpisodeLimits.PreparationWallClockMS >= 0
	}
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
		!closureModeValid || !riskInputValid || !preparationLimitValid ||
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
		id == agenticMethodImplementationM4n19 ||
		id == agenticMethodImplementationM4n18 ||
		id == agenticMethodImplementationM4n17 ||
		id == agenticMethodImplementationM4n16 ||
		id == agenticMethodImplementationM4n15 ||
		id == agenticMethodImplementationM4n14 ||
		id == agenticMethodImplementationM4n13 ||
		id == agenticMethodImplementationM4n12 ||
		id == agenticMethodImplementationM4n11 ||
		id == agenticMethodM4n10LegacyID ||
		id == agenticMethodM4n8LegacyID ||
		id == agenticMethodM4n7LegacyID ||
		id == agenticMethodM4n6LegacyID ||
		id == agenticMethodM4n5LegacyID ||
		id == agenticMethodM4n2LegacyID ||
		id == agenticMethodM4n1LegacyID ||
		id == agenticMethodRiskFidelityLegacyID ||
		id == agenticMethodM4m1LegacyID ||
		id == agenticMethodClosureHandoffLegacyImplementationID ||
		id == agenticMethodClosureLegacyImplementationID ||
		id == agenticMethodLegacyImplementationID
}

// AgenticMethodRequiresOracleAttribution reports whether an implementation
// version treats root/post-root Oracle attribution as part of the persisted
// execution meaning. M4n11 introduced that requirement; later versions must
// not accidentally weaken it when the current implementation ID advances.
func AgenticMethodRequiresOracleAttribution(implementationID string) bool {
	return implementationID == AgenticMethodImplementationID ||
		implementationID == agenticMethodImplementationM4n19 ||
		implementationID == agenticMethodImplementationM4n18 ||
		implementationID == agenticMethodImplementationM4n17 ||
		implementationID == agenticMethodImplementationM4n16 ||
		implementationID == agenticMethodImplementationM4n15 ||
		implementationID == agenticMethodImplementationM4n14 ||
		implementationID == agenticMethodImplementationM4n13 ||
		implementationID == agenticMethodImplementationM4n12 ||
		implementationID == agenticMethodImplementationM4n11
}

func validAgenticEpisodeLimits(limits AgenticEpisodeLimits, budget AgenticLogicalBudget) bool {
	preparationValid := limits.PreparationWallClockMS > 0
	if limits.PreparationWallClockMS == 0 {
		preparationValid = true // historical MethodSpecs predate bounded preparation
	}
	return limits.MaxRiskCalls > 0 && limits.MaxScenarioCalls > 0 && limits.MaxTotalCalls > 0 &&
		limits.MaxRiskCalls+limits.MaxScenarioCalls <= limits.MaxTotalCalls &&
		limits.MaxTotalCalls == budget.MaxModelCalls &&
		limits.MaxObservedTokens == budget.MaxModelTokens && limits.MaxScenarioPlanSteps > 0 &&
		limits.MaxRuntimeDecisions > 0 && limits.SessionWallClockMS > 0 && preparationValid &&
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
		return len(source.ReferencePrefixes) == 0 && source.CatalogDigest == "" &&
			len(source.SUTBindings) == 0
	case AgenticSourceExposureDossierV1, AgenticSourceExposureDossierV2,
		AgenticSourceExposureRepositorySearchV3:
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
	for index, binding := range source.SUTBindings {
		if binding.Validate() != nil ||
			(index > 0 && source.SUTBindings[index-1].ReferencePrefix >= binding.ReferencePrefix) ||
			!sortedStringsContain(source.ReferencePrefixes, binding.ReferencePrefix) {
			return false
		}
	}
	return true
}

func sortedStringsContain(values []string, value string) bool {
	index := sort.SearchStrings(values, value)
	return index < len(values) && values[index] == value
}
