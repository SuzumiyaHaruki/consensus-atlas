package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
