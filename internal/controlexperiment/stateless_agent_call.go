package controlexperiment

import (
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const (
	StatelessAgentCallIntentVersion   = "consensus-atlas/stateless-agent-call-intent/v1"
	StatelessAgentCallDispatchVersion = "consensus-atlas/stateless-agent-call-dispatch/v1"
	StatelessAgentCallResultVersion   = "consensus-atlas/stateless-agent-call-result/v1"
	StatelessAgentCallCompleted       = "completed"
	// StatelessAgentCallContentReady means that the provider response and its
	// accounting evidence are durable, while acceptance of the response is
	// deliberately delegated to a higher-level, typed planner contract.
	StatelessAgentCallContentReady = "content-ready"
	StatelessAgentCallFailed       = "failed"
	StatelessAgentCallRejected     = "proposal-rejected"
	StatelessAgentCallPrepared     = "prepared"
	StatelessAgentCallAmbiguous    = "ambiguous"
	StatelessAgentCallAuditVersion = "consensus-atlas/stateless-agent-call-audit/v1"
	statelessAgentCallMaxBytes     = 1 << 20
)

// StatelessAgentCallAudit is the compact, secret-free evidence embedded in a
// Campaign attempt artifact. Exact prompt/request/result bytes remain in the
// durable sidecar; their validated digests and charged work are sealed here.
type StatelessAgentCallAudit struct {
	SchemaVersion       string    `json:"schema_version"`
	Ordinal             int       `json:"ordinal"`
	RootID              string    `json:"root_id"`
	SearchRequestDigest string    `json:"search_request_digest"`
	IntentDigest        string    `json:"intent_digest"`
	DispatchDigest      string    `json:"dispatch_digest,omitempty"`
	ResultDigest        string    `json:"result_digest,omitempty"`
	Status              string    `json:"status"`
	Work                ModelWork `json:"work"`
	TransportAttempts   int       `json:"transport_attempts,omitempty"`
	ProviderUsageStatus string    `json:"provider_usage_status,omitempty"`
	Digest              string    `json:"digest"`
}

func NewStatelessAgentCallAudit(
	intent StatelessAgentCallIntent,
	dispatch *StatelessAgentCallDispatch,
	result *StatelessAgentCallResult,
) (StatelessAgentCallAudit, error) {
	if intent.Validate() != nil {
		return StatelessAgentCallAudit{}, errors.New("EXPERIMENT_STATELESS_AGENT_CALL_AUDIT_INTENT_INVALID")
	}
	audit := StatelessAgentCallAudit{
		SchemaVersion: StatelessAgentCallAuditVersion, Ordinal: intent.Ordinal,
		RootID: intent.RootID, SearchRequestDigest: intent.SearchRequestDigest,
		IntentDigest: intent.Digest, Status: StatelessAgentCallPrepared,
	}
	if dispatch != nil {
		if dispatch.ValidateIntent(intent) != nil {
			return StatelessAgentCallAudit{}, errors.New("EXPERIMENT_STATELESS_AGENT_CALL_AUDIT_DISPATCH_INVALID")
		}
		audit.DispatchDigest = dispatch.Digest
		audit.Status = StatelessAgentCallAmbiguous
		audit.Work = ModelWork{Calls: 1}
	}
	if result != nil {
		if dispatch == nil || result.ValidateInputs(intent, *dispatch) != nil {
			return StatelessAgentCallAudit{}, errors.New("EXPERIMENT_STATELESS_AGENT_CALL_AUDIT_RESULT_INVALID")
		}
		audit.ResultDigest = result.Digest
		audit.Status = result.Status
		audit.Work = result.Work
		audit.TransportAttempts = result.TransportAttempts
		audit.ProviderUsageStatus = result.ProviderUsageStatus
	}
	sealed, err := audit.seal()
	if err != nil || sealed.Validate() != nil {
		return StatelessAgentCallAudit{}, errors.New("EXPERIMENT_STATELESS_AGENT_CALL_AUDIT_INVALID")
	}
	return sealed, nil
}

func (audit StatelessAgentCallAudit) Validate() error {
	if audit.SchemaVersion != StatelessAgentCallAuditVersion || audit.Ordinal <= 0 ||
		!validMethodToken(audit.RootID) || !validSHA256(audit.SearchRequestDigest) ||
		!validSHA256(audit.IntentDigest) || audit.Work.Calls < 0 || audit.Work.Calls > 1 ||
		audit.TransportAttempts < 0 || audit.TransportAttempts > 3 ||
		audit.Work.InputTokens < 0 || audit.Work.OutputTokens < 0 ||
		audit.Work.TotalTokens != audit.Work.InputTokens+audit.Work.OutputTokens ||
		!validProviderUsageStatus(audit.ProviderUsageStatus) {
		return errors.New("EXPERIMENT_STATELESS_AGENT_CALL_AUDIT_INVALID")
	}
	switch audit.Status {
	case StatelessAgentCallPrepared:
		if audit.DispatchDigest != "" || audit.ResultDigest != "" || audit.Work != (ModelWork{}) ||
			audit.TransportAttempts != 0 {
			return errors.New("EXPERIMENT_STATELESS_AGENT_CALL_AUDIT_PREPARED_INVALID")
		}
	case StatelessAgentCallAmbiguous:
		if !validSHA256(audit.DispatchDigest) || audit.ResultDigest != "" ||
			audit.Work != (ModelWork{Calls: 1}) || audit.TransportAttempts != 0 {
			return errors.New("EXPERIMENT_STATELESS_AGENT_CALL_AUDIT_AMBIGUOUS_INVALID")
		}
	case StatelessAgentCallCompleted, StatelessAgentCallContentReady,
		StatelessAgentCallRejected, StatelessAgentCallFailed:
		if !validSHA256(audit.DispatchDigest) || !validSHA256(audit.ResultDigest) || audit.Work.Calls > 1 {
			return errors.New("EXPERIMENT_STATELESS_AGENT_CALL_AUDIT_TERMINAL_INVALID")
		}
		if audit.Status != StatelessAgentCallFailed && (audit.Work.Calls != 1 || audit.Work.TotalTokens <= 0) {
			return errors.New("EXPERIMENT_STATELESS_AGENT_CALL_AUDIT_TERMINAL_WORK_INVALID")
		}
	default:
		return errors.New("EXPERIMENT_STATELESS_AGENT_CALL_AUDIT_STATUS_INVALID")
	}
	want, err := audit.seal()
	if err != nil || !validSHA256(audit.Digest) || want.Digest != audit.Digest {
		return errors.New("EXPERIMENT_STATELESS_AGENT_CALL_AUDIT_DIGEST_MISMATCH")
	}
	return nil
}

func (audit StatelessAgentCallAudit) seal() (StatelessAgentCallAudit, error) {
	audit.Digest = ""
	digest, err := control.CanonicalDigest(audit)
	audit.Digest = digest
	return audit, err
}

// StatelessAgentCallIntent freezes exact public request bytes before a
// provider dispatch. It contains no credential or execution authority.
type StatelessAgentCallIntent struct {
	SchemaVersion       string               `json:"schema_version"`
	ID                  string               `json:"id"`
	Ordinal             int                  `json:"ordinal"`
	RootID              string               `json:"root_id"`
	SearchRequestDigest string               `json:"search_request_digest"`
	Transport           AgentTransportFreeze `json:"transport"`
	PromptBytes         []byte               `json:"prompt_bytes"`
	PromptDigest        string               `json:"prompt_digest"`
	RequestBytes        []byte               `json:"request_bytes"`
	RequestDigest       string               `json:"request_digest"`
	Digest              string               `json:"digest"`
}

// NewPlanningAgentCallIntent is the protocol-neutral compatibility constructor
// for the durable call journal. SearchRequestDigest retains its v1 JSON name so
// existing frontier-call artifacts keep their exact canonical representation.
func NewPlanningAgentCallIntent(
	id string,
	ordinal int,
	rootID string,
	planningRequestDigest string,
	transport AgentTransportFreeze,
	prompt []byte,
	payload []byte,
) (StatelessAgentCallIntent, error) {
	intent := StatelessAgentCallIntent{
		SchemaVersion: StatelessAgentCallIntentVersion,
		ID:            id, Ordinal: ordinal, RootID: rootID,
		SearchRequestDigest: planningRequestDigest, Transport: transport,
		PromptBytes:   append([]byte(nil), prompt...),
		PromptDigest:  AgentInvocationDigest(prompt),
		RequestBytes:  append([]byte(nil), payload...),
		RequestDigest: AgentInvocationDigest(payload),
	}
	sealed, err := intent.seal()
	if err != nil || sealed.ValidatePlanningRequestDigest(planningRequestDigest) != nil {
		return StatelessAgentCallIntent{}, errors.New("EXPERIMENT_STATELESS_AGENT_CALL_INTENT_INPUT_INVALID")
	}
	return sealed, nil
}

func (intent StatelessAgentCallIntent) Validate() error {
	if intent.SchemaVersion != StatelessAgentCallIntentVersion || !validMethodToken(intent.ID) ||
		intent.Ordinal <= 0 || !validMethodToken(intent.RootID) ||
		!validSHA256(intent.SearchRequestDigest) || intent.Transport.Validate() != nil ||
		len(intent.PromptBytes) == 0 || len(intent.PromptBytes) > statelessAgentCallMaxBytes ||
		len(intent.RequestBytes) == 0 || len(intent.RequestBytes) > statelessAgentCallMaxBytes ||
		AgentInvocationDigest(intent.PromptBytes) != intent.PromptDigest ||
		AgentInvocationDigest(intent.RequestBytes) != intent.RequestDigest {
		return errors.New("EXPERIMENT_STATELESS_AGENT_CALL_INTENT_INVALID")
	}
	want, err := intent.seal()
	if err != nil || !validSHA256(intent.Digest) || want.Digest != intent.Digest {
		return errors.New("EXPERIMENT_STATELESS_AGENT_CALL_INTENT_DIGEST_MISMATCH")
	}
	return nil
}

// ValidatePlanningRequestDigest binds a journal intent to any already sealed,
// typed planning request without teaching the journal that request's schema.
func (intent StatelessAgentCallIntent) ValidatePlanningRequestDigest(requestDigest string) error {
	if intent.Validate() != nil || !validSHA256(requestDigest) ||
		intent.SearchRequestDigest != requestDigest {
		return errors.New("EXPERIMENT_STATELESS_AGENT_CALL_REQUEST_MISMATCH")
	}
	return nil
}

func (intent StatelessAgentCallIntent) seal() (StatelessAgentCallIntent, error) {
	intent.PromptBytes = append([]byte(nil), intent.PromptBytes...)
	intent.RequestBytes = append([]byte(nil), intent.RequestBytes...)
	intent.Digest = ""
	digest, err := control.CanonicalDigest(intent)
	intent.Digest = digest
	return intent, err
}

type StatelessAgentCallDispatch struct {
	SchemaVersion string `json:"schema_version"`
	IntentDigest  string `json:"intent_digest"`
	Digest        string `json:"digest"`
}

func NewStatelessAgentCallDispatch(intent StatelessAgentCallIntent) (StatelessAgentCallDispatch, error) {
	if intent.Validate() != nil {
		return StatelessAgentCallDispatch{}, errors.New("EXPERIMENT_STATELESS_AGENT_CALL_DISPATCH_INPUT_INVALID")
	}
	dispatch := StatelessAgentCallDispatch{
		SchemaVersion: StatelessAgentCallDispatchVersion,
		IntentDigest:  intent.Digest,
	}
	dispatch.Digest, _ = control.CanonicalDigest(dispatch)
	if dispatch.ValidateIntent(intent) != nil {
		return StatelessAgentCallDispatch{}, errors.New("EXPERIMENT_STATELESS_AGENT_CALL_DISPATCH_INVALID")
	}
	return dispatch, nil
}

func (dispatch StatelessAgentCallDispatch) ValidateIntent(intent StatelessAgentCallIntent) error {
	if intent.Validate() != nil || dispatch.SchemaVersion != StatelessAgentCallDispatchVersion ||
		dispatch.IntentDigest != intent.Digest || !validSHA256(dispatch.Digest) {
		return errors.New("EXPERIMENT_STATELESS_AGENT_CALL_DISPATCH_INVALID")
	}
	want := dispatch
	want.Digest = ""
	digest, err := control.CanonicalDigest(want)
	if err != nil || digest != dispatch.Digest {
		return errors.New("EXPERIMENT_STATELESS_AGENT_CALL_DISPATCH_DIGEST_MISMATCH")
	}
	return nil
}

// StatelessAgentCallResult is terminal. Completed means both provider output
// and the strict frontier-permutation contract were accepted. ContentReady
// means exact provider output is durable but a higher-level typed contract owns
// acceptance. Rejected keeps exact negative evidence; failed records transport
// or pre-transport failure without authorizing a retry.
type StatelessAgentCallResult struct {
	SchemaVersion       string                 `json:"schema_version"`
	DispatchDigest      string                 `json:"dispatch_digest"`
	Status              string                 `json:"status"`
	FailureCode         string                 `json:"failure_code,omitempty"`
	Content             []byte                 `json:"content,omitempty"`
	ContentDigest       string                 `json:"content_digest,omitempty"`
	ProposalDigest      string                 `json:"proposal_digest,omitempty"`
	ResponseDigest      string                 `json:"response_digest,omitempty"`
	Response            *AgentResponseIdentity `json:"response,omitempty"`
	DurationMillis      int64                  `json:"duration_millis"`
	Work                ModelWork              `json:"work"`
	TransportAttempts   int                    `json:"transport_attempts,omitempty"`
	ProviderUsageStatus string                 `json:"provider_usage_status,omitempty"`
	Digest              string                 `json:"digest"`
}

func NewStatelessAgentCallResult(
	intent StatelessAgentCallIntent,
	dispatch StatelessAgentCallDispatch,
	result StatelessAgentCallResult,
) (StatelessAgentCallResult, error) {
	result.SchemaVersion = StatelessAgentCallResultVersion
	result.DispatchDigest = dispatch.Digest
	result.Content = append([]byte(nil), result.Content...)
	if len(result.Content) > 0 {
		result.ContentDigest = AgentInvocationDigest(result.Content)
	}
	sealed, err := result.seal()
	if err != nil || sealed.ValidateInputs(intent, dispatch) != nil {
		return StatelessAgentCallResult{}, errors.New("EXPERIMENT_STATELESS_AGENT_CALL_RESULT_INPUT_INVALID")
	}
	return sealed, nil
}

func (result StatelessAgentCallResult) ValidateInputs(
	intent StatelessAgentCallIntent,
	dispatch StatelessAgentCallDispatch,
) error {
	if dispatch.ValidateIntent(intent) != nil || result.SchemaVersion != StatelessAgentCallResultVersion ||
		result.DispatchDigest != dispatch.Digest || result.DurationMillis < 0 ||
		result.Work.Calls < 0 || result.Work.Calls > 1 || result.Work.InputTokens < 0 ||
		result.Work.OutputTokens < 0 || result.Work.TotalTokens != result.Work.InputTokens+result.Work.OutputTokens ||
		result.TransportAttempts < 0 || result.TransportAttempts > intent.Transport.MaxRetries+1 ||
		!validProviderUsageStatus(result.ProviderUsageStatus) {
		return errors.New("EXPERIMENT_STATELESS_AGENT_CALL_RESULT_INVALID")
	}
	switch result.Status {
	case StatelessAgentCallCompleted:
		if result.FailureCode != "" || result.Work.Calls != 1 || result.Work.TotalTokens <= 0 ||
			result.Response == nil || !validSHA256(result.ResponseDigest) ||
			len(result.Content) == 0 || len(result.Content) > statelessAgentCallMaxBytes ||
			AgentInvocationDigest(result.Content) != result.ContentDigest || !validSHA256(result.ProposalDigest) {
			return errors.New("EXPERIMENT_STATELESS_AGENT_CALL_COMPLETED_INVALID")
		}
	case StatelessAgentCallContentReady:
		if result.FailureCode != "" || result.Work.Calls != 1 || result.Work.TotalTokens <= 0 ||
			result.Response == nil || !validSHA256(result.ResponseDigest) ||
			len(result.Content) == 0 || len(result.Content) > statelessAgentCallMaxBytes ||
			AgentInvocationDigest(result.Content) != result.ContentDigest || result.ProposalDigest != "" {
			return errors.New("EXPERIMENT_STATELESS_AGENT_CALL_CONTENT_READY_INVALID")
		}
	case StatelessAgentCallRejected:
		if !validMethodToken(result.FailureCode) || result.Work.Calls != 1 || result.Work.TotalTokens <= 0 ||
			result.Response == nil || !validSHA256(result.ResponseDigest) || len(result.Content) == 0 ||
			AgentInvocationDigest(result.Content) != result.ContentDigest {
			return errors.New("EXPERIMENT_STATELESS_AGENT_CALL_REJECTED_INVALID")
		}
	case StatelessAgentCallFailed:
		if !validMethodToken(result.FailureCode) || len(result.Content) != 0 || result.ContentDigest != "" ||
			result.ProposalDigest != "" || result.Response != nil {
			return errors.New("EXPERIMENT_STATELESS_AGENT_CALL_FAILED_INVALID")
		}
	default:
		return errors.New("EXPERIMENT_STATELESS_AGENT_CALL_RESULT_STATUS_INVALID")
	}
	want, err := result.seal()
	if err != nil || !validSHA256(result.Digest) || want.Digest != result.Digest {
		return errors.New("EXPERIMENT_STATELESS_AGENT_CALL_RESULT_DIGEST_MISMATCH")
	}
	return nil
}

func validProviderUsageStatus(status string) bool {
	return status == "" || status == "unknown" || status == "observed"
}

func (result StatelessAgentCallResult) seal() (StatelessAgentCallResult, error) {
	result.Content = append([]byte(nil), result.Content...)
	result.Digest = ""
	digest, err := control.CanonicalDigest(result)
	result.Digest = digest
	return result, err
}
