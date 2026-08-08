package controlexperiment

import (
	"strings"
	"testing"
)

func TestAgentInvocationAuditSealsProgressiveStages(t *testing.T) {
	digest := strings.Repeat("a", 64)
	base := AgentInvocationAudit{
		ID: "one-shot", ViewDigest: digest, Provider: "deepseek",
		Endpoint: "https://api.deepseek.com/chat/completions", Model: "deepseek-v4-flash",
		Thinking: "disabled", Temperature: 0, MaxOutputTokens: 1200,
		PromptDigest: digest, RequestDigest: digest, DurationMillis: 1,
		Work: WorkLedger{Model: ModelWork{Calls: 1}},
	}
	transport := base
	transport.Status, transport.FailureCode = AgentInvocationTransportFailed, "AGENT_TRANSPORT_FAILED"
	sealed, err := NewAgentInvocationAudit(transport)
	if err != nil {
		t.Fatal(err)
	}
	if err := sealed.Validate(); err != nil {
		t.Fatal(err)
	}

	completed := base
	completed.Status = AgentInvocationCompleted
	completed.ResponseDigest = digest
	completed.Response = &AgentResponseIdentity{ID: "response-1", Model: "deepseek-v4-flash", FinishReason: "stop"}
	completed.IntentDigest, completed.PlanDigest = digest, digest
	completed.ReportDigest, completed.BundleDigest = digest, digest
	completed.CompilerWork = &IntentCompilerWork{CandidatesEvaluated: 2, PreferenceChecks: 2, WorkUnits: 4}
	completed.Work = AgentInvocationWork(WorkLedger{
		Primary: PhaseWork{SetupAttempts: 1, RuntimeInitializations: 1, SchedulerDecisions: 1, WorkUnits: 2},
		Replay:  PhaseWork{SetupAttempts: 1, RuntimeInitializations: 1, SchedulerDecisions: 1, WorkUnits: 2},
	}, ModelWork{Calls: 1, InputTokens: 10, OutputTokens: 5, TotalTokens: 15})
	sealed, err = NewAgentInvocationAudit(completed)
	if err != nil {
		t.Fatal(err)
	}
	if err := sealed.Validate(); err != nil {
		t.Fatal(err)
	}
	tampered := sealed
	tampered.Work.Model.TotalTokens++
	if err := tampered.Validate(); err == nil || !strings.Contains(err.Error(), "METHOD_WORK_INVALID") {
		t.Fatalf("tampered token accounting error = %v", err)
	}
}

func TestAgentInvocationAuditRejectsStageAuthorityExpansion(t *testing.T) {
	digest := strings.Repeat("a", 64)
	audit := AgentInvocationAudit{
		ID: "rejected", Status: AgentInvocationIntentRejected,
		ViewDigest: digest, Provider: "deepseek", Endpoint: "https://api.deepseek.com/chat/completions",
		Model: "deepseek-v4-flash", Thinking: "disabled", MaxOutputTokens: 1200,
		PromptDigest: digest, RequestDigest: digest, ResponseDigest: digest,
		Response:    &AgentResponseIdentity{ID: "response-1", Model: "deepseek-v4-flash", FinishReason: "stop"},
		FailureCode: "AGENT_INTENT_REJECTED", Work: WorkLedger{Model: ModelWork{Calls: 1}},
		ReportDigest: digest,
	}
	if _, err := NewAgentInvocationAudit(audit); err == nil ||
		!strings.Contains(err.Error(), "STATUS_INVALID") {
		t.Fatalf("rejected invocation accepted an execution artifact: %v", err)
	}
}
