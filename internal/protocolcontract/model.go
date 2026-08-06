// Package protocolcontract defines the deterministic protocol knowledge that
// a user supplies once. Agents may bind an implementation to this contract,
// but they cannot change its semantics or coverage denominator during a run.
package protocolcontract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
)

const Version = 1

var categories = []string{"transition", "ordering", "fault", "boundary", "property"}

type Contract struct {
	Version           int               `json:"version"`
	ID                string            `json:"id"`
	Family            string            `json:"family"`
	Protocol          string            `json:"protocol"`
	PSSID             string            `json:"pss_id"`
	FaultModel        string            `json:"fault_model"`
	Nodes             []string          `json:"nodes"`
	InitialMembership InitialMembership `json:"initial_membership"`
	StateDimensions   []string          `json:"state_dimensions"`
	Capabilities      []Capability      `json:"capabilities"`
	Operations        []Operation       `json:"operations"`
	Semantics         Semantics         `json:"semantics"`
	Evaluation        Evaluation        `json:"evaluation"`
}

type InitialMembership struct {
	Voters   []string `json:"voters"`
	Learners []string `json:"learners"`
}

type Capability struct {
	ID            string   `json:"id"`
	Description   string   `json:"description"`
	WitnessLabels []string `json:"witness_labels"`
}

type Operation struct {
	ID          string         `json:"id"`
	EventKind   core.EventKind `json:"event_kind"`
	Description string         `json:"description"`
	Requires    []string       `json:"requires,omitempty"`
}

type Semantics struct {
	Transitions []Transition `json:"transitions"`
	Orderings   []Ordering   `json:"orderings"`
	Faults      []Fault      `json:"faults"`
	Boundaries  []Boundary   `json:"boundaries"`
	Invariants  []Invariant  `json:"invariants"`
}

type Transition struct {
	ID           string   `json:"id"`
	From         string   `json:"from"`
	To           string   `json:"to"`
	Trigger      string   `json:"trigger"`
	WitnessLabel string   `json:"witness_label"`
	Monitor      string   `json:"monitor"`
	Requires     []string `json:"requires,omitempty"`
}

type Ordering struct {
	ID           string   `json:"id"`
	Before       string   `json:"before"`
	After        string   `json:"after"`
	Description  string   `json:"description"`
	WitnessLabel string   `json:"witness_label"`
	Monitor      string   `json:"monitor"`
	Requires     []string `json:"requires,omitempty"`
}

type Fault struct {
	ID           string   `json:"id"`
	Action       string   `json:"action"`
	Description  string   `json:"description"`
	WitnessLabel string   `json:"witness_label"`
	Monitor      string   `json:"monitor"`
	Requires     []string `json:"requires,omitempty"`
}

type Boundary struct {
	ID           string   `json:"id"`
	Description  string   `json:"description"`
	WitnessLabel string   `json:"witness_label"`
	Monitor      string   `json:"monitor"`
	Requires     []string `json:"requires,omitempty"`
}

type Invariant struct {
	ID              string   `json:"id"`
	Description     string   `json:"description"`
	ActivationLabel string   `json:"activation_label"`
	Monitor         string   `json:"monitor"`
	Requires        []string `json:"requires,omitempty"`
}

type Evaluation struct {
	Weights   map[string]float64 `json:"weights"`
	Threshold coverage.Threshold `json:"threshold"`
}

// Obligation is the protocol-neutral compiled view used by onboarding. It is
// derived from Semantics; an agent never supplies or edits this structure.
type Obligation struct {
	ID           string
	Category     string
	WitnessLabel string
	Monitor      string
	Requires     []string
}

func Load(path string) (*Contract, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var contract Contract
	if err := decoder.Decode(&contract); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values in %s", path)
		}
		return nil, err
	}
	return &contract, nil
}

func Digest(contract *Contract) (string, error) {
	if contract == nil {
		return "", errors.New("protocol contract is nil")
	}
	encoded, err := json.Marshal(contract)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (c *Contract) Validate() error {
	if c == nil {
		return errors.New("protocol contract is nil")
	}
	if c.Version != Version {
		return fmt.Errorf("unsupported protocol contract version %d", c.Version)
	}
	if c.ID == "" || c.Family == "" || c.Protocol == "" || c.PSSID == "" {
		return errors.New("contract id, family, protocol, and pss_id are required")
	}
	if c.FaultModel != "cft" && c.FaultModel != "bft-limited" {
		return errors.New("fault_model must be cft or bft-limited")
	}
	if !uniqueNonEmpty(c.Nodes) || len(c.Nodes) == 0 {
		return errors.New("nodes must be non-empty and unique")
	}
	if !uniqueNonEmpty(c.InitialMembership.Voters) || len(c.InitialMembership.Voters) == 0 ||
		!uniqueNonEmpty(c.InitialMembership.Learners) {
		return errors.New("initial_membership requires non-empty unique voters and unique learners")
	}
	members := make(map[string]bool, len(c.Nodes))
	declaredNodes := make(map[string]bool, len(c.Nodes))
	for _, node := range c.Nodes {
		declaredNodes[node] = true
	}
	for _, node := range append(append([]string(nil), c.InitialMembership.Voters...), c.InitialMembership.Learners...) {
		if !declaredNodes[node] || members[node] {
			return fmt.Errorf("initial membership node %q is unknown or assigned more than once", node)
		}
		members[node] = true
	}
	if len(members) != len(c.Nodes) {
		return errors.New("every declared node must have exactly one initial membership role")
	}
	if !uniqueNonEmpty(c.StateDimensions) || len(c.StateDimensions) == 0 {
		return errors.New("state_dimensions must be non-empty and unique")
	}
	capabilities := make(map[string]bool, len(c.Capabilities))
	for index, capability := range c.Capabilities {
		if capability.ID == "" || capability.Description == "" || capabilities[capability.ID] || !uniqueNonEmpty(capability.WitnessLabels) || len(capability.WitnessLabels) == 0 {
			return fmt.Errorf("capabilities[%d] requires a unique id, description, and witness labels", index)
		}
		capabilities[capability.ID] = true
	}
	if len(capabilities) == 0 {
		return errors.New("contract requires at least one capability")
	}
	operations := make(map[string]Operation, len(c.Operations))
	for index, operation := range c.Operations {
		if operation.ID == "" || operation.Description == "" || operations[operation.ID].ID != "" {
			return fmt.Errorf("operations[%d] requires a unique id and description", index)
		}
		if !validEventKind(operation.EventKind) {
			return fmt.Errorf("operations[%d] has invalid event_kind %q", index, operation.EventKind)
		}
		if err := validateRequires(operation.Requires, capabilities); err != nil {
			return fmt.Errorf("operations[%d]: %w", index, err)
		}
		operations[operation.ID] = operation
	}
	if len(operations) == 0 {
		return errors.New("contract requires at least one operation")
	}
	seen := make(map[string]bool)
	obligationCount := 0
	for index, item := range c.Semantics.Transitions {
		if item.ID == "" || item.From == "" || item.To == "" || item.WitnessLabel == "" || item.Monitor == "" || seen[item.ID] {
			return fmt.Errorf("semantics.transitions[%d] is incomplete or duplicate", index)
		}
		if operations[item.Trigger].ID == "" {
			return fmt.Errorf("semantics.transitions[%d] references unknown trigger %q", index, item.Trigger)
		}
		if err := validateRequires(item.Requires, capabilities); err != nil {
			return fmt.Errorf("semantics.transitions[%d]: %w", index, err)
		}
		seen[item.ID], obligationCount = true, obligationCount+1
	}
	for index, item := range c.Semantics.Orderings {
		if item.ID == "" || item.Description == "" || item.WitnessLabel == "" || item.Monitor == "" || seen[item.ID] {
			return fmt.Errorf("semantics.orderings[%d] is incomplete or duplicate", index)
		}
		if operations[item.Before].ID == "" || operations[item.After].ID == "" {
			return fmt.Errorf("semantics.orderings[%d] references unknown operations", index)
		}
		if err := validateRequires(item.Requires, capabilities); err != nil {
			return fmt.Errorf("semantics.orderings[%d]: %w", index, err)
		}
		seen[item.ID], obligationCount = true, obligationCount+1
	}
	for index, item := range c.Semantics.Faults {
		if item.ID == "" || item.Description == "" || item.WitnessLabel == "" || item.Monitor == "" || seen[item.ID] {
			return fmt.Errorf("semantics.faults[%d] is incomplete or duplicate", index)
		}
		if operations[item.Action].ID == "" {
			return fmt.Errorf("semantics.faults[%d] references unknown action %q", index, item.Action)
		}
		if err := validateRequires(item.Requires, capabilities); err != nil {
			return fmt.Errorf("semantics.faults[%d]: %w", index, err)
		}
		seen[item.ID], obligationCount = true, obligationCount+1
	}
	for index, item := range c.Semantics.Boundaries {
		if item.ID == "" || item.Description == "" || item.WitnessLabel == "" || item.Monitor == "" || seen[item.ID] {
			return fmt.Errorf("semantics.boundaries[%d] is incomplete or duplicate", index)
		}
		if err := validateRequires(item.Requires, capabilities); err != nil {
			return fmt.Errorf("semantics.boundaries[%d]: %w", index, err)
		}
		seen[item.ID], obligationCount = true, obligationCount+1
	}
	for index, item := range c.Semantics.Invariants {
		if item.ID == "" || item.Description == "" || item.ActivationLabel == "" || item.Monitor == "" || seen[item.ID] {
			return fmt.Errorf("semantics.invariants[%d] is incomplete or duplicate", index)
		}
		if err := validateRequires(item.Requires, capabilities); err != nil {
			return fmt.Errorf("semantics.invariants[%d]: %w", index, err)
		}
		seen[item.ID], obligationCount = true, obligationCount+1
	}
	if obligationCount == 0 {
		return errors.New("contract requires at least one semantic obligation")
	}
	weightSum := 0.0
	for _, category := range categories {
		weight, ok := c.Evaluation.Weights[category]
		if !ok || weight <= 0 {
			return fmt.Errorf("evaluation requires a positive %s weight", category)
		}
		weightSum += weight
	}
	if len(c.Evaluation.Weights) != len(categories) || math.Abs(weightSum-1) > 0.001 {
		return fmt.Errorf("evaluation weights must contain five categories and sum to 1, got %.4f", weightSum)
	}
	if c.Evaluation.Threshold.Score < 0 || c.Evaluation.Threshold.Score > 100 ||
		c.Evaluation.Threshold.MinCategory < 0 || c.Evaluation.Threshold.MinCategory > 1 {
		return errors.New("evaluation threshold is outside its valid range")
	}
	return nil
}

// Compile deterministically creates the trusted runtime Profile. An
// obligation remains in the denominator even when its required implementation
// capabilities have not been validated; in that case its status is unsupported.
func Compile(c *Contract, validatedCapabilities map[string]bool) (coverage.Profile, error) {
	if err := c.Validate(); err != nil {
		return coverage.Profile{}, err
	}
	profile := coverage.Profile{
		Version: coverage.ProfileVersion, ID: c.ID + "-compiled", Protocol: c.Protocol, PSSID: c.PSSID,
		Nodes:     append([]string(nil), c.Nodes...),
		Coverage:  coverage.CoverageDefinition{Weights: cloneWeights(c.Evaluation.Weights)},
		Threshold: c.Evaluation.Threshold,
	}
	operationKinds := make(map[string]core.EventKind, len(c.Operations))
	for _, operation := range c.Operations {
		operationKinds[operation.ID] = operation.EventKind
	}
	labelEvidence := func(label string) coverage.EvidenceRequirement {
		predicate := coverage.TracePredicate{ObservationLabel: label}
		return coverage.EvidenceRequirement{
			Reach: []coverage.TracePredicate{predicate}, Observe: []coverage.TracePredicate{predicate},
		}
	}
	add := func(id, category, description, monitor string, requires []string, evidence coverage.EvidenceRequirement) {
		status := "supported"
		for _, capability := range requires {
			if !validatedCapabilities[capability] {
				status = "unsupported"
				break
			}
		}
		profile.Coverage.Obligations = append(profile.Coverage.Obligations, coverage.Obligation{
			ID: id, Category: category, Description: description,
			Evidence: evidence,
			Monitors: []string{monitor}, Requires: append([]string(nil), requires...),
			Risk: coverage.RiskStandard, Status: status,
		})
	}
	for _, item := range c.Semantics.Transitions {
		description := fmt.Sprintf("%s transitions from %s to %s", item.Trigger, item.From, item.To)
		predicate := coverage.TracePredicate{
			EventKind: operationKinds[item.Trigger], ObservationLabel: item.WitnessLabel,
		}
		add(item.ID, "transition", description, item.Monitor, item.Requires, coverage.EvidenceRequirement{
			Reach: []coverage.TracePredicate{predicate}, Observe: []coverage.TracePredicate{predicate},
		})
	}
	for _, item := range c.Semantics.Orderings {
		before := coverage.TracePredicate{EventKind: operationKinds[item.Before]}
		after := coverage.TracePredicate{EventKind: operationKinds[item.After]}
		add(item.ID, "ordering", item.Description, item.Monitor, item.Requires, coverage.EvidenceRequirement{
			Reach:     []coverage.TracePredicate{before},
			Observe:   []coverage.TracePredicate{{ObservationLabel: item.WitnessLabel}},
			Orderings: []coverage.OrderingConstraint{{Before: before, After: after}},
		})
	}
	for _, item := range c.Semantics.Faults {
		predicate := coverage.TracePredicate{
			EventKind: operationKinds[item.Action], ObservationLabel: item.WitnessLabel,
		}
		add(item.ID, "fault", item.Description, item.Monitor, item.Requires, coverage.EvidenceRequirement{
			Reach: []coverage.TracePredicate{predicate}, Observe: []coverage.TracePredicate{predicate},
		})
	}
	for _, item := range c.Semantics.Boundaries {
		add(item.ID, "boundary", item.Description, item.Monitor, item.Requires, labelEvidence(item.WitnessLabel))
	}
	for _, item := range c.Semantics.Invariants {
		add(item.ID, "property", item.Description, item.Monitor, item.Requires, labelEvidence(item.ActivationLabel))
	}
	// The capability denominator comes from the contract, not only from the
	// subset currently referenced by an obligation. This prevents a binding
	// from making an integration requirement disappear by leaving it unused.
	for _, capability := range c.Capabilities {
		profile.RequiredCapabilities = append(profile.RequiredCapabilities, capability.ID)
	}
	sort.Strings(profile.RequiredCapabilities)
	if err := profile.Validate(); err != nil {
		return coverage.Profile{}, fmt.Errorf("compiled profile: %w", err)
	}
	return profile, nil
}

func (c *Contract) ObligationRequirements() map[string][]string {
	result := make(map[string][]string)
	for _, item := range c.Semantics.Transitions {
		result[item.ID] = append([]string(nil), item.Requires...)
	}
	for _, item := range c.Semantics.Orderings {
		result[item.ID] = append([]string(nil), item.Requires...)
	}
	for _, item := range c.Semantics.Faults {
		result[item.ID] = append([]string(nil), item.Requires...)
	}
	for _, item := range c.Semantics.Boundaries {
		result[item.ID] = append([]string(nil), item.Requires...)
	}
	for _, item := range c.Semantics.Invariants {
		result[item.ID] = append([]string(nil), item.Requires...)
	}
	return result
}

func (c *Contract) Obligations() []Obligation {
	var result []Obligation
	add := func(id, category, label, monitor string, requires []string) {
		result = append(result, Obligation{
			ID: id, Category: category, WitnessLabel: label, Monitor: monitor,
			Requires: append([]string(nil), requires...),
		})
	}
	for _, item := range c.Semantics.Transitions {
		add(item.ID, "transition", item.WitnessLabel, item.Monitor, item.Requires)
	}
	for _, item := range c.Semantics.Orderings {
		add(item.ID, "ordering", item.WitnessLabel, item.Monitor, item.Requires)
	}
	for _, item := range c.Semantics.Faults {
		add(item.ID, "fault", item.WitnessLabel, item.Monitor, item.Requires)
	}
	for _, item := range c.Semantics.Boundaries {
		add(item.ID, "boundary", item.WitnessLabel, item.Monitor, item.Requires)
	}
	for _, item := range c.Semantics.Invariants {
		add(item.ID, "property", item.ActivationLabel, item.Monitor, item.Requires)
	}
	return result
}

func validateRequires(requires []string, capabilities map[string]bool) error {
	seen := make(map[string]bool, len(requires))
	for _, capability := range requires {
		if capability == "" || seen[capability] || !capabilities[capability] {
			return fmt.Errorf("requires contains an empty, duplicate, or unknown capability %q", capability)
		}
		seen[capability] = true
	}
	return nil
}

func uniqueNonEmpty(values []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func validEventKind(kind core.EventKind) bool {
	switch kind {
	case core.EventStart, core.EventCampaign, core.EventPropose, core.EventQuery, core.EventMessage, core.EventTimeout,
		core.EventPersist, core.EventSync, core.EventEmit, core.EventApply, core.EventAcknowledge,
		core.EventCrash, core.EventRestart, core.EventDuplicate, core.EventPartition, core.EventHeal,
		core.EventClockAdvance:
		return true
	default:
		return false
	}
}

func cloneWeights(weights map[string]float64) map[string]float64 {
	result := make(map[string]float64, len(weights))
	for key, value := range weights {
		result[key] = value
	}
	return result
}
