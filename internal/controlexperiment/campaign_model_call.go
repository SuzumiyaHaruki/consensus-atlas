package controlexperiment

import "errors"

const (
	CampaignModelCallIntentVersion   = "consensus-atlas/campaign-model-call-intent/v1"
	CampaignModelCallDispatchVersion = "consensus-atlas/campaign-model-call-dispatch/v1"
	CampaignModelCallResultVersion   = "consensus-atlas/campaign-model-call-result/v1"
	CampaignModelCallPrepared        = "prepared"
	CampaignModelCallAmbiguous       = "ambiguous"
	CampaignModelCallCompleted       = "completed"
	CampaignModelCallFailed          = "failed"
	campaignModelCallMaxBytes        = 1 << 20
)

type CampaignModelCallIntent struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`
	Ordinal       int    `json:"ordinal"`
	RequestDigest string `json:"campaign_request_digest"`
	ViewDigest    string `json:"planner_view_digest"`
	CallKey       string `json:"call_key"`

	Transport     AgentTransportFreeze `json:"transport"`
	PromptBytes   []byte               `json:"prompt_bytes"`
	PromptDigest  string               `json:"prompt_digest"`
	RequestBytes  []byte               `json:"request_bytes"`
	PayloadDigest string               `json:"request_digest"`
	Digest        string               `json:"digest"`
}

func NewCampaignModelCallIntent(
	id string,
	request CampaignAttemptRequest,
	viewDigest string,
	transport AgentTransportFreeze,
	prompt []byte,
	payload []byte,
) (CampaignModelCallIntent, error) {
	intent := CampaignModelCallIntent{
		SchemaVersion: CampaignModelCallIntentVersion, ID: id, Ordinal: request.Ordinal,
		RequestDigest: request.Digest, ViewDigest: viewDigest, Transport: transport,
		PromptBytes: append([]byte(nil), prompt...), PromptDigest: AgentInvocationDigest(prompt),
		RequestBytes: append([]byte(nil), payload...), PayloadDigest: AgentInvocationDigest(payload),
	}
	intent.CallKey = AgentInvocationDigest([]byte(request.Digest + ":" + intent.PayloadDigest))
	sealed, err := intent.seal()
	if err != nil {
		return CampaignModelCallIntent{}, err
	}
	if err := sealed.ValidateRequest(request); err != nil {
		return CampaignModelCallIntent{}, err
	}
	return sealed, nil
}

func (intent CampaignModelCallIntent) Validate() error {
	if intent.SchemaVersion != CampaignModelCallIntentVersion || !validMethodToken(intent.ID) ||
		intent.Ordinal <= 0 || !validSHA256(intent.RequestDigest) || !validSHA256(intent.ViewDigest) ||
		!validSHA256(intent.CallKey) || !intent.Transport.valid() || len(intent.PromptBytes) == 0 ||
		len(intent.PromptBytes) > campaignModelCallMaxBytes || len(intent.RequestBytes) == 0 ||
		len(intent.RequestBytes) > campaignModelCallMaxBytes ||
		AgentInvocationDigest(intent.PromptBytes) != intent.PromptDigest ||
		AgentInvocationDigest(intent.RequestBytes) != intent.PayloadDigest {
		return errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_INTENT_INVALID")
	}
	sealed, err := intent.seal()
	if err != nil || !validSHA256(intent.Digest) || sealed.Digest != intent.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_INTENT_DIGEST_MISMATCH")
	}
	return nil
}

func (intent CampaignModelCallIntent) ValidateRequest(request CampaignAttemptRequest) error {
	if err := intent.Validate(); err != nil {
		return err
	}
	if err := request.Validate(); err != nil {
		return err
	}
	if intent.Ordinal != request.Ordinal || intent.RequestDigest != request.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_REQUEST_MISMATCH")
	}
	return nil
}

func (intent CampaignModelCallIntent) seal() (CampaignModelCallIntent, error) {
	intent.PromptBytes = append([]byte(nil), intent.PromptBytes...)
	intent.RequestBytes = append([]byte(nil), intent.RequestBytes...)
	intent.Digest = ""
	digest, err := portableJSONDigest(intent)
	intent.Digest = digest
	return intent, err
}

type CampaignModelCallDispatch struct {
	SchemaVersion string `json:"schema_version"`
	IntentDigest  string `json:"intent_digest"`
	Digest        string `json:"digest"`
}

func NewCampaignModelCallDispatch(intent CampaignModelCallIntent) (CampaignModelCallDispatch, error) {
	if err := intent.Validate(); err != nil {
		return CampaignModelCallDispatch{}, err
	}
	dispatch := CampaignModelCallDispatch{
		SchemaVersion: CampaignModelCallDispatchVersion, IntentDigest: intent.Digest,
	}
	sealed, err := dispatch.seal()
	if err != nil {
		return CampaignModelCallDispatch{}, err
	}
	return sealed, sealed.ValidateInputs(intent)
}

func (dispatch CampaignModelCallDispatch) ValidateInputs(intent CampaignModelCallIntent) error {
	if err := intent.Validate(); err != nil {
		return err
	}
	if dispatch.SchemaVersion != CampaignModelCallDispatchVersion || dispatch.IntentDigest != intent.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_DISPATCH_INVALID")
	}
	sealed, err := dispatch.seal()
	if err != nil || !validSHA256(dispatch.Digest) || sealed.Digest != dispatch.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_DISPATCH_DIGEST_MISMATCH")
	}
	return nil
}

func (dispatch CampaignModelCallDispatch) seal() (CampaignModelCallDispatch, error) {
	dispatch.Digest = ""
	digest, err := portableJSONDigest(dispatch)
	dispatch.Digest = digest
	return dispatch, err
}

type CampaignModelCallResult struct {
	SchemaVersion  string                 `json:"schema_version"`
	DispatchDigest string                 `json:"dispatch_digest"`
	Status         string                 `json:"status"`
	FailureCode    string                 `json:"failure_code,omitempty"`
	Content        []byte                 `json:"content,omitempty"`
	ContentDigest  string                 `json:"content_digest,omitempty"`
	ResponseDigest string                 `json:"response_digest,omitempty"`
	Response       *AgentResponseIdentity `json:"response,omitempty"`
	DurationMillis int64                  `json:"duration_millis"`
	Work           ModelWork              `json:"work"`
	Digest         string                 `json:"digest"`
}

func NewCampaignModelCallResult(
	intent CampaignModelCallIntent,
	dispatch CampaignModelCallDispatch,
	result CampaignModelCallResult,
) (CampaignModelCallResult, error) {
	result.SchemaVersion, result.DispatchDigest = CampaignModelCallResultVersion, dispatch.Digest
	result.Content = append([]byte(nil), result.Content...)
	if len(result.Content) > 0 {
		result.ContentDigest = AgentInvocationDigest(result.Content)
	}
	sealed, err := result.seal()
	if err != nil {
		return CampaignModelCallResult{}, err
	}
	if err := sealed.ValidateInputs(intent, dispatch); err != nil {
		return CampaignModelCallResult{}, err
	}
	return sealed, nil
}

func (result CampaignModelCallResult) ValidateInputs(
	intent CampaignModelCallIntent,
	dispatch CampaignModelCallDispatch,
) error {
	if err := dispatch.ValidateInputs(intent); err != nil {
		return err
	}
	if result.SchemaVersion != CampaignModelCallResultVersion || result.DispatchDigest != dispatch.Digest ||
		result.DurationMillis < 0 ||
		result.Work.Calls != 1 || result.Work.InputTokens < 0 || result.Work.OutputTokens < 0 ||
		result.Work.TotalTokens != result.Work.InputTokens+result.Work.OutputTokens {
		return errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_RESULT_INVALID")
	}
	switch result.Status {
	case CampaignModelCallCompleted:
		if result.FailureCode != "" || result.Response == nil || !validSHA256(result.ResponseDigest) ||
			len(result.Content) == 0 || len(result.Content) > campaignModelCallMaxBytes ||
			AgentInvocationDigest(result.Content) != result.ContentDigest || result.Work.TotalTokens <= 0 ||
			result.Response.ID == "" || result.Response.Model == "" || result.Response.FinishReason == "" {
			return errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_RESULT_COMPLETED_INVALID")
		}
	case CampaignModelCallFailed:
		if !validMethodToken(result.FailureCode) || len(result.Content) != 0 || result.ContentDigest != "" ||
			result.Response != nil || (result.ResponseDigest != "" && !validSHA256(result.ResponseDigest)) {
			return errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_RESULT_FAILED_INVALID")
		}
	default:
		return errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_RESULT_STATUS_INVALID")
	}
	sealed, err := result.seal()
	if err != nil || !validSHA256(result.Digest) || sealed.Digest != result.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_RESULT_DIGEST_MISMATCH")
	}
	return nil
}

func (result CampaignModelCallResult) seal() (CampaignModelCallResult, error) {
	result.Content = append([]byte(nil), result.Content...)
	result.Digest = ""
	digest, err := portableJSONDigest(result)
	result.Digest = digest
	return result, err
}

type CampaignModelCallRecovery struct {
	Status   string
	Intent   CampaignModelCallIntent
	Dispatch *CampaignModelCallDispatch
	Result   *CampaignModelCallResult
}

// CampaignModelCallProviderFailure carries only an already-durable result;
// the store remains responsible for binding its accounting evidence.
type CampaignModelCallProviderFailure struct {
	cause  error
	result CampaignModelCallResult
}

func NewCampaignModelCallProviderFailure(
	cause error,
	result CampaignModelCallResult,
) error {
	if cause == nil || !validSHA256(result.Digest) ||
		(result.Status != CampaignModelCallCompleted && result.Status != CampaignModelCallFailed) ||
		result.Work.Calls != 1 || result.Work.InputTokens < 0 || result.Work.OutputTokens < 0 ||
		result.Work.TotalTokens != result.Work.InputTokens+result.Work.OutputTokens {
		return errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_PROVIDER_FAILURE_INVALID")
	}
	return &CampaignModelCallProviderFailure{cause: cause, result: result}
}

func (failure *CampaignModelCallProviderFailure) Error() string { return failure.cause.Error() }
func (failure *CampaignModelCallProviderFailure) Unwrap() error { return failure.cause }
