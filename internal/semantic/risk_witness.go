package semantic

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"sort"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const (
	RiskWitnessSpecSchemaVersion   = "consensus-atlas/risk-witness-spec/v1"
	RiskWitnessResultSchemaVersion = "consensus-atlas/risk-witness-result/v1"

	RiskWitnessReached    = "reached"
	RiskWitnessNotReached = "not-reached"

	RiskWitnessReasonMilestoneMissing = "milestone-missing"
	RiskWitnessReasonOrderUnsatisfied = "milestone-order-unsatisfied"
)

// RiskWitnessMilestone is a family-owned semantic event name. It deliberately
// contains no target event selector or open temporal expression.
type RiskWitnessMilestone struct {
	ID string `json:"id"`
}

// RiskWitnessOrder is one frozen edge in the family-owned milestone DAG.
type RiskWitnessOrder struct {
	Before string `json:"before"`
	After  string `json:"after"`
}

// RiskWitnessSpec is the trusted family definition of a protocol risk. A
// target projector may only attach evidence to these frozen milestone IDs.
type RiskWitnessSpec struct {
	SchemaVersion string                 `json:"schema_version"`
	ID            string                 `json:"id"`
	FamilyID      string                 `json:"family_id"`
	RiskID        string                 `json:"risk_id"`
	Milestones    []RiskWitnessMilestone `json:"milestones"`
	RequiredOrder []RiskWitnessOrder     `json:"required_order"`
	Digest        string                 `json:"digest"`
}

func NewRiskWitnessSpec(
	id string,
	familyID string,
	riskID string,
	milestoneIDs []string,
	requiredOrder []RiskWitnessOrder,
) (RiskWitnessSpec, error) {
	spec := RiskWitnessSpec{
		SchemaVersion: RiskWitnessSpecSchemaVersion,
		ID:            id,
		FamilyID:      familyID,
		RiskID:        riskID,
		Milestones:    make([]RiskWitnessMilestone, len(milestoneIDs)),
		RequiredOrder: append([]RiskWitnessOrder(nil), requiredOrder...),
	}
	for index, milestoneID := range milestoneIDs {
		spec.Milestones[index] = RiskWitnessMilestone{ID: milestoneID}
	}
	sort.Slice(spec.RequiredOrder, func(i, j int) bool {
		if spec.RequiredOrder[i].Before != spec.RequiredOrder[j].Before {
			return spec.RequiredOrder[i].Before < spec.RequiredOrder[j].Before
		}
		return spec.RequiredOrder[i].After < spec.RequiredOrder[j].After
	})
	sealed, err := spec.seal()
	if err != nil {
		return RiskWitnessSpec{}, err
	}
	if err := sealed.Validate(); err != nil {
		return RiskWitnessSpec{}, err
	}
	return sealed, nil
}

func (spec RiskWitnessSpec) Validate() error {
	if spec.SchemaVersion != RiskWitnessSpecSchemaVersion ||
		!validRiskWitnessToken(spec.ID) || !validRiskWitnessToken(spec.FamilyID) ||
		!validRiskWitnessToken(spec.RiskID) || len(spec.Milestones) == 0 {
		return errors.New("RISK_WITNESS_SPEC_IDENTITY_INVALID")
	}
	milestones := make(map[string]bool, len(spec.Milestones))
	for _, milestone := range spec.Milestones {
		if !validRiskWitnessToken(milestone.ID) || milestones[milestone.ID] {
			return errors.New("RISK_WITNESS_SPEC_MILESTONE_INVALID")
		}
		milestones[milestone.ID] = true
	}
	edges := make(map[RiskWitnessOrder]bool, len(spec.RequiredOrder))
	for index, edge := range spec.RequiredOrder {
		if !milestones[edge.Before] || !milestones[edge.After] || edge.Before == edge.After || edges[edge] {
			return errors.New("RISK_WITNESS_SPEC_ORDER_INVALID")
		}
		if index > 0 {
			previous := spec.RequiredOrder[index-1]
			if previous.Before > edge.Before ||
				(previous.Before == edge.Before && previous.After > edge.After) {
				return errors.New("RISK_WITNESS_SPEC_ORDER_NOT_CANONICAL")
			}
		}
		edges[edge] = true
	}
	if riskWitnessOrderHasCycle(spec.Milestones, spec.RequiredOrder) {
		return errors.New("RISK_WITNESS_SPEC_ORDER_CYCLE")
	}
	sealed, err := spec.seal()
	if err != nil || !validRiskWitnessSHA256(spec.Digest) || sealed.Digest != spec.Digest {
		return errors.New("RISK_WITNESS_SPEC_DIGEST_MISMATCH")
	}
	return nil
}

func (spec RiskWitnessSpec) seal() (RiskWitnessSpec, error) {
	spec.Milestones = append([]RiskWitnessMilestone(nil), spec.Milestones...)
	spec.RequiredOrder = append([]RiskWitnessOrder(nil), spec.RequiredOrder...)
	spec.Digest = ""
	digest, err := control.CanonicalDigest(spec)
	if err != nil {
		return RiskWitnessSpec{}, err
	}
	spec.Digest = digest
	return spec, nil
}

// RiskWitnessMilestoneEvidence is a target-owned, trace-bound projection. The
// generic validator checks identity and order but never interprets Kind.
type RiskWitnessMilestoneEvidence struct {
	MilestoneID        string           `json:"milestone_id"`
	Step               uint64           `json:"step"`
	Kind               string           `json:"kind"`
	EvidenceDigest     string           `json:"evidence_digest"`
	Participant        *control.NodeRef `json:"participant,omitempty"`
	RelatedParticipant *control.NodeRef `json:"related_participant,omitempty"`
}

func (evidence RiskWitnessMilestoneEvidence) validate() error {
	if !validRiskWitnessToken(evidence.MilestoneID) || evidence.Step == 0 ||
		!validRiskWitnessToken(evidence.Kind) || !validRiskWitnessSHA256(evidence.EvidenceDigest) {
		return errors.New("RISK_WITNESS_MILESTONE_EVIDENCE_INVALID")
	}
	if evidence.Participant != nil {
		if err := evidence.Participant.Validate(); err != nil {
			return err
		}
	}
	if evidence.RelatedParticipant != nil {
		if err := evidence.RelatedParticipant.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// RiskWitnessResult binds one frozen family spec to one trusted execution.
// Status, satisfied/missing milestones, and order violations are recomputed by
// trusted generic code; they are not accepted from a target or Agent.
type RiskWitnessResult struct {
	SchemaVersion        string                         `json:"schema_version"`
	ID                   string                         `json:"id"`
	SpecDigest           string                         `json:"spec_digest"`
	RiskID               string                         `json:"risk_id"`
	TargetIdentityDigest string                         `json:"target_identity_digest"`
	ExecutionDigest      string                         `json:"execution_digest"`
	ProjectorID          string                         `json:"projector_id"`
	Milestones           []RiskWitnessMilestoneEvidence `json:"milestones"`
	SatisfiedMilestones  []string                       `json:"satisfied_milestones"`
	MissingMilestones    []string                       `json:"missing_milestones"`
	OrderViolations      []RiskWitnessOrder             `json:"order_violations"`
	Status               string                         `json:"status"`
	ReasonCode           string                         `json:"reason_code,omitempty"`
	Digest               string                         `json:"digest"`
}

func NewRiskWitnessResult(
	id string,
	spec RiskWitnessSpec,
	targetIdentityDigest string,
	executionDigest string,
	projectorID string,
	milestones []RiskWitnessMilestoneEvidence,
) (RiskWitnessResult, error) {
	if err := spec.Validate(); err != nil {
		return RiskWitnessResult{}, err
	}
	if !validRiskWitnessToken(id) || !validRiskWitnessSHA256(targetIdentityDigest) ||
		!validRiskWitnessSHA256(executionDigest) || projectorID == "" || strings.TrimSpace(projectorID) != projectorID {
		return RiskWitnessResult{}, errors.New("RISK_WITNESS_RESULT_IDENTITY_INVALID")
	}
	normalized, err := normalizeRiskWitnessEvidence(spec, milestones)
	if err != nil {
		return RiskWitnessResult{}, err
	}
	satisfied, missing, violations := classifyRiskWitness(spec, normalized)
	result := RiskWitnessResult{
		SchemaVersion: RiskWitnessResultSchemaVersion,
		ID:            id, SpecDigest: spec.Digest, RiskID: spec.RiskID,
		TargetIdentityDigest: targetIdentityDigest, ExecutionDigest: executionDigest,
		ProjectorID: projectorID, Milestones: normalized,
		SatisfiedMilestones: satisfied, MissingMilestones: missing,
		OrderViolations: violations, Status: RiskWitnessReached,
	}
	if len(satisfied) != len(spec.Milestones) {
		result.Status = RiskWitnessNotReached
		if len(missing) > 0 {
			result.ReasonCode = RiskWitnessReasonMilestoneMissing
		} else {
			result.ReasonCode = RiskWitnessReasonOrderUnsatisfied
		}
	}
	sealed, err := result.seal()
	if err != nil {
		return RiskWitnessResult{}, err
	}
	if err := sealed.Validate(spec); err != nil {
		return RiskWitnessResult{}, err
	}
	return sealed, nil
}

func (result RiskWitnessResult) Validate(spec RiskWitnessSpec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	if result.SchemaVersion != RiskWitnessResultSchemaVersion || !validRiskWitnessToken(result.ID) ||
		result.SpecDigest != spec.Digest || result.RiskID != spec.RiskID ||
		!validRiskWitnessSHA256(result.TargetIdentityDigest) ||
		!validRiskWitnessSHA256(result.ExecutionDigest) || result.ProjectorID == "" ||
		strings.TrimSpace(result.ProjectorID) != result.ProjectorID {
		return errors.New("RISK_WITNESS_RESULT_IDENTITY_INVALID")
	}
	normalized, err := normalizeRiskWitnessEvidence(spec, result.Milestones)
	if err != nil || !reflect.DeepEqual(normalized, result.Milestones) {
		return errors.New("RISK_WITNESS_RESULT_EVIDENCE_INVALID")
	}
	satisfied, missing, violations := classifyRiskWitness(spec, normalized)
	if !reflect.DeepEqual(satisfied, result.SatisfiedMilestones) ||
		!reflect.DeepEqual(missing, result.MissingMilestones) ||
		!reflect.DeepEqual(violations, result.OrderViolations) {
		return errors.New("RISK_WITNESS_RESULT_CLASSIFICATION_MISMATCH")
	}
	wantStatus, wantReason := RiskWitnessReached, ""
	if len(satisfied) != len(spec.Milestones) {
		wantStatus = RiskWitnessNotReached
		if len(missing) > 0 {
			wantReason = RiskWitnessReasonMilestoneMissing
		} else {
			wantReason = RiskWitnessReasonOrderUnsatisfied
		}
	}
	if result.Status != wantStatus || result.ReasonCode != wantReason {
		return errors.New("RISK_WITNESS_RESULT_STATUS_MISMATCH")
	}
	sealed, err := result.seal()
	if err != nil || !validRiskWitnessSHA256(result.Digest) || sealed.Digest != result.Digest {
		return errors.New("RISK_WITNESS_RESULT_DIGEST_MISMATCH")
	}
	return nil
}

func (result RiskWitnessResult) seal() (RiskWitnessResult, error) {
	result.Milestones = cloneRiskWitnessEvidence(result.Milestones)
	result.SatisfiedMilestones = cloneRiskWitnessStrings(result.SatisfiedMilestones)
	result.MissingMilestones = cloneRiskWitnessStrings(result.MissingMilestones)
	result.OrderViolations = cloneRiskWitnessOrder(result.OrderViolations)
	result.Digest = ""
	digest, err := control.CanonicalDigest(result)
	if err != nil {
		return RiskWitnessResult{}, err
	}
	result.Digest = digest
	return result, nil
}

func normalizeRiskWitnessEvidence(
	spec RiskWitnessSpec,
	values []RiskWitnessMilestoneEvidence,
) ([]RiskWitnessMilestoneEvidence, error) {
	positions := make(map[string]int, len(spec.Milestones))
	for index, milestone := range spec.Milestones {
		positions[milestone.ID] = index
	}
	result := cloneRiskWitnessEvidence(values)
	seen := make(map[string]bool, len(result))
	for _, evidence := range result {
		if err := evidence.validate(); err != nil {
			return nil, err
		}
		if _, ok := positions[evidence.MilestoneID]; !ok || seen[evidence.MilestoneID] {
			return nil, errors.New("RISK_WITNESS_RESULT_MILESTONE_UNKNOWN_OR_DUPLICATE")
		}
		seen[evidence.MilestoneID] = true
	}
	sort.Slice(result, func(i, j int) bool {
		return positions[result[i].MilestoneID] < positions[result[j].MilestoneID]
	})
	if result == nil {
		result = make([]RiskWitnessMilestoneEvidence, 0)
	}
	return result, nil
}

func classifyRiskWitness(
	spec RiskWitnessSpec,
	values []RiskWitnessMilestoneEvidence,
) ([]string, []string, []RiskWitnessOrder) {
	observed := make(map[string]RiskWitnessMilestoneEvidence, len(values))
	for _, evidence := range values {
		observed[evidence.MilestoneID] = evidence
	}
	missing := make([]string, 0)
	for _, milestone := range spec.Milestones {
		if _, ok := observed[milestone.ID]; !ok {
			missing = append(missing, milestone.ID)
		}
	}
	violations := make([]RiskWitnessOrder, 0)
	predecessors := make(map[string][]string, len(spec.Milestones))
	for _, edge := range spec.RequiredOrder {
		before, beforeOK := observed[edge.Before]
		after, afterOK := observed[edge.After]
		if beforeOK && afterOK && before.Step >= after.Step {
			violations = append(violations, edge)
		}
		predecessors[edge.After] = append(predecessors[edge.After], edge.Before)
	}
	satisfiedSet := make(map[string]bool, len(values))
	for changed := true; changed; {
		changed = false
		for _, milestone := range spec.Milestones {
			if satisfiedSet[milestone.ID] {
				continue
			}
			current, ok := observed[milestone.ID]
			if !ok {
				continue
			}
			ready := true
			for _, predecessorID := range predecessors[milestone.ID] {
				predecessor, predecessorObserved := observed[predecessorID]
				if !predecessorObserved || !satisfiedSet[predecessorID] || predecessor.Step >= current.Step {
					ready = false
					break
				}
			}
			if ready {
				satisfiedSet[milestone.ID] = true
				changed = true
			}
		}
	}
	satisfied := make([]string, 0, len(satisfiedSet))
	for _, milestone := range spec.Milestones {
		if satisfiedSet[milestone.ID] {
			satisfied = append(satisfied, milestone.ID)
		}
	}
	if missing == nil {
		missing = make([]string, 0)
	}
	if violations == nil {
		violations = make([]RiskWitnessOrder, 0)
	}
	return satisfied, missing, violations
}

func cloneRiskWitnessEvidence(values []RiskWitnessMilestoneEvidence) []RiskWitnessMilestoneEvidence {
	result := make([]RiskWitnessMilestoneEvidence, len(values))
	copy(result, values)
	for index := range result {
		if values[index].Participant != nil {
			participant := *values[index].Participant
			result[index].Participant = &participant
		}
		if values[index].RelatedParticipant != nil {
			related := *values[index].RelatedParticipant
			result[index].RelatedParticipant = &related
		}
	}
	return result
}

func cloneRiskWitnessStrings(values []string) []string {
	result := make([]string, len(values))
	copy(result, values)
	return result
}

func cloneRiskWitnessOrder(values []RiskWitnessOrder) []RiskWitnessOrder {
	result := make([]RiskWitnessOrder, len(values))
	copy(result, values)
	return result
}

func riskWitnessOrderHasCycle(
	milestones []RiskWitnessMilestone,
	edges []RiskWitnessOrder,
) bool {
	indegree := make(map[string]int, len(milestones))
	children := make(map[string][]string, len(milestones))
	for _, milestone := range milestones {
		indegree[milestone.ID] = 0
	}
	for _, edge := range edges {
		indegree[edge.After]++
		children[edge.Before] = append(children[edge.Before], edge.After)
	}
	queue := make([]string, 0, len(milestones))
	for _, milestone := range milestones {
		if indegree[milestone.ID] == 0 {
			queue = append(queue, milestone.ID)
		}
	}
	visited := 0
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		visited++
		for _, child := range children[current] {
			indegree[child]--
			if indegree[child] == 0 {
				queue = append(queue, child)
			}
		}
	}
	return visited != len(milestones)
}

func validRiskWitnessToken(value string) bool {
	if value == "" || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, current := range value {
		if (current >= 'a' && current <= 'z') || (current >= '0' && current <= '9') || current == '-' {
			continue
		}
		return false
	}
	return true
}

func validRiskWitnessSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}
