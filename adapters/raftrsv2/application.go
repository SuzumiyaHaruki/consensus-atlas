package raftrsv2

import (
	"encoding/json"
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

// Input remains opaque to the Runtime and raft-rs worker. Only this Adapter
// interprets its request identity and value.
type Input struct {
	RequestID string `json:"request_id"`
	Value     []byte `json:"value"`
}

func InputPayload(input Input) (control.PayloadEnvelope, error) {
	if err := input.validate(); err != nil {
		return control.PayloadEnvelope{}, err
	}
	return control.NewJSONPayload(inputSchema, input)
}

func (input Input) validate() error {
	if input.RequestID == "" {
		return errors.New("RAFT_RS_INPUT_REQUEST_ID_REQUIRED")
	}
	return nil
}

func decodeInput(payload control.PayloadEnvelope) (Input, error) {
	if err := payload.Validate(); err != nil {
		return Input{}, err
	}
	if payload.SchemaVersion != inputSchema || payload.Encoding != "json" {
		return Input{}, errors.New("RAFT_RS_INPUT_SCHEMA_MISMATCH")
	}
	var input Input
	if err := json.Unmarshal(payload.Bytes, &input); err != nil {
		return Input{}, err
	}
	return input, input.validate()
}

type proposalEntry struct {
	SchemaVersion string          `json:"schema_version"`
	RequestID     string          `json:"request_id"`
	Origin        control.NodeRef `json:"origin"`
	Value         []byte          `json:"value"`
}

func encodeProposal(input Input, origin control.NodeRef) ([]byte, error) {
	if err := input.validate(); err != nil {
		return nil, err
	}
	if err := origin.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(proposalEntry{
		SchemaVersion: proposalSchema, RequestID: input.RequestID,
		Origin: origin, Value: append([]byte(nil), input.Value...),
	})
}

func decodeProposal(data []byte) (proposalEntry, error) {
	var proposal proposalEntry
	if err := json.Unmarshal(data, &proposal); err != nil {
		return proposalEntry{}, err
	}
	if proposal.SchemaVersion != proposalSchema || proposal.RequestID == "" {
		return proposalEntry{}, errors.New("RAFT_RS_PROPOSAL_IDENTITY_INVALID")
	}
	if err := proposal.Origin.Validate(); err != nil {
		return proposalEntry{}, err
	}
	return proposal, nil
}

type clientResult struct {
	Index uint64 `json:"index"`
	Term  uint64 `json:"term"`
	Value []byte `json:"value"`
}
