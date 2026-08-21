package controlruntime

import (
	"fmt"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const TraceSchemaVersion = "consensus-atlas/control-trace/v2alpha1"

type ClockAdvance struct {
	From     uint64             `json:"from"`
	To       uint64             `json:"to"`
	Reason   string             `json:"reason"`
	Temporal control.TemporalID `json:"temporal_id"`
}

type ItemTransition struct {
	Item   control.ItemID    `json:"item_id"`
	Before control.ItemState `json:"before"`
	After  control.ItemState `json:"after"`
}

type NodeTransition struct {
	Node   control.NodeID `json:"node"`
	Before NodeSnapshot   `json:"before"`
	After  NodeSnapshot   `json:"after"`
}

type ActionRecord struct {
	Step              uint64                        `json:"step"`
	LogicalTime       uint64                        `json:"logical_time"`
	EnabledSetDigest  string                        `json:"enabled_set_digest"`
	Action            control.Action                `json:"action"`
	Command           *control.AdapterCommand       `json:"command,omitempty"`
	Yield             *control.Yield                `json:"yield,omitempty"`
	EmissionDigest    string                        `json:"emission_digest,omitempty"`
	EvidenceDigest    string                        `json:"evidence_digest,omitempty"`
	EntropyTapeDigest string                        `json:"entropy_tape_digest,omitempty"`
	Evidence          *control.EvidenceEnvelope     `json:"evidence,omitempty"`
	Entropy           *control.EntropyAuditEnvelope `json:"entropy,omitempty"`
	ClockAdvance      *ClockAdvance                 `json:"clock_advance,omitempty"`
	ItemTransitions   []ItemTransition              `json:"item_transitions,omitempty"`
	NodeTransitions   []NodeTransition              `json:"node_transitions,omitempty"`
	BeforeStateDigest string                        `json:"before_state_digest"`
	AfterStateDigest  string                        `json:"after_state_digest"`
	Outcome           string                        `json:"outcome"`
}

type Trace struct {
	SchemaVersion         string                       `json:"schema_version"`
	ManifestDigest        string                       `json:"manifest_digest"`
	SeedDigest            string                       `json:"seed_digest"`
	InitialYield          control.Yield                `json:"initial_yield"`
	InitialEmissionDigest string                       `json:"initial_emission_digest"`
	InitialEvidenceDigest string                       `json:"initial_evidence_digest"`
	InitialEntropyDigest  string                       `json:"initial_entropy_digest"`
	InitialEvidence       control.EvidenceEnvelope     `json:"initial_evidence"`
	InitialEntropy        control.EntropyAuditEnvelope `json:"initial_entropy"`
	InitialStateDigest    string                       `json:"initial_state_digest"`
	Records               []ActionRecord               `json:"records"`
	FinalStateDigest      string                       `json:"final_state_digest"`
	Digest                string                       `json:"digest"`
}

func (trace Trace) Seal() (Trace, error) {
	trace.SchemaVersion = TraceSchemaVersion
	trace.Digest = ""
	digest, err := control.CanonicalDigest(trace)
	if err != nil {
		return Trace{}, err
	}
	trace.Digest = digest
	return trace, nil
}

func (trace Trace) Validate() error {
	if trace.SchemaVersion != TraceSchemaVersion {
		return fmt.Errorf("TRACE_SCHEMA_MISMATCH: %s", trace.SchemaVersion)
	}
	sealed, err := trace.Seal()
	if err != nil {
		return err
	}
	if sealed.Digest != trace.Digest {
		return fmt.Errorf("TRACE_DIGEST_MISMATCH")
	}
	if trace.ManifestDigest == "" || trace.SeedDigest == "" || trace.InitialStateDigest == "" ||
		trace.FinalStateDigest == "" || trace.InitialEmissionDigest == "" {
		return fmt.Errorf("TRACE_IDENTITY_INCOMPLETE")
	}
	if trace.InitialYield.ID == "" || trace.InitialYield.Kind != control.YieldStable ||
		trace.InitialYield.StateDigest == "" {
		return fmt.Errorf("TRACE_INITIAL_YIELD_INVALID")
	}
	if trace.InitialEvidence.Yield != trace.InitialYield.ID {
		return fmt.Errorf("TRACE_INITIAL_EVIDENCE_YIELD_MISMATCH")
	}
	if err := trace.InitialEvidence.Payload.Validate(); err != nil {
		return fmt.Errorf("TRACE_INITIAL_EVIDENCE_INVALID: %w", err)
	}
	evidenceDigest, err := control.CanonicalDigest(trace.InitialEvidence)
	if err != nil || evidenceDigest != trace.InitialEvidenceDigest {
		return fmt.Errorf("TRACE_INITIAL_EVIDENCE_DIGEST_MISMATCH")
	}
	if trace.InitialEntropy.Yield != trace.InitialYield.ID || trace.InitialEntropy.Algorithm == "" ||
		trace.InitialEntropy.SeedDigest != trace.SeedDigest ||
		trace.InitialEntropy.TapeDigest != trace.InitialEntropyDigest {
		return fmt.Errorf("TRACE_INITIAL_ENTROPY_INVALID")
	}
	if err := trace.InitialEntropy.Tape.Validate(); err != nil {
		return fmt.Errorf("TRACE_INITIAL_ENTROPY_TAPE_INVALID: %w", err)
	}
	logicalTime := uint64(0)
	for index := range trace.Records {
		if err := trace.Records[index].validate(index+1, trace.SeedDigest, logicalTime); err != nil {
			return err
		}
		logicalTime = trace.Records[index].LogicalTime
	}
	wantFinal := trace.InitialStateDigest
	if len(trace.Records) > 0 {
		wantFinal = trace.Records[len(trace.Records)-1].AfterStateDigest
	}
	if trace.FinalStateDigest != wantFinal {
		return fmt.Errorf("TRACE_FINAL_STATE_MISMATCH")
	}
	return nil
}

func (record ActionRecord) validate(step int, seedDigest string, previousLogicalTime uint64) error {
	if record.Step != uint64(step) {
		return fmt.Errorf("TRACE_STEP_NONCONTIGUOUS: %d", step)
	}
	if record.LogicalTime < previousLogicalTime {
		return fmt.Errorf("TRACE_LOGICAL_TIME_REGRESSION: %d", step)
	}
	if record.Action.ID == "" || record.Action.Kind.Validate() != nil {
		return fmt.Errorf("TRACE_ACTION_INVALID: %d", step)
	}
	if record.EnabledSetDigest == "" || record.BeforeStateDigest == "" || record.AfterStateDigest == "" ||
		record.Outcome != "applied" {
		return fmt.Errorf("TRACE_RECORD_INCOMPLETE: %d", step)
	}
	if (record.Command == nil) != (record.Yield == nil) {
		return fmt.Errorf("TRACE_ADAPTER_CYCLE_INCOMPLETE: %d", step)
	}
	if record.Command != nil {
		if record.Command.ID == "" || record.Command.Action != record.Action.ID ||
			record.Command.Kind != record.Action.Kind || record.Command.Node != record.Action.Node ||
			record.Command.Item != record.Action.Item {
			return fmt.Errorf("TRACE_COMMAND_ACTION_MISMATCH: %d", step)
		}
		if err := record.Command.Payload.Validate(); err != nil {
			return fmt.Errorf("TRACE_COMMAND_PAYLOAD_INVALID: %d: %w", step, err)
		}
		if record.Yield.ID == "" || record.Yield.StateDigest == "" ||
			(record.Yield.Kind != control.YieldStable && record.Yield.Kind != control.YieldTerminal) ||
			record.EmissionDigest == "" || record.Evidence == nil || record.Entropy == nil {
			return fmt.Errorf("TRACE_ADAPTER_CYCLE_INCOMPLETE: %d", step)
		}
	} else if record.EmissionDigest != "" || record.Evidence != nil || record.Entropy != nil {
		return fmt.Errorf("TRACE_NATIVE_RECORD_HAS_ADAPTER_CYCLE: %d", step)
	}
	if record.Evidence == nil {
		if record.EvidenceDigest != "" {
			return fmt.Errorf("TRACE_EVIDENCE_WITHOUT_PAYLOAD: %d", step)
		}
	} else {
		if record.Evidence.Yield != record.Yield.ID {
			return fmt.Errorf("TRACE_EVIDENCE_YIELD_MISMATCH: %d", step)
		}
		if err := record.Evidence.Payload.Validate(); err != nil {
			return fmt.Errorf("TRACE_EVIDENCE_INVALID: %d: %w", step, err)
		}
		digest, err := control.CanonicalDigest(*record.Evidence)
		if err != nil || digest != record.EvidenceDigest {
			return fmt.Errorf("TRACE_EVIDENCE_DIGEST_MISMATCH: %d", step)
		}
	}
	if record.Entropy == nil {
		if record.EntropyTapeDigest != "" {
			return fmt.Errorf("TRACE_ENTROPY_WITHOUT_PAYLOAD: %d", step)
		}
	} else {
		if record.Entropy.Yield != record.Yield.ID || record.Entropy.Algorithm == "" ||
			record.Entropy.SeedDigest != seedDigest ||
			record.EntropyTapeDigest != record.Entropy.TapeDigest {
			return fmt.Errorf("TRACE_ENTROPY_BINDING_INVALID: %d", step)
		}
		if err := record.Entropy.Tape.Validate(); err != nil {
			return fmt.Errorf("TRACE_ENTROPY_TAPE_INVALID: %d: %w", step, err)
		}
	}
	if record.Action.Kind == control.ActionFireTemporal {
		if record.ClockAdvance == nil || record.ClockAdvance.Temporal == "" ||
			record.ClockAdvance.To < record.ClockAdvance.From || record.LogicalTime != record.ClockAdvance.To {
			return fmt.Errorf("TRACE_CLOCK_ADVANCE_INVALID: %d", step)
		}
	} else if record.ClockAdvance != nil {
		return fmt.Errorf("TRACE_CLOCK_ADVANCE_UNEXPECTED: %d", step)
	}
	return nil
}

func itemTransitions(before, after Snapshot) []ItemTransition {
	beforeStates := make(map[control.ItemID]control.ItemState, len(before.Items))
	for _, item := range before.Items {
		beforeStates[item.ID] = item.State
	}
	afterStates := make(map[control.ItemID]control.ItemState, len(after.Items))
	for _, item := range after.Items {
		afterStates[item.ID] = item.State
	}
	ids := make([]control.ItemID, 0, len(beforeStates)+len(afterStates))
	seen := make(map[control.ItemID]struct{}, cap(ids))
	for id := range beforeStates {
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for id := range afterStates {
		if _, ok := seen[id]; !ok {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	result := make([]ItemTransition, 0, len(ids))
	for _, id := range ids {
		left := beforeStates[id]
		right := afterStates[id]
		if left != right {
			result = append(result, ItemTransition{Item: id, Before: left, After: right})
		}
	}
	return result
}

func nodeTransitions(before, after Snapshot) []NodeTransition {
	left := make(map[control.NodeID]NodeSnapshot, len(before.Nodes))
	for _, node := range before.Nodes {
		left[node.Ref.Node] = node
	}
	result := make([]NodeTransition, 0)
	for _, node := range after.Nodes {
		if old := left[node.Ref.Node]; old != node {
			result = append(result, NodeTransition{Node: node.Ref.Node, Before: old, After: node})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Node < result[j].Node })
	return result
}
