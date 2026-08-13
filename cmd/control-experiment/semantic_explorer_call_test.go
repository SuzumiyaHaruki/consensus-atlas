package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

type a2b2FixtureProjector struct {
	preferred control.ActionID
}

func (a2b2FixtureProjector) ID() string { return "a2b2-fixture-projector-v1" }

func (projector a2b2FixtureProjector) Project(
	id string,
	spec semantic.RiskWitnessSpec,
	trace controlruntime.Trace,
) (semantic.RiskWitnessResult, error) {
	milestones := make([]semantic.RiskWitnessMilestoneEvidence, 0, 1)
	for _, record := range trace.Records {
		if record.Action.ID != projector.preferred {
			continue
		}
		digest, err := control.CanonicalDigest(record)
		if err != nil {
			return semantic.RiskWitnessResult{}, err
		}
		milestones = append(milestones, semantic.RiskWitnessMilestoneEvidence{
			MilestoneID: "preferred-prefix", Step: record.Step,
			Kind: "trace-action", EvidenceDigest: digest,
		})
		break
	}
	return semantic.NewRiskWitnessResult(
		id, spec, trace.ManifestDigest, trace.Digest, projector.ID(), milestones,
	)
}

func TestA2b2SemanticExplorerUsesDurableProviderJournalAndRecoversExactContent(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := controlexperiment.RuntimeConfig{
		SeedHex: "613262322d73656d616e7469632d6a6f75726e616c", MaxClones: 1,
	}
	seed, err := hex.DecodeString(runtimeConfig.SeedHex)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := controlruntime.New(ctx, fixture.New(), controlruntime.Config{Seed: seed, MaxClones: 1})
	if err != nil {
		t.Fatal(err)
	}
	root, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	envelope := &controlexperiment.FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1}
	frontier, _, err := controlexperiment.ReconstructActionFrontierView(
		ctx, "a2b2-root-frontier", root, 0, runtimeConfig, envelope, factory,
	)
	if err != nil || len(frontier.Actions) < 2 {
		t.Fatalf("fixture root has no useful frontier: %#v/%v", frontier, err)
	}
	riskSpec, err := semantic.NewRiskWitnessSpec(
		"a2b2-risk", "fixture-cft", "semantic-prefix-risk",
		[]string{"preferred-prefix", "confirmed-prefix"},
		[]semantic.RiskWitnessOrder{{Before: "preferred-prefix", After: "confirmed-prefix"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	knowledge, err := controlexperiment.NewProtocolKnowledgePack(controlexperiment.ProtocolKnowledgePack{
		ID: "a2b2-knowledge", Family: riskSpec.FamilyID, Protocol: "fixture-consensus",
		Knowledge: []controlexperiment.KnowledgeStatement{{
			ID: "semantic-progress", Text: "Prefer bounded prefixes with relevant semantic progress.",
		}},
		Risks: []controlexperiment.ProtocolRisk{{
			ID: riskSpec.RiskID, Summary: "Fixture semantic prefix risk.",
			RequiredCapabilities: []string{"natural-time"},
			RequiredActions:      []control.ActionKind{control.ActionCrash, control.ActionFireTemporal},
			AllowedBackendIDs: []string{
				controlexperiment.StatelessSearchBoundedDepthFirst,
				controlexperiment.SemanticBestFirstAlgorithmID,
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	hypothesis, err := controlexperiment.NewTestHypothesis(
		"a2b2-hypothesis", knowledge, riskSpec,
		"Compare bounded candidates using only public semantic progress.",
		controlexperiment.SemanticBestFirstAlgorithmID,
	)
	if err != nil {
		t.Fatal(err)
	}
	searchSpec, err := controlexperiment.NewStatelessDFSSpec(
		"a2b2-search", root, runtimeConfig, envelope, 2, len(frontier.Actions)+1, 12000,
	)
	if err != nil {
		t.Fatal(err)
	}

	var current controlexperiment.SemanticExplorerAgentView
	views := make([]controlexperiment.SemanticExplorerAgentView, 0, 2)
	transportCalls := 0
	client := fixtureOpenRouterIntentClient()
	client.HTTP = agentHTTPDoerFunc(func(request *http.Request) (*http.Response, error) {
		transportCalls++
		if request.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Fatalf("provider dispatch did not use the activated fixture credential")
		}
		content := []byte(`{"unexpected_authority":true}`)
		if transportCalls > 1 {
			ordered := make([]string, len(current.Request.Queue.Candidates))
			for index, candidate := range current.Request.Queue.Candidates {
				ordered[len(ordered)-index-1] = candidate.CandidateID
			}
			content, err = json.Marshal(controlexperiment.SemanticExplorerProposal{
				SchemaVersion: controlexperiment.SemanticExplorerProposalVersion,
				ID:            current.Request.ID, RequestDigest: current.Request.Digest,
				QueueDigest: current.Request.Queue.Digest, OrderedCandidateIDs: ordered,
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		response := a2b2OpenRouterResponse(t, transportCalls, content)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(response))}, nil
	})
	directory := filepath.Join(t.TempDir(), "semantic-attempt")
	journal, err := newSemanticExplorerCallJournal(directory, client, "fixture-key")
	if err != nil || journal.SetRoot("a2b2-root") != nil {
		t.Fatalf("semantic journal setup failed: %#v/%v", journal, err)
	}
	planner := func(
		ctx context.Context,
		view controlexperiment.SemanticExplorerAgentView,
	) ([]byte, controlexperiment.ModelWork, error) {
		current = view
		views = append(views, view)
		if err := journal.ActivateKey("fixture-key"); err != nil {
			return nil, controlexperiment.ModelWork{}, err
		}
		return journal.Planner(ctx, view)
	}
	result, err := controlexperiment.ExploreBoundedSemanticBestFirstWithExplorer(
		ctx, "a2b2-explorer", controlexperiment.SemanticExplorerBudget{MaxCalls: 4, MaxTokens: 100},
		knowledge, hypothesis, searchSpec, root, riskSpec, factory,
		a2b2FixtureProjector{preferred: frontier.Actions[len(frontier.Actions)-1].ActionID}, planner,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Calls) != 2 || transportCalls != 2 ||
		result.Calls[0].Status != controlexperiment.SemanticExplorerCallRejected ||
		result.Calls[1].Status != controlexperiment.SemanticExplorerCallAccepted ||
		result.Calls[1].Request.PriorFeedback == nil ||
		result.ModelWork != (controlexperiment.ModelWork{Calls: 2, InputTokens: 8, OutputTokens: 6, TotalTokens: 14}) {
		t.Fatalf("semantic journal did not preserve rejection/repair: %#v calls=%d", result, transportCalls)
	}
	audits, err := journal.Audits()
	if err != nil || len(audits) != len(result.Calls) {
		t.Fatalf("semantic transport audits missing: %#v/%v", audits, err)
	}
	for index, audit := range audits {
		if audit.Ordinal != index+1 || audit.Status != controlexperiment.StatelessAgentCallContentReady ||
			audit.SearchRequestDigest != result.Calls[index].Request.Digest || audit.Work != result.Calls[index].ModelWork {
			t.Fatalf("semantic audit %d is not request-bound: %#v", index, audit)
		}
	}

	recovered, err := recoverSemanticExplorerCallJournal(directory, client)
	if err != nil || recovered.SetRoot("a2b2-root") != nil {
		t.Fatalf("semantic journal did not recover: %#v/%v", recovered, err)
	}
	for index, view := range views {
		content, work, replayErr := recovered.Planner(ctx, view)
		if replayErr != nil || !bytes.Equal(content, result.Calls[index].ResponseBytes) ||
			work != result.Calls[index].ModelWork {
			t.Fatalf("recovered call %d changed exact evidence: %q/%#v/%v", index, content, work, replayErr)
		}
	}
	if transportCalls != 2 {
		t.Fatalf("recovery repeated provider dispatch: %d", transportCalls)
	}
	if err := result.ValidateSources(
		ctx, root, riskSpec, knowledge, hypothesis, factory,
		a2b2FixtureProjector{preferred: frontier.Actions[len(frontier.Actions)-1].ActionID},
	); err != nil {
		t.Fatalf("semantic result lost planner-free source replay: %v", err)
	}

	failureCalls := 0
	failingClient := fixtureOpenRouterIntentClient()
	failingClient.HTTP = agentHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
		failureCalls++
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"status":"fixture-unavailable"}`))),
		}, nil
	})
	failureDirectory := filepath.Join(t.TempDir(), "semantic-failed-attempt")
	failedJournal, err := newSemanticExplorerCallJournal(failureDirectory, failingClient, "fixture-key")
	if err != nil || failedJournal.SetRoot("a2b2-failed-root") != nil {
		t.Fatalf("failed semantic journal setup failed: %#v/%v", failedJournal, err)
	}
	if _, work, failureErr := failedJournal.Planner(ctx, views[0]); failureErr == nil ||
		work != (controlexperiment.ModelWork{Calls: 1}) || failureCalls != 1 {
		t.Fatalf("provider failure was not charged terminally: %#v/%v calls=%d", work, failureErr, failureCalls)
	}
	failureAudits, err := failedJournal.Audits()
	if err != nil || len(failureAudits) != 1 ||
		failureAudits[0].Status != controlexperiment.StatelessAgentCallFailed ||
		failureAudits[0].Work != (controlexperiment.ModelWork{Calls: 1}) {
		t.Fatalf("provider failure audit missing: %#v/%v", failureAudits, err)
	}
	recoveredFailure, err := recoverSemanticExplorerCallJournal(failureDirectory, failingClient)
	if err != nil || recoveredFailure.SetRoot("a2b2-failed-root") != nil {
		t.Fatalf("terminal provider failure did not recover: %#v/%v", recoveredFailure, err)
	}
	if _, work, failureErr := recoveredFailure.Planner(ctx, views[0]); failureErr == nil ||
		work != (controlexperiment.ModelWork{Calls: 1}) || failureCalls != 1 {
		t.Fatalf("recovered failure was retried: %#v/%v calls=%d", work, failureErr, failureCalls)
	}
}

func a2b2OpenRouterResponse(t *testing.T, ordinal int, content []byte) []byte {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"id": "a2b2-response-" + string(rune('0'+ordinal)), "model": openRouterFixtureModel,
		"system_fingerprint": "fixture-a2b2", "choices": []any{map[string]any{
			"index": 0, "message": map[string]any{"role": "assistant", "content": string(content)},
			"finish_reason": "stop",
		}},
		"usage": map[string]int{"prompt_tokens": 4, "completion_tokens": 3, "total_tokens": 7},
	})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
