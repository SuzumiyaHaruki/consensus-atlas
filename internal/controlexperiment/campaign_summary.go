package controlexperiment

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	CampaignSummaryVersion       = "consensus-atlas/campaign-summary/v1"
	CampaignSummaryStatusRunning = "running"
	CampaignSummaryStatusStopped = "stopped"
	CampaignSummaryStatusFailed  = "failed"
)

type CampaignAttemptSummary struct {
	Ordinal                  int                   `json:"ordinal"`
	PreviousCheckpointDigest string                `json:"previous_checkpoint_digest"`
	CheckpointDigest         string                `json:"checkpoint_digest"`
	Record                   CampaignAttemptRecord `json:"record"`
}

type CampaignSummary struct {
	SchemaVersion string `json:"schema_version"`
	CampaignID    string `json:"campaign_id"`
	ConfigDigest  string `json:"config_digest"`

	TargetID               string                `json:"target_id"`
	TargetIdentityDigest   string                `json:"target_identity_digest"`
	ExperimentSpecDigest   string                `json:"experiment_spec_digest"`
	Budget                 CampaignLogicalBudget `json:"budget"`
	WallClockCeilingMillis int64                 `json:"wall_clock_ceiling_ms"`
	CheckpointPolicy       string                `json:"checkpoint_policy"`

	InitialCheckpointDigest string                   `json:"initial_checkpoint_digest"`
	HeadCheckpointDigest    string                   `json:"head_checkpoint_digest"`
	Sequence                int                      `json:"sequence"`
	Attempts                []CampaignAttemptSummary `json:"attempts"`
	Totals                  WorkLedger               `json:"totals"`
	ElapsedMillis           int64                    `json:"elapsed_ms"`
	StopReason              string                   `json:"stop_reason"`
	Status                  string                   `json:"status"`
	Failure                 *CampaignFailureMarker   `json:"failure,omitempty"`
	Digest                  string                   `json:"digest"`
}

func NewCampaignSummary(recovered *CampaignRecovery) (CampaignSummary, error) {
	if err := recovered.validateReadToken(); err != nil {
		return CampaignSummary{}, err
	}
	config := recovered.Config
	attempts := make([]CampaignAttemptSummary, 0, recovered.Head.Sequence)
	for index := 1; index < len(recovered.Checkpoints); index++ {
		checkpoint := recovered.Checkpoints[index]
		attempts = append(attempts, CampaignAttemptSummary{
			Ordinal: checkpoint.Sequence, PreviousCheckpointDigest: checkpoint.PreviousDigest,
			CheckpointDigest: checkpoint.Digest, Record: *cloneCampaignRecord(checkpoint.Record),
		})
	}
	status := CampaignSummaryStatusRunning
	if recovered.Failure != nil {
		status = CampaignSummaryStatusFailed
	} else if recovered.Head.StopReason != CampaignStopRunning {
		status = CampaignSummaryStatusStopped
	}
	summary := CampaignSummary{
		SchemaVersion: CampaignSummaryVersion,
		CampaignID:    config.ID, ConfigDigest: config.Digest,
		TargetID: config.TargetID, TargetIdentityDigest: config.TargetIdentityDigest,
		ExperimentSpecDigest: config.ExperimentSpecDigest,
		Budget:               config.Budget, WallClockCeilingMillis: config.WallClockCeilingMillis,
		CheckpointPolicy:        config.CheckpointPolicy,
		InitialCheckpointDigest: recovered.Checkpoints[0].Digest,
		HeadCheckpointDigest:    recovered.Head.Digest, Sequence: recovered.Head.Sequence,
		Attempts: attempts, Totals: recovered.Head.Totals,
		ElapsedMillis: recovered.Head.ElapsedMillis, StopReason: recovered.Head.StopReason,
		Status: status, Failure: cloneCampaignFailure(recovered.Failure),
	}
	sealed, err := summary.seal()
	if err != nil {
		return CampaignSummary{}, err
	}
	if err := sealed.Validate(); err != nil {
		return CampaignSummary{}, err
	}
	return sealed, nil
}

func ReadCampaignSummary(
	directory string,
	expected CampaignConfig,
) (CampaignSummary, error) {
	recovered, err := RecoverCampaignDirectory(directory, expected)
	if err != nil {
		return CampaignSummary{}, err
	}
	return NewCampaignSummary(&recovered)
}

func (summary CampaignSummary) Validate() error {
	if summary.SchemaVersion != CampaignSummaryVersion ||
		!validMethodToken(summary.CampaignID) || !validSHA256(summary.ConfigDigest) ||
		!validMethodToken(summary.TargetID) || !validSHA256(summary.TargetIdentityDigest) ||
		!validSHA256(summary.ExperimentSpecDigest) ||
		summary.WallClockCeilingMillis <= 0 || summary.CheckpointPolicy != CampaignCheckpointPolicy ||
		!validSHA256(summary.InitialCheckpointDigest) ||
		!validSHA256(summary.HeadCheckpointDigest) || summary.Sequence < 0 ||
		len(summary.Attempts) != summary.Sequence || summary.ElapsedMillis < 0 {
		return errors.New("EXPERIMENT_CAMPAIGN_SUMMARY_INVALID")
	}
	if err := summary.Budget.validate(); err != nil {
		return err
	}
	previous := summary.InitialCheckpointDigest
	totals := emptyWork()
	for index, attempt := range summary.Attempts {
		if attempt.Ordinal != index+1 || attempt.Record.Ordinal != attempt.Ordinal ||
			attempt.PreviousCheckpointDigest != previous || !validSHA256(attempt.CheckpointDigest) {
			return errors.New("EXPERIMENT_CAMPAIGN_SUMMARY_CHAIN_INVALID")
		}
		if err := attempt.Record.Validate(); err != nil {
			return err
		}
		totals = addWorkLedgers(totals, attempt.Record.Work)
		previous = attempt.CheckpointDigest
	}
	if previous != summary.HeadCheckpointDigest || totals != summary.Totals {
		return errors.New("EXPERIMENT_CAMPAIGN_SUMMARY_TOTAL_MISMATCH")
	}
	if campaignBudgetExceeded(summary.Budget, summary.Sequence, summary.Totals) {
		return errors.New("EXPERIMENT_CAMPAIGN_SUMMARY_BUDGET_EXCEEDED")
	}
	wantStop := campaignStopReason(
		CampaignConfig{Budget: summary.Budget, WallClockCeilingMillis: summary.WallClockCeilingMillis},
		summary.Sequence, summary.Totals, summary.ElapsedMillis,
	)
	if wantStop != summary.StopReason {
		return errors.New("EXPERIMENT_CAMPAIGN_SUMMARY_STOP_MISMATCH")
	}
	switch summary.Status {
	case CampaignSummaryStatusRunning:
		if summary.StopReason != CampaignStopRunning || summary.Failure != nil {
			return errors.New("EXPERIMENT_CAMPAIGN_SUMMARY_STATUS_INVALID")
		}
	case CampaignSummaryStatusStopped:
		if summary.StopReason == CampaignStopRunning || summary.Failure != nil {
			return errors.New("EXPERIMENT_CAMPAIGN_SUMMARY_STATUS_INVALID")
		}
	case CampaignSummaryStatusFailed:
		if summary.StopReason != CampaignStopRunning || summary.Failure == nil {
			return errors.New("EXPERIMENT_CAMPAIGN_SUMMARY_STATUS_INVALID")
		}
		if err := summary.Failure.Validate(); err != nil {
			return err
		}
		if summary.Failure.CampaignID != summary.CampaignID ||
			summary.Failure.ConfigDigest != summary.ConfigDigest ||
			summary.Failure.HeadDigest != summary.HeadCheckpointDigest ||
			summary.Failure.Ordinal != summary.Sequence+1 {
			return errors.New("EXPERIMENT_CAMPAIGN_SUMMARY_FAILURE_MISMATCH")
		}
	default:
		return errors.New("EXPERIMENT_CAMPAIGN_SUMMARY_STATUS_INVALID")
	}
	sealed, err := summary.seal()
	if err != nil || !validSHA256(summary.Digest) || sealed.Digest != summary.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_SUMMARY_DIGEST_MISMATCH")
	}
	return nil
}

func (summary CampaignSummary) seal() (CampaignSummary, error) {
	summary.Attempts = cloneCampaignAttemptSummaries(summary.Attempts)
	summary.Failure = cloneCampaignFailure(summary.Failure)
	summary.Digest = ""
	digest, err := portableJSONDigest(summary)
	if err != nil {
		return CampaignSummary{}, err
	}
	summary.Digest = digest
	return summary, nil
}

func (recovered *CampaignRecovery) ReadAttemptArtifact(ordinal int) ([]byte, error) {
	if err := recovered.validateReadToken(); err != nil {
		return nil, err
	}
	if ordinal <= 0 || ordinal >= len(recovered.Checkpoints) {
		return nil, errors.New("EXPERIMENT_CAMPAIGN_ARTIFACT_ORDINAL_INVALID")
	}
	record := recovered.Checkpoints[ordinal].Record
	if record == nil || record.Ordinal != ordinal {
		return nil, errors.New("EXPERIMENT_CAMPAIGN_ARTIFACT_RECORD_INVALID")
	}
	path := filepath.Join(
		recovered.directory, campaignArtifactsDir, record.ArtifactDigest+campaignArtifactSuffix,
	)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() > campaignMaxArtifactBytes {
		return nil, errors.New("EXPERIMENT_CAMPAIGN_ARTIFACT_FILE_INVALID")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("EXPERIMENT_CAMPAIGN_ARTIFACT_OPEN: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || openedInfo.Size() > campaignMaxArtifactBytes ||
		!os.SameFile(info, openedInfo) {
		return nil, errors.New("EXPERIMENT_CAMPAIGN_ARTIFACT_FILE_CHANGED")
	}
	artifact, err := io.ReadAll(io.LimitReader(file, campaignMaxArtifactBytes+1))
	if err != nil || len(artifact) > campaignMaxArtifactBytes {
		return nil, errors.New("EXPERIMENT_CAMPAIGN_ARTIFACT_READ_INVALID")
	}
	if CampaignArtifactDigest(artifact) != record.ArtifactDigest {
		return nil, errors.New("EXPERIMENT_CAMPAIGN_ARTIFACT_DIGEST_MISMATCH")
	}
	return artifact, nil
}

func (recovered *CampaignRecovery) validateReadToken() error {
	if recovered == nil || recovered.directory == "" ||
		recovered.Config.Digest != recovered.validatedConfigDigest ||
		recovered.Head.Digest != recovered.validatedHeadDigest {
		return errors.New("EXPERIMENT_CAMPAIGN_STORE_RECOVERY_TOKEN_INVALID")
	}
	if recovered.Head.Sequence < 0 || len(recovered.Checkpoints) == 0 ||
		len(recovered.Checkpoints) != recovered.Head.Sequence+1 ||
		recovered.Checkpoints[len(recovered.Checkpoints)-1].Digest != recovered.Head.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_STORE_RECOVERY_TOKEN_INVALID")
	}
	if err := ValidateCampaignCheckpointChain(recovered.Config, recovered.Checkpoints); err != nil {
		return errors.New("EXPERIMENT_CAMPAIGN_STORE_RECOVERY_TOKEN_INVALID")
	}
	if recovered.Failure == nil {
		if recovered.validatedFailureDigest != "" {
			return errors.New("EXPERIMENT_CAMPAIGN_STORE_RECOVERY_TOKEN_INVALID")
		}
	} else if recovered.Failure.Digest != recovered.validatedFailureDigest ||
		recovered.Failure.ValidateInputs(recovered.Config, recovered.Head) != nil {
		return errors.New("EXPERIMENT_CAMPAIGN_STORE_RECOVERY_TOKEN_INVALID")
	}
	return nil
}

func cloneCampaignAttemptSummaries(attempts []CampaignAttemptSummary) []CampaignAttemptSummary {
	cloned := make([]CampaignAttemptSummary, len(attempts))
	for index := range attempts {
		cloned[index] = attempts[index]
		cloned[index].Record = *cloneCampaignRecord(&attempts[index].Record)
	}
	return cloned
}

func cloneCampaignFailure(failure *CampaignFailureMarker) *CampaignFailureMarker {
	if failure == nil {
		return nil
	}
	cloned := *failure
	return &cloned
}
