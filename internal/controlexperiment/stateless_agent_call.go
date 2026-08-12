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
	StatelessAgentCallFailed          = "failed"
	StatelessAgentCallRejected        = "proposal-rejected"
	statelessAgentCallMaxBytes        = 1 << 20
)

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

func NewStatelessAgentCallIntent(
	id string,
	ordinal int,
	rootID string,
	request StatelessFrontierOrderRequest,
	transport AgentTransportFreeze,
	prompt []byte,
	payload []byte,
) (StatelessAgentCallIntent, error) {
	intent := StatelessAgentCallIntent{
		SchemaVersion: StatelessAgentCallIntentVersion,
		ID:            id, Ordinal: ordinal, RootID: rootID,
		SearchRequestDigest: request.Digest, Transport: transport,
		PromptBytes:   append([]byte(nil), prompt...),
		PromptDigest:  AgentInvocationDigest(prompt),
		RequestBytes:  append([]byte(nil), payload...),
		RequestDigest: AgentInvocationDigest(payload),
	}
	sealed, err := intent.seal()
	if err != nil || sealed.ValidateRequest(request) != nil {
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

func (intent StatelessAgentCallIntent) ValidateRequest(request StatelessFrontierOrderRequest) error {
	if intent.Validate() != nil || request.Validate() != nil ||
		intent.SearchRequestDigest != request.Digest {
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
// and the strict frontier-permutation contract were accepted. Rejected keeps
// the exact model content as negative evidence; failed records transport or
// pre-transport failure without authorizing a retry.
type StatelessAgentCallResult struct {
	SchemaVersion  string                 `json:"schema_version"`
	DispatchDigest string                 `json:"dispatch_digest"`
	Status         string                 `json:"status"`
	FailureCode    string                 `json:"failure_code,omitempty"`
	Content        []byte                 `json:"content,omitempty"`
	ContentDigest  string                 `json:"content_digest,omitempty"`
	ProposalDigest string                 `json:"proposal_digest,omitempty"`
	ResponseDigest string                 `json:"response_digest,omitempty"`
	Response       *AgentResponseIdentity `json:"response,omitempty"`
	DurationMillis int64                  `json:"duration_millis"`
	Work           ModelWork              `json:"work"`
	Digest         string                 `json:"digest"`
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
		result.Work.OutputTokens < 0 || result.Work.TotalTokens != result.Work.InputTokens+result.Work.OutputTokens {
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

func (result StatelessAgentCallResult) seal() (StatelessAgentCallResult, error) {
	result.Content = append([]byte(nil), result.Content...)
	result.Digest = ""
	digest, err := control.CanonicalDigest(result)
	result.Digest = digest
	return result, err
}
