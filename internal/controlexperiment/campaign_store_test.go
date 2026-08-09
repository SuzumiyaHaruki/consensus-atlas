package controlexperiment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCampaignDirectoryCreatesAppendsAndRecovers(t *testing.T) {
	config := campaignStoreTestConfig(t, "persist")
	directory := filepath.Join(t.TempDir(), "campaign")
	created, err := CreateCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	if created.Head.Sequence != 0 || len(created.Checkpoints) != 1 ||
		len(created.OrphanArtifactDigests) != 0 || len(created.PendingRelativeFilePaths) != 0 {
		t.Fatalf("unexpected new campaign: %#v", created)
	}
	if _, err := CreateCampaignDirectory(directory, config); err == nil ||
		!strings.Contains(err.Error(), "DIRECTORY_NOT_NEW") {
		t.Fatalf("existing campaign directory was overwritten: %v", err)
	}

	artifact := []byte(`{"status":"completed","trace":"fixture"}`)
	record := campaignStoreTestRecord(t, 1, "persist-attempt-1", artifact)
	stale := created
	head, err := created.CommitAttempt(record, artifact, 25)
	if err != nil {
		t.Fatal(err)
	}
	if head.Sequence != 1 || head.Record == nil || head.Record.Digest != record.Digest ||
		head.PreviousDigest != stale.Head.Digest {
		t.Fatalf("unexpected committed head: %#v", head)
	}
	recovered, err := RecoverCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Head.Digest != head.Digest || len(recovered.Checkpoints) != 2 ||
		recovered.Head.Totals != record.Work || len(recovered.OrphanArtifactDigests) != 0 {
		t.Fatalf("recovered campaign drifted: %#v", recovered)
	}
	tamperedRecovery := recovered
	tamperedRecovery.Head.Digest = strings.Repeat("f", 64)
	secondArtifact := []byte("second artifact")
	secondRecord := campaignStoreTestRecord(t, 2, "persist-attempt-2", secondArtifact)
	if _, err := tamperedRecovery.CommitAttempt(secondRecord, secondArtifact, 30); err == nil ||
		!strings.Contains(err.Error(), "RECOVERY_TOKEN_INVALID") {
		t.Fatalf("mutated recovery state was allowed to commit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(
		directory, campaignArtifactsDir, record.ArtifactDigest+campaignArtifactSuffix,
	)); err != nil {
		t.Fatalf("artifact was not durable before checkpoint return: %v", err)
	}

	other := campaignStoreTestConfig(t, "persist-other")
	if _, err := RecoverCampaignDirectory(directory, other); err == nil ||
		!strings.Contains(err.Error(), "IDENTITY_MISMATCH") {
		t.Fatalf("resume accepted another campaign identity: %v", err)
	}
	if _, err := stale.CommitAttempt(record, artifact, 30); err == nil ||
		!strings.Contains(err.Error(), "HEAD_STALE") {
		t.Fatalf("committed attempt was overwritten or appended twice: %v", err)
	}
}

func TestCampaignDirectoryRecoversArtifactOnlyInterruption(t *testing.T) {
	config := campaignStoreTestConfig(t, "orphan")
	directory := filepath.Join(t.TempDir(), "campaign")
	created, err := CreateCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	artifact := []byte(`{"status":"failed-before-checkpoint"}`)
	record := campaignStoreTestRecord(t, 1, "orphan-attempt-1", artifact)
	artifactPath := filepath.Join(
		directory, campaignArtifactsDir, record.ArtifactDigest+campaignArtifactSuffix,
	)
	if err := os.WriteFile(artifactPath, artifact, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(directory, campaignPendingPrefix+"root"),
		filepath.Join(directory, campaignCheckpointsDir, campaignPendingPrefix+"checkpoint"),
	} {
		if err := os.WriteFile(path, []byte("interrupted temporary bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	recovered, err := RecoverCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Head.Sequence != 0 || len(recovered.OrphanArtifactDigests) != 1 ||
		recovered.OrphanArtifactDigests[0] != record.ArtifactDigest ||
		len(recovered.PendingRelativeFilePaths) != 2 {
		t.Fatalf("interrupted files changed trusted state: %#v", recovered)
	}
	head, err := created.CommitAttempt(record, artifact, 10)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err = RecoverCampaignDirectory(directory, config)
	if err != nil || head.Sequence != 1 || len(recovered.OrphanArtifactDigests) != 0 ||
		len(recovered.PendingRelativeFilePaths) != 2 {
		t.Fatalf("orphan was not safely reused: head=%#v recovered=%#v err=%v", head, recovered, err)
	}
}

func TestCampaignDirectoryPersistsFailureMarkerAndRejectsFurtherWrites(t *testing.T) {
	config := campaignStoreTestConfig(t, "failure-marker")
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
	if marker.Ordinal != 1 || marker.RequestDigest != request.Digest ||
		marker.Code != CampaignFailureProvider {
		t.Fatalf("failure marker drifted: %#v", marker)
	}
	artifact := []byte("must not be committed after failure")
	record := campaignStoreTestRecord(t, 1, "after-failure", artifact)
	if _, err := recovered.CommitAttempt(record, artifact, 1); err == nil ||
		!strings.Contains(err.Error(), "RECOVERY_TOKEN_INVALID") {
		t.Fatalf("failed recovery remained writable: %v", err)
	}

	checked, err := RecoverCampaignDirectory(directory, config)
	if err != nil || checked.Failure == nil || checked.Failure.Digest != marker.Digest ||
		checked.Head.Sequence != 0 {
		t.Fatalf("failure marker did not recover: %#v/%v", checked, err)
	}
	if _, err := checked.FailAttempt(request, CampaignFailureProvider); err == nil ||
		!strings.Contains(err.Error(), "RECOVERY_TOKEN_INVALID") {
		t.Fatalf("failure marker was replaceable: %v", err)
	}

	path := filepath.Join(directory, campaignFailureFile)
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var tampered CampaignFailureMarker
	if err := json.Unmarshal(encoded, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered.Code = CampaignFailureClock
	encoded, err = campaignJSONBytes(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RecoverCampaignDirectory(directory, config); err == nil ||
		!strings.Contains(err.Error(), "FAILURE_DIGEST_MISMATCH") {
		t.Fatalf("tampered failure marker recovered: %v", err)
	}
}

func TestCampaignDirectoryRejectsUntrustedDiskState(t *testing.T) {
	t.Run("artifact-input-mismatch", func(t *testing.T) {
		config, directory, recovered := newCampaignStoreFixture(t, "input-mismatch")
		artifact := []byte("expected")
		record := campaignStoreTestRecord(t, 1, "input-mismatch-attempt", artifact)
		if _, err := recovered.CommitAttempt(record, []byte("different"), 1); err == nil ||
			!strings.Contains(err.Error(), "ARTIFACT_INPUT_MISMATCH") {
			t.Fatalf("mismatched artifact input was accepted: %v", err)
		}
		recovered, err := RecoverCampaignDirectory(directory, config)
		if err != nil || recovered.Head.Sequence != 0 || len(recovered.OrphanArtifactDigests) != 0 {
			t.Fatalf("failed input changed campaign: %#v/%v", recovered, err)
		}
	})

	t.Run("artifact-tamper", func(t *testing.T) {
		config, directory, recovered := newCampaignStoreFixture(t, "artifact-tamper")
		artifact := []byte("original artifact")
		record := campaignStoreTestRecord(t, 1, "artifact-tamper-attempt", artifact)
		if _, err := recovered.CommitAttempt(record, artifact, 1); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, campaignArtifactsDir, record.ArtifactDigest+campaignArtifactSuffix)
		if err := os.WriteFile(path, []byte("tampered artifact"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := RecoverCampaignDirectory(directory, config); err == nil ||
			!strings.Contains(err.Error(), "ARTIFACT_DIGEST_MISMATCH") {
			t.Fatalf("tampered artifact was accepted: %v", err)
		}
	})

	t.Run("artifact-conflict-and-missing-reference", func(t *testing.T) {
		config, directory, recovered := newCampaignStoreFixture(t, "artifact-conflict")
		artifact := []byte("expected content")
		record := campaignStoreTestRecord(t, 1, "artifact-conflict-attempt", artifact)
		path := filepath.Join(directory, campaignArtifactsDir, record.ArtifactDigest+campaignArtifactSuffix)
		if err := os.WriteFile(path, []byte("conflicting content"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := recovered.CommitAttempt(record, artifact, 1); err == nil ||
			!strings.Contains(err.Error(), "ARTIFACT_CONFLICT") {
			t.Fatalf("conflicting content-addressed artifact was accepted: %v", err)
		}

		config, directory, recovered = newCampaignStoreFixture(t, "artifact-missing")
		artifact = []byte("durable then missing")
		record = campaignStoreTestRecord(t, 1, "artifact-missing-attempt", artifact)
		if _, err := recovered.CommitAttempt(record, artifact, 1); err != nil {
			t.Fatal(err)
		}
		artifactDirectory := filepath.Join(directory, campaignArtifactsDir)
		if err := os.Rename(
			filepath.Join(artifactDirectory, record.ArtifactDigest+campaignArtifactSuffix),
			filepath.Join(artifactDirectory, campaignPendingPrefix+"missing-artifact"),
		); err != nil {
			t.Fatal(err)
		}
		if _, err := RecoverCampaignDirectory(directory, config); err == nil ||
			!strings.Contains(err.Error(), "ARTIFACT_MISSING") {
			t.Fatalf("checkpoint with missing artifact was accepted: %v", err)
		}
	})

	t.Run("checkpoint-tamper", func(t *testing.T) {
		config, directory, recovered := newCampaignStoreFixture(t, "checkpoint-tamper")
		artifact := []byte("checkpoint artifact")
		record := campaignStoreTestRecord(t, 1, "checkpoint-tamper-attempt", artifact)
		head, err := recovered.CommitAttempt(record, artifact, 1)
		if err != nil {
			t.Fatal(err)
		}
		head.ElapsedMillis++
		encoded, err := json.Marshal(head)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, campaignCheckpointsDir, campaignCheckpointFile(1))
		if err := os.WriteFile(path, encoded, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := RecoverCampaignDirectory(directory, config); err == nil ||
			!strings.Contains(err.Error(), "DIGEST_MISMATCH") {
			t.Fatalf("tampered checkpoint was accepted: %v", err)
		}
	})

	t.Run("checkpoint-gap", func(t *testing.T) {
		config, directory, recovered := newCampaignStoreFixture(t, "checkpoint-gap")
		for ordinal := 1; ordinal <= 2; ordinal++ {
			artifact := []byte{byte(ordinal)}
			record := campaignStoreTestRecord(t, ordinal, "checkpoint-gap-attempt-"+string(rune('0'+ordinal)), artifact)
			_, err := recovered.CommitAttempt(record, artifact, int64(ordinal))
			if err != nil {
				t.Fatal(err)
			}
		}
		checkpointDirectory := filepath.Join(directory, campaignCheckpointsDir)
		if err := os.Rename(
			filepath.Join(checkpointDirectory, campaignCheckpointFile(1)),
			filepath.Join(checkpointDirectory, campaignPendingPrefix+"missing-one"),
		); err != nil {
			t.Fatal(err)
		}
		if _, err := RecoverCampaignDirectory(directory, config); err == nil ||
			!strings.Contains(err.Error(), "CHECKPOINT_GAP") {
			t.Fatalf("checkpoint gap was accepted: %v", err)
		}
	})

	t.Run("unknown-file-and-symlink", func(t *testing.T) {
		config, directory, _ := newCampaignStoreFixture(t, "unknown")
		unknown := filepath.Join(directory, "unexpected.txt")
		if err := os.WriteFile(unknown, []byte("unexpected"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := RecoverCampaignDirectory(directory, config); err == nil ||
			!strings.Contains(err.Error(), "UNKNOWN_FILE") {
			t.Fatalf("unknown file was accepted: %v", err)
		}

		config, directory, _ = newCampaignStoreFixture(t, "symlink")
		target := filepath.Join(t.TempDir(), "outside")
		if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
			t.Fatal(err)
		}
		digest := CampaignArtifactDigest([]byte("outside"))
		if err := os.Symlink(
			target, filepath.Join(directory, campaignArtifactsDir, digest+campaignArtifactSuffix),
		); err != nil {
			t.Fatal(err)
		}
		if _, err := RecoverCampaignDirectory(directory, config); err == nil ||
			!strings.Contains(err.Error(), "ARTIFACT_FILE_INVALID") {
			t.Fatalf("artifact symlink was accepted: %v", err)
		}
	})
}

func campaignStoreTestConfig(t *testing.T, id string) CampaignConfig {
	t.Helper()
	return campaignTestConfig(t, id, strings.Repeat("a", 64), CampaignLogicalBudget{
		MaxAttempts: 4, MaxPrimarySchedulerDecisions: 40,
		MaxPrimaryWorkUnits: 48, MaxReplayWorkUnits: 48,
	}, 1_000)
}

func campaignStoreTestRecord(
	t *testing.T,
	ordinal int,
	id string,
	artifact []byte,
) CampaignAttemptRecord {
	t.Helper()
	record, err := NewCampaignAttemptRecord(CampaignAttemptRecord{
		Ordinal: ordinal, ID: id, InputDigest: strings.Repeat("d", 64),
		ArtifactDigest: CampaignArtifactDigest(artifact), Outcome: CampaignAttemptCompleted,
		Work: campaignTestWork(5, 5, 0, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func newCampaignStoreFixture(t *testing.T, id string) (CampaignConfig, string, CampaignRecovery) {
	t.Helper()
	config := campaignStoreTestConfig(t, id)
	directory := filepath.Join(t.TempDir(), "campaign")
	created, err := CreateCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	return config, directory, created
}
