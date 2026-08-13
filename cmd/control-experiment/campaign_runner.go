package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	campaignOutputPendingPrefix = ".campaign-output-pending-"
)

type etcdraftCampaignRunOptions struct {
	Directory              string
	SummaryOut             string
	ObservationOut         string
	Resume                 bool
	Attempts               int
	WallClockCeilingMillis int64
}

func prepareEtcdraftCampaignPaths(
	options etcdraftCampaignRunOptions,
) (string, string, string, error) {
	if options.Directory == "" || options.SummaryOut == "" || options.ObservationOut == "" {
		return "", "", "", errors.New("ETCDRAFT_CAMPAIGN_RUNNER_PATH_REQUIRED")
	}
	directory, err := filepath.Abs(filepath.Clean(options.Directory))
	if err != nil || directory == string(filepath.Separator) || directory == "." {
		return "", "", "", errors.New("ETCDRAFT_CAMPAIGN_RUNNER_DIRECTORY_INVALID")
	}
	summaryOut, err := prepareEtcdraftCampaignOutputPath(directory, options.SummaryOut, "SUMMARY")
	if err != nil {
		return "", "", "", err
	}
	observationOut, err := prepareEtcdraftCampaignOutputPath(
		directory, options.ObservationOut, "OBSERVATION",
	)
	if err != nil {
		return "", "", "", err
	}
	if summaryOut == observationOut {
		return "", "", "", errors.New("ETCDRAFT_CAMPAIGN_RUNNER_OUTPUT_PATH_COLLISION")
	}
	info, err := os.Lstat(directory)
	if options.Resume {
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", "", "", errors.New("ETCDRAFT_CAMPAIGN_RUNNER_RESUME_DIRECTORY_INVALID")
		}
	} else if err == nil {
		return "", "", "", errors.New("ETCDRAFT_CAMPAIGN_RUNNER_NEW_DIRECTORY_REQUIRED")
	} else if !os.IsNotExist(err) {
		return "", "", "", fmt.Errorf("ETCDRAFT_CAMPAIGN_RUNNER_DIRECTORY_STAT: %w", err)
	}
	return directory, summaryOut, observationOut, nil
}

func prepareEtcdraftCampaignOutputPath(directory string, path string, kind string) (string, error) {
	clean, err := filepath.Abs(filepath.Clean(path))
	if err != nil || clean == string(filepath.Separator) || clean == "." {
		return "", fmt.Errorf("ETCDRAFT_CAMPAIGN_RUNNER_%s_PATH_INVALID", kind)
	}
	relative, err := filepath.Rel(directory, clean)
	if err != nil || relative == "." ||
		(relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return "", fmt.Errorf("ETCDRAFT_CAMPAIGN_RUNNER_%s_INSIDE_CAMPAIGN", kind)
	}
	if _, err := os.Lstat(clean); err == nil {
		return "", fmt.Errorf("ETCDRAFT_CAMPAIGN_RUNNER_%s_EXISTS", kind)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("ETCDRAFT_CAMPAIGN_RUNNER_%s_STAT: %w", kind, err)
	}
	if err := os.MkdirAll(filepath.Dir(clean), 0o700); err != nil {
		return "", fmt.Errorf("ETCDRAFT_CAMPAIGN_RUNNER_%s_PARENT_CREATE: %w", kind, err)
	}
	parent, err := os.Lstat(filepath.Dir(clean))
	if err != nil || !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("ETCDRAFT_CAMPAIGN_RUNNER_%s_PARENT_INVALID", kind)
	}
	return clean, nil
}

func persistCampaignSummaryNoReplace(
	path string,
	summary controlexperiment.CampaignSummary,
) error {
	if err := summary.Validate(); err != nil {
		return err
	}
	return persistCampaignOutputNoReplace(path, summary, "SUMMARY")
}

func persistCampaignOutputNoReplace(path string, value any, kind string) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, campaignOutputPendingPrefix)
	if err != nil {
		return fmt.Errorf("ETCDRAFT_CAMPAIGN_%s_TEMP_CREATE: %w", kind, err)
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
			return fmt.Errorf("ETCDRAFT_CAMPAIGN_RUNNER_%s_EXISTS", kind)
		}
		return fmt.Errorf("ETCDRAFT_CAMPAIGN_%s_COMMIT: %w", kind, err)
	}
	if err := syncCampaignSummaryDirectory(directory); err != nil {
		return err
	}
	if err := os.Remove(temporaryPath); err != nil {
		return fmt.Errorf("ETCDRAFT_CAMPAIGN_%s_TEMP_REMOVE: %w", kind, err)
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
