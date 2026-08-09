package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	etcdraftCampaignRunnerStrategy = "campaign-etcdraft-v1"
	campaignSummaryPendingPrefix   = ".campaign-summary-pending-"
)

type etcdraftCampaignRunOptions struct {
	Directory              string
	SummaryOut             string
	Resume                 bool
	Attempts               int
	DecisionsPerAttempt    int
	FirstPolicySeed        uint64
	WallClockCeilingMillis int64
}

type etcdraftCampaignProviderFactory func(
	context.Context,
	etcdraftCampaignSpec,
) (etcdraftCampaignProvider, error)

func runEtcdraftCampaign(
	ctx context.Context,
	options etcdraftCampaignRunOptions,
	stdout io.Writer,
) error {
	return runEtcdraftCampaignWithFactory(
		ctx, options, stdout, newEtcdraftCampaignProvider,
	)
}

func runEtcdraftCampaignWithFactory(
	ctx context.Context,
	options etcdraftCampaignRunOptions,
	stdout io.Writer,
	newProvider etcdraftCampaignProviderFactory,
) error {
	directory, summaryOut, err := prepareEtcdraftCampaignPaths(options)
	if err != nil {
		return err
	}
	if stdout == nil || newProvider == nil || options.Attempts <= 0 ||
		options.DecisionsPerAttempt <= 0 || options.FirstPolicySeed == 0 ||
		options.WallClockCeilingMillis <= 0 {
		return errors.New("ETCDRAFT_CAMPAIGN_RUNNER_OPTIONS_INVALID")
	}
	spec, err := newEtcdraftCampaignSpec(
		"etcdraft-offline-campaign-spec-v1",
		options.DecisionsPerAttempt,
		options.FirstPolicySeed,
	)
	if err != nil {
		return err
	}
	provider, err := newProvider(ctx, spec)
	if err != nil {
		return err
	}
	config, err := provider.campaignConfig(
		"etcdraft-offline-campaign-v1", options.Attempts, options.WallClockCeilingMillis,
	)
	if err != nil {
		return err
	}
	var recovered controlexperiment.CampaignRecovery
	if options.Resume {
		recovered, err = controlexperiment.RecoverCampaignDirectory(directory, config)
	} else {
		recovered, err = controlexperiment.CreateCampaignDirectory(directory, config)
	}
	if err != nil {
		return err
	}
	var stageErr error
	if recovered.Failure != nil {
		stageErr = errors.New("ETCDRAFT_CAMPAIGN_DURABLY_FAILED")
	} else {
		coordinator, coordinatorErr := controlexperiment.NewCampaignCoordinator(&recovered, provider)
		if coordinatorErr != nil {
			stageErr = coordinatorErr
		} else {
			_, stageErr = coordinator.Run(ctx)
		}
	}
	summary, summaryErr := controlexperiment.NewCampaignSummary(&recovered)
	if summaryErr != nil {
		if stageErr != nil {
			return fmt.Errorf("%v; ETCDRAFT_CAMPAIGN_SUMMARY_FAILED: %w", stageErr, summaryErr)
		}
		return summaryErr
	}
	if err := persistCampaignSummaryNoReplace(summaryOut, summary); err != nil {
		return err
	}
	fmt.Fprintf(
		stdout, "wrote %s\nstatus=%s attempts=%d primary=%d replay=%d digest=%s\n",
		summaryOut, summary.Status, summary.Sequence,
		summary.Totals.Primary.WorkUnits, summary.Totals.Replay.WorkUnits, summary.Digest,
	)
	return stageErr
}

func prepareEtcdraftCampaignPaths(
	options etcdraftCampaignRunOptions,
) (string, string, error) {
	if options.Directory == "" || options.SummaryOut == "" {
		return "", "", errors.New("ETCDRAFT_CAMPAIGN_RUNNER_PATH_REQUIRED")
	}
	directory, err := filepath.Abs(filepath.Clean(options.Directory))
	if err != nil || directory == string(filepath.Separator) || directory == "." {
		return "", "", errors.New("ETCDRAFT_CAMPAIGN_RUNNER_DIRECTORY_INVALID")
	}
	summaryOut, err := filepath.Abs(filepath.Clean(options.SummaryOut))
	if err != nil || summaryOut == string(filepath.Separator) || summaryOut == "." {
		return "", "", errors.New("ETCDRAFT_CAMPAIGN_RUNNER_SUMMARY_PATH_INVALID")
	}
	relative, err := filepath.Rel(directory, summaryOut)
	if err != nil || relative == "." ||
		(relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return "", "", errors.New("ETCDRAFT_CAMPAIGN_RUNNER_SUMMARY_INSIDE_CAMPAIGN")
	}
	if _, err := os.Lstat(summaryOut); err == nil {
		return "", "", errors.New("ETCDRAFT_CAMPAIGN_RUNNER_SUMMARY_EXISTS")
	} else if !os.IsNotExist(err) {
		return "", "", fmt.Errorf("ETCDRAFT_CAMPAIGN_RUNNER_SUMMARY_STAT: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(summaryOut), 0o700); err != nil {
		return "", "", fmt.Errorf("ETCDRAFT_CAMPAIGN_RUNNER_SUMMARY_PARENT_CREATE: %w", err)
	}
	parent, err := os.Lstat(filepath.Dir(summaryOut))
	if err != nil || !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 {
		return "", "", errors.New("ETCDRAFT_CAMPAIGN_RUNNER_SUMMARY_PARENT_INVALID")
	}
	info, err := os.Lstat(directory)
	if options.Resume {
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", "", errors.New("ETCDRAFT_CAMPAIGN_RUNNER_RESUME_DIRECTORY_INVALID")
		}
	} else if err == nil {
		return "", "", errors.New("ETCDRAFT_CAMPAIGN_RUNNER_NEW_DIRECTORY_REQUIRED")
	} else if !os.IsNotExist(err) {
		return "", "", fmt.Errorf("ETCDRAFT_CAMPAIGN_RUNNER_DIRECTORY_STAT: %w", err)
	}
	return directory, summaryOut, nil
}

func persistCampaignSummaryNoReplace(
	path string,
	summary controlexperiment.CampaignSummary,
) error {
	if err := summary.Validate(); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, campaignSummaryPendingPrefix)
	if err != nil {
		return fmt.Errorf("ETCDRAFT_CAMPAIGN_SUMMARY_TEMP_CREATE: %w", err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(encoded); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	closed = true
	if err := os.Link(temporaryPath, path); err != nil {
		if os.IsExist(err) {
			return errors.New("ETCDRAFT_CAMPAIGN_RUNNER_SUMMARY_EXISTS")
		}
		return fmt.Errorf("ETCDRAFT_CAMPAIGN_SUMMARY_COMMIT: %w", err)
	}
	if err := syncCampaignSummaryDirectory(directory); err != nil {
		return err
	}
	if err := os.Remove(temporaryPath); err != nil {
		return fmt.Errorf("ETCDRAFT_CAMPAIGN_SUMMARY_TEMP_REMOVE: %w", err)
	}
	if err := syncCampaignSummaryDirectory(directory); err != nil {
		return err
	}
	return nil
}

func syncCampaignSummaryDirectory(directory string) error {
	opened, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer opened.Close()
	return opened.Sync()
}
