package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestEtcdraftM519cRunsRecoverableQualifiedCampaign(t *testing.T) {
	ctx := context.Background()
	spec, err := newEtcdraftCampaignSpec("etcdraft-qualified-campaign-m5-19c", 16, 41)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := newEtcdraftCampaignProvider(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	config, err := provider.campaignConfig("etcdraft-campaign-m5-19c", 2, 120_000)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "campaign")
	recovered, err := controlexperiment.CreateCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := controlexperiment.NewCampaignCoordinator(&recovered, provider)
	if err != nil {
		t.Fatal(err)
	}
	first, err := coordinator.Step(ctx)
	if err != nil || first.Sequence != 1 || first.StopReason != controlexperiment.CampaignStopRunning {
		t.Fatalf("first real attempt did not commit: head=%#v err=%v", first, err)
	}

	resumed, err := controlexperiment.RecoverCampaignDirectory(directory, config)
	if err != nil || resumed.Failure != nil || resumed.Head.Digest != first.Digest {
		t.Fatalf("real campaign did not recover: recovered=%#v err=%v", resumed, err)
	}
	coordinator, err = controlexperiment.NewCampaignCoordinator(&resumed, provider)
	if err != nil {
		t.Fatal(err)
	}
	head, err := coordinator.Run(ctx)
	if err != nil || head.Sequence != 2 ||
		head.StopReason != controlexperiment.CampaignStopAttemptLimit {
		t.Fatalf("resumed real campaign did not stop mechanically: head=%#v err=%v", head, err)
	}

	checked, err := controlexperiment.RecoverCampaignDirectory(directory, config)
	if err != nil || checked.Failure != nil || len(checked.Checkpoints) != 3 ||
		checked.Head.Digest != head.Digest {
		t.Fatalf("completed campaign did not recover: recovered=%#v err=%v", checked, err)
	}
	var primaryDecisions, primaryWork, replayWork int
	for ordinal := 1; ordinal <= 2; ordinal++ {
		previous := checked.Checkpoints[ordinal-1]
		request, err := controlexperiment.NewCampaignAttemptRequest(config, previous)
		if err != nil {
			t.Fatal(err)
		}
		record := checked.Checkpoints[ordinal].Record
		if record == nil || record.InputDigest != request.Digest ||
			record.Outcome != controlexperiment.CampaignAttemptCompleted {
			t.Fatalf("attempt %d record drifted: %#v", ordinal, record)
		}
		encoded, err := os.ReadFile(filepath.Join(
			directory, "artifacts", record.ArtifactDigest+".artifact",
		))
		if err != nil {
			t.Fatal(err)
		}
		if controlexperiment.CampaignArtifactDigest(encoded) != record.ArtifactDigest {
			t.Fatalf("attempt %d artifact digest drifted", ordinal)
		}
		var artifact etcdraftCampaignArtifact
		if err := json.Unmarshal(encoded, &artifact); err != nil {
			t.Fatal(err)
		}
		if err := artifact.validate(request, spec, provider.targetIdentityDigest); err != nil {
			t.Fatalf("attempt %d artifact did not revalidate: %v", ordinal, err)
		}
		if artifact.Report == nil || artifact.Bundle == nil ||
			!artifact.Report.Runs[0].Replay.Stable ||
			len(artifact.Bundle.CorePSS) != artifact.Report.Runs[0].ChargedDecisions+1 ||
			artifact.PolicySeed != 40+uint64(ordinal) {
			t.Fatalf("attempt %d lost qualified evidence: %#v", ordinal, artifact)
		}
		primaryDecisions += artifact.Work.Primary.SchedulerDecisions
		primaryWork += artifact.Work.Primary.WorkUnits
		replayWork += artifact.Work.Replay.WorkUnits
	}
	if checked.Head.Totals.Primary.SchedulerDecisions != primaryDecisions ||
		checked.Head.Totals.Primary.WorkUnits != primaryWork ||
		checked.Head.Totals.Replay.WorkUnits != replayWork {
		t.Fatalf("campaign totals do not equal real artifacts: %#v", checked.Head.Totals)
	}
	observation, err := newEtcdraftCampaignObservation(&checked, provider)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Terminal.Status != controlexperiment.CampaignSummaryStatusStopped ||
		observation.Terminal.Completed != 2 || observation.Terminal.Work != checked.Head.Totals ||
		observation.PSS == nil || observation.PSS.EvidenceAttempts != 2 ||
		observation.PSS.TotalDecisions != 32 || observation.PSS.TotalSamples != 34 ||
		observation.PSS.UniqueStates <= 0 || observation.Faults.ObservedAttempts != 2 ||
		observation.Workload.ObservedAttempts != 2 || observation.Workload.Planned != 2 ||
		observation.Workload.Pending != observation.Workload.Planned-observation.Workload.Completed ||
		len(observation.Monitors.Checked) != 2 || observation.Monitors.Checked[0].Count != 2 ||
		observation.Monitors.Checked[1].Count != 2 || len(observation.Monitors.Triggers) != 0 {
		t.Fatalf("campaign observation drifted: %#v", observation)
	}
	observationJSON, err := json.Marshal(observation)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(observationJSON, []byte(`"report"`)) ||
		bytes.Contains(observationJSON, []byte(`"final_snapshot"`)) {
		t.Fatal("campaign observation copied a report or bundle")
	}

	request, err := controlexperiment.NewCampaignAttemptRequest(config, checked.Checkpoints[0])
	if err != nil {
		t.Fatal(err)
	}
	failedProvider := provider
	failedWork := checked.Checkpoints[1].Record.Work
	failedProvider.execute = func(
		context.Context, string, int, uint64, bool,
	) (controlexperiment.Report, controlexperiment.ExecutionBundle, error) {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{},
			&controlexperiment.ExecutionFailure{
				Phase: "policy", Code: "FIXTURE_TYPED_EXECUTION_FAILURE",
				Decision: 7, Work: failedWork,
			}
	}
	failed, err := failedProvider.Attempt(ctx, request)
	if err != nil || failed.Outcome != controlexperiment.CampaignAttemptFailed ||
		failed.Failure == nil || failed.Failure.Decision != 7 || failed.Work != failedWork {
		t.Fatalf("typed execution failure lost terminal evidence: result=%#v err=%v", failed, err)
	}
	var failedArtifact etcdraftCampaignArtifact
	if err := json.Unmarshal(failed.Artifact, &failedArtifact); err != nil {
		t.Fatal(err)
	}
	if err := failedArtifact.validate(request, spec, provider.targetIdentityDigest); err != nil ||
		failedArtifact.Report != nil || failedArtifact.Bundle != nil {
		t.Fatalf("failed terminal artifact did not revalidate: %#v/%v", failedArtifact, err)
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
