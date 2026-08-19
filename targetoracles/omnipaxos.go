package targetoracles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

const OmnipaxosClientDecisionBindingMonitorID = "omnipaxos-client-decision-binding"

// OmnipaxosClientDecisionBindingMonitor joins three target-owned facts: the
// invoked request, the worker decision carried by a completed client result,
// and the decided-prefix observation at that exact log position. Pending
// client results are deliberately outside this safety verdict.
type OmnipaxosClientDecisionBindingMonitor struct{}

func (OmnipaxosClientDecisionBindingMonitor) Name() string {
	return OmnipaxosClientDecisionBindingMonitorID
}

func (OmnipaxosClientDecisionBindingMonitor) CheckBundle(
	bundle controlexperiment.ExecutionBundle,
) []oracle.Violation {
	type invocation struct {
		step   int
		origin control.NodeRef
		input  omnipaxosv2.Input
	}
	type decisionBinding struct {
		step   int
		result omnipaxosv2.ClientResult
	}

	invocations := make(map[string]invocation)
	for _, record := range bundle.Trace.Records {
		if record.Action.Kind != control.ActionInvoke {
			continue
		}
		var parameters control.AdapterInvokeParameters
		if err := json.Unmarshal(record.Action.Parameters, &parameters); err != nil {
			return omnipaxosClientDecisionBindingViolation(
				int(record.Step), "invoke parameters could not be decoded",
			)
		}
		input, err := omnipaxosv2.ProjectInput(parameters.Input)
		if err != nil {
			return omnipaxosClientDecisionBindingViolation(
				int(record.Step), "invoke input projection failed",
			)
		}
		if _, exists := invocations[input.RequestID]; exists {
			return omnipaxosClientDecisionBindingViolation(
				int(record.Step), fmt.Sprintf("duplicate invoke request %s", input.RequestID),
			)
		}
		invocations[input.RequestID] = invocation{
			step: int(record.Step), origin: record.Action.Node, input: input,
		}
	}

	byRequest := make(map[string]decisionBinding)
	byPosition := make(map[uint64]decisionBinding)
	for _, entry := range bundle.ClientHistory {
		if entry.State != control.ItemCompleted {
			continue
		}
		result, err := omnipaxosv2.ProjectClientResult(entry.Response)
		if err != nil {
			return omnipaxosClientDecisionBindingViolation(
				entry.Step, fmt.Sprintf("client result %s projection failed", entry.Response.RequestID),
			)
		}
		invoke, exists := invocations[result.RequestID]
		if !exists {
			return omnipaxosClientDecisionBindingViolation(
				entry.Step, fmt.Sprintf("decided result %s has no invoke", result.RequestID),
			)
		}
		if entry.Step <= invoke.step || result.Origin != invoke.origin.Node ||
			!bytes.Equal(result.Value, invoke.input.Value) {
			return omnipaxosClientDecisionBindingViolation(
				entry.Step, fmt.Sprintf("decided result %s does not match its invoke", result.RequestID),
			)
		}
		binding := decisionBinding{step: entry.Step, result: result}
		if previous, exists := byRequest[result.RequestID]; exists &&
			!sameOmnipaxosDecision(previous.result, result) {
			return omnipaxosClientDecisionBindingViolation(
				entry.Step, fmt.Sprintf("request %s maps to different decisions", result.RequestID),
			)
		}
		if previous, exists := byPosition[result.Index]; exists &&
			!sameOmnipaxosDecision(previous.result, result) {
			return omnipaxosClientDecisionBindingViolation(
				entry.Step, fmt.Sprintf("decision position %d maps to different requests", result.Index),
			)
		}
		byRequest[result.RequestID] = binding
		byPosition[result.Index] = binding

		position := strconv.FormatUint(result.Index, 10)
		witnessed := false
		for _, observation := range bundle.Decisions.Observations {
			if observation.Participant == result.Node && observation.Position == position &&
				observation.Step <= entry.Step {
				witnessed = true
				break
			}
		}
		if !witnessed {
			return omnipaxosClientDecisionBindingViolation(
				entry.Step, fmt.Sprintf(
					"decided result %s has no decided-prefix witness at position %d",
					result.RequestID, result.Index,
				),
			)
		}
	}
	return nil
}

func sameOmnipaxosDecision(left, right omnipaxosv2.ClientResult) bool {
	return left.Node == right.Node && left.Index == right.Index &&
		left.RequestID == right.RequestID && left.Origin == right.Origin &&
		bytes.Equal(left.Value, right.Value)
}

func omnipaxosClientDecisionBindingViolation(step int, message string) []oracle.Violation {
	return []oracle.Violation{{
		Monitor: OmnipaxosClientDecisionBindingMonitorID, Step: step, Message: message,
	}}
}
