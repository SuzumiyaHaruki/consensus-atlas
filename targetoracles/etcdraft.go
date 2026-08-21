package targetoracles

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

const (
	LogProgressMonitorID              = "etcdraft-log-progress"
	ClientApplicationBindingMonitorID = "etcdraft-client-application-binding"
	ElectionSafetyMonitorID           = "etcdraft-election-safety"
)

// ElectionSafetyMonitor checks the Raft election invariant exposed by
// Adapter-owned Evidence: at most one distinct running leader may be observed
// for one term. It deliberately does not inspect vote bookkeeping or native
// tracker state.
type ElectionSafetyMonitor struct{}

func (ElectionSafetyMonitor) Name() string { return ElectionSafetyMonitorID }

func (ElectionSafetyMonitor) CheckBundle(
	bundle controlexperiment.ExecutionBundle,
) []oracle.Violation {
	leaders := make(map[uint64]control.NodeID)
	points := []struct {
		step     int
		evidence control.EvidenceEnvelope
	}{{evidence: bundle.Trace.InitialEvidence}}
	for _, record := range bundle.Trace.Records {
		if record.Evidence != nil {
			points = append(points, struct {
				step     int
				evidence control.EvidenceEnvelope
			}{step: int(record.Step), evidence: *record.Evidence})
		}
	}
	for _, point := range points {
		evidence, err := etcdraftv2.ProjectEvidence(point.evidence)
		if err != nil {
			return electionSafetyViolation(point.step, "evidence projection failed")
		}
		for _, node := range evidence.Nodes {
			if !node.Running || node.Role != "StateLeader" || node.Term == 0 {
				continue
			}
			if previous, exists := leaders[node.Term]; exists && previous != node.Node {
				return electionSafetyViolation(point.step, fmt.Sprintf(
					"term %d has distinct leaders %s and %s", node.Term, previous, node.Node,
				))
			}
			leaders[node.Term] = node.Node
		}
	}
	return nil
}

func electionSafetyViolation(step int, message string) []oracle.Violation {
	return []oracle.Violation{{Monitor: ElectionSafetyMonitorID, Step: step, Message: message}}
}

// LogProgressMonitor stays target-local. It checks only Adapter-owned
// Evidence and never consumes PSS, Risk progress, or Agent output.
type LogProgressMonitor struct{}

func (LogProgressMonitor) Name() string { return LogProgressMonitorID }

func (LogProgressMonitor) CheckBundle(
	bundle controlexperiment.ExecutionBundle,
) []oracle.Violation {
	type frontier struct {
		incarnation uint64
		commit      uint64
		applied     uint64
		prefixes    []etcdraftv2.ApplicationPrefixEvidence
	}
	previous := make(map[control.NodeID]frontier)
	points := []struct {
		step     int
		evidence control.EvidenceEnvelope
	}{{evidence: bundle.Trace.InitialEvidence}}
	for _, record := range bundle.Trace.Records {
		if record.Evidence != nil {
			points = append(points, struct {
				step     int
				evidence control.EvidenceEnvelope
			}{step: int(record.Step), evidence: *record.Evidence})
		}
	}
	for _, point := range points {
		evidence, err := etcdraftv2.ProjectEvidence(point.evidence)
		if err != nil {
			return logProgressViolation(point.step, "evidence projection failed")
		}
		for _, node := range evidence.Nodes {
			if node.Applied > node.Commit {
				return logProgressViolation(point.step, fmt.Sprintf(
					"node %s/%d applied frontier %d exceeds commit frontier %d",
					node.Node, node.Incarnation, node.Applied, node.Commit,
				))
			}
			if before, ok := previous[node.Node]; ok {
				if node.Commit < before.commit {
					return logProgressViolation(point.step, fmt.Sprintf(
						"node %s commit frontier regressed across incarnation %d -> %d from %d to %d",
						node.Node, before.incarnation, node.Incarnation, before.commit, node.Commit,
					))
				}
				if node.Applied < before.applied {
					return logProgressViolation(point.step, fmt.Sprintf(
						"node %s applied frontier regressed across incarnation %d -> %d from %d to %d",
						node.Node, before.incarnation, node.Incarnation, before.applied, node.Applied,
					))
				}
				for index, prefix := range before.prefixes {
					if index >= len(node.ApplicationPrefixes) {
						break
					}
					if prefix.Digest != node.ApplicationPrefixes[index].Digest {
						return logProgressViolation(point.step, fmt.Sprintf(
							"node %s changed applied prefix %d across incarnation %d -> %d",
							node.Node, prefix.Position, before.incarnation, node.Incarnation,
						))
					}
				}
			}
			previous[node.Node] = frontier{
				incarnation: node.Incarnation, commit: node.Commit, applied: node.Applied,
				prefixes: append([]etcdraftv2.ApplicationPrefixEvidence(nil), node.ApplicationPrefixes...),
			}
		}
	}
	return nil
}

func logProgressViolation(step int, message string) []oracle.Violation {
	return []oracle.Violation{{Monitor: LogProgressMonitorID, Step: step, Message: message}}
}

// ClientApplicationBindingMonitor joins three independently recorded facts:
// the Invoke input, the Adapter's command-applied observation, and the
// committed ClientResult.
type ClientApplicationBindingMonitor struct{}

func (ClientApplicationBindingMonitor) Name() string {
	return ClientApplicationBindingMonitorID
}

func (ClientApplicationBindingMonitor) CheckBundle(
	bundle controlexperiment.ExecutionBundle,
) []oracle.Violation {
	type invocation struct {
		step   int
		origin control.NodeRef
		input  etcdraftv2.Input
	}
	invocations := make(map[string]invocation)
	itemSteps := make(map[control.ItemID]int)
	for _, record := range bundle.Trace.Records {
		for _, transition := range record.ItemTransitions {
			if _, exists := itemSteps[transition.Item]; !exists {
				itemSteps[transition.Item] = int(record.Step)
			}
		}
		if record.Action.Kind != control.ActionInvoke {
			continue
		}
		var parameters control.AdapterInvokeParameters
		if err := json.Unmarshal(record.Action.Parameters, &parameters); err != nil {
			return clientApplicationBindingViolation(
				int(record.Step), "invoke parameters could not be decoded",
			)
		}
		input, err := etcdraftv2.ProjectInput(parameters.Input)
		if err != nil {
			return clientApplicationBindingViolation(
				int(record.Step), "invoke input projection failed",
			)
		}
		if _, exists := invocations[input.RequestID]; exists {
			return clientApplicationBindingViolation(
				int(record.Step), fmt.Sprintf("duplicate invoke request %s", input.RequestID),
			)
		}
		invocations[input.RequestID] = invocation{
			step: int(record.Step), origin: record.Action.Node, input: input,
		}
	}

	type witnessedCommand struct {
		step    int
		owner   control.NodeRef
		command etcdraftv2.AppliedCommandEvidence
	}
	var witnessed []witnessedCommand
	for _, item := range bundle.FinalSnapshot.Items {
		if item.Kind != control.ItemObservation || item.Value.Observation == nil ||
			item.Value.Observation.Kind != etcdraftv2.ReadyAdvancedObservationKind {
			continue
		}
		step := itemSteps[item.ID]
		if step <= 0 {
			return clientApplicationBindingViolation(
				0, fmt.Sprintf("ready-advanced observation %s has no trace step", item.ID),
			)
		}
		projected, err := etcdraftv2.ProjectReadyAdvancedObservation(*item.Value.Observation)
		if err != nil {
			return clientApplicationBindingViolation(
				step, "ready-advanced observation projection failed",
			)
		}
		for _, command := range projected.Commands {
			witnessed = append(witnessed, witnessedCommand{
				step: step, owner: item.Owner, command: command,
			})
		}
	}

	for _, entry := range bundle.ClientHistory {
		result, err := etcdraftv2.ProjectClientResult(entry.Response)
		if err != nil {
			return clientApplicationBindingViolation(
				entry.Step, fmt.Sprintf("client result %s projection failed", entry.Response.RequestID),
			)
		}
		if result.Status != "committed" {
			continue
		}
		invoke, exists := invocations[result.RequestID]
		if !exists {
			return clientApplicationBindingViolation(
				entry.Step, fmt.Sprintf("committed result %s has no invoke", result.RequestID),
			)
		}
		if entry.Step < invoke.step || entry.Response.Owner.Node != invoke.origin.Node ||
			!bytes.Equal(result.Value, invoke.input.Value) {
			return clientApplicationBindingViolation(
				entry.Step, fmt.Sprintf("committed result %s does not match its invoke", result.RequestID),
			)
		}
		matches := 0
		for _, candidate := range witnessed {
			if candidate.step != entry.Step || candidate.owner != entry.Response.Owner {
				continue
			}
			command := candidate.command
			if command.RequestID == result.RequestID && command.Index == result.Index &&
				command.Term == result.Term && command.Origin == invoke.origin &&
				bytes.Equal(command.Value, result.Value) {
				matches++
			}
		}
		if matches != 1 {
			return clientApplicationBindingViolation(
				entry.Step, fmt.Sprintf(
					"committed result %s has %d exact applied-command witnesses", result.RequestID, matches,
				),
			)
		}
	}
	type logPosition struct {
		index uint64
		term  uint64
	}
	positions := make(map[string]logPosition)
	for _, candidate := range witnessed {
		command := candidate.command
		invoke, exists := invocations[command.RequestID]
		if !exists {
			return clientApplicationBindingViolation(
				candidate.step, fmt.Sprintf("applied command %s has no prior invoke", command.RequestID),
			)
		}
		if candidate.step <= invoke.step || command.Origin != invoke.origin ||
			!bytes.Equal(command.Value, invoke.input.Value) {
			return clientApplicationBindingViolation(
				candidate.step, fmt.Sprintf("applied command %s does not match its invoke", command.RequestID),
			)
		}
		position := logPosition{index: command.Index, term: command.Term}
		if previous, exists := positions[command.RequestID]; exists && previous != position {
			return clientApplicationBindingViolation(
				candidate.step, fmt.Sprintf("applied command %s has multiple log positions", command.RequestID),
			)
		}
		positions[command.RequestID] = position
	}
	return nil
}

func clientApplicationBindingViolation(step int, message string) []oracle.Violation {
	return []oracle.Violation{{
		Monitor: ClientApplicationBindingMonitorID, Step: step, Message: message,
	}}
}
