// Package autoonboard validates an agent-produced implementation binding
// against a deterministic protocol contract. The agent proposes mappings and
// executable witnesses; only mechanical checks can accept them.
package autoonboard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
)

const Version = 1

type Binding struct {
	Version        int                 `json:"version"`
	ID             string              `json:"id"`
	ContractID     string              `json:"contract_id"`
	ContractDigest string              `json:"contract_digest"`
	Protocol       string              `json:"protocol"`
	Driver         string              `json:"driver"`
	Capabilities   []CapabilityBinding `json:"capabilities"`
	Operations     []OperationBinding  `json:"operations"`
	Witnesses      []Witness           `json:"witnesses"`
}

type CapabilityBinding struct {
	ContractID string `json:"contract_id"`
	RuntimeID  string `json:"runtime_id"`
}

type OperationBinding struct {
	OperationID string         `json:"operation_id"`
	RuntimeKind core.EventKind `json:"runtime_kind"`
}

// Witness covers IDs from the immutable contract. Assertions are deliberately
// absent: expected labels, monitors, replay, and conformance are contract- and
// validator-owned, so an agent cannot weaken acceptance.
type Witness struct {
	ID                    string   `json:"id"`
	Scenario              string   `json:"scenario"`
	Covers                []string `json:"covers,omitempty"`
	ValidatesCapabilities []string `json:"validates_capabilities,omitempty"`
}

func Load(path string) (*Binding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var binding Binding
	if err := decoder.Decode(&binding); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values in %s", path)
		}
		return nil, err
	}
	return &binding, nil
}
