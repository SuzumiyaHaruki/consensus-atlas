package defectbench

import (
	"errors"
	"fmt"
)

const SubmissionVersion = 2

// Submission maps opaque trial IDs to trusted Campaign report artifacts. It
// contains no defect metadata and can therefore be produced by a blind runner.
type Submission struct {
	Version         int               `json:"version"`
	BenchmarkID     string            `json:"benchmark_id"`
	BenchmarkDigest string            `json:"benchmark_digest"`
	Trials          []TrialSubmission `json:"trials"`
}

type TrialSubmission struct {
	TrialID        string `json:"trial_id"`
	CampaignReport string `json:"campaign_report"`
	BuildAudit     string `json:"build_audit"`
	Binary         string `json:"binary"`
	Profile        string `json:"profile"`
	Plans          string `json:"plans"`
}

func (submission Submission) Validate(blind BlindManifest) error {
	if submission.Version != SubmissionVersion || submission.BenchmarkID != blind.BenchmarkID ||
		submission.BenchmarkDigest != blind.BenchmarkDigest {
		return errors.New("submission identity does not match blind benchmark manifest")
	}
	if len(submission.Trials) != len(blind.Trials) {
		return fmt.Errorf("submission has %d trials, want %d", len(submission.Trials), len(blind.Trials))
	}
	wanted := make(map[string]bool, len(blind.Trials))
	for _, trial := range blind.Trials {
		wanted[trial.TrialID] = true
	}
	seen := make(map[string]bool, len(submission.Trials))
	for index, trial := range submission.Trials {
		if !wanted[trial.TrialID] || seen[trial.TrialID] || trial.CampaignReport == "" ||
			trial.BuildAudit == "" || trial.Binary == "" || trial.Profile == "" || trial.Plans == "" {
			return fmt.Errorf("submission trial[%d] is unknown, duplicated, or missing a required artifact", index)
		}
		seen[trial.TrialID] = true
	}
	return nil
}
