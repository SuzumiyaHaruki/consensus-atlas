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
	deepSeekProvider         = "deepseek"
	deepSeekV4Flash          = "deepseek-v4-flash"
	deepSeekChatEndpoint     = "https://api.deepseek.com/chat/completions"
	deepSeekMaxResponse      = 2 << 20
	deepSeekDefaultTokens    = 1200
	deepSeekFailureTransport = "AGENT_TRANSPORT_FAILED"
	deepSeekFailureHTTP      = "AGENT_HTTP_STATUS_REJECTED"
	deepSeekFailureResponse  = "AGENT_RESPONSE_REJECTED"
)

type agentHTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type agentKeyReader func(string) (string, error)

type deepSeekIntentClient struct {
	Endpoint        string
	Model           string
	MaxOutputTokens int
	HTTP            agentHTTPDoer
	Now             func() time.Time
}

type deepSeekMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type deepSeekChatRequest struct {
	Model          string            `json:"model"`
	Messages       []deepSeekMessage `json:"messages"`
	ResponseFormat struct {
		Type string `json:"type"`
	} `json:"response_format"`
	Thinking struct {
		Type string `json:"type"`
	} `json:"thinking"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"max_tokens"`
	Stream      bool    `json:"stream"`
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

type deepSeekCall struct {
	PromptDigest   string
	RequestDigest  string
	ResponseDigest string
	Response       *controlexperiment.AgentResponseIdentity
	Content        []byte
	Work           controlexperiment.ModelWork
	DurationMillis int64
	FailureCode    string
}

// deepSeekPreparedRequest freezes exact public bytes before credentials are
// read or provider transport is attempted.
type deepSeekPreparedRequest struct {
	PromptBytes   []byte
	RequestBytes  []byte
	PromptDigest  string
	RequestDigest string
}

func defaultDeepSeekIntentClient() deepSeekIntentClient {
	return deepSeekIntentClient{
		Endpoint: deepSeekChatEndpoint, Model: deepSeekV4Flash,
		MaxOutputTokens: deepSeekDefaultTokens,
		HTTP:            &http.Client{Timeout: 60 * time.Second},
	}
}

func (client deepSeekIntentClient) prepare(
	systemPrompt string,
	userPrompt string,
) (deepSeekPreparedRequest, error) {
	if client.Endpoint != deepSeekChatEndpoint || client.Model != deepSeekV4Flash ||
		client.MaxOutputTokens <= 0 || client.MaxOutputTokens > 4096 ||
		strings.TrimSpace(systemPrompt) == "" || strings.TrimSpace(userPrompt) == "" {
		return deepSeekPreparedRequest{}, errors.New("AGENT_CLIENT_CONFIG_INVALID")
	}
	messages := []deepSeekMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
	requestBody := deepSeekChatRequest{
		Model: client.Model, Messages: messages, Temperature: 0,
		MaxTokens: client.MaxOutputTokens, Stream: false,
	}
	requestBody.ResponseFormat.Type = "json_object"
	requestBody.Thinking.Type = "disabled"
	encodedRequest, err := json.Marshal(requestBody)
	if err != nil {
		return deepSeekPreparedRequest{}, err
	}
	encodedPrompt, err := json.Marshal(messages)
	if err != nil {
		return deepSeekPreparedRequest{}, err
	}
	prepared := deepSeekPreparedRequest{
		PromptBytes: append([]byte(nil), encodedPrompt...), RequestBytes: append([]byte(nil), encodedRequest...),
		PromptDigest:  controlexperiment.AgentInvocationDigest(encodedPrompt),
		RequestDigest: controlexperiment.AgentInvocationDigest(encodedRequest),
	}
	if err := prepared.validate(client); err != nil {
		return deepSeekPreparedRequest{}, err
	}
	return prepared, nil
}

func (prepared deepSeekPreparedRequest) validate(client deepSeekIntentClient) error {
	if len(prepared.PromptBytes) == 0 || len(prepared.RequestBytes) == 0 ||
		controlexperiment.AgentInvocationDigest(prepared.PromptBytes) != prepared.PromptDigest ||
		controlexperiment.AgentInvocationDigest(prepared.RequestBytes) != prepared.RequestDigest {
		return errors.New("AGENT_PREPARED_REQUEST_DIGEST_MISMATCH")
	}
	var messages []deepSeekMessage
	var request deepSeekChatRequest
	if err := json.Unmarshal(prepared.PromptBytes, &messages); err != nil ||
		json.Unmarshal(prepared.RequestBytes, &request) != nil || len(messages) != 2 ||
		request.Model != client.Model || request.ResponseFormat.Type != "json_object" ||
		request.Thinking.Type != "disabled" || request.Temperature != 0 || request.Stream ||
		request.MaxTokens != client.MaxOutputTokens || len(request.Messages) != len(messages) {
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
	prepared deepSeekPreparedRequest,
) (deepSeekCall, error) {
	if client.Endpoint != deepSeekChatEndpoint || client.Model != deepSeekV4Flash ||
		client.MaxOutputTokens <= 0 || client.MaxOutputTokens > 4096 || client.HTTP == nil ||
		strings.TrimSpace(key) == "" {
		return deepSeekCall{}, errors.New("AGENT_CLIENT_CONFIG_INVALID")
	}
	if err := prepared.validate(client); err != nil {
		return deepSeekCall{}, err
	}
	call := deepSeekCall{
		PromptDigest: prepared.PromptDigest, RequestDigest: prepared.RequestDigest,
		Work: controlexperiment.ModelWork{Calls: 1},
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, client.Endpoint, bytes.NewReader(prepared.RequestBytes),
	)
	if err != nil {
		return deepSeekCall{}, errors.New("AGENT_REQUEST_CONSTRUCTION_FAILED")
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(key))
	request.Header.Set("Content-Type", "application/json")
	now := client.Now
	if now == nil {
		now = time.Now
	}
	started := now()
	response, transportErr := client.HTTP.Do(request)
	call.DurationMillis = now().Sub(started).Milliseconds()
	if call.DurationMillis < 0 {
		call.DurationMillis = 0
	}
	if transportErr != nil || response == nil {
		call.FailureCode = deepSeekFailureTransport
		return call, nil
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, deepSeekMaxResponse+1))
	if readErr != nil || len(responseBody) > deepSeekMaxResponse {
		call.FailureCode = deepSeekFailureResponse
		return call, nil
	}
	call.ResponseDigest = controlexperiment.AgentInvocationDigest(responseBody)
	if response.StatusCode != http.StatusOK {
		call.FailureCode = deepSeekFailureHTTP
		return call, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	var parsed deepSeekChatResponse
	if err := decoder.Decode(&parsed); err != nil {
		call.FailureCode = deepSeekFailureResponse
		return call, nil
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		call.FailureCode = deepSeekFailureResponse
		return call, nil
	}
	if parsed.ID == "" || parsed.Model != client.Model || len(parsed.Choices) != 1 ||
		parsed.Choices[0].Index != 0 || parsed.Choices[0].Message.Role != "assistant" ||
		parsed.Choices[0].FinishReason == "" || parsed.Usage.PromptTokens < 0 ||
		parsed.Usage.CompletionTokens < 0 ||
		parsed.Usage.TotalTokens != parsed.Usage.PromptTokens+parsed.Usage.CompletionTokens {
		call.FailureCode = deepSeekFailureResponse
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
		call.FailureCode = deepSeekFailureResponse
		return call, nil
	}
	call.Content = []byte(content)
	return call, nil
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
