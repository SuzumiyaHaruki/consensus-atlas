package etcdraftv2

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const DecisionProjectionID = "official-etcdraft-v2/applied-prefix-digest-v1"

const ReadyAdvancedObservationKind = "ready-advanced"

type DecisionProjector struct{}

func (DecisionProjector) ID() string { return DecisionProjectionID }

// Project maps a node's exact applied log frontier and cumulative application
// digest into the generic Agreement input. The generic monitor never imports
// Raft types or decodes this Adapter's Evidence schema.
func (DecisionProjector) Project(envelope control.EvidenceEnvelope) ([]semantic.DecisionObservation, error) {
	evidence, err := ProjectEvidence(envelope)
	if err != nil {
		return nil, err
	}
	result := make([]semantic.DecisionObservation, 0, len(evidence.Nodes))
	for _, node := range evidence.Nodes {
		if node.Applied == 0 {
			continue
		}
		observation := semantic.DecisionObservation{
			Participant: node.Node, Position: strconv.FormatUint(node.Applied, 10),
			ValueDigest: node.ApplicationDigest,
		}
		if err := observation.Validate(); err != nil {
			return nil, err
		}
		result = append(result, observation)
	}
	return result, nil
}

// Evidence is the stable Adapter-owned projection available to PSS, Oracle,
// migration, and diagnostics. It intentionally omits native raft structs.
type Evidence struct {
	LogicalTime uint64         `json:"logical_time"`
	Nodes       []NodeEvidence `json:"nodes"`
}

type NodeEvidence struct {
	Node                control.NodeID `json:"node"`
	Incarnation         uint64         `json:"incarnation"`
	Running             bool           `json:"running"`
	Role                string         `json:"role"`
	Term                uint64         `json:"term"`
	Commit              uint64         `json:"commit"`
	Applied             uint64         `json:"applied"`
	ApplicationDigest   string         `json:"application_digest"`
	ApplicationCommands int            `json:"application_commands"`
}

// ProjectEvidence validates the opaque envelope and returns only the public
// semantic projection. Protocol-independent packages should consume an
// interface supplied by the composition root rather than import this Adapter.
func ProjectEvidence(envelope control.EvidenceEnvelope) (Evidence, error) {
	if err := envelope.Payload.Validate(); err != nil {
		return Evidence{}, err
	}
	if envelope.Payload.SchemaVersion != evidenceSchema || envelope.Payload.Encoding != "json" {
		return Evidence{}, fmt.Errorf(
			"ETCDRAFT_V2_EVIDENCE_SCHEMA_MISMATCH: %s/%s",
			envelope.Payload.SchemaVersion, envelope.Payload.Encoding,
		)
	}
	var snapshot clusterSnapshot
	if err := json.Unmarshal(envelope.Payload.Bytes, &snapshot); err != nil {
		return Evidence{}, err
	}
	result := Evidence{LogicalTime: snapshot.LogicalTime}
	for _, node := range snapshot.Nodes {
		if node.Node == "" || node.Incarnation == 0 || node.ApplicationDigest == "" ||
			node.ApplicationCommands < 0 {
			return Evidence{}, errors.New("ETCDRAFT_V2_EVIDENCE_NODE_INVALID")
		}
		result.Nodes = append(result.Nodes, NodeEvidence{
			Node: node.Node, Incarnation: node.Incarnation, Running: node.Running,
			Role: node.Role, Term: node.Term, Commit: node.Commit, Applied: node.Applied,
			ApplicationDigest: node.ApplicationDigest, ApplicationCommands: node.ApplicationCommands,
		})
	}
	sort.Slice(result.Nodes, func(i, j int) bool { return result.Nodes[i].Node < result.Nodes[j].Node })
	for index := range result.Nodes {
		if index > 0 && result.Nodes[index-1].Node == result.Nodes[index].Node {
			return Evidence{}, errors.New("ETCDRAFT_V2_EVIDENCE_NODE_DUPLICATE")
		}
	}
	return result, nil
}

type ClientResult struct {
	RequestID  string `json:"request_id"`
	Status     string `json:"status"`
	Index      uint64 `json:"index,omitempty"`
	Term       uint64 `json:"term,omitempty"`
	Value      []byte `json:"value,omitempty"`
	ReasonCode string `json:"reason_code,omitempty"`
}

// ProjectInput decodes the target-local command carried by a generic Invoke.
// Protocol-independent packages keep the payload opaque.
func ProjectInput(payload control.PayloadEnvelope) (Input, error) {
	input, err := decodeInput(payload)
	if err != nil {
		return Input{}, err
	}
	input.Value = append([]byte(nil), input.Value...)
	return input, nil
}

// AppliedCommandEvidence is emitted only by the ready-advanced observation
// for the yield that actually applied the command. It avoids copying the full
// application history into every state Evidence snapshot.
type AppliedCommandEvidence struct {
	Index     uint64          `json:"index"`
	Term      uint64          `json:"term"`
	RequestID string          `json:"request_id"`
	Origin    control.NodeRef `json:"origin"`
	Value     []byte          `json:"value"`
}

type ReadyAdvancedEvidence struct {
	ReadyID  string                   `json:"value"`
	Commands []AppliedCommandEvidence `json:"commands,omitempty"`
}

func ProjectReadyAdvancedObservation(
	observation control.TypedObservation,
) (ReadyAdvancedEvidence, error) {
	if observation.Kind != ReadyAdvancedObservationKind ||
		observation.Payload.SchemaVersion != observationV1 ||
		observation.Payload.Encoding != "json" {
		return ReadyAdvancedEvidence{}, errors.New("ETCDRAFT_V2_READY_ADVANCED_SCHEMA_MISMATCH")
	}
	if err := observation.Owner.Validate(); err != nil {
		return ReadyAdvancedEvidence{}, err
	}
	var wire readyAdvancedObservation
	if err := json.Unmarshal(observation.Payload.Bytes, &wire); err != nil {
		return ReadyAdvancedEvidence{}, err
	}
	if wire.ReadyID == "" {
		return ReadyAdvancedEvidence{}, errors.New("ETCDRAFT_V2_READY_ADVANCED_ID_REQUIRED")
	}
	result := ReadyAdvancedEvidence{ReadyID: wire.ReadyID}
	lastIndex := uint64(0)
	for _, command := range wire.Commands {
		if command.Index == 0 || command.Term == 0 || command.Index <= lastIndex ||
			command.RequestID == "" {
			return ReadyAdvancedEvidence{}, errors.New("ETCDRAFT_V2_APPLIED_COMMAND_INVALID")
		}
		if err := command.Origin.Validate(); err != nil {
			return ReadyAdvancedEvidence{}, err
		}
		result.Commands = append(result.Commands, AppliedCommandEvidence{
			Index: command.Index, Term: command.Term, RequestID: command.RequestID,
			Origin: command.Origin, Value: append([]byte(nil), command.Value...),
		})
		lastIndex = command.Index
	}
	return result, nil
}

// ProjectClientResult decodes only result schemas declared by this Adapter.
func ProjectClientResult(response control.ClientResponse) (ClientResult, error) {
	if response.RequestID == "" || response.Status == "" {
		return ClientResult{}, errors.New("ETCDRAFT_V2_CLIENT_RESULT_IDENTITY_INVALID")
	}
	if err := response.Payload.Validate(); err != nil {
		return ClientResult{}, err
	}
	result := ClientResult{RequestID: response.RequestID, Status: response.Status}
	switch response.Status {
	case "committed":
		if response.Payload.SchemaVersion != clientResultSchema || response.Payload.Encoding != "json" {
			return ClientResult{}, errors.New("ETCDRAFT_V2_CLIENT_RESULT_SCHEMA_MISMATCH")
		}
		var committed clientResult
		if err := json.Unmarshal(response.Payload.Bytes, &committed); err != nil {
			return ClientResult{}, err
		}
		if committed.Index == 0 || committed.Term == 0 {
			return ClientResult{}, errors.New("ETCDRAFT_V2_CLIENT_RESULT_POSITION_INVALID")
		}
		result.Index = committed.Index
		result.Term = committed.Term
		result.Value = append([]byte(nil), committed.Value...)
	case "rejected":
		if response.Payload.SchemaVersion != clientRejectionSchema || response.Payload.Encoding != "json" {
			return ClientResult{}, errors.New("ETCDRAFT_V2_CLIENT_REJECTION_SCHEMA_MISMATCH")
		}
		var rejected clientRejection
		if err := json.Unmarshal(response.Payload.Bytes, &rejected); err != nil {
			return ClientResult{}, err
		}
		if rejected.ReasonCode == "" {
			return ClientResult{}, errors.New("ETCDRAFT_V2_CLIENT_REJECTION_REASON_REQUIRED")
		}
		result.ReasonCode = rejected.ReasonCode
	default:
		return ClientResult{}, fmt.Errorf("ETCDRAFT_V2_CLIENT_RESULT_STATUS_UNSUPPORTED: %s", response.Status)
	}
	return result, nil
}
