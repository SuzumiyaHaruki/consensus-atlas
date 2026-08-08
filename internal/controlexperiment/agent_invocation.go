package controlexperiment

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

const (
	AgentInvocationAuditVersion = "consensus-atlas/agent-invocation-audit/v1"

	AgentInvocationTransportFailed  = "transport-failed"
	AgentInvocationResponseRejected = "response-rejected"
	AgentInvocationIntentRejected   = "intent-rejected"
	AgentInvocationCompileRejected  = "compile-rejected"
	AgentInvocationExecutionFailed  = "execution-failed"
	AgentInvocationCompleted        = "completed"
)

// AgentResponseIdentity records only non-secret provider metadata. The exact
// response bytes are digest-bound by AgentInvocationAudit.ResponseDigest.
type AgentResponseIdentity struct {
	ID                string `json:"id"`
	Model             string `json:"model"`
	FinishReason      string `json:"finish_reason"`
	SystemFingerprint string `json:"system_fingerprint,omitempty"`
}

// AgentInvocationAudit is the durable boundary around one model request. It
// intentionally contains neither credentials nor prompt/response bodies.
// Later trusted stages append only identities of accepted artifacts.
type AgentInvocationAudit struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`
	Status        string `json:"status"`

	ViewDigest      string `json:"view_digest"`
	Provider        string `json:"provider"`
	Endpoint        string `json:"endpoint"`
	Model           string `json:"model"`
	Thinking        string `json:"thinking"`
	Temperature     int    `json:"temperature"`
	MaxOutputTokens int    `json:"max_output_tokens"`

	PromptDigest   string                 `json:"prompt_digest"`
	RequestDigest  string                 `json:"request_digest"`
	ResponseDigest string                 `json:"response_digest,omitempty"`
	Response       *AgentResponseIdentity `json:"response,omitempty"`
	DurationMillis int64                  `json:"duration_millis"`

	IntentDigest string              `json:"intent_digest,omitempty"`
	PlanDigest   string              `json:"plan_digest,omitempty"`
	ReportDigest string              `json:"report_digest,omitempty"`
	BundleDigest string              `json:"bundle_digest,omitempty"`
	CompilerWork *IntentCompilerWork `json:"compiler_work,omitempty"`
	FailureCode  string              `json:"failure_code,omitempty"`
	Work         WorkLedger          `json:"work"`
	Digest       string              `json:"digest"`
}

func NewAgentInvocationAudit(audit AgentInvocationAudit) (AgentInvocationAudit, error) {
	audit.SchemaVersion = AgentInvocationAuditVersion
	if audit.Work.Resources.WallTime == "" {
		audit.Work.Resources.WallTime = ResourceNotCollected
	}
	if audit.Work.Resources.CPUTime == "" {
		audit.Work.Resources.CPUTime = ResourceNotCollected
	}
	if audit.Work.Resources.PeakRSS == "" {
		audit.Work.Resources.PeakRSS = ResourceNotCollected
	}
	audit.Digest = ""
	if err := audit.validateContent(); err != nil {
		return AgentInvocationAudit{}, err
	}
	digest, err := portableJSONDigest(audit)
	if err != nil {
		return AgentInvocationAudit{}, err
	}
	audit.Digest = digest
	return audit, nil
}

func (audit AgentInvocationAudit) Validate() error {
	if err := audit.validateContent(); err != nil {
		return err
	}
	want, err := NewAgentInvocationAudit(audit)
	if err != nil || !validSHA256(audit.Digest) || want.Digest != audit.Digest {
		return errors.New("EXPERIMENT_AGENT_INVOCATION_DIGEST_MISMATCH")
	}
	return nil
}

func (audit AgentInvocationAudit) validateContent() error {
	if audit.SchemaVersion != AgentInvocationAuditVersion || !validMethodToken(audit.ID) ||
		!validMethodToken(audit.Provider) || audit.Endpoint == "" || !validMethodToken(audit.Model) ||
		audit.Thinking != "disabled" || audit.Temperature != 0 || audit.MaxOutputTokens <= 0 ||
		!validSHA256(audit.ViewDigest) || !validSHA256(audit.PromptDigest) ||
		!validSHA256(audit.RequestDigest) || audit.DurationMillis < 0 || audit.Work.Model.Calls != 1 {
		return errors.New("EXPERIMENT_AGENT_INVOCATION_INVALID")
	}
	if err := validateMethodWork(audit.Work); err != nil {
		return err
	}
	if audit.Response != nil && audit.ResponseDigest == "" ||
		(audit.ResponseDigest != "" && !validSHA256(audit.ResponseDigest)) {
		return errors.New("EXPERIMENT_AGENT_INVOCATION_RESPONSE_INVALID")
	}
	if audit.Response != nil && (audit.Response.ID == "" || audit.Response.Model == "" ||
		audit.Response.FinishReason == "") {
		return errors.New("EXPERIMENT_AGENT_INVOCATION_RESPONSE_INVALID")
	}
	if audit.CompilerWork != nil && (audit.CompilerWork.CandidatesEvaluated <= 0 ||
		audit.CompilerWork.PreferenceChecks < 0 ||
		audit.CompilerWork.WorkUnits != audit.CompilerWork.CandidatesEvaluated+audit.CompilerWork.PreferenceChecks) {
		return errors.New("EXPERIMENT_AGENT_INVOCATION_COMPILER_WORK_INVALID")
	}
	if !optionalDigestsValid(audit.IntentDigest, audit.PlanDigest, audit.ReportDigest, audit.BundleDigest) {
		return errors.New("EXPERIMENT_AGENT_INVOCATION_ARTIFACT_INVALID")
	}
	return audit.validateStatus()
}

func (audit AgentInvocationAudit) validateStatus() error {
	requireFailure := func() bool { return audit.FailureCode != "" }
	noArtifacts := func() bool {
		return audit.IntentDigest == "" && audit.PlanDigest == "" && audit.ReportDigest == "" &&
			audit.BundleDigest == "" && audit.CompilerWork == nil
	}
	switch audit.Status {
	case AgentInvocationTransportFailed:
		if audit.Response != nil || !requireFailure() || !noArtifacts() {
			return errors.New("EXPERIMENT_AGENT_INVOCATION_STATUS_INVALID")
		}
	case AgentInvocationResponseRejected:
		if !requireFailure() || !noArtifacts() {
			return errors.New("EXPERIMENT_AGENT_INVOCATION_STATUS_INVALID")
		}
	case AgentInvocationIntentRejected:
		if audit.Response == nil || !requireFailure() || !noArtifacts() {
			return errors.New("EXPERIMENT_AGENT_INVOCATION_STATUS_INVALID")
		}
	case AgentInvocationCompileRejected:
		if audit.Response == nil || !requireFailure() || !validSHA256(audit.IntentDigest) ||
			audit.PlanDigest != "" || audit.ReportDigest != "" || audit.BundleDigest != "" ||
			audit.CompilerWork != nil {
			return errors.New("EXPERIMENT_AGENT_INVOCATION_STATUS_INVALID")
		}
	case AgentInvocationExecutionFailed:
		if audit.Response == nil || !requireFailure() || !validSHA256(audit.IntentDigest) ||
			!validSHA256(audit.PlanDigest) || audit.ReportDigest != "" || audit.BundleDigest != "" ||
			audit.CompilerWork == nil {
			return errors.New("EXPERIMENT_AGENT_INVOCATION_STATUS_INVALID")
		}
	case AgentInvocationCompleted:
		if audit.Response == nil || audit.FailureCode != "" || !validSHA256(audit.IntentDigest) ||
			!validSHA256(audit.PlanDigest) || !validSHA256(audit.ReportDigest) ||
			!validSHA256(audit.BundleDigest) || audit.CompilerWork == nil {
			return errors.New("EXPERIMENT_AGENT_INVOCATION_STATUS_INVALID")
		}
	default:
		return errors.New("EXPERIMENT_AGENT_INVOCATION_STATUS_INVALID")
	}
	return nil
}

func optionalDigestsValid(values ...string) bool {
	for _, value := range values {
		if value != "" && !validSHA256(value) {
			return false
		}
	}
	return true
}

// AgentInvocationWork attaches one bounded model call to existing execution
// work without changing the report or bundle identities.
func AgentInvocationWork(execution WorkLedger, model ModelWork) WorkLedger {
	execution.Model = model
	if execution.Resources.WallTime == "" {
		execution.Resources = emptyWork().Resources
	}
	return execution
}

// AgentInvocationDigest hashes exact transport bytes without persisting them.
func AgentInvocationDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
