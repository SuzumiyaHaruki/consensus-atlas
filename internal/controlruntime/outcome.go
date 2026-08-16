package controlruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const TerminalOutcomeSchemaVersion = "consensus-atlas/terminal-outcome/v1"

// TerminalOutcome is a sidecar for an attempted Action that did not commit.
// It binds the successful Trace prefix and enabled frontier, but deliberately
// is not an ActionRecord and grants no protocol-finding status by itself.
type TerminalOutcome struct {
	SchemaVersion     string                        `json:"schema_version"`
	Class             control.ExecutionFailureClass `json:"class"`
	Code              string                        `json:"code"`
	Decision          uint64                        `json:"decision"`
	PrefixTraceDigest string                        `json:"prefix_trace_digest"`
	EnabledSetDigest  string                        `json:"enabled_set_digest"`
	AttemptedAction   control.Action                `json:"attempted_action"`
	Digest            string                        `json:"digest"`
}

func newTerminalOutcome(
	class control.ExecutionFailureClass,
	code string,
	decision uint64,
	prefix Trace,
	enabledSetDigest string,
	action control.Action,
) (TerminalOutcome, error) {
	outcome := TerminalOutcome{
		SchemaVersion: TerminalOutcomeSchemaVersion, Class: class, Code: code,
		Decision: decision, PrefixTraceDigest: prefix.Digest,
		EnabledSetDigest: enabledSetDigest, AttemptedAction: cloneAction(action),
	}
	return outcome.seal()
}

func (outcome TerminalOutcome) Validate() error {
	if outcome.SchemaVersion != TerminalOutcomeSchemaVersion || outcome.Class.Validate() != nil ||
		!control.ValidExecutionFailureCode(outcome.Code) || outcome.Decision == 0 ||
		!validOutcomeDigest(outcome.PrefixTraceDigest) || !validOutcomeDigest(outcome.EnabledSetDigest) ||
		outcome.AttemptedAction.ID == "" || outcome.AttemptedAction.Kind.Validate() != nil ||
		!validOutcomeDigest(outcome.Digest) {
		return errors.New("TERMINAL_OUTCOME_INVALID")
	}
	sealed, err := outcome.seal()
	if err != nil || sealed.Digest != outcome.Digest {
		return errors.New("TERMINAL_OUTCOME_DIGEST_MISMATCH")
	}
	return nil
}

func (outcome TerminalOutcome) seal() (TerminalOutcome, error) {
	outcome.AttemptedAction = cloneAction(outcome.AttemptedAction)
	outcome.Digest = ""
	digest, err := control.CanonicalDigest(outcome)
	if err != nil {
		return TerminalOutcome{}, err
	}
	outcome.Digest = digest
	return outcome, nil
}

func validOutcomeDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}

type TerminalExecutionError struct {
	Terminal TerminalOutcome
	cause    error
}

func (failure *TerminalExecutionError) Error() string {
	return fmt.Sprintf("TERMINAL_EXECUTION: class=%s code=%s decision=%d: %v",
		failure.Terminal.Class, failure.Terminal.Code, failure.Terminal.Decision, failure.cause)
}

func (failure *TerminalExecutionError) Unwrap() error { return failure.cause }

func cloneAction(action control.Action) control.Action {
	action.Parameters = append([]byte(nil), action.Parameters...)
	return action
}
