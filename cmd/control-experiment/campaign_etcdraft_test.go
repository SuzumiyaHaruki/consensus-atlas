package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestEtcdraftM521cRecoversDurablePlanWithoutReplanning(t *testing.T) {
	ctx := context.Background()
	spec, err := newEtcdraftCampaignSpec("etcdraft-planned-campaign-m5-21c", 8, 41)
	if err != nil {
		t.Fatal(err)
	}
	base, err := newEtcdraftCampaignProvider(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	config, err := base.campaignConfig("etcdraft-campaign-m5-21c", 2, 120_000)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "campaign")
	recovered, err := controlexperiment.CreateCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	plannerCalls := 0
	provider := newEtcdraftPlannedCampaignProvider(base, &recovered)
	provider.planner = func(view controlexperiment.CampaignPlannerView) (controlexperiment.GuardedTestIntent, error) {
		plannerCalls++
		return controlexperiment.PlanDeterministicCampaignFixture(view)
	}
	coordinator, err := controlexperiment.NewCampaignCoordinator(&recovered, provider)
	if err != nil {
		t.Fatal(err)
	}
	if head, err := coordinator.Step(ctx); err != nil || head.Sequence != 1 || plannerCalls != 1 {
		t.Fatalf("first planned attempt failed: head=%#v calls=%d err=%v", head, plannerCalls, err)
	}
	next, err := controlexperiment.NewCampaignAttemptRequest(config, recovered.Head)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := provider.prepare(next)
	if err != nil || plannerCalls != 2 || len(recovered.PlannedAttempts) != 2 {
		t.Fatalf("next plan was not durable: plan=%#v calls=%d err=%v", pending, plannerCalls, err)
	}

	resumed, err := controlexperiment.RecoverCampaignDirectory(directory, config)
	if err != nil || resumed.Head.Sequence != 1 || len(resumed.PlannedAttempts) != 2 {
		t.Fatalf("interrupted plan did not recover: %#v/%v", resumed, err)
	}
	resumeProvider := newEtcdraftPlannedCampaignProvider(base, &resumed)
	resumeProvider.planner = func(controlexperiment.CampaignPlannerView) (controlexperiment.GuardedTestIntent, error) {
		plannerCalls++
		return controlexperiment.GuardedTestIntent{}, errors.New("planner must not run")
	}
	coordinator, err = controlexperiment.NewCampaignCoordinator(&resumed, resumeProvider)
	if err != nil {
		t.Fatal(err)
	}
	head, err := coordinator.Run(ctx)
	if err != nil || head.Sequence != 2 || plannerCalls != 2 ||
		head.StopReason != controlexperiment.CampaignStopAttemptLimit {
		t.Fatalf("resume replanned or failed: head=%#v calls=%d err=%v", head, plannerCalls, err)
	}

	checked, err := controlexperiment.RecoverCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	for index, planned := range checked.PlannedAttempts {
		record := checked.Checkpoints[index+1].Record
		if record == nil || record.InputDigest != planned.Digest {
			t.Fatalf("attempt %d did not bind its durable plan", index+1)
		}
		encoded, err := os.ReadFile(filepath.Join(directory, "artifacts", record.ArtifactDigest+".artifact"))
		if err != nil {
			t.Fatal(err)
		}
		var artifact etcdraftCampaignArtifact
		if err := json.Unmarshal(encoded, &artifact); err != nil {
			t.Fatal(err)
		}
		bound := base
		bound.intent, bound.plan, bound.plannedAttemptDigest = planned.Proposal, planned.Plan, planned.Digest
		if err := artifact.validate(planned.View.Request, bound); err != nil ||
			artifact.Choice.Digest != planned.Choice.Digest {
			t.Fatalf("attempt %d artifact lost planned binding: %#v/%v", index+1, artifact, err)
		}
	}
	observation, err := newEtcdraftCampaignObservation(&checked, base)
	if err != nil || observation.Terminal.Completed != 2 || observation.PSS == nil ||
		observation.PSS.EvidenceAttempts != 2 {
		t.Fatalf("completed planned campaign did not project: %#v/%v", observation, err)
	}
}

func TestEtcdraftCampaignObservationUsesStrictArtifactJSON(t *testing.T) {
	if _, err := decodeEtcdraftCampaignArtifact([]byte(`{"unexpected":true}`)); err == nil {
		t.Fatal("campaign observation accepted an unknown artifact field")
	}
	if _, err := decodeEtcdraftCampaignArtifact([]byte(`{} {}`)); err == nil {
		t.Fatal("campaign observation accepted trailing JSON")
	}
}

func TestEtcdraftM521d2RecoversResultWithoutSecondTransport(t *testing.T) {
	ctx := context.Background()
	calls := 0
	var content []byte
	provider, config, recovered, directory := newEtcdraftModelCampaignFixture(t, func(
		_ context.Context, prepared deepSeekPreparedRequest,
	) (deepSeekCall, error) {
		calls++
		return deepSeekCall{
			PromptDigest: prepared.PromptDigest, RequestDigest: prepared.RequestDigest,
			ResponseDigest: controlexperiment.AgentInvocationDigest([]byte("offline-response")),
			Response: &controlexperiment.AgentResponseIdentity{
				ID: "offline-call-1", Model: deepSeekV4Flash, FinishReason: "stop",
			},
			Content: content, DurationMillis: 1,
			Work: controlexperiment.ModelWork{Calls: 1, InputTokens: 4, OutputTokens: 3, TotalTokens: 7},
		}, nil
	})
	request, err := controlexperiment.NewCampaignAttemptRequest(config, recovered.Head)
	if err != nil {
		t.Fatal(err)
	}
	view, err := provider.planned.plannerView(request)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := controlexperiment.PlanDeterministicCampaignFixture(view)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Digest = ""
	content, err = json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := provider.prepareRequest(view)
	if err != nil {
		t.Fatal(err)
	}
	var messages []deepSeekMessage
	if err := json.Unmarshal(prepared.PromptBytes, &messages); err != nil || len(messages) != 2 ||
		!strings.Contains(messages[0].Content, "proposal_template") ||
		!strings.Contains(messages[1].Content, `"backend_ids": []`) ||
		!strings.Contains(messages[1].Content, `"actions": []`) ||
		!strings.Contains(messages[1].Content, `"allowed_backend_ids"`) ||
		strings.Contains(messages[1].Content, `"backend_id":`) {
		t.Fatalf("campaign request omitted the exact proposal contract: %#v/%v", messages, err)
	}
	result, err := provider.resolveCall(ctx, request, view, prepared)
	if err != nil || calls != 1 || len(recovered.PlannedAttempts) != 0 {
		t.Fatalf("result was not isolated before plan: calls=%d result=%#v err=%v", calls, result, err)
	}

	resumed, err := controlexperiment.RecoverCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	provider.planned.recovered = &resumed
	provider.transport = func(context.Context, deepSeekPreparedRequest) (deepSeekCall, error) {
		calls++
		return deepSeekCall{}, errors.New("transport must not run")
	}
	coordinator, err := controlexperiment.NewCampaignCoordinator(&resumed, provider)
	if err != nil {
		t.Fatal(err)
	}
	head, err := coordinator.Step(ctx)
	if err != nil || calls != 1 || head.Sequence != 1 || len(resumed.PlannedAttempts) != 1 {
		t.Fatalf("durable result did not resume: calls=%d head=%#v err=%v", calls, head, err)
	}
	planned := resumed.PlannedAttempts[0]
	if planned.PlanningWork != result.Work || head.Totals.Model != result.Work || head.Record == nil ||
		head.Record.Work.Model != result.Work {
		t.Fatalf("model work was not charged exactly once: plan=%#v head=%#v", planned.PlanningWork, head)
	}
	encoded, err := resumed.ReadAttemptArtifact(1)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := decodeEtcdraftCampaignArtifact(encoded)
	if err != nil || artifact.Work.Model != result.Work || artifact.Report == nil || artifact.Bundle == nil ||
		artifact.Report.Work.Model != (controlexperiment.ModelWork{}) ||
		artifact.Bundle.Work.Model != (controlexperiment.ModelWork{}) {
		t.Fatalf("artifact work boundary drifted: artifact=%#v err=%v", artifact, err)
	}
}

func TestEtcdraftM521d2DoesNotRetryAmbiguousOrFailedCall(t *testing.T) {
	t.Run("ambiguous", func(t *testing.T) {
		calls := 0
		provider, config, recovered, directory := newEtcdraftModelCampaignFixture(t, func(
			context.Context, deepSeekPreparedRequest,
		) (deepSeekCall, error) {
			calls++
			return deepSeekCall{}, nil
		})
		request, _ := controlexperiment.NewCampaignAttemptRequest(config, recovered.Head)
		view, _ := provider.planned.plannerView(request)
		prepared, _ := provider.prepareRequest(view)
		intent, err := controlexperiment.NewCampaignModelCallIntent(
			"etcdraft-model-call-1", request, view.Digest, provider.freeze,
			prepared.PromptBytes, prepared.RequestBytes,
		)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = recovered.PrepareModelCall(intent); err != nil {
			t.Fatal(err)
		}
		if _, err = recovered.DispatchModelCall(); err != nil {
			t.Fatal(err)
		}
		resumed, err := controlexperiment.RecoverCampaignDirectory(directory, config)
		if err != nil {
			t.Fatal(err)
		}
		provider.planned.recovered = &resumed
		if _, err = provider.prepare(context.Background(), request); err == nil ||
			err.Error() != etcdraftCampaignModelAmbiguous || calls != 0 {
			t.Fatalf("ambiguous call retried: calls=%d err=%v", calls, err)
		}
	})

	t.Run("failed", func(t *testing.T) {
		calls := 0
		provider, config, recovered, directory := newEtcdraftModelCampaignFixture(t, func(
			_ context.Context, prepared deepSeekPreparedRequest,
		) (deepSeekCall, error) {
			calls++
			return deepSeekCall{
				PromptDigest: prepared.PromptDigest, RequestDigest: prepared.RequestDigest,
				FailureCode: deepSeekFailureTransport,
				Work:        controlexperiment.ModelWork{Calls: 1},
			}, nil
		})
		request, _ := controlexperiment.NewCampaignAttemptRequest(config, recovered.Head)
		if _, err := provider.prepare(context.Background(), request); err == nil ||
			err.Error() != etcdraftCampaignModelFailed || calls != 1 {
			t.Fatalf("terminal failure was not recorded: calls=%d err=%v", calls, err)
		}
		resumed, err := controlexperiment.RecoverCampaignDirectory(directory, config)
		if err != nil || resumed.ModelCalls[0].Status != controlexperiment.CampaignModelCallFailed {
			t.Fatalf("failed call did not recover: %#v/%v", resumed.ModelCalls, err)
		}
		provider.planned.recovered = &resumed
		if _, err = provider.prepare(context.Background(), request); err == nil ||
			err.Error() != etcdraftCampaignModelFailed || calls != 1 {
			t.Fatalf("failed call retried: calls=%d err=%v", calls, err)
		}
	})
}

func newEtcdraftModelCampaignFixture(
	t *testing.T,
	transport etcdraftCampaignModelTransport,
) (etcdraftDurableModelCampaignProvider, controlexperiment.CampaignConfig,
	controlexperiment.CampaignRecovery, string) {
	t.Helper()
	baseSpec, err := newEtcdraftCampaignSpec("etcdraft-model-campaign-m5-21d2", 8, 71)
	if err != nil {
		t.Fatal(err)
	}
	base, err := newEtcdraftCampaignProvider(context.Background(), baseSpec)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := newEtcdraftDurableModelCampaignProvider(
		base, nil, defaultDeepSeekIntentClient(), transport,
	)
	if err != nil {
		t.Fatal(err)
	}
	config, err := provider.campaignConfig("etcdraft-model-campaign-m5-21d2", 1, 100, 120_000)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "campaign")
	recovered, err := controlexperiment.CreateCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	provider.planned.recovered = &recovered
	return provider, config, recovered, directory
}
