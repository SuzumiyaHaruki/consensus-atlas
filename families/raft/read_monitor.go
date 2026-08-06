package raft

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

// LinearizableRead checks the safety half of the ReadIndex contract. For each
// request context, the lower bound is derived from the trusted pre-input Driver
// snapshot rather than from a value supplied by the test plan or returned by
// the SUT. A request may remain unanswered; availability is a separate liveness
// property.
type LinearizableRead struct{}

func (LinearizableRead) Name() string { return "linearizable-read" }

func (LinearizableRead) ValidateEvidence(trace []core.TraceRecord) error {
	for _, record := range trace {
		if record.Event.Kind == core.EventQuery && record.Outcome == string(core.StatusApplied) {
			_, relevant, err := linearizableReadRequest(record.Event.Payload)
			if err != nil {
				return fmt.Errorf("%w at trace step %d: %s", oracle.ErrMalformedEvidence, record.Step, err)
			}
			if relevant {
				if _, err := committedLowerBound(record.Before); err != nil {
					return fmt.Errorf("%w at trace step %d: %s", oracle.ErrMalformedEvidence, record.Step, err)
				}
			}
		}
		for _, observation := range record.Observations {
			if observation.Kind != "read-state" {
				continue
			}
			if err := validateReadStateObservation(observation); err != nil {
				return fmt.Errorf("%w at trace step %d: %s", oracle.ErrMalformedEvidence, record.Step, err)
			}
		}
	}
	return nil
}

func (LinearizableRead) Check(trace []core.TraceRecord) []oracle.Violation {
	lowerBounds := make(map[string]uint64)
	var violations []oracle.Violation
	for _, record := range trace {
		if record.Event.Kind == core.EventQuery && record.Outcome == string(core.StatusApplied) {
			requestID, relevant, err := linearizableReadRequest(record.Event.Payload)
			if err != nil {
				continue
			}
			if relevant {
				bound, boundErr := committedLowerBound(record.Before)
				if boundErr != nil {
					continue
				}
				if bound > lowerBounds[requestID] {
					lowerBounds[requestID] = bound
				}
			}
		}
		for _, observation := range record.Observations {
			if observation.Kind != "read-state" {
				continue
			}
			if validateReadStateObservation(observation) != nil {
				continue
			}
			requestID := observation.Value
			if observation.Evidence != nil && observation.Evidence["request_id"] != "" {
				requestID = observation.Evidence["request_id"]
			}
			bound, exists := lowerBounds[requestID]
			if !exists {
				violations = append(violations, readViolation(record.Step,
					fmt.Sprintf("read state for unknown request context %q", requestID)))
				continue
			}
			indexText := ""
			if observation.Evidence != nil {
				indexText = observation.Evidence["index"]
			}
			index, err := strconv.ParseUint(indexText, 10, 64)
			if err != nil {
				violations = append(violations, readViolation(record.Step,
					fmt.Sprintf("read state for request %q has an invalid index", requestID)))
				continue
			}
			if index < bound {
				violations = append(violations, readViolation(record.Step,
					fmt.Sprintf("read state for request %q returned index %d below trusted lower bound %d", requestID, index, bound)))
			}
		}
	}
	return violations
}

func validateReadStateObservation(observation core.Observation) error {
	if observation.Evidence == nil {
		return fmt.Errorf("read state has no evidence")
	}
	requestID := observation.Evidence["request_id"]
	if requestID == "" {
		return fmt.Errorf("read state has no request context")
	}
	if observation.Value != "" && observation.Value != requestID {
		return fmt.Errorf("read state value disagrees with evidence")
	}
	if _, err := strconv.ParseUint(observation.Evidence["index"], 10, 64); err != nil {
		return fmt.Errorf("read state for request %q has an invalid index", requestID)
	}
	return nil
}

func linearizableReadRequest(payload json.RawMessage) (string, bool, error) {
	var request struct {
		Operation string `json:"operation"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(payload, &request); err != nil {
		return "", false, fmt.Errorf("cannot decode applied query payload")
	}
	if request.Operation != "linearizable-read" {
		return "", false, nil
	}
	if request.RequestID == "" {
		return "", false, fmt.Errorf("linearizable read query has no request context")
	}
	return request.RequestID, true, nil
}

func committedLowerBound(snapshot any) (uint64, error) {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return 0, fmt.Errorf("cannot encode trusted pre-query snapshot")
	}
	var current struct {
		Driver struct {
			Nodes map[string]struct {
				Commit uint64 `json:"commit"`
			} `json:"nodes"`
		} `json:"driver"`
	}
	if err := json.Unmarshal(encoded, &current); err != nil || len(current.Driver.Nodes) == 0 {
		return 0, fmt.Errorf("trusted pre-query snapshot has no Raft commit state")
	}
	var bound uint64
	for _, node := range current.Driver.Nodes {
		if node.Commit > bound {
			bound = node.Commit
		}
	}
	return bound, nil
}

func readViolation(step int, message string) oracle.Violation {
	return oracle.Violation{Monitor: "linearizable-read", Step: step, Message: message}
}
