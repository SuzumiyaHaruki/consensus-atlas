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
	openRouterDefaultTokens          = 4096
	openRouterDefaultReasoningEffort = "high"
	agentFailureTransport            = "AGENT_TRANSPORT_FAILED"
	agentFailureHTTP                 = "AGENT_HTTP_STATUS_REJECTED"
	agentFailureResponse             = "AGENT_RESPONSE_REJECTED"
)

type agentHTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type agentKeyReader func(string) (string, error)

// openRouterIntentClient is the only model transport used by active Agent
// paths. Different model families are selected through Model rather than by
// adding provider-specific clients.
type openRouterIntentClient struct {
	Endpoint         string
	Model            string
	ReasoningEffort  string
	ExcludeReasoning bool
	MaxOutputTokens  int
	MaxRetries       int
	HTTP             agentHTTPDoer
	Now              func() time.Time
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
		MaxOutputTokens: openRouterDefaultTokens, HTTP: &http.Client{Timeout: 60 * time.Second},
	}
}

func (client openRouterIntentClient) prepare(
	systemPrompt string,
	userPrompt string,
	output openRouterStructuredOutput,
) (agentPreparedRequest, error) {
	if client.Endpoint != openRouterChatEndpoint || !validOpenRouterModelID(client.Model) ||
		!validOpenRouterReasoningEffort(client.ReasoningEffort) ||
		client.MaxOutputTokens <= 0 || client.MaxOutputTokens > 4096 ||
		client.MaxRetries < 0 || client.MaxRetries > 2 ||
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
		client.MaxOutputTokens > 4096 || client.HTTP == nil ||
		client.MaxRetries < 0 || client.MaxRetries > 2 ||
		strings.TrimSpace(key) == "" {
		return agentCall{}, errors.New("AGENT_CLIENT_CONFIG_INVALID")
	}
	if err := prepared.validate(client); err != nil {
		return agentCall{}, err
	}
	call := agentCall{
		PromptDigest: prepared.PromptDigest, RequestDigest: prepared.RequestDigest,
		Work: controlexperiment.ModelWork{Calls: 1},
	}
	now := client.Now
	if now == nil {
		now = time.Now
	}
	started := now()
	var responseBody []byte
	for attempt := 0; attempt <= client.MaxRetries; attempt++ {
		request, err := http.NewRequestWithContext(
			ctx, http.MethodPost, client.Endpoint, bytes.NewReader(prepared.RequestBytes),
		)
		if err != nil {
			return agentCall{}, errors.New("AGENT_REQUEST_CONSTRUCTION_FAILED")
		}
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(key))
		request.Header.Set("Content-Type", "application/json")
		call.TransportAttempts++
		response, transportErr := client.HTTP.Do(request)
		if transportErr != nil || response == nil {
			if attempt < client.MaxRetries {
				continue
			}
			call.FailureCode = agentFailureTransport
			break
		}
		responseBody, err = io.ReadAll(io.LimitReader(response.Body, openRouterMaxResponse+1))
		closeErr := response.Body.Close()
		if err != nil || closeErr != nil || len(responseBody) > openRouterMaxResponse {
			if attempt < client.MaxRetries {
				continue
			}
			call.FailureCode = agentFailureResponse
			break
		}
		if response.StatusCode != http.StatusOK {
			if retryableOpenRouterStatus(response.StatusCode) && attempt < client.MaxRetries {
				continue
			}
			call.ResponseDigest = controlexperiment.AgentInvocationDigest(responseBody)
			call.FailureCode = agentFailureHTTP
			break
		}
		call.ResponseDigest = controlexperiment.AgentInvocationDigest(responseBody)
		break
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

func retryableOpenRouterStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests ||
		status >= http.StatusInternalServerError && status <= 599
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
