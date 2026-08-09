package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestEtcdraftM519dRunnerCreatesResumesAndPersistsFailureSummary(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	newDirectory := filepath.Join(root, "new", "campaign")
	newSummary := filepath.Join(root, "new", "summary.json")
	args := []string{
		"-strategy", etcdraftCampaignRunnerStrategy,
		"-campaign-dir", newDirectory,
		"-campaign-attempts", "2",
		"-campaign-wall-clock-ms", "120000",
		"-decisions", "8",
		"-policy-seed", "61",
		"-out", newSummary,
	}
	var stdout bytes.Buffer
	if err := run(ctx, args, &stdout); err != nil {
		t.Fatal(err)
	}
	created := readEtcdraftCampaignSummary(t, newSummary)
	if created.Status != controlexperiment.CampaignSummaryStatusStopped ||
		created.StopReason != controlexperiment.CampaignStopAttemptLimit ||
		created.Sequence != 2 || created.Totals.Primary.SchedulerDecisions != 16 ||
		created.Totals.Primary.WorkUnits != 18 || created.Totals.Replay.WorkUnits != 18 ||
		!strings.Contains(stdout.String(), "status=stopped attempts=2") {
		t.Fatalf("new runner summary drifted: %#v stdout=%q", created, stdout.String())
	}

	existingOut := filepath.Join(root, "new", "second-summary.json")
	existingArgs := append([]string(nil), args...)
	existingArgs[len(existingArgs)-1] = existingOut
	if err := run(ctx, existingArgs, &bytes.Buffer{}); err == nil ||
		!strings.Contains(err.Error(), "NEW_DIRECTORY_REQUIRED") {
		t.Fatalf("non-resume runner took over an existing directory: %v", err)
	}
	otherDirectory := filepath.Join(root, "must-not-start", "campaign")
	overwriteArgs := append([]string(nil), args...)
	overwriteArgs[3] = otherDirectory
	if err := run(ctx, overwriteArgs, &bytes.Buffer{}); err == nil ||
		!strings.Contains(err.Error(), "SUMMARY_EXISTS") {
		t.Fatalf("runner overwrote a summary: %v", err)
	}
	if _, err := os.Lstat(otherDirectory); !os.IsNotExist(err) {
		t.Fatalf("summary preflight failure still created Campaign: %v", err)
	}

	resumeDirectory := filepath.Join(root, "resume", "campaign")
	resumeSummary := filepath.Join(root, "resume", "summary.json")
	spec, provider, config := etcdraftCampaignRunnerFixture(t, ctx, 8, 71, 2, 120_000)
	_ = spec
	recovered, err := controlexperiment.CreateCampaignDirectory(resumeDirectory, config)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := controlexperiment.NewCampaignCoordinator(&recovered, provider)
	if err != nil {
		t.Fatal(err)
	}
	first, err := coordinator.Step(ctx)
	if err != nil || first.Sequence != 1 || first.StopReason != controlexperiment.CampaignStopRunning {
		t.Fatalf("resume fixture did not stop after one attempt: %#v/%v", first, err)
	}
	resumeArgs := []string{
		"-strategy", etcdraftCampaignRunnerStrategy,
		"-campaign-dir", resumeDirectory,
		"-campaign-resume",
		"-campaign-attempts", "2",
		"-campaign-wall-clock-ms", "120000",
		"-decisions", "8",
		"-policy-seed", "71",
		"-out", resumeSummary,
	}
	if err := run(ctx, resumeArgs, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	resumed := readEtcdraftCampaignSummary(t, resumeSummary)
	if resumed.Status != controlexperiment.CampaignSummaryStatusStopped || resumed.Sequence != 2 ||
		resumed.Attempts[0].CheckpointDigest != first.Digest {
		t.Fatalf("explicit resume did not continue the same head: %#v", resumed)
	}

	failureOptions := etcdraftCampaignRunOptions{
		Directory:  filepath.Join(root, "failed", "campaign"),
		SummaryOut: filepath.Join(root, "failed", "summary.json"),
		Attempts:   1, DecisionsPerAttempt: 8, FirstPolicySeed: 81,
		WallClockCeilingMillis: 120_000,
	}
	failureFactory := func(
		ctx context.Context,
		spec etcdraftCampaignSpec,
	) (etcdraftCampaignProvider, error) {
		provider, err := newEtcdraftCampaignProvider(ctx, spec)
		if err != nil {
			return etcdraftCampaignProvider{}, err
		}
		provider.execute = func(
			context.Context, string, int, uint64, bool,
		) (controlexperiment.Report, controlexperiment.ExecutionBundle, error) {
			return controlexperiment.Report{}, controlexperiment.ExecutionBundle{},
				errors.New("private fixture provider diagnostic")
		}
		return provider, nil
	}
	err = runEtcdraftCampaignWithFactory(
		ctx, failureOptions, &bytes.Buffer{}, failureFactory,
	)
	if err == nil || !strings.Contains(err.Error(), "PROVIDER_FAILED") {
		t.Fatalf("artifact-less provider failure was hidden: %v", err)
	}
	failed := readEtcdraftCampaignSummary(t, failureOptions.SummaryOut)
	if failed.Status != controlexperiment.CampaignSummaryStatusFailed || failed.Sequence != 0 ||
		failed.Failure == nil || failed.Failure.Code != controlexperiment.CampaignFailureProvider {
		t.Fatalf("failed runner summary drifted: %#v", failed)
	}
	encoded, err := os.ReadFile(failureOptions.SummaryOut)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("private fixture provider diagnostic")) {
		t.Fatal("failed runner summary leaked private provider diagnostic")
	}
}

func etcdraftCampaignRunnerFixture(
	t *testing.T,
	ctx context.Context,
	decisions int,
	seed uint64,
	attempts int,
	wallMillis int64,
) (etcdraftCampaignSpec, etcdraftCampaignProvider, controlexperiment.CampaignConfig) {
	t.Helper()
	spec, err := newEtcdraftCampaignSpec("etcdraft-offline-campaign-spec-v1", decisions, seed)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := newEtcdraftCampaignProvider(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	config, err := provider.campaignConfig("etcdraft-offline-campaign-v1", attempts, wallMillis)
	if err != nil {
		t.Fatal(err)
	}
	return spec, provider, config
}

func readEtcdraftCampaignSummary(
	t *testing.T,
	path string,
) controlexperiment.CampaignSummary {
	t.Helper()
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var summary controlexperiment.CampaignSummary
	if err := json.Unmarshal(encoded, &summary); err != nil {
		t.Fatal(err)
	}
	if err := summary.Validate(); err != nil {
		t.Fatal(err)
	}
	return summary
}
