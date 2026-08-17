package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	openRouterProvider               = "openrouter"
	openRouterChatEndpoint           = "https://openrouter.ai/api/v1/chat/completions"
	openRouterMaxResponse            = 2 << 20
	openRouterDefaultTokens          = 32000
	openRouterMaxOutputTokens        = 32000
	openRouterDefaultReasoningEffort = "high"
	openRouterDefaultTimeout         = 900 * time.Second
	scenarioAgentMaxOutputTokens     = 32000
	agentFailureTransport            = "AGENT_TRANSPORT_FAILED"
	agentFailureHTTP                 = "AGENT_HTTP_STATUS_REJECTED"
	agentFailureResponse             = "AGENT_RESPONSE_REJECTED"
	agentProviderUsageUnknown        = "unknown"
	agentProviderUsageObserved       = "observed"
)

func validateAgentKeyFileName(value string) bool {
	return strings.TrimSpace(value) != "" && !strings.ContainsAny(value, "\r\n\x00")
}

type agentHTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type agentKeyReader func(string) (string, error)

// agentIntentTransport is the narrow provider boundary used by durable Agent
// journals. Provider-specific request and response formats stay behind it.
type agentIntentTransport interface {
	prepare(string, string, openRouterStructuredOutput) (agentPreparedRequest, error)
	invokePrepared(context.Context, string, agentPreparedRequest) (agentCall, error)
	freeze() controlexperiment.AgentTransportFreeze
	ready() bool
}

type openRouterIntentClient struct {
	Endpoint         string
	Model            string
	ReasoningEffort  string
	ExcludeReasoning bool
	MaxOutputTokens  int
	MaxRetries       int
	RequestTimeout   time.Duration
	HTTP             agentHTTPDoer
	Now              func() time.Time
}

func (client openRouterIntentClient) ready() bool {
	return client.HTTP != nil && client.freeze().Validate() == nil
}

func (client openRouterIntentClient) freeze() controlexperiment.AgentTransportFreeze {
	requestTimeout := client.RequestTimeout
	if requestTimeout == 0 {
		requestTimeout = openRouterDefaultTimeout
	}
	return controlexperiment.AgentTransportFreeze{
		Provider: openRouterProvider, Endpoint: client.Endpoint, Model: client.Model,
		Thinking: client.ReasoningEffort, ExcludeReasoning: client.ExcludeReasoning,
		StructuredOutputMode: "json-schema", Stream: false,
		RequestTimeoutMS: requestTimeout.Milliseconds(),
		RoutingPolicy:    "openrouter-default", AllowProviderFallback: true,
		Temperature: 0, MaxOutputTokens: client.MaxOutputTokens,
		MaxCallsPerArm: 1, MaxRetries: client.MaxRetries,
	}
}

type openRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openRouterChatRequest struct {
	Model          string              `json:"model"`
	Messages       []openRouterMessage `json:"messages"`
	ResponseFormat struct {
		Type       string `json:"type"`
		JSONSchema struct {
			Name   string          `json:"name"`
			Strict bool            `json:"strict"`
			Schema json.RawMessage `json:"schema"`
		} `json:"json_schema"`
	} `json:"response_format"`
	Reasoning struct {
		Effort  string `json:"effort"`
		Exclude bool   `json:"exclude"`
	} `json:"reasoning"`
	Provider struct {
		RequireParameters bool `json:"require_parameters"`
	} `json:"provider"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"max_tokens"`
	Stream      bool    `json:"stream"`
}

type openRouterStructuredOutput struct {
	Name   string
	Schema json.RawMessage
}

type openRouterChatResponse struct {
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

type agentCall struct {
	PromptDigest      string
	RequestDigest     string
	ResponseDigest    string
	Response          *controlexperiment.AgentResponseIdentity
	Content           []byte
	Work              controlexperiment.ModelWork
	DurationMillis    int64
	TransportAttempts int
	UsageStatus       string
	FailureCode       string
}

// agentPreparedRequest freezes exact public bytes before credentials are
// read or provider transport is attempted.
type agentPreparedRequest struct {
	PromptBytes   []byte
	RequestBytes  []byte
	PromptDigest  string
	RequestDigest string
}

func newOpenRouterIntentClient(model string) openRouterIntentClient {
	return openRouterIntentClient{
		Endpoint: openRouterChatEndpoint, Model: strings.TrimSpace(model),
		ReasoningEffort: openRouterDefaultReasoningEffort, ExcludeReasoning: true,
		MaxOutputTokens: openRouterDefaultTokens, MaxRetries: 0,
		RequestTimeout: openRouterDefaultTimeout,
		HTTP:           &http.Client{Timeout: openRouterDefaultTimeout},
	}
}

func newAgentIntentTransport(provider string, model string) (agentIntentTransport, error) {
	switch strings.TrimSpace(provider) {
	case "", openRouterProvider:
		client := newOpenRouterIntentClient(model)
		if !client.ready() {
			return nil, errors.New("AGENT_CLIENT_CONFIG_INVALID")
		}
		return client, nil
	case deepSeekProvider:
		client := newDeepSeekIntentClient(model)
		if !client.ready() {
			return nil, errors.New("AGENT_CLIENT_CONFIG_INVALID")
		}
		return client, nil
	default:
		return nil, errors.New("AGENT_CLIENT_PROVIDER_UNSUPPORTED")
	}
}

func newScenarioAgentIntentTransport(provider string, model string) (agentIntentTransport, error) {
	transport, err := newAgentIntentTransport(provider, model)
	if err != nil {
		return nil, err
	}
	return configureAgentIntentTransport(
		transport, "high", false, scenarioAgentMaxOutputTokens, 0,
	)
}

func configureAgentIntentTransport(
	transport agentIntentTransport,
	reasoning string,
	excludeReasoning bool,
	maxOutputTokens int,
	maxRetries int,
) (agentIntentTransport, error) {
	switch client := transport.(type) {
	case openRouterIntentClient:
		client.ReasoningEffort = reasoning
		client.ExcludeReasoning = excludeReasoning
		client.MaxOutputTokens = maxOutputTokens
		client.MaxRetries = maxRetries
		if !client.ready() {
			return nil, errors.New("AGENT_CLIENT_CONFIG_INVALID")
		}
		return client, nil
	case deepSeekIntentClient:
		client.ReasoningEffort = reasoning
		client.MaxOutputTokens = maxOutputTokens
		client.MaxRetries = maxRetries
		if !client.ready() {
			return nil, errors.New("AGENT_CLIENT_CONFIG_INVALID")
		}
		return client, nil
	default:
		return nil, errors.New("AGENT_CLIENT_PROVIDER_UNSUPPORTED")
	}
}

func (client openRouterIntentClient) prepare(
	systemPrompt string,
	userPrompt string,
	output openRouterStructuredOutput,
) (agentPreparedRequest, error) {
	if client.Endpoint != openRouterChatEndpoint || !validOpenRouterModelID(client.Model) ||
		!validOpenRouterReasoningEffort(client.ReasoningEffort) ||
		client.MaxOutputTokens <= 0 || client.MaxOutputTokens > openRouterMaxOutputTokens ||
		client.MaxRetries != 0 ||
		strings.TrimSpace(systemPrompt) == "" || strings.TrimSpace(userPrompt) == "" ||
		!validOpenRouterStructuredOutput(output.Name, output.Schema) {
		return agentPreparedRequest{}, errors.New("AGENT_CLIENT_CONFIG_INVALID")
	}
	messages := []openRouterMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
	requestBody := openRouterChatRequest{
		Model: client.Model, Messages: messages, Temperature: 0,
		MaxTokens: client.MaxOutputTokens, Stream: false,
	}
	requestBody.ResponseFormat.Type = "json_schema"
	requestBody.ResponseFormat.JSONSchema.Name = output.Name
	requestBody.ResponseFormat.JSONSchema.Strict = true
	requestBody.ResponseFormat.JSONSchema.Schema = append(json.RawMessage(nil), output.Schema...)
	requestBody.Reasoning.Effort = client.ReasoningEffort
	requestBody.Reasoning.Exclude = client.ExcludeReasoning
	requestBody.Provider.RequireParameters = true
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
	if err := prepared.validate(client); err != nil {
		return agentPreparedRequest{}, err
	}
	return prepared, nil
}

func (prepared agentPreparedRequest) validate(client openRouterIntentClient) error {
	if len(prepared.PromptBytes) == 0 || len(prepared.RequestBytes) == 0 ||
		controlexperiment.AgentInvocationDigest(prepared.PromptBytes) != prepared.PromptDigest ||
		controlexperiment.AgentInvocationDigest(prepared.RequestBytes) != prepared.RequestDigest {
		return errors.New("AGENT_PREPARED_REQUEST_DIGEST_MISMATCH")
	}
	var messages []openRouterMessage
	var request openRouterChatRequest
	if err := json.Unmarshal(prepared.PromptBytes, &messages); err != nil ||
		json.Unmarshal(prepared.RequestBytes, &request) != nil || len(messages) != 2 ||
		request.Model != client.Model || request.ResponseFormat.Type != "json_schema" ||
		!validOpenRouterStructuredOutput(
			request.ResponseFormat.JSONSchema.Name, request.ResponseFormat.JSONSchema.Schema,
		) || !request.ResponseFormat.JSONSchema.Strict ||
		request.Reasoning.Effort != client.ReasoningEffort ||
		request.Reasoning.Exclude != client.ExcludeReasoning || !request.Provider.RequireParameters ||
		request.Temperature != 0 || request.Stream ||
		request.MaxTokens != client.MaxOutputTokens || len(request.Messages) != len(messages) {
		return errors.New("AGENT_PREPARED_REQUEST_INVALID")
	}
	encodedMessages, err := json.Marshal(request.Messages)
	if err != nil || !bytes.Equal(encodedMessages, prepared.PromptBytes) {
		return errors.New("AGENT_PREPARED_REQUEST_PROMPT_MISMATCH")
	}
	return nil
}

func (client openRouterIntentClient) invokePrepared(
	ctx context.Context,
	key string,
	prepared agentPreparedRequest,
) (agentCall, error) {
	if client.Endpoint != openRouterChatEndpoint || !validOpenRouterModelID(client.Model) ||
		!validOpenRouterReasoningEffort(client.ReasoningEffort) || client.MaxOutputTokens <= 0 ||
		client.MaxOutputTokens > openRouterMaxOutputTokens || client.HTTP == nil ||
		client.MaxRetries != 0 ||
		strings.TrimSpace(key) == "" {
		return agentCall{}, errors.New("AGENT_CLIENT_CONFIG_INVALID")
	}
	if err := prepared.validate(client); err != nil {
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
	var responseBody []byte
	response, transportErr := client.HTTP.Do(request)
	if transportErr != nil || response == nil {
		// Once a POST has reached Do, completion and provider billing are
		// ambiguous. Never replay it inside the transport.
		call.FailureCode = agentFailureTransport
	} else {
		responseBody, err = io.ReadAll(io.LimitReader(response.Body, openRouterMaxResponse+1))
		closeErr := response.Body.Close()
		if err != nil || closeErr != nil {
			// Headers can arrive before the response body is complete. A body
			// read/close failure therefore has the same unknown-completion and
			// unknown-billing semantics as a failed Do call.
			call.FailureCode = agentFailureTransport
		} else if len(responseBody) > openRouterMaxResponse {
			call.FailureCode = agentFailureResponse
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
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	var parsed openRouterChatResponse
	if err := decoder.Decode(&parsed); err != nil {
		call.FailureCode = agentFailureResponse
		return call, nil
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		call.FailureCode = agentFailureResponse
		return call, nil
	}
	if parsed.ID == "" || strings.TrimSpace(parsed.Model) == "" || len(parsed.Choices) != 1 ||
		parsed.Choices[0].Index != 0 || parsed.Choices[0].Message.Role != "assistant" ||
		parsed.Choices[0].FinishReason == "" || parsed.Usage.PromptTokens < 0 ||
		parsed.Usage.CompletionTokens < 0 ||
		parsed.Usage.TotalTokens != parsed.Usage.PromptTokens+parsed.Usage.CompletionTokens {
		call.FailureCode = agentFailureResponse
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
	if parsed.Choices[0].FinishReason != "stop" || content == "" {
		call.FailureCode = agentFailureResponse
		return call, nil
	}
	call.Content = []byte(content)
	return call, nil
}

func validOpenRouterReasoningEffort(effort string) bool {
	switch effort {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
		return true
	default:
		return false
	}
}

func validOpenRouterStructuredOutput(name string, schema json.RawMessage) bool {
	if name == "" || len(name) > 64 || !json.Valid(schema) || len(schema) == 0 || schema[0] != '{' {
		return false
	}
	for _, character := range name {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') ||
			character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func validOpenRouterModelID(model string) bool {
	if model == "" || model != strings.TrimSpace(model) || strings.ToLower(model) != model ||
		len(model) > 200 || !strings.Contains(model, "/") {
		return false
	}
	for _, character := range model {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') ||
			character == '-' || character == '_' || character == '.' || character == '/' ||
			character == ':' || character == '~' {
			continue
		}
		return false
	}
	return true
}

func readAgentKey(path string) (string, error) {
	if path == "" {
		return "", errors.New("AGENT_KEY_FILE_REQUIRED")
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 ||
		before.Mode().Perm()&0o077 != 0 || before.Size() <= 0 || before.Size() > 16<<10 {
		return "", errors.New("AGENT_KEY_FILE_INVALID")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", errors.New("AGENT_KEY_FILE_OPEN_FAILED")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return "", errors.New("AGENT_KEY_FILE_CHANGED")
	}
	encoded, err := io.ReadAll(io.LimitReader(file, (16<<10)+1))
	if err != nil || len(encoded) > 16<<10 {
		return "", errors.New("AGENT_KEY_FILE_READ_FAILED")
	}
	key := strings.TrimSpace(string(encoded))
	if key == "" || strings.ContainsAny(key, "\r\n\x00") {
		return "", errors.New("AGENT_KEY_FILE_CONTENT_INVALID")
	}
	return key, nil
}
