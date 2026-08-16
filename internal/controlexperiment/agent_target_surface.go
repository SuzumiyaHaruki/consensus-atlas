package controlexperiment

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
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
}

func NewAgentTargetSurface(
	targetID string,
	manifest control.AdapterManifest,
	workload WorkloadPlan,
	runtime RuntimeConfig,
	faultAllowance FaultEnvelope,
) (AgentTargetSurface, error) {
	if !validMethodToken(targetID) || manifest.Validate() != nil || workload.Validate() != nil ||
		faultAllowance.Validate() != nil {
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
	}
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
	return nil
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
	cloned.Workload.Invocations = append([]AgentWorkloadInvocation(nil), surface.Workload.Invocations...)
	for index := range cloned.Workload.Invocations {
		cloned.Workload.Invocations[index].InputJSON = append(
			json.RawMessage(nil), surface.Workload.Invocations[index].InputJSON...,
		)
	}
	return &cloned
}
