package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	deepSeekFailureIntent    = "AGENT_INTENT_REJECTED"
	deepSeekFailureCompile   = "AGENT_COMPILE_REJECTED"
	deepSeekFailureExecution = "AGENT_EXECUTION_FAILED"
)

type agentHTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

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

// deepSeekPreparedRequest freezes the exact public prompt and request bytes
// before a key is read or any transport is attempted.
type deepSeekPreparedRequest struct {
	PromptBytes   []byte
	RequestBytes  []byte
	PromptDigest  string
	RequestDigest string
}

type etcdraftAgentOneShot struct {
	View   controlexperiment.AgentSemanticView
	Intent *controlexperiment.GuardedTestIntent
	Plan   *controlexperiment.CompiledIntentPlan
	Report *controlexperiment.Report
	Bundle *controlexperiment.ExecutionBundle
	Audit  controlexperiment.AgentInvocationAudit
}

func defaultDeepSeekIntentClient() deepSeekIntentClient {
	return deepSeekIntentClient{
		Endpoint: deepSeekChatEndpoint, Model: deepSeekV4Flash,
		MaxOutputTokens: deepSeekDefaultTokens,
		HTTP:            &http.Client{Timeout: 60 * time.Second},
	}
}

func executeEtcdraftAgentOneShot(
	ctx context.Context,
	key string,
	client deepSeekIntentClient,
) (etcdraftAgentOneShot, error) {
	inputs, err := newEtcdraftIntentInputs(ctx)
	if err != nil {
		return etcdraftAgentOneShot{}, err
	}
	result := etcdraftAgentOneShot{View: inputs.View}
	call, err := client.invoke(ctx, key, inputs.View)
	if err != nil {
		return result, err
	}
	audit := controlexperiment.AgentInvocationAudit{
		ID: "etcdraft-deepseek-one-shot-m5-18b1", ViewDigest: inputs.View.Digest,
		Provider: deepSeekProvider, Endpoint: client.Endpoint, Model: client.Model,
		Thinking: "disabled", Temperature: 0, MaxOutputTokens: client.MaxOutputTokens,
		PromptDigest: call.PromptDigest, RequestDigest: call.RequestDigest,
		ResponseDigest: call.ResponseDigest, Response: call.Response,
		DurationMillis: call.DurationMillis,
		Work: controlexperiment.AgentInvocationWork(
			controlexperiment.WorkLedger{}, call.Work,
		),
	}
	if call.FailureCode != "" {
		if call.FailureCode == deepSeekFailureTransport {
			audit.Status = controlexperiment.AgentInvocationTransportFailed
		} else {
			audit.Status = controlexperiment.AgentInvocationResponseRejected
		}
		audit.FailureCode = call.FailureCode
		result.Audit, err = controlexperiment.NewAgentInvocationAudit(audit)
		if err != nil {
			return result, err
		}
		return result, errors.New(call.FailureCode)
	}
	intent, err := controlexperiment.ParseGuardedTestIntentProposal(call.Content)
	if err != nil {
		audit.Status = controlexperiment.AgentInvocationIntentRejected
		audit.FailureCode = deepSeekFailureIntent
		result.Audit, err = controlexperiment.NewAgentInvocationAudit(audit)
		if err != nil {
			return result, err
		}
		return result, fmt.Errorf("%s: %w", deepSeekFailureIntent, err)
	}
	result.Intent = &intent
	plan, err := controlexperiment.CompileGuardedTestIntent(
		inputs.View, inputs.Knowledge, inputs.Catalog, inputs.Qualification.Manifest,
		inputs.Qualification.Qualification, intent,
	)
	if err != nil {
		compileErr := err
		audit.Status = controlexperiment.AgentInvocationCompileRejected
		audit.FailureCode = deepSeekFailureCompile
		audit.IntentDigest = intent.Digest
		result.Audit, err = controlexperiment.NewAgentInvocationAudit(audit)
		if err != nil {
			return result, err
		}
		return result, fmt.Errorf("%s: %w", deepSeekFailureCompile, compileErr)
	}
	result.Plan = &plan
	report, bundle, err := executeEtcdraftCompiledIntent(ctx, inputs, intent, plan)
	if err != nil {
		executionErr := err
		audit.Status = controlexperiment.AgentInvocationExecutionFailed
		audit.FailureCode = deepSeekFailureExecution
		audit.IntentDigest, audit.PlanDigest = intent.Digest, plan.Digest
		audit.CompilerWork = &plan.CompilerWork
		result.Audit, err = controlexperiment.NewAgentInvocationAudit(audit)
		if err != nil {
			return result, err
		}
		return result, fmt.Errorf("%s: %w", deepSeekFailureExecution, executionErr)
	}
	result.Report, result.Bundle = &report, &bundle
	audit.Status = controlexperiment.AgentInvocationCompleted
	audit.IntentDigest, audit.PlanDigest = intent.Digest, plan.Digest
	audit.ReportDigest, audit.BundleDigest = report.Digest, bundle.Digest
	audit.CompilerWork = &plan.CompilerWork
	audit.Work = controlexperiment.AgentInvocationWork(report.Work, call.Work)
	result.Audit, err = controlexperiment.NewAgentInvocationAudit(audit)
	if err != nil {
		return result, err
	}
	return result, nil
}

func persistEtcdraftAgentOneShot(directory string, result etcdraftAgentOneShot) error {
	clean := filepath.Clean(directory)
	if directory == "" || clean == "." || clean == string(filepath.Separator) {
		return errors.New("AGENT_ARTIFACT_DIRECTORY_INVALID")
	}
	if err := result.View.Validate(); err != nil {
		return err
	}
	if err := result.Audit.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(clean, 0o755); err != nil {
		return errors.New("AGENT_ARTIFACT_DIRECTORY_NOT_NEW")
	}
	artifacts := []struct {
		name  string
		value any
	}{
		{name: "audit.json", value: result.Audit},
		{name: "view.json", value: result.View},
	}
	if result.Intent != nil {
		if err := result.Intent.Validate(); err != nil {
			return err
		}
		artifacts = append(artifacts, struct {
			name  string
			value any
		}{name: "intent.json", value: *result.Intent})
	}
	if result.Plan != nil {
		if err := result.Plan.Validate(); err != nil {
			return err
		}
		artifacts = append(artifacts, struct {
			name  string
			value any
		}{name: "plan.json", value: *result.Plan})
	}
	if result.Report != nil {
		if err := result.Report.Validate(); err != nil {
			return err
		}
		artifacts = append(artifacts, struct {
			name  string
			value any
		}{name: "report.json", value: *result.Report})
	}
	if result.Bundle != nil {
		if err := result.Bundle.Validate(); err != nil {
			return err
		}
		artifacts = append(artifacts, struct {
			name  string
			value any
		}{name: "bundle.json", value: *result.Bundle})
	}
	for _, artifact := range artifacts {
		encoded, err := json.MarshalIndent(artifact.value, "", "  ")
		if err != nil {
			return err
		}
		path := filepath.Join(clean, artifact.name)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(append(encoded, '\n'))
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func (client deepSeekIntentClient) invoke(
	ctx context.Context,
	key string,
	view controlexperiment.AgentSemanticView,
) (deepSeekCall, error) {
	if err := view.Validate(); err != nil {
		return deepSeekCall{}, err
	}
	if client.Endpoint != deepSeekChatEndpoint || client.Model != deepSeekV4Flash ||
		client.MaxOutputTokens <= 0 || client.MaxOutputTokens > 4096 || client.HTTP == nil ||
		strings.TrimSpace(key) == "" {
		return deepSeekCall{}, errors.New("AGENT_CLIENT_CONFIG_INVALID")
	}
	systemPrompt, userPrompt, err := guardedIntentPrompt(view)
	if err != nil {
		return deepSeekCall{}, err
	}
	prepared, err := client.prepare(systemPrompt, userPrompt)
	if err != nil {
		return deepSeekCall{}, err
	}
	return client.invokePrepared(ctx, key, prepared)
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

func guardedIntentPrompt(view controlexperiment.AgentSemanticView) (string, string, error) {
	viewJSON, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return "", "", err
	}
	if len(view.KnowledgePack.Risks) == 0 || len(view.EligibleBackends) == 0 {
		return "", "", errors.New("AGENT_VIEW_PROMPT_INPUT_INVALID")
	}
	example := controlexperiment.GuardedTestIntent{
		SchemaVersion: controlexperiment.GuardedTestIntentVersion,
		ID:            "fictional-structure-only", ViewDigest: strings.Repeat("0", 64), RiskID: "fictional-risk",
		Must: controlexperiment.IntentMust{
			Decisions: 1, FaultEnvelope: controlexperiment.FaultEnvelope{},
		},
		Prefer: controlexperiment.IntentPrefer{},
	}
	exampleJSON, err := json.MarshalIndent(example, "", "  ")
	if err != nil {
		return "", "", err
	}
	system := "You are a constrained distributed-consensus test-intent planner. " +
		"Return exactly one JSON object and no prose. Use only IDs and limits present in the supplied semantic view. " +
		"Do not add a digest, action ID, node ID, decision sequence, oracle, verdict, candidate identity, or root-cause claim."
	user := "Select one protocol risk and propose one bounded GuardedTestIntent. " +
		"Hard constraints must be satisfiable by one eligible backend. Fault-envelope values are upper bounds and must not exceed that backend's max_fault_envelope. " +
		"Copy the exact view digest from AgentSemanticView, choose a real risk/backend, and create a new lowercase-hyphenated id. " +
		"The following JSON is a structural example only: every fictional value must be replaced and the digest field must remain empty. Do not copy its choices:\n" + string(exampleJSON) +
		"\n\nAgentSemanticView JSON:\n" + string(viewJSON)
	return system, user, nil
}

func guardedFeedbackIntentPrompt(
	view controlexperiment.AgentSemanticView,
	feedback controlexperiment.AgentBatchFeedbackView,
	sources []controlexperiment.AgentBatchFeedbackInput,
	baseline controlexperiment.GuardedTestIntent,
) (string, string, error) {
	if err := feedback.ValidateInputs(view, sources); err != nil {
		return "", "", err
	}
	if err := baseline.Validate(); err != nil {
		return "", "", err
	}
	if baseline.ViewDigest != view.Digest {
		return "", "", errors.New("AGENT_FEEDBACK_BASELINE_VIEW_MISMATCH")
	}
	viewJSON, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return "", "", err
	}
	feedbackJSON, err := json.MarshalIndent(feedback, "", "  ")
	if err != nil {
		return "", "", err
	}
	baselineJSON, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return "", "", err
	}
	system := "You are a constrained distributed-consensus test-intent planner in a preference-only feedback ablation. " +
		"Return exactly one GuardedTestIntent JSON object and no prose. Keep risk_id and every must field exactly equal to the baseline. " +
		"You may change only prefer.backend_ids and prefer.actions, using IDs present in the semantic and feedback views. " +
		"The feedback reports coarse discovery and execution cost, not completeness, correctness, an oracle result, or a defect label. " +
		"Create a new lowercase-hyphenated id and leave digest empty."
	user := "Produce the with-feedback preference proposal. Do not compensate for a failed backend by changing hard constraints or budget.\n\n" +
		"Baseline GuardedTestIntent JSON:\n" + string(baselineJSON) +
		"\n\nAgentBatchFeedbackView JSON:\n" + string(feedbackJSON) +
		"\n\nAgentSemanticView JSON:\n" + string(viewJSON)
	return system, user, nil
}

type preferenceAblationPromptInput struct {
	SemanticView controlexperiment.AgentSemanticView       `json:"agent_semantic_view"`
	Baseline     controlexperiment.GuardedTestIntent       `json:"hard_constraint_baseline"`
	Feedback     *controlexperiment.AgentBatchFeedbackView `json:"agent_batch_feedback"`
}

// guardedPreferenceAblationPrompt builds both b4 arms through one template.
// The no-feedback arm carries an explicit JSON null; the other carries the
// trusted feedback object. All semantic and hard fields are otherwise exact.
func guardedPreferenceAblationPrompt(
	view controlexperiment.AgentSemanticView,
	baseline controlexperiment.GuardedTestIntent,
	feedback *controlexperiment.AgentBatchFeedbackView,
) (string, string, error) {
	if err := view.Validate(); err != nil {
		return "", "", err
	}
	if err := baseline.Validate(); err != nil {
		return "", "", err
	}
	if baseline.ViewDigest != view.Digest || len(baseline.Prefer.BackendIDs) != 0 ||
		len(baseline.Prefer.Actions) != 0 {
		return "", "", errors.New("AGENT_ABLATION_PROMPT_BASELINE_INVALID")
	}
	if feedback == nil {
		// Explicit nil is the entire experimental information difference.
	} else {
		if err := feedback.Validate(); err != nil {
			return "", "", err
		}
		if feedback.SemanticViewDigest != view.Digest {
			return "", "", errors.New("AGENT_ABLATION_PROMPT_FEEDBACK_INPUT_INVALID")
		}
	}
	inputJSON, err := json.MarshalIndent(preferenceAblationPromptInput{
		SemanticView: view, Baseline: baseline, Feedback: feedback,
	}, "", "  ")
	if err != nil {
		return "", "", err
	}
	system := "You are a constrained distributed-consensus preference planner in a two-arm ablation. " +
		"Return exactly one GuardedTestIntent JSON object and no prose. Copy view_digest, risk_id, and every must field exactly from hard_constraint_baseline. " +
		"Create a new lowercase-hyphenated id, leave digest empty, and change only prefer.backend_ids and prefer.actions using eligible values from agent_semantic_view. " +
		"When agent_batch_feedback is null, use only the semantic view. When it is present, treat it only as coarse discovery and cost evidence, not completeness, correctness, an oracle result, or a defect label. " +
		"Do not add action IDs, node IDs, decision sequences, or verdict claims."
	user := "Produce the preference-only proposal from this frozen input:\n" + string(inputJSON)
	return system, user, nil
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
