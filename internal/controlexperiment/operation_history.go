package controlexperiment

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

const OperationHistorySchemaVersion = "consensus-atlas/operation-history/v1"

// OperationRecord binds one actually invoked workload operation to its opaque
// input and, when present, its client response. The trusted Experiment owns
// the ordinal-to-WorkloadPlan mapping; no protocol-specific payload parsing is
// required in the generic bundle layer.
type OperationRecord struct {
	Ordinal    int                     `json:"ordinal"`
	RequestID  string                  `json:"request_id"`
	InvokeStep int                     `json:"invoke_step"`
	Target     control.NodeRef         `json:"target"`
	Input      control.PayloadEnvelope `json:"input"`
	ReturnStep int                     `json:"return_step,omitempty"`
	Response   *control.ClientResponse `json:"response,omitempty"`
}

// OperationHistory contains the frozen workload plan as well as the observed
// invoke/return interval history. Planned but not invoked operations stay in
// Plan; Operations contains exactly the offered prefix.
type OperationHistory struct {
	SchemaVersion string            `json:"schema_version"`
	Plan          WorkloadPlan      `json:"plan"`
	Operations    []OperationRecord `json:"operations"`
	Digest        string            `json:"digest"`
}

func newOperationHistory(
	plan *WorkloadPlan,
	workload *WorkloadRunReport,
	trace controlruntime.Trace,
	clients []ClientHistoryEntry,
) (OperationHistory, error) {
	if plan == nil || workload == nil {
		return OperationHistory{}, errors.New("OPERATION_HISTORY_WORKLOAD_REQUIRED")
	}
	history := OperationHistory{
		SchemaVersion: OperationHistorySchemaVersion,
		Plan:          cloneWorkloadPlan(*plan),
	}
	returns := make(map[string]ClientHistoryEntry, len(clients))
	for _, entry := range clients {
		requestID := entry.Response.RequestID
		if _, exists := returns[requestID]; exists {
			return OperationHistory{}, fmt.Errorf("OPERATION_HISTORY_RETURN_DUPLICATE: %s", requestID)
		}
		returns[requestID] = entry
	}
	invoked := 0
	for _, record := range trace.Records {
		if record.Action.Kind != control.ActionInvoke {
			continue
		}
		if invoked >= len(plan.Invocations) || record.Command == nil {
			return OperationHistory{}, errors.New("OPERATION_HISTORY_INVOKE_OVERFLOW")
		}
		planned := plan.Invocations[invoked]
		input, err := operationInvokeInput(record.Action)
		if err != nil {
			return OperationHistory{}, err
		}
		operation := OperationRecord{
			Ordinal: invoked + 1, RequestID: planned.ID, InvokeStep: int(record.Step),
			Target: record.Action.Node, Input: input,
		}
		if returned, exists := returns[planned.ID]; exists {
			response := returned.Response
			response.Payload = clonePayload(response.Payload)
			operation.ReturnStep = returned.Step
			operation.Response = &response
		}
		history.Operations = append(history.Operations, operation)
		invoked++
	}
	sealed, err := history.seal()
	if err != nil {
		return OperationHistory{}, err
	}
	if err := sealed.Validate(workload, trace, clients); err != nil {
		return OperationHistory{}, err
	}
	return sealed, nil
}

func (history OperationHistory) Validate(
	workload *WorkloadRunReport,
	trace controlruntime.Trace,
	clients []ClientHistoryEntry,
) error {
	if history.SchemaVersion != OperationHistorySchemaVersion || workload == nil {
		return errors.New("OPERATION_HISTORY_IDENTITY_INVALID")
	}
	if err := history.Plan.Validate(); err != nil {
		return err
	}
	planDigest, err := history.Plan.Digest()
	if err != nil || workload.PlanID != history.Plan.ID || workload.PlanDigest != planDigest ||
		workload.Planned != len(history.Plan.Invocations) ||
		workload.Offered != len(history.Operations) {
		return errors.New("OPERATION_HISTORY_PLAN_MISMATCH")
	}
	invokes := make([]controlruntime.ActionRecord, 0, len(history.Operations))
	for _, record := range trace.Records {
		if record.Action.Kind == control.ActionInvoke {
			invokes = append(invokes, record)
		}
	}
	if len(invokes) != len(history.Operations) {
		return errors.New("OPERATION_HISTORY_INVOKE_COUNT_MISMATCH")
	}
	returns := make(map[string]ClientHistoryEntry, len(clients))
	for _, entry := range clients {
		requestID := entry.Response.RequestID
		if requestID == "" || entry.State != control.ItemCompleted || entry.Step <= 0 ||
			entry.Step > len(trace.Records) {
			return errors.New("OPERATION_HISTORY_RETURN_INVALID")
		}
		if _, exists := returns[requestID]; exists {
			return errors.New("OPERATION_HISTORY_RETURN_DUPLICATE")
		}
		returns[requestID] = entry
	}
	completed := 0
	for index, operation := range history.Operations {
		planned := history.Plan.Invocations[index]
		invoked := invokes[index]
		if operation.Ordinal != index+1 || operation.RequestID != planned.ID ||
			operation.InvokeStep != int(invoked.Step) || operation.Target != invoked.Action.Node ||
			invoked.Command == nil || operation.InvokeStep <= 0 ||
			operation.InvokeStep > len(trace.Records) {
			return fmt.Errorf("OPERATION_HISTORY_INVOKE_MISMATCH: %d", index+1)
		}
		if err := operation.Target.Validate(); err != nil {
			return err
		}
		plannedInput, err := control.CanonicalDigest(planned.Input)
		if err != nil {
			return err
		}
		invokedInput, err := operationInvokeInput(invoked.Action)
		if err != nil {
			return err
		}
		commandInput, err := control.CanonicalDigest(invokedInput)
		if err != nil {
			return err
		}
		recordedInput, err := control.CanonicalDigest(operation.Input)
		if err != nil || plannedInput != commandInput || plannedInput != recordedInput {
			return fmt.Errorf("OPERATION_HISTORY_INPUT_MISMATCH: %d", index+1)
		}
		returned, exists := returns[operation.RequestID]
		if !exists {
			if operation.ReturnStep != 0 || operation.Response != nil {
				return fmt.Errorf("OPERATION_HISTORY_RETURN_UNEXPECTED: %s", operation.RequestID)
			}
			continue
		}
		if operation.Response == nil || operation.ReturnStep != returned.Step ||
			operation.ReturnStep < operation.InvokeStep {
			return fmt.Errorf("OPERATION_HISTORY_RETURN_MISMATCH: %s", operation.RequestID)
		}
		wantResponse, err := control.CanonicalDigest(returned.Response)
		if err != nil {
			return err
		}
		gotResponse, err := control.CanonicalDigest(*operation.Response)
		if err != nil || wantResponse != gotResponse || operation.Response.RequestID != operation.RequestID {
			return fmt.Errorf("OPERATION_HISTORY_RESPONSE_MISMATCH: %s", operation.RequestID)
		}
		completed++
	}
	if completed != workload.Completed || len(returns) != workload.Completed {
		return errors.New("OPERATION_HISTORY_COMPLETION_MISMATCH")
	}
	sealed, err := history.seal()
	if err != nil || !validSHA256(history.Digest) || sealed.Digest != history.Digest {
		return errors.New("OPERATION_HISTORY_DIGEST_MISMATCH")
	}
	return nil
}

func (history OperationHistory) seal() (OperationHistory, error) {
	history.Plan = cloneWorkloadPlan(history.Plan)
	history.Operations = cloneOperationRecords(history.Operations)
	history.Digest = ""
	digest, err := portableJSONDigest(history)
	if err != nil {
		return OperationHistory{}, err
	}
	history.Digest = digest
	return history, nil
}

func cloneOperationRecords(records []OperationRecord) []OperationRecord {
	result := make([]OperationRecord, len(records))
	for index, record := range records {
		result[index] = record
		result[index].Input = clonePayload(record.Input)
		if record.Response != nil {
			response := *record.Response
			response.Payload = clonePayload(response.Payload)
			result[index].Response = &response
		}
	}
	return result
}

func cloneWorkloadPlan(plan WorkloadPlan) WorkloadPlan {
	result := plan
	result.Invocations = append([]WorkloadInvocation(nil), plan.Invocations...)
	for index := range result.Invocations {
		result.Invocations[index].Input = clonePayload(result.Invocations[index].Input)
	}
	return result
}

func clonePayload(payload control.PayloadEnvelope) control.PayloadEnvelope {
	payload.Bytes = append([]byte(nil), payload.Bytes...)
	return payload
}

func operationInvokeInput(action control.Action) (control.PayloadEnvelope, error) {
	if action.Kind != control.ActionInvoke || len(action.Parameters) == 0 {
		return control.PayloadEnvelope{}, errors.New("OPERATION_HISTORY_INVOKE_PARAMETERS_INVALID")
	}
	parameters := struct {
		Input control.PayloadEnvelope `json:"input"`
	}{}
	decoder := json.NewDecoder(bytes.NewReader(action.Parameters))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parameters); err != nil {
		return control.PayloadEnvelope{}, errors.New("OPERATION_HISTORY_INVOKE_PARAMETERS_INVALID")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || parameters.Input.Validate() != nil {
		return control.PayloadEnvelope{}, errors.New("OPERATION_HISTORY_INVOKE_PARAMETERS_INVALID")
	}
	return clonePayload(parameters.Input), nil
}
