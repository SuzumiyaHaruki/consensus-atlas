package control

import (
	"context"
	"errors"
	"regexp"
)

type ExecutionFailureClass string

const (
	ExecutionFailureDeadline     ExecutionFailureClass = "deadline-exceeded"
	ExecutionFailureProcessExit  ExecutionFailureClass = "sut-process-exit"
	ExecutionFailureAdapterIO    ExecutionFailureClass = "adapter-io-failure"
	ExecutionFailureAdapter      ExecutionFailureClass = "adapter-failure"
	ExecutionFailureHarness      ExecutionFailureClass = "harness-failure"
	ExecutionFailureProcessPanic ExecutionFailureClass = "sut-panic"
)

var executionFailureCodes = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,127}$`)

func (class ExecutionFailureClass) Validate() error {
	switch class {
	case ExecutionFailureDeadline, ExecutionFailureProcessExit,
		ExecutionFailureAdapterIO, ExecutionFailureAdapter,
		ExecutionFailureHarness, ExecutionFailureProcessPanic:
		return nil
	default:
		return invalid("EXECUTION_FAILURE_CLASS_INVALID", "class", string(class))
	}
}

// ExecutionFailureClassifier is implemented by Adapter errors that can make
// a stable distinction without exposing protocol-specific details to Runtime.
type ExecutionFailureClassifier interface {
	ExecutionFailureClass() ExecutionFailureClass
	ExecutionFailureCode() string
}

func ClassifyExecutionFailure(err error) (ExecutionFailureClass, string) {
	if errors.Is(err, context.DeadlineExceeded) {
		return ExecutionFailureDeadline, "ACTION_DEADLINE_EXCEEDED"
	}
	var classified ExecutionFailureClassifier
	if errors.As(err, &classified) {
		class, code := classified.ExecutionFailureClass(), classified.ExecutionFailureCode()
		if class.Validate() == nil && executionFailureCodes.MatchString(code) {
			return class, code
		}
	}
	return ExecutionFailureAdapter, "ADAPTER_ACTION_FAILED"
}

func ValidExecutionFailureCode(code string) bool {
	return executionFailureCodes.MatchString(code)
}
