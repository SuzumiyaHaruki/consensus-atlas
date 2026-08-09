package controlexperiment

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCampaignSummaryIndexesCommittedAttemptsWithoutCopyingArtifacts(t *testing.T) {
	config := campaignTestConfig(t, "summary-stopped", strings.Repeat("a", 64), CampaignLogicalBudget{
		MaxAttempts: 2, MaxPrimarySchedulerDecisions: 10,
		MaxPrimaryWorkUnits: 12, MaxReplayWorkUnits: 12,
	}, 1_000)
	directory := filepath.Join(t.TempDir(), "campaign")
	recovered, err := CreateCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	running, err := NewCampaignSummary(&recovered)
	if err != nil || running.Status != CampaignSummaryStatusRunning || running.Sequence != 0 ||
		len(running.Attempts) != 0 {
		t.Fatalf("initial running summary drifted: %#v/%v", running, err)
	}

	artifacts := [][]byte{
		[]byte("large-payload-must-not-be-copied/attempt-one"),
		[]byte("large-payload-must-not-be-copied/attempt-two"),
	}
	for index, artifact := range artifacts {
		record := campaignStoreTestRecord(
			t, index+1, "summary-attempt-"+string(rune('1'+index)), artifact,
		)
		if _, err := recovered.CommitAttempt(record, artifact, int64(index+1)*10); err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			middle, err := NewCampaignSummary(&recovered)
			if err != nil || middle.Status != CampaignSummaryStatusRunning || middle.Sequence != 1 {
				t.Fatalf("mid-campaign summary drifted: %#v/%v", middle, err)
			}
		}
	}
	summary, err := NewCampaignSummary(&recovered)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != CampaignSummaryStatusStopped ||
		summary.StopReason != CampaignStopAttemptLimit || summary.Sequence != 2 ||
		summary.Totals.Primary.SchedulerDecisions != 10 ||
		summary.Totals.Primary.WorkUnits != 12 || summary.Totals.Replay.WorkUnits != 12 {
		t.Fatalf("stopped summary drifted: %#v", summary)
	}
	for ordinal, want := range artifacts {
		got, err := recovered.ReadAttemptArtifact(ordinal + 1)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("artifact %d = %q/%v, want %q", ordinal+1, got, err, want)
		}
	}
	for _, ordinal := range []int{-1, 0, 3} {
		if _, err := recovered.ReadAttemptArtifact(ordinal); err == nil ||
			!strings.Contains(err.Error(), "ORDINAL_INVALID") {
			t.Fatalf("artifact ordinal %d was accepted: %v", ordinal, err)
		}
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("large-payload-must-not-be-copied")) {
		t.Fatal("summary copied artifact payload")
	}
	read, err := ReadCampaignSummary(directory, config)
	if err != nil || read.Digest != summary.Digest {
		t.Fatalf("summary reader drifted: %#v/%v", read, err)
	}

	tampered := summary
	tampered.Attempts = cloneCampaignAttemptSummaries(summary.Attempts)
	tampered.Attempts[1].PreviousCheckpointDigest = strings.Repeat("f", 64)
	if err := tampered.Validate(); err == nil || !strings.Contains(err.Error(), "CHAIN_INVALID") {
		t.Fatalf("tampered summary chain remained valid: %v", err)
	}
	tampered = summary
	tampered.Totals.Primary.WorkUnits--
	if err := tampered.Validate(); err == nil || !strings.Contains(err.Error(), "TOTAL_MISMATCH") {
		t.Fatalf("tampered summary totals remained valid: %v", err)
	}

	recovered.Head.Digest = strings.Repeat("e", 64)
	if _, err := recovered.ReadAttemptArtifact(1); err == nil ||
		!strings.Contains(err.Error(), "RECOVERY_TOKEN_INVALID") {
		t.Fatalf("modified recovery token read an artifact: %v", err)
	}
	checked, err := RecoverCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	checked.Head.Sequence = -1
	if _, err := checked.ReadAttemptArtifact(1); err == nil ||
		!strings.Contains(err.Error(), "RECOVERY_TOKEN_INVALID") {
		t.Fatalf("invalid recovery sequence read an artifact: %v", err)
	}
}

func TestCampaignSummaryRepresentsDurableFailureWithoutTerminalAttempt(t *testing.T) {
	config := campaignStoreTestConfig(t, "summary-failed")
	directory := filepath.Join(t.TempDir(), "campaign")
	recovered, err := CreateCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewCampaignAttemptRequest(config, recovered.Head)
	if err != nil {
		t.Fatal(err)
	}
	marker, err := recovered.FailAttempt(request, CampaignFailureProvider)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := NewCampaignSummary(&recovered)
	if err != nil || summary.Status != CampaignSummaryStatusFailed || summary.Sequence != 0 ||
		summary.StopReason != CampaignStopRunning || summary.Failure == nil ||
		summary.Failure.Digest != marker.Digest || summary.Totals != emptyWork() {
		t.Fatalf("failed summary drifted: %#v/%v", summary, err)
	}
	if err := summary.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("provider diagnostic")) {
		t.Fatal("failed summary leaked a provider diagnostic")
	}
}

func TestCampaignArtifactReaderRejectsChangedOrOversizedCommittedFile(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(string) error
	}{
		{name: "changed", change: func(path string) error {
			return os.WriteFile(path, []byte("changed"), 0o600)
		}},
		{name: "oversized", change: func(path string) error {
			return os.Truncate(path, campaignMaxArtifactBytes+1)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, directory, recovered := newCampaignStoreFixture(t, "reader-"+test.name)
			artifact := []byte("committed artifact")
			record := campaignStoreTestRecord(t, 1, "reader-attempt", artifact)
			if _, err := recovered.CommitAttempt(record, artifact, 1); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(
				directory, campaignArtifactsDir, record.ArtifactDigest+campaignArtifactSuffix,
			)
			if err := test.change(path); err != nil {
				t.Fatal(err)
			}
			if _, err := recovered.ReadAttemptArtifact(1); err == nil ||
				!strings.Contains(err.Error(), "ARTIFACT_") {
				t.Fatalf("%s artifact was readable: %v", test.name, err)
			}
			if _, err := RecoverCampaignDirectory(directory, config); err == nil {
				t.Fatalf("%s artifact survived full recovery", test.name)
			}
		})
	}
}
