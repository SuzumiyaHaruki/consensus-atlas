package controlexperiment

import (
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const agentWorkloadInputJSONMaxBytes = 16 * 1024

// AgentWorkloadInvocation is a read-only projection of one configured client
// input. JSON payloads remain visible because their meaning is useful planning
// context; non-JSON payloads expose only their declared schema and size.
type AgentWorkloadInvocation struct {
	ID                  string          `json:"id"`
	ExpectedStatus      string          `json:"expected_status"`
	InputSchemaVersion  string          `json:"input_schema_version"`
	InputEncoding       string          `json:"input_encoding"`
	InputBytes          int             `json:"input_bytes"`
	InputJSON           json.RawMessage `json:"input_json,omitempty"`
	InputContentOmitted bool            `json:"input_content_omitted,omitempty"`
}

type AgentWorkloadSurface struct {
	ID             string                    `json:"id"`
	TargetSelector string                    `json:"target_selector"`
	Invocations    []AgentWorkloadInvocation `json:"invocations"`
}

type AgentRuntimeSurface struct {
	RuntimeClockError   uint64 `json:"runtime_clock_error"`
	TemporalClockError  uint64 `json:"temporal_clock_error"`
	MaxMessageClones    uint64 `json:"max_message_clones"`
	StrictYield         bool   `json:"strict_yield"`
	StrictReplay        bool   `json:"strict_replay"`
	EntropyProvider     string `json:"entropy_provider,omitempty"`
	EntropyAlgorithm    string `json:"entropy_algorithm,omitempty"`
	EntropyDomainPolicy string `json:"entropy_domain_policy,omitempty"`
	EntropyResetPolicy  string `json:"entropy_reset_policy,omitempty"`
	EntropyStrictReplay bool   `json:"entropy_strict_replay"`
}

const (
	AgentOracleScopeGeneric = "generic"
	AgentOracleScopeTarget  = "target"
)

// AgentOracleCapability describes a registered deterministic monitor. Empty
// PropertyIDs mean the monitor validates execution evidence rather than one
// protocol property (for example, trace integrity).
type AgentOracleCapability struct {
	ID          string   `json:"id"`
	Scope       string   `json:"scope"`
	PropertyIDs []string `json:"property_ids,omitempty"`
}

// AgentFidelityBoundary is a Target-Pack-authored disclosure of behavior the
// active composition cannot faithfully exercise. It is planning context, not
// an execution gate or a claim that every linked hypothesis requires it.
type AgentFidelityBoundary struct {
	ID                  string   `json:"id"`
	Summary             string   `json:"summary"`
	AffectedPropertyIDs []string `json:"affected_property_ids,omitempty"`
}

const (
	AgentCapabilityGapTargetFidelity        = "target-fidelity-gap"
	AgentCapabilityGapMissingAction         = "missing-action"
	AgentCapabilityGapMissingControl        = "missing-control-capability"
	AgentCapabilityNoticeFidelityUnassessed = "fidelity-unassessed"
	AgentFidelityNotApplicable              = "not-applicable"
	AgentFidelityUnassessed                 = "fidelity-unassessed"
)

type AgentCapabilityGap struct {
	Code      string `json:"code"`
	Reference string `json:"reference"`
	Summary   string `json:"summary"`
}

// AgentTargetExtensions are supplied by the Target Pack. The constructor
// checks them against the actual Manifest and the semantic declarations before
// exposing them to an Agent.
type AgentTargetExtensions struct {
	ComposableActions       []control.ActionKind             `json:"composable_actions"`
	ObservationCapabilities []semantic.ObservationCapability `json:"observation_capabilities"`
	OracleCapabilities      []AgentOracleCapability          `json:"oracle_capabilities"`
	FidelityBoundaries      []AgentFidelityBoundary          `json:"fidelity_boundaries,omitempty"`
}

type AgentCapabilitySurface struct {
	DeclaredActions         []control.ActionKind             `json:"declared_actions"`
	ComposableActions       []control.ActionKind             `json:"composable_actions"`
	Message                 *control.MessageCapability       `json:"message,omitempty"`
	HostEffects             []control.HostEffectCapability   `json:"host_effects,omitempty"`
	ObservationCapabilities []semantic.ObservationCapability `json:"observation_capabilities"`
	OracleCapabilities      []AgentOracleCapability          `json:"oracle_capabilities"`
	FidelityBoundaries      []AgentFidelityBoundary          `json:"fidelity_boundaries,omitempty"`
}

// AgentTargetSurface is mechanically projected from the active target's
// validated Manifest, workload and execution configuration. It describes what
// this episode can actually exercise; it grants no Action or verdict authority.
type AgentTargetSurface struct {
	TargetID           string                 `json:"target_id"`
	AdapterID          string                 `json:"adapter_id"`
	ImplementationID   string                 `json:"implementation_id"`
	Nodes              []control.NodeID       `json:"nodes"`
	Workload           AgentWorkloadSurface   `json:"workload"`
	Runtime            AgentRuntimeSurface    `json:"runtime"`
	FaultAllowance     FaultEnvelope          `json:"fault_allowance"`
	TemporalKinds      []control.TemporalKind `json:"temporal_kinds,omitempty"`
	CrashModes         []string               `json:"crash_modes,omitempty"`
	EffectKinds        []string               `json:"effect_kinds,omitempty"`
	DurableCheckpoints bool                   `json:"durable_checkpoints"`
	Capabilities       AgentCapabilitySurface `json:"capabilities"`
}

func NewAgentTargetSurface(
	targetID string,
	manifest control.AdapterManifest,
	workload WorkloadPlan,
	runtime RuntimeConfig,
	faultAllowance FaultEnvelope,
	extensions AgentTargetExtensions,
) (AgentTargetSurface, error) {
	if !validMethodToken(targetID) || manifest.Validate() != nil || workload.Validate() != nil ||
		faultAllowance.Validate() != nil ||
		validateAgentTargetExtensions(extensions, manifest, faultAllowance) != nil {
		return AgentTargetSurface{}, errors.New("EXPERIMENT_AGENT_TARGET_SURFACE_INPUT_INVALID")
	}
	if _, err := runtime.runtimeConfig(); err != nil {
		return AgentTargetSurface{}, errors.New("EXPERIMENT_AGENT_TARGET_SURFACE_INPUT_INVALID")
	}
	invocations := make([]AgentWorkloadInvocation, 0, len(workload.Invocations))
	for _, invocation := range workload.Invocations {
		projected := AgentWorkloadInvocation{
			ID: invocation.ID, ExpectedStatus: invocation.ExpectedStatus,
			InputSchemaVersion: invocation.Input.SchemaVersion,
			InputEncoding:      invocation.Input.Encoding, InputBytes: len(invocation.Input.Bytes),
		}
		if invocation.Input.Encoding == "json" && json.Valid(invocation.Input.Bytes) &&
			len(invocation.Input.Bytes) <= agentWorkloadInputJSONMaxBytes {
			projected.InputJSON = append(json.RawMessage(nil), invocation.Input.Bytes...)
		} else if invocation.Input.Encoding == "json" {
			projected.InputContentOmitted = true
		}
		invocations = append(invocations, projected)
	}
	declaredActions := append([]control.ActionKind(nil), manifest.Capabilities.Actions...)
	sort.Slice(declaredActions, func(i, j int) bool { return declaredActions[i] < declaredActions[j] })
	surface := AgentTargetSurface{
		TargetID: targetID, AdapterID: manifest.AdapterID,
		ImplementationID: manifest.ImplementationID,
		Nodes:            append([]control.NodeID(nil), manifest.Nodes...),
		Workload: AgentWorkloadSurface{
			ID: workload.ID, TargetSelector: workload.TargetSelector, Invocations: invocations,
		},
		Runtime: AgentRuntimeSurface{
			RuntimeClockError: runtime.ClockError, TemporalClockError: manifest.Capabilities.Temporal.ClockError,
			MaxMessageClones: runtime.MaxClones, StrictYield: manifest.Capabilities.StrictYield,
			StrictReplay: manifest.Capabilities.StrictReplay, EntropyProvider: manifest.Capabilities.Entropy.Provider,
			EntropyAlgorithm:    manifest.Capabilities.Entropy.Algorithm,
			EntropyDomainPolicy: manifest.Capabilities.Entropy.DomainPolicy,
			EntropyResetPolicy:  manifest.Capabilities.Entropy.ResetPolicy,
			EntropyStrictReplay: manifest.Capabilities.Entropy.StrictReplay,
		},
		FaultAllowance:     faultAllowance,
		TemporalKinds:      append([]control.TemporalKind(nil), manifest.Capabilities.Temporal.Kinds...),
		CrashModes:         append([]string(nil), manifest.Capabilities.CrashModes...),
		EffectKinds:        append([]string(nil), manifest.Capabilities.EffectKinds...),
		DurableCheckpoints: manifest.Capabilities.DurableCheckpoints,
		Capabilities: AgentCapabilitySurface{
			DeclaredActions:         declaredActions,
			ComposableActions:       append([]control.ActionKind(nil), extensions.ComposableActions...),
			Message:                 cloneMessageCapability(manifest.Capabilities.Message),
			HostEffects:             cloneHostEffectCapabilities(manifest.Capabilities.HostEffects),
			ObservationCapabilities: cloneObservationCapabilities(extensions.ObservationCapabilities),
			OracleCapabilities:      cloneAgentOracleCapabilities(extensions.OracleCapabilities),
			FidelityBoundaries:      cloneAgentFidelityBoundaries(extensions.FidelityBoundaries),
		},
	}
	sortAgentCapabilitySurface(&surface.Capabilities)
	if surface.Validate() != nil {
		return AgentTargetSurface{}, errors.New("EXPERIMENT_AGENT_TARGET_SURFACE_INVALID")
	}
	return surface, nil
}

func (surface AgentTargetSurface) Validate() error {
	if !validMethodToken(surface.TargetID) || strings.TrimSpace(surface.AdapterID) == "" ||
		strings.TrimSpace(surface.ImplementationID) == "" || len(surface.Nodes) == 0 ||
		surface.Workload.ID == "" || surface.Workload.TargetSelector == "" ||
		len(surface.Workload.Invocations) == 0 || surface.FaultAllowance.Validate() != nil {
		return errors.New("EXPERIMENT_AGENT_TARGET_SURFACE_INVALID")
	}
	seenNodes := make(map[control.NodeID]bool, len(surface.Nodes))
	for _, node := range surface.Nodes {
		if node == "" || seenNodes[node] {
			return errors.New("EXPERIMENT_AGENT_TARGET_SURFACE_INVALID")
		}
		seenNodes[node] = true
	}
	seenInvocations := make(map[string]bool, len(surface.Workload.Invocations))
	for _, invocation := range surface.Workload.Invocations {
		if invocation.ID == "" || invocation.ExpectedStatus == "" ||
			invocation.InputSchemaVersion == "" || invocation.InputEncoding == "" ||
			invocation.InputBytes < 0 || seenInvocations[invocation.ID] ||
			(len(invocation.InputJSON) > 0 && (!json.Valid(invocation.InputJSON) ||
				len(invocation.InputJSON) > agentWorkloadInputJSONMaxBytes || invocation.InputContentOmitted)) {
			return errors.New("EXPERIMENT_AGENT_TARGET_SURFACE_INVALID")
		}
		seenInvocations[invocation.ID] = true
	}
	for _, kind := range surface.TemporalKinds {
		if kind.Validate() != nil {
			return errors.New("EXPERIMENT_AGENT_TARGET_SURFACE_INVALID")
		}
	}
	if validateAgentCapabilitySurface(surface.Capabilities) != nil ||
		!surfaceHostEffectsMatchKinds(surface.EffectKinds, surface.Capabilities.HostEffects) ||
		validateComposableFaultAllowance(
			surface.Capabilities.ComposableActions, surface.FaultAllowance,
		) != nil {
		return errors.New("EXPERIMENT_AGENT_TARGET_SURFACE_INVALID")
	}
	return nil
}

func surfaceHostEffectsMatchKinds(kinds []string, capabilities []control.HostEffectCapability) bool {
	if len(capabilities) == 0 {
		return true
	}
	if len(kinds) != len(capabilities) {
		return false
	}
	declared := make(map[string]bool, len(kinds))
	for _, kind := range kinds {
		if kind == "" || declared[kind] {
			return false
		}
		declared[kind] = true
	}
	for _, capability := range capabilities {
		if !declared[capability.Kind] {
			return false
		}
	}
	return true
}

// ValidateAgainstKnowledge keeps Oracle and fidelity references honest without
// teaching common orchestration what any protocol property means.
func (surface AgentTargetSurface) ValidateAgainstKnowledge(knowledge ProtocolKnowledgePack) error {
	if surface.Validate() != nil || knowledge.ValidateAgentMaterials() != nil {
		return errors.New("EXPERIMENT_AGENT_TARGET_KNOWLEDGE_INVALID")
	}
	properties := make(map[string]ProtocolProperty, len(knowledge.Properties))
	oracleBacked := make(map[string]bool)
	for _, property := range knowledge.Properties {
		properties[property.ID] = property
		if property.EvidenceLevel == PropertyEvidenceOracleBacked {
			oracleBacked[property.ID] = false
		}
	}
	for _, oracle := range surface.Capabilities.OracleCapabilities {
		for _, propertyID := range oracle.PropertyIDs {
			property, ok := properties[propertyID]
			if !ok || property.EvidenceLevel != PropertyEvidenceOracleBacked {
				return errors.New("EXPERIMENT_AGENT_TARGET_ORACLE_PROPERTY_INVALID")
			}
			oracleBacked[propertyID] = true
		}
	}
	for _, supported := range oracleBacked {
		if !supported {
			return errors.New("EXPERIMENT_AGENT_TARGET_ORACLE_PROPERTY_UNMAPPED")
		}
	}
	for _, boundary := range surface.Capabilities.FidelityBoundaries {
		for _, propertyID := range boundary.AffectedPropertyIDs {
			if _, ok := properties[propertyID]; !ok {
				return errors.New("EXPERIMENT_AGENT_TARGET_FIDELITY_PROPERTY_INVALID")
			}
		}
	}
	return nil
}

func (surface AgentTargetSurface) OracleIDsForProperty(propertyID string) []string {
	var result []string
	for _, capability := range surface.Capabilities.OracleCapabilities {
		for _, supported := range capability.PropertyIDs {
			if supported == propertyID {
				result = append(result, capability.ID)
				break
			}
		}
	}
	return result
}

func (surface AgentTargetSurface) FidelityGaps(required []string) ([]AgentCapabilityGap, error) {
	if !canonicalStrings(required, false) {
		return nil, errors.New("EXPERIMENT_AGENT_TARGET_FIDELITY_REQUIREMENTS_INVALID")
	}
	if len(required) == 0 {
		return nil, nil
	}
	boundaries := make(map[string]AgentFidelityBoundary, len(surface.Capabilities.FidelityBoundaries))
	for _, boundary := range surface.Capabilities.FidelityBoundaries {
		boundaries[boundary.ID] = boundary
	}
	result := make([]AgentCapabilityGap, 0, len(required))
	for _, reference := range required {
		boundary, ok := boundaries[reference]
		if !ok {
			return nil, errors.New("EXPERIMENT_AGENT_TARGET_FIDELITY_REQUIREMENT_UNKNOWN")
		}
		result = append(result, AgentCapabilityGap{
			Code: AgentCapabilityGapTargetFidelity, Reference: reference, Summary: boundary.Summary,
		})
	}
	return result, nil
}

// ScenarioCapabilityGaps rejects only controls that the validated Target
// surface can prove unavailable. Optional rich declarations are deliberately
// not required: an older Target without them remains unassessed and the real
// frontier is still authoritative.
func (surface AgentTargetSurface) ScenarioCapabilityGaps(
	plan ScenarioPlan,
	frontier []FrontierActionRef,
) ([]AgentCapabilityGap, error) {
	if surface.Validate() != nil || plan.Validate() != nil {
		return nil, errors.New("EXPERIMENT_AGENT_TARGET_SCENARIO_CAPABILITY_INPUT_INVALID")
	}
	composable := actionKindSet(surface.Capabilities.ComposableActions)
	var gaps []AgentCapabilityGap
	for _, step := range plan.Steps {
		selector := step.Selector
		if selector.ActionID != "" {
			for _, action := range frontier {
				if action.ActionID == selector.ActionID {
					selector.Kind = action.Kind
					break
				}
			}
			if selector.Kind == "" {
				continue
			}
		}
		if !composable[selector.Kind] {
			gaps = append(gaps, AgentCapabilityGap{
				Code: AgentCapabilityGapMissingAction, Reference: step.ID,
				Summary: "step requests a non-composable Action: " + string(selector.Kind),
			})
			continue
		}
		if selector.MessageTypeHint != "" && surface.Capabilities.Message != nil &&
			!containsAgentString(surface.Capabilities.Message.TypeHints, selector.MessageTypeHint) {
			gaps = append(gaps, AgentCapabilityGap{
				Code: AgentCapabilityGapMissingControl, Reference: step.ID,
				Summary: "step requests an undeclared message type hint: " + selector.MessageTypeHint,
			})
			continue
		}
		if (selector.Kind == control.ActionCompleteEffect || selector.Kind == control.ActionFailEffect) &&
			len(surface.Capabilities.HostEffects) > 0 &&
			!surface.surfaceSupportsEffectSelector(selector) {
			gaps = append(gaps, AgentCapabilityGap{
				Code: AgentCapabilityGapMissingControl, Reference: step.ID,
				Summary: "step requests an effect control outside the declared Target capability",
			})
		}
	}
	return gaps, nil
}

func (surface AgentTargetSurface) surfaceSupportsEffectSelector(
	selector FrontierActionSelector,
) bool {
	for _, capability := range surface.Capabilities.HostEffects {
		if selector.EffectKind != "" && selector.EffectKind != capability.Kind ||
			selector.EffectPhase != "" && !containsAgentString(capability.Phases, selector.EffectPhase) ||
			selector.Durability != "" && !containsAgentDurability(capability.Durabilities, selector.Durability) {
			continue
		}
		outcomes := capability.AllowedResults
		if selector.Kind == control.ActionFailEffect {
			outcomes = capability.AllowedFailures
		}
		if len(outcomes) > 0 &&
			(selector.EffectOutcome == "" || containsAgentString(outcomes, selector.EffectOutcome)) {
			return true
		}
	}
	return false
}

func containsAgentString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsAgentDurability(values []control.DurabilityClass, target control.DurabilityClass) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// FidelityAssessmentForProperty reports relevant disclosed boundaries when an
// Agent did not claim that its mechanism requires one. This is an honest
// planning/evidence notice, not a qualification gate: an affected property can
// still have hypotheses that do not depend on the disclosed boundary.
func (surface AgentTargetSurface) FidelityAssessmentForProperty(
	propertyID string,
	required []string,
) (string, []AgentCapabilityGap, error) {
	if !validMethodToken(propertyID) || !canonicalStrings(required, false) {
		return "", nil, errors.New("EXPERIMENT_AGENT_TARGET_FIDELITY_ASSESSMENT_INVALID")
	}
	if len(required) > 0 {
		if _, err := surface.FidelityGaps(required); err != nil {
			return "", nil, err
		}
		return AgentFidelityNotApplicable, nil, nil
	}
	var notices []AgentCapabilityGap
	for _, boundary := range surface.Capabilities.FidelityBoundaries {
		for _, affected := range boundary.AffectedPropertyIDs {
			if affected == propertyID {
				notices = append(notices, AgentCapabilityGap{
					Code:      AgentCapabilityNoticeFidelityUnassessed,
					Reference: boundary.ID, Summary: boundary.Summary,
				})
				break
			}
		}
	}
	if len(notices) > 0 {
		return AgentFidelityUnassessed, notices, nil
	}
	return AgentFidelityNotApplicable, nil, nil
}

// MatchesPlanningInputs confirms that the independently passed Risk-Agent
// inputs are exactly the canonical capabilities disclosed by this surface.
func (surface AgentTargetSurface) MatchesPlanningInputs(
	actions []control.ActionKind,
	observations []semantic.ObservationCapability,
) bool {
	candidate := AgentCapabilitySurface{
		ComposableActions:       append([]control.ActionKind(nil), actions...),
		ObservationCapabilities: cloneObservationCapabilities(observations),
	}
	sortAgentCapabilitySurface(&candidate)
	return reflect.DeepEqual(surface.Capabilities.ComposableActions, candidate.ComposableActions) &&
		reflect.DeepEqual(
			surface.Capabilities.ObservationCapabilities,
			candidate.ObservationCapabilities,
		)
}

func cloneAgentTargetSurface(surface *AgentTargetSurface) *AgentTargetSurface {
	if surface == nil {
		return nil
	}
	cloned := *surface
	cloned.Nodes = append([]control.NodeID(nil), surface.Nodes...)
	cloned.TemporalKinds = append([]control.TemporalKind(nil), surface.TemporalKinds...)
	cloned.CrashModes = append([]string(nil), surface.CrashModes...)
	cloned.EffectKinds = append([]string(nil), surface.EffectKinds...)
	cloned.Capabilities.DeclaredActions = append(
		[]control.ActionKind(nil), surface.Capabilities.DeclaredActions...,
	)
	cloned.Capabilities.ComposableActions = append(
		[]control.ActionKind(nil), surface.Capabilities.ComposableActions...,
	)
	cloned.Capabilities.Message = cloneMessageCapability(surface.Capabilities.Message)
	cloned.Capabilities.HostEffects = cloneHostEffectCapabilities(surface.Capabilities.HostEffects)
	cloned.Capabilities.ObservationCapabilities = cloneObservationCapabilities(
		surface.Capabilities.ObservationCapabilities,
	)
	cloned.Capabilities.OracleCapabilities = cloneAgentOracleCapabilities(
		surface.Capabilities.OracleCapabilities,
	)
	cloned.Capabilities.FidelityBoundaries = cloneAgentFidelityBoundaries(
		surface.Capabilities.FidelityBoundaries,
	)
	cloned.Workload.Invocations = append([]AgentWorkloadInvocation(nil), surface.Workload.Invocations...)
	for index := range cloned.Workload.Invocations {
		cloned.Workload.Invocations[index].InputJSON = append(
			json.RawMessage(nil), surface.Workload.Invocations[index].InputJSON...,
		)
	}
	return &cloned
}

func validateAgentTargetExtensions(
	extensions AgentTargetExtensions,
	manifest control.AdapterManifest,
	faultAllowance FaultEnvelope,
) error {
	if !canonicalActionKinds(extensions.ComposableActions, true) ||
		semantic.ValidateObservationCapabilities(extensions.ObservationCapabilities) != nil ||
		len(extensions.ObservationCapabilities) == 0 || len(extensions.OracleCapabilities) == 0 {
		return errors.New("EXPERIMENT_AGENT_TARGET_EXTENSIONS_INVALID")
	}
	declared := actionKindSet(manifest.Capabilities.Actions)
	for _, action := range extensions.ComposableActions {
		if !declared[action] {
			return errors.New("EXPERIMENT_AGENT_TARGET_ACTION_NOT_DECLARED")
		}
	}
	if validateComposableFaultAllowance(extensions.ComposableActions, faultAllowance) != nil {
		return errors.New("EXPERIMENT_AGENT_TARGET_ACTION_NOT_COMPOSABLE")
	}
	capabilities := AgentCapabilitySurface{
		DeclaredActions: append([]control.ActionKind(nil), manifest.Capabilities.Actions...),
		ComposableActions: append(
			[]control.ActionKind(nil), extensions.ComposableActions...,
		),
		ObservationCapabilities: cloneObservationCapabilities(extensions.ObservationCapabilities),
		OracleCapabilities:      cloneAgentOracleCapabilities(extensions.OracleCapabilities),
		FidelityBoundaries:      cloneAgentFidelityBoundaries(extensions.FidelityBoundaries),
	}
	sortAgentCapabilitySurface(&capabilities)
	return validateAgentCapabilitySurface(capabilities)
}

func validateComposableFaultAllowance(
	actions []control.ActionKind,
	allowance FaultEnvelope,
) error {
	for _, action := range actions {
		unsupported := false
		switch action {
		case control.ActionCrash, control.ActionRestart:
			unsupported = allowance.MaxCrashes == 0
		case control.ActionDropMessage:
			unsupported = allowance.MaxMessageDrops == 0
		case control.ActionDuplicateMessage:
			unsupported = allowance.MaxMessageDuplicates == 0
		case control.ActionPartition, control.ActionHeal:
			unsupported = allowance.MaxPartitions == 0
		}
		if unsupported {
			return errors.New("EXPERIMENT_AGENT_TARGET_ACTION_FAULT_ALLOWANCE_ZERO")
		}
	}
	return nil
}

func validateAgentCapabilitySurface(surface AgentCapabilitySurface) error {
	if !canonicalActionKinds(surface.DeclaredActions, true) ||
		!canonicalActionKinds(surface.ComposableActions, true) ||
		!validAgentMessageCapability(surface.Message) ||
		!validAgentHostEffectCapabilities(surface.HostEffects) ||
		semantic.ValidateObservationCapabilities(surface.ObservationCapabilities) != nil ||
		len(surface.ObservationCapabilities) == 0 || len(surface.OracleCapabilities) == 0 {
		return errors.New("EXPERIMENT_AGENT_TARGET_CAPABILITIES_INVALID")
	}
	declared := actionKindSet(surface.DeclaredActions)
	for _, action := range surface.ComposableActions {
		if !declared[action] {
			return errors.New("EXPERIMENT_AGENT_TARGET_CAPABILITIES_INVALID")
		}
	}
	for _, capability := range surface.HostEffects {
		if len(capability.AllowedResults) > 0 && !declared[control.ActionCompleteEffect] ||
			len(capability.AllowedFailures) > 0 && !declared[control.ActionFailEffect] {
			return errors.New("EXPERIMENT_AGENT_TARGET_EFFECT_ACTION_INVALID")
		}
	}
	for index, oracle := range surface.OracleCapabilities {
		if !validMethodToken(oracle.ID) ||
			(oracle.Scope != AgentOracleScopeGeneric && oracle.Scope != AgentOracleScopeTarget) ||
			!canonicalStrings(oracle.PropertyIDs, false) ||
			(index > 0 && surface.OracleCapabilities[index-1].ID >= oracle.ID) {
			return errors.New("EXPERIMENT_AGENT_TARGET_ORACLE_INVALID")
		}
	}
	for index, boundary := range surface.FidelityBoundaries {
		if !validMethodToken(boundary.ID) || strings.TrimSpace(boundary.Summary) != boundary.Summary ||
			boundary.Summary == "" || len(boundary.Summary) > protocolKnowledgeTextMaxBytes ||
			!canonicalStrings(boundary.AffectedPropertyIDs, false) ||
			(index > 0 && surface.FidelityBoundaries[index-1].ID >= boundary.ID) {
			return errors.New("EXPERIMENT_AGENT_TARGET_FIDELITY_INVALID")
		}
	}
	return nil
}

func sortAgentCapabilitySurface(surface *AgentCapabilitySurface) {
	sort.Slice(surface.DeclaredActions, func(i, j int) bool {
		return surface.DeclaredActions[i] < surface.DeclaredActions[j]
	})
	sort.Slice(surface.ComposableActions, func(i, j int) bool {
		return surface.ComposableActions[i] < surface.ComposableActions[j]
	})
	if surface.Message != nil {
		sort.Strings(surface.Message.TypeHints)
		sort.Strings(surface.Message.MetadataKeys)
	}
	for index := range surface.HostEffects {
		capability := &surface.HostEffects[index]
		sort.Strings(capability.Phases)
		sort.Slice(capability.Durabilities, func(i, j int) bool {
			return capability.Durabilities[i] < capability.Durabilities[j]
		})
		sort.Strings(capability.AllowedResults)
		sort.Strings(capability.AllowedFailures)
	}
	sort.Slice(surface.HostEffects, func(i, j int) bool {
		return surface.HostEffects[i].Kind < surface.HostEffects[j].Kind
	})
	sort.Slice(surface.ObservationCapabilities, func(i, j int) bool {
		return surface.ObservationCapabilities[i].Kind < surface.ObservationCapabilities[j].Kind
	})
	for index := range surface.ObservationCapabilities {
		sort.Slice(surface.ObservationCapabilities[index].Fields, func(i, j int) bool {
			return surface.ObservationCapabilities[index].Fields[i] <
				surface.ObservationCapabilities[index].Fields[j]
		})
	}
	sort.Slice(surface.OracleCapabilities, func(i, j int) bool {
		return surface.OracleCapabilities[i].ID < surface.OracleCapabilities[j].ID
	})
	for index := range surface.OracleCapabilities {
		sort.Strings(surface.OracleCapabilities[index].PropertyIDs)
	}
	sort.Slice(surface.FidelityBoundaries, func(i, j int) bool {
		return surface.FidelityBoundaries[i].ID < surface.FidelityBoundaries[j].ID
	})
	for index := range surface.FidelityBoundaries {
		sort.Strings(surface.FidelityBoundaries[index].AffectedPropertyIDs)
	}
}

func validAgentMessageCapability(capability *control.MessageCapability) bool {
	return capability == nil ||
		canonicalStrings(capability.TypeHints, false) && canonicalStrings(capability.MetadataKeys, false)
}

func validAgentHostEffectCapabilities(capabilities []control.HostEffectCapability) bool {
	for index, capability := range capabilities {
		if capability.Kind == "" || index > 0 && capabilities[index-1].Kind >= capability.Kind ||
			!canonicalStrings(capability.Phases, false) || len(capability.Durabilities) == 0 ||
			!canonicalStrings(capability.AllowedResults, false) ||
			!canonicalStrings(capability.AllowedFailures, false) ||
			len(capability.AllowedResults)+len(capability.AllowedFailures) == 0 {
			return false
		}
		for durabilityIndex, durability := range capability.Durabilities {
			if durability != control.DurabilityVolatile && durability != control.DurabilityVisible &&
				durability != control.DurabilityDurable && durability != control.DurabilityApplied ||
				durabilityIndex > 0 && capability.Durabilities[durabilityIndex-1] >= durability {
				return false
			}
		}
	}
	return true
}

func cloneMessageCapability(capability *control.MessageCapability) *control.MessageCapability {
	if capability == nil {
		return nil
	}
	result := *capability
	result.TypeHints = append([]string(nil), capability.TypeHints...)
	result.MetadataKeys = append([]string(nil), capability.MetadataKeys...)
	return &result
}

func cloneHostEffectCapabilities(values []control.HostEffectCapability) []control.HostEffectCapability {
	result := append([]control.HostEffectCapability(nil), values...)
	for index := range result {
		result[index].Phases = append([]string(nil), values[index].Phases...)
		result[index].Durabilities = append([]control.DurabilityClass(nil), values[index].Durabilities...)
		result[index].AllowedResults = append([]string(nil), values[index].AllowedResults...)
		result[index].AllowedFailures = append([]string(nil), values[index].AllowedFailures...)
	}
	return result
}

func cloneAgentOracleCapabilities(values []AgentOracleCapability) []AgentOracleCapability {
	result := append([]AgentOracleCapability(nil), values...)
	for index := range result {
		result[index].PropertyIDs = append([]string(nil), values[index].PropertyIDs...)
	}
	return result
}

func cloneAgentFidelityBoundaries(values []AgentFidelityBoundary) []AgentFidelityBoundary {
	result := append([]AgentFidelityBoundary(nil), values...)
	for index := range result {
		result[index].AffectedPropertyIDs = append(
			[]string(nil), values[index].AffectedPropertyIDs...,
		)
	}
	return result
}
