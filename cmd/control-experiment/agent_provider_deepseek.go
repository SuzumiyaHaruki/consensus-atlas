package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	deepSeekProvider     = "deepseek"
	deepSeekChatEndpoint = "https://api.deepseek.com/chat/completions"
	deepSeekDefaultModel = "deepseek-v4-flash"
)

type deepSeekIntentClient struct {
	Endpoint        string
	Model           string
	ReasoningEffort string
	MaxOutputTokens int
	MaxRetries      int
	RequestTimeout  time.Duration
	HTTP            agentHTTPDoer
	Now             func() time.Time
}

type deepSeekChatRequest struct {
	Model          string              `json:"model"`
	Messages       []openRouterMessage `json:"messages"`
	ResponseFormat struct {
		Type string `json:"type"`
	} `json:"response_format"`
	Thinking struct {
		Type string `json:"type"`
	} `json:"thinking"`
	ReasoningEffort string  `json:"reasoning_effort,omitempty"`
	Temperature     float64 `json:"temperature"`
	MaxTokens       int     `json:"max_tokens"`
	Stream          bool    `json:"stream"`
}

type deepSeekChatResponse struct {
	ID                string `json:"id"`
	Model             string `json:"model"`
	SystemFingerprint string `json:"system_fingerprint"`
	Choices           []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func newDeepSeekIntentClient(model string) deepSeekIntentClient {
	if strings.TrimSpace(model) == "" {
		model = deepSeekDefaultModel
	}
	return deepSeekIntentClient{
		Endpoint: deepSeekChatEndpoint, Model: strings.TrimSpace(model),
		ReasoningEffort: openRouterDefaultReasoningEffort,
		MaxOutputTokens: openRouterDefaultTokens, MaxRetries: 0,
		RequestTimeout: openRouterDefaultTimeout,
		HTTP:           &http.Client{Timeout: openRouterDefaultTimeout},
	}
}

func (client deepSeekIntentClient) ready() bool {
	return client.HTTP != nil && client.freeze().Validate() == nil &&
		client.Endpoint == deepSeekChatEndpoint && !strings.Contains(client.Model, "/")
}

func (client deepSeekIntentClient) freeze() controlexperiment.AgentTransportFreeze {
	return controlexperiment.AgentTransportFreeze{
		Provider: deepSeekProvider, Endpoint: client.Endpoint, Model: client.Model,
		Thinking: client.ReasoningEffort, StructuredOutputMode: "json-object",
		RequestTimeoutMS: client.RequestTimeout.Milliseconds(), RoutingPolicy: "provider-fixed",
		AllowProviderFallback: false, Temperature: 0, MaxOutputTokens: client.MaxOutputTokens,
		MaxCallsPerArm: 1, MaxRetries: client.MaxRetries,
	}
}

func (client deepSeekIntentClient) prepare(
	systemPrompt string,
	userPrompt string,
	output openRouterStructuredOutput,
) (agentPreparedRequest, error) {
	if !client.ready() || strings.TrimSpace(systemPrompt) == "" || strings.TrimSpace(userPrompt) == "" ||
		!validOpenRouterStructuredOutput(output.Name, output.Schema) {
		return agentPreparedRequest{}, errors.New("AGENT_CLIENT_CONFIG_INVALID")
	}
	// DeepSeek JSON Output guarantees a JSON object, while ConsensusAtlas keeps
	// schema and semantic validation local. Supplying the exact schema here
	// preserves the same typed planning contract without claiming provider-side
	// strict-schema enforcement.
	systemPrompt += "\nReturn a JSON object conforming to this schema; local validation is authoritative:\n" +
		string(output.Schema)
	messages := []openRouterMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
	requestBody := deepSeekChatRequest{
		Model: client.Model, Messages: messages, Temperature: 0,
		MaxTokens: client.MaxOutputTokens, Stream: false,
	}
	requestBody.ResponseFormat.Type = "json_object"
	requestBody.Thinking.Type, requestBody.ReasoningEffort = deepSeekThinking(client.ReasoningEffort)
	encodedRequest, err := json.Marshal(requestBody)
	if err != nil {
		return agentPreparedRequest{}, err
	}
	encodedPrompt, err := json.Marshal(messages)
	if err != nil {
		return agentPreparedRequest{}, err
	}
	prepared := agentPreparedRequest{
		PromptBytes: append([]byte(nil), encodedPrompt...), RequestBytes: append([]byte(nil), encodedRequest...),
		PromptDigest:  controlexperiment.AgentInvocationDigest(encodedPrompt),
		RequestDigest: controlexperiment.AgentInvocationDigest(encodedRequest),
	}
	if err := prepared.validateDeepSeek(client); err != nil {
		return agentPreparedRequest{}, err
	}
	return prepared, nil
}

func (prepared agentPreparedRequest) validateDeepSeek(client deepSeekIntentClient) error {
	if len(prepared.PromptBytes) == 0 || len(prepared.RequestBytes) == 0 ||
		controlexperiment.AgentInvocationDigest(prepared.PromptBytes) != prepared.PromptDigest ||
		controlexperiment.AgentInvocationDigest(prepared.RequestBytes) != prepared.RequestDigest {
		return errors.New("AGENT_PREPARED_REQUEST_DIGEST_MISMATCH")
	}
	var messages []openRouterMessage
	var request deepSeekChatRequest
	thinking, effort := deepSeekThinking(client.ReasoningEffort)
	if json.Unmarshal(prepared.PromptBytes, &messages) != nil ||
		json.Unmarshal(prepared.RequestBytes, &request) != nil || len(messages) != 2 ||
		request.Model != client.Model || request.ResponseFormat.Type != "json_object" ||
		request.Thinking.Type != thinking || request.ReasoningEffort != effort ||
		request.Temperature != 0 || request.Stream || request.MaxTokens != client.MaxOutputTokens ||
		len(request.Messages) != len(messages) {
		return errors.New("AGENT_PREPARED_REQUEST_INVALID")
	}
	encodedMessages, err := json.Marshal(request.Messages)
	if err != nil || !bytes.Equal(encodedMessages, prepared.PromptBytes) {
		return errors.New("AGENT_PREPARED_REQUEST_PROMPT_MISMATCH")
	}
	return nil
}

func (client deepSeekIntentClient) invokePrepared(
	ctx context.Context,
	key string,
	prepared agentPreparedRequest,
) (agentCall, error) {
	if !client.ready() || strings.TrimSpace(key) == "" {
		return agentCall{}, errors.New("AGENT_CLIENT_CONFIG_INVALID")
	}
	if err := prepared.validateDeepSeek(client); err != nil {
		return agentCall{}, err
	}
	call := agentCall{
		PromptDigest: prepared.PromptDigest, RequestDigest: prepared.RequestDigest,
		Work: controlexperiment.ModelWork{Calls: 1}, UsageStatus: agentProviderUsageUnknown,
	}
	now := client.Now
	if now == nil {
		now = time.Now
	}
	started := now()
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, client.Endpoint, bytes.NewReader(prepared.RequestBytes),
	)
	if err != nil {
		return agentCall{}, errors.New("AGENT_REQUEST_CONSTRUCTION_FAILED")
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(key))
	request.Header.Set("Content-Type", "application/json")
	call.TransportAttempts = 1
	response, transportErr := client.HTTP.Do(request)
	var responseBody []byte
	if transportErr != nil || response == nil {
		call.FailureCode = agentFailureTransport
	} else {
		responseBody, err = io.ReadAll(io.LimitReader(response.Body, openRouterMaxResponse+1))
		closeErr := response.Body.Close()
		if err != nil || closeErr != nil {
			call.FailureCode = agentFailureTransport
		} else if len(responseBody) > openRouterMaxResponse {
			call.FailureCode = agentFailureResponseTooLarge
		} else if response.StatusCode != http.StatusOK {
			call.ResponseDigest = controlexperiment.AgentInvocationDigest(responseBody)
			call.FailureCode = agentFailureHTTP
		} else {
			call.ResponseDigest = controlexperiment.AgentInvocationDigest(responseBody)
		}
	}
	call.DurationMillis = now().Sub(started).Milliseconds()
	if call.DurationMillis < 0 {
		call.DurationMillis = 0
	}
	if call.FailureCode != "" {
		return call, nil
	}
	if len(bytes.TrimSpace(responseBody)) == 0 {
		// A HTTP 200 response without a JSON envelope has no trustworthy usage
		// to reconcile. Keep it distinct from a syntactically malformed body.
		call.FailureCode = agentFailureResponseEmptyContent
		return call, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	var parsed deepSeekChatResponse
	if decoder.Decode(&parsed) != nil {
		call.FailureCode = agentFailureResponseMalformed
		return call, nil
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF || parsed.ID == "" || strings.TrimSpace(parsed.Model) == "" ||
		len(parsed.Choices) != 1 || parsed.Choices[0].Index != 0 ||
		parsed.Choices[0].Message.Role != "assistant" || parsed.Choices[0].FinishReason == "" ||
		parsed.Usage.PromptTokens < 0 || parsed.Usage.CompletionTokens < 0 ||
		parsed.Usage.TotalTokens != parsed.Usage.PromptTokens+parsed.Usage.CompletionTokens {
		call.FailureCode = agentFailureResponseMalformed
		return call, nil
	}
	call.Response = &controlexperiment.AgentResponseIdentity{
		ID: parsed.ID, Model: parsed.Model, FinishReason: parsed.Choices[0].FinishReason,
		SystemFingerprint: parsed.SystemFingerprint,
	}
	call.Work = controlexperiment.ModelWork{
		Calls: 1, InputTokens: parsed.Usage.PromptTokens,
		OutputTokens: parsed.Usage.CompletionTokens, TotalTokens: parsed.Usage.TotalTokens,
	}
	call.UsageStatus = agentProviderUsageObserved
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if parsed.Choices[0].FinishReason == "length" {
		call.FailureCode = agentFailureResponseFinishLength
		return call, nil
	}
	if parsed.Choices[0].FinishReason != "stop" {
		call.FailureCode = agentFailureResponseMalformed
		return call, nil
	}
	if content == "" {
		call.FailureCode = agentFailureResponseEmptyContent
		return call, nil
	}
	call.Content = []byte(content)
	return call, nil
}

func deepSeekThinking(reasoning string) (string, string) {
	switch reasoning {
	case "disabled", "none":
		return "disabled", ""
	case "minimal", "low":
		return "enabled", "low"
	case "max":
		return "enabled", "max"
	default:
		return "enabled", "high"
	}
}
