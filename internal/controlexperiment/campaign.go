package controlexperiment

import (
	"errors"
	"fmt"
)

const (
	CampaignConfigVersion     = "consensus-atlas/campaign-config/v1"
	CampaignAttemptVersion    = "consensus-atlas/campaign-attempt/v1"
	CampaignCheckpointVersion = "consensus-atlas/campaign-checkpoint/v1"
	CampaignFailureVersion    = "consensus-atlas/campaign-failure/v1"
	CampaignCheckpointPolicy  = "checkpoint-after-each-terminal-attempt"
	CampaignAttemptCompleted  = "completed"
	CampaignAttemptRejected   = "rejected"
	CampaignAttemptFailed     = "failed"
	CampaignAttemptInvalid    = "invalid"
	CampaignStopRunning       = "running"
	CampaignStopAttemptLimit  = "attempt-limit"
	CampaignStopLogicalBudget = "logical-budget"
	CampaignStopWallClock     = "wall-clock-ceiling"
	CampaignFailureProvider   = "provider-error"
	CampaignFailureClock      = "clock-invalid"
	CampaignFailureResult     = "provider-result-invalid"
)

type CampaignLogicalBudget struct {
	MaxAttempts                  int `json:"max_attempts"`
	MaxPrimarySchedulerDecisions int `json:"max_primary_scheduler_decisions"`
	MaxPrimaryWorkUnits          int `json:"max_primary_work_units"`
	MaxReplayWorkUnits           int `json:"max_replay_work_units"`
	MaxModelCalls                int `json:"max_model_calls"`
	MaxModelTokens               int `json:"max_model_tokens"`
}

func (budget CampaignLogicalBudget) validate() error {
	if budget.MaxAttempts <= 0 || budget.MaxPrimarySchedulerDecisions <= 0 ||
		budget.MaxPrimaryWorkUnits <= 0 || budget.MaxReplayWorkUnits <= 0 ||
		budget.MaxModelCalls < 0 || budget.MaxModelTokens < 0 ||
		(budget.MaxModelCalls == 0) != (budget.MaxModelTokens == 0) {
		return errors.New("EXPERIMENT_CAMPAIGN_BUDGET_INVALID")
	}
	return nil
}

type CampaignConfig struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`

	TargetID               string                `json:"target_id"`
	TargetIdentityDigest   string                `json:"target_identity_digest"`
	ExperimentSpecDigest   string                `json:"experiment_spec_digest"`
	Budget                 CampaignLogicalBudget `json:"budget"`
	WallClockCeilingMillis int64                 `json:"wall_clock_ceiling_ms"`
	CheckpointPolicy       string                `json:"checkpoint_policy"`
	Digest                 string                `json:"digest"`
}

func NewCampaignConfig(
	id string,
	targetID string,
	targetIdentityDigest string,
	experimentSpecDigest string,
	budget CampaignLogicalBudget,
	wallClockCeilingMillis int64,
) (CampaignConfig, error) {
	config := CampaignConfig{
		SchemaVersion: CampaignConfigVersion, ID: id,
		TargetID: targetID, TargetIdentityDigest: targetIdentityDigest,
		ExperimentSpecDigest: experimentSpecDigest, Budget: budget,
		WallClockCeilingMillis: wallClockCeilingMillis,
		CheckpointPolicy:       CampaignCheckpointPolicy,
	}
	sealed, err := config.seal()
	if err != nil {
		return CampaignConfig{}, err
	}
	if err := sealed.Validate(); err != nil {
		return CampaignConfig{}, err
	}
	return sealed, nil
}

func (config CampaignConfig) Validate() error {
	if config.SchemaVersion != CampaignConfigVersion || !validMethodToken(config.ID) ||
		!validMethodToken(config.TargetID) || !validSHA256(config.TargetIdentityDigest) ||
		!validSHA256(config.ExperimentSpecDigest) || config.WallClockCeilingMillis <= 0 ||
		config.CheckpointPolicy != CampaignCheckpointPolicy {
		return errors.New("EXPERIMENT_CAMPAIGN_CONFIG_INVALID")
	}
	if err := config.Budget.validate(); err != nil {
		return err
	}
	sealed, err := config.seal()
	if err != nil || !validSHA256(config.Digest) || sealed.Digest != config.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_CONFIG_DIGEST_MISMATCH")
	}
	return nil
}

func (config CampaignConfig) seal() (CampaignConfig, error) {
	config.Digest = ""
	digest, err := portableJSONDigest(config)
	if err != nil {
		return CampaignConfig{}, err
	}
	config.Digest = digest
	return config, nil
}

type CampaignAttemptRecord struct {
	SchemaVersion  string         `json:"schema_version"`
	Ordinal        int            `json:"ordinal"`
	ID             string         `json:"id"`
	InputDigest    string         `json:"input_digest"`
	ArtifactDigest string         `json:"artifact_digest"`
	Outcome        string         `json:"outcome"`
	Failure        *MethodFailure `json:"failure,omitempty"`
	Work           WorkLedger     `json:"work"`
	Digest         string         `json:"digest"`
}

func NewCampaignAttemptRecord(record CampaignAttemptRecord) (CampaignAttemptRecord, error) {
	record.SchemaVersion = CampaignAttemptVersion
	record.Digest = ""
	sealed, err := record.seal()
	if err != nil {
		return CampaignAttemptRecord{}, err
	}
	if err := sealed.Validate(); err != nil {
		return CampaignAttemptRecord{}, err
	}
	return sealed, nil
}

func (record CampaignAttemptRecord) Validate() error {
	if record.SchemaVersion != CampaignAttemptVersion || record.Ordinal <= 0 ||
		!validMethodToken(record.ID) || !validSHA256(record.InputDigest) ||
		!validSHA256(record.ArtifactDigest) {
		return errors.New("EXPERIMENT_CAMPAIGN_ATTEMPT_INVALID")
	}
	if err := validateMethodWork(record.Work); err != nil {
		return err
	}
	switch record.Outcome {
	case CampaignAttemptCompleted:
		if record.Failure != nil {
			return errors.New("EXPERIMENT_CAMPAIGN_ATTEMPT_OUTCOME_INVALID")
		}
	case CampaignAttemptRejected, CampaignAttemptFailed, CampaignAttemptInvalid:
		if record.Failure == nil || record.Failure.Phase == "" || record.Failure.Code == "" ||
			record.Failure.Decision < 0 {
			return errors.New("EXPERIMENT_CAMPAIGN_ATTEMPT_OUTCOME_INVALID")
		}
	default:
		return errors.New("EXPERIMENT_CAMPAIGN_ATTEMPT_OUTCOME_INVALID")
	}
	sealed, err := record.seal()
	if err != nil || !validSHA256(record.Digest) || sealed.Digest != record.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_ATTEMPT_DIGEST_MISMATCH")
	}
	return nil
}

func (record CampaignAttemptRecord) seal() (CampaignAttemptRecord, error) {
	if record.Failure != nil {
		failure := *record.Failure
		record.Failure = &failure
	}
	record.Digest = ""
	digest, err := portableJSONDigest(record)
	if err != nil {
		return CampaignAttemptRecord{}, err
	}
	record.Digest = digest
	return record, nil
}

// CampaignFailureMarker makes an artifact-less coordinator failure durable.
// It deliberately excludes the provider's diagnostic text: the marker only
// prevents the exact next request from being retried after process recovery.
type CampaignFailureMarker struct {
	SchemaVersion string `json:"schema_version"`
	CampaignID    string `json:"campaign_id"`
	ConfigDigest  string `json:"config_digest"`
	HeadDigest    string `json:"head_digest"`
	Ordinal       int    `json:"ordinal"`
	RequestDigest string `json:"request_digest"`
	Code          string `json:"code"`
	Digest        string `json:"digest"`
}

func NewCampaignFailureMarker(
	config CampaignConfig,
	head CampaignCheckpoint,
	request CampaignAttemptRequest,
	code string,
) (CampaignFailureMarker, error) {
	marker := CampaignFailureMarker{
		SchemaVersion: CampaignFailureVersion,
		CampaignID:    config.ID,
		ConfigDigest:  config.Digest,
		HeadDigest:    head.Digest,
		Ordinal:       request.Ordinal,
		RequestDigest: request.Digest,
		Code:          code,
	}
	sealed, err := marker.seal()
	if err != nil {
		return CampaignFailureMarker{}, err
	}
	if err := sealed.ValidateInputs(config, head); err != nil {
		return CampaignFailureMarker{}, err
	}
	return sealed, nil
}

func (marker CampaignFailureMarker) ValidateInputs(
	config CampaignConfig,
	head CampaignCheckpoint,
) error {
	if err := marker.Validate(); err != nil {
		return err
	}
	if err := head.ValidateInputs(config); err != nil {
		return err
	}
	request, err := NewCampaignAttemptRequest(config, head)
	if err != nil {
		return err
	}
	if marker.CampaignID != config.ID || marker.ConfigDigest != config.Digest ||
		marker.HeadDigest != head.Digest || marker.Ordinal != request.Ordinal ||
		marker.RequestDigest != request.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_FAILURE_IDENTITY_MISMATCH")
	}
	return nil
}

func (marker CampaignFailureMarker) Validate() error {
	if marker.SchemaVersion != CampaignFailureVersion ||
		!validMethodToken(marker.CampaignID) || !validSHA256(marker.ConfigDigest) ||
		!validSHA256(marker.HeadDigest) || marker.Ordinal <= 0 ||
		!validSHA256(marker.RequestDigest) || !validMethodToken(marker.Code) {
		return errors.New("EXPERIMENT_CAMPAIGN_FAILURE_INVALID")
	}
	sealed, err := marker.seal()
	if err != nil || !validSHA256(marker.Digest) || sealed.Digest != marker.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_FAILURE_DIGEST_MISMATCH")
	}
	return nil
}

func (marker CampaignFailureMarker) seal() (CampaignFailureMarker, error) {
	marker.Digest = ""
	digest, err := portableJSONDigest(marker)
	if err != nil {
		return CampaignFailureMarker{}, err
	}
	marker.Digest = digest
	return marker, nil
}

type CampaignCheckpoint struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`

	ConfigID               string                `json:"config_id"`
	ConfigDigest           string                `json:"config_digest"`
	TargetID               string                `json:"target_id"`
	TargetIdentityDigest   string                `json:"target_identity_digest"`
	ExperimentSpecDigest   string                `json:"experiment_spec_digest"`
	Budget                 CampaignLogicalBudget `json:"budget"`
	WallClockCeilingMillis int64                 `json:"wall_clock_ceiling_ms"`
	CheckpointPolicy       string                `json:"checkpoint_policy"`

	Sequence       int                    `json:"sequence"`
	PreviousDigest string                 `json:"previous_digest,omitempty"`
	Record         *CampaignAttemptRecord `json:"record,omitempty"`
	Totals         WorkLedger             `json:"totals"`
	ElapsedMillis  int64                  `json:"elapsed_ms"`
	StopReason     string                 `json:"stop_reason"`
	Digest         string                 `json:"digest"`
}

func NewCampaignCheckpoint(config CampaignConfig) (CampaignCheckpoint, error) {
	if err := config.Validate(); err != nil {
		return CampaignCheckpoint{}, err
	}
	checkpoint := campaignCheckpointFromConfig(config)
	checkpoint.ID = campaignCheckpointID(config.ID, 0)
	checkpoint.Totals = emptyWork()
	checkpoint.StopReason = CampaignStopRunning
	sealed, err := checkpoint.seal()
	if err != nil {
		return CampaignCheckpoint{}, err
	}
	if err := sealed.ValidateInputs(config); err != nil {
		return CampaignCheckpoint{}, err
	}
	return sealed, nil
}

func (checkpoint CampaignCheckpoint) AppendAttempt(
	config CampaignConfig,
	record CampaignAttemptRecord,
	elapsedMillis int64,
) (CampaignCheckpoint, error) {
	if err := checkpoint.ValidateInputs(config); err != nil {
		return CampaignCheckpoint{}, err
	}
	if checkpoint.StopReason != CampaignStopRunning {
		return CampaignCheckpoint{}, errors.New("EXPERIMENT_CAMPAIGN_ALREADY_STOPPED")
	}
	if err := record.Validate(); err != nil {
		return CampaignCheckpoint{}, err
	}
	if record.Ordinal != checkpoint.Sequence+1 || elapsedMillis < checkpoint.ElapsedMillis {
		return CampaignCheckpoint{}, errors.New("EXPERIMENT_CAMPAIGN_APPEND_ORDER_INVALID")
	}
	next := campaignCheckpointFromConfig(config)
	next.Sequence = checkpoint.Sequence + 1
	next.ID = campaignCheckpointID(config.ID, next.Sequence)
	next.PreviousDigest = checkpoint.Digest
	next.Record = cloneCampaignRecord(&record)
	next.Totals = addWorkLedgers(checkpoint.Totals, record.Work)
	next.ElapsedMillis = elapsedMillis
	if campaignBudgetExceeded(config.Budget, next.Sequence, next.Totals) {
		return CampaignCheckpoint{}, errors.New("EXPERIMENT_CAMPAIGN_BUDGET_EXCEEDED")
	}
	next.StopReason = campaignStopReason(config, next.Sequence, next.Totals, elapsedMillis)
	sealed, err := next.seal()
	if err != nil {
		return CampaignCheckpoint{}, err
	}
	if err := sealed.ValidateInputs(config); err != nil {
		return CampaignCheckpoint{}, err
	}
	return sealed, nil
}

func (checkpoint CampaignCheckpoint) Validate() error {
	if checkpoint.SchemaVersion != CampaignCheckpointVersion ||
		!validMethodToken(checkpoint.ConfigID) ||
		checkpoint.ID != campaignCheckpointID(checkpoint.ConfigID, checkpoint.Sequence) ||
		!validSHA256(checkpoint.ConfigDigest) || !validMethodToken(checkpoint.TargetID) ||
		!validSHA256(checkpoint.TargetIdentityDigest) ||
		!validSHA256(checkpoint.ExperimentSpecDigest) ||
		checkpoint.WallClockCeilingMillis <= 0 || checkpoint.CheckpointPolicy != CampaignCheckpointPolicy ||
		checkpoint.Sequence < 0 || checkpoint.ElapsedMillis < 0 {
		return errors.New("EXPERIMENT_CAMPAIGN_CHECKPOINT_INVALID")
	}
	if err := checkpoint.Budget.validate(); err != nil {
		return err
	}
	if checkpoint.Sequence == 0 {
		if checkpoint.PreviousDigest != "" || checkpoint.Record != nil || checkpoint.ElapsedMillis != 0 ||
			checkpoint.Totals != emptyWork() || checkpoint.StopReason != CampaignStopRunning {
			return errors.New("EXPERIMENT_CAMPAIGN_CHECKPOINT_CHAIN_INVALID")
		}
	} else {
		if !validSHA256(checkpoint.PreviousDigest) || checkpoint.Record == nil {
			return errors.New("EXPERIMENT_CAMPAIGN_CHECKPOINT_CHAIN_INVALID")
		}
		if err := checkpoint.Record.Validate(); err != nil {
			return err
		}
		if checkpoint.Record.Ordinal != checkpoint.Sequence {
			return errors.New("EXPERIMENT_CAMPAIGN_ATTEMPT_ORDER_INVALID")
		}
	}
	if err := validateMethodWork(checkpoint.Totals); err != nil {
		return err
	}
	if campaignBudgetExceeded(
		checkpoint.Budget, checkpoint.Sequence, checkpoint.Totals,
	) {
		return errors.New("EXPERIMENT_CAMPAIGN_TOTAL_MISMATCH")
	}
	if checkpoint.StopReason != campaignStopReasonFromCheckpoint(checkpoint) {
		return errors.New("EXPERIMENT_CAMPAIGN_STOP_REASON_MISMATCH")
	}
	sealed, err := checkpoint.seal()
	if err != nil || !validSHA256(checkpoint.Digest) || sealed.Digest != checkpoint.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_CHECKPOINT_DIGEST_MISMATCH")
	}
	return nil
}

func (checkpoint CampaignCheckpoint) ValidateInputs(config CampaignConfig) error {
	if err := config.Validate(); err != nil {
		return err
	}
	if err := checkpoint.Validate(); err != nil {
		return err
	}
	if checkpoint.ConfigID != config.ID || checkpoint.ConfigDigest != config.Digest ||
		checkpoint.TargetID != config.TargetID ||
		checkpoint.TargetIdentityDigest != config.TargetIdentityDigest ||
		checkpoint.ExperimentSpecDigest != config.ExperimentSpecDigest ||
		checkpoint.Budget != config.Budget ||
		checkpoint.WallClockCeilingMillis != config.WallClockCeilingMillis ||
		checkpoint.CheckpointPolicy != config.CheckpointPolicy {
		return errors.New("EXPERIMENT_CAMPAIGN_RESUME_IDENTITY_MISMATCH")
	}
	return nil
}

func (checkpoint CampaignCheckpoint) ValidatePrevious(
	config CampaignConfig,
	previous CampaignCheckpoint,
) error {
	if err := checkpoint.ValidateInputs(config); err != nil {
		return err
	}
	if err := previous.ValidateInputs(config); err != nil {
		return err
	}
	if checkpoint.Sequence != previous.Sequence+1 || checkpoint.PreviousDigest != previous.Digest ||
		checkpoint.ElapsedMillis < previous.ElapsedMillis || previous.StopReason != CampaignStopRunning ||
		checkpoint.Record == nil {
		return errors.New("EXPERIMENT_CAMPAIGN_CHECKPOINT_CHAIN_MISMATCH")
	}
	if want := addWorkLedgers(previous.Totals, checkpoint.Record.Work); want != checkpoint.Totals {
		return errors.New("EXPERIMENT_CAMPAIGN_CHECKPOINT_TOTAL_MISMATCH")
	}
	return nil
}

func ValidateCampaignCheckpointChain(
	config CampaignConfig,
	checkpoints []CampaignCheckpoint,
) error {
	if len(checkpoints) == 0 || checkpoints[0].Sequence != 0 {
		return errors.New("EXPERIMENT_CAMPAIGN_CHECKPOINT_CHAIN_INVALID")
	}
	if err := checkpoints[0].ValidateInputs(config); err != nil {
		return err
	}
	for index := 1; index < len(checkpoints); index++ {
		if err := checkpoints[index].ValidatePrevious(config, checkpoints[index-1]); err != nil {
			return err
		}
	}
	return nil
}

func (checkpoint CampaignCheckpoint) seal() (CampaignCheckpoint, error) {
	checkpoint.Record = cloneCampaignRecord(checkpoint.Record)
	checkpoint.Digest = ""
	digest, err := portableJSONDigest(checkpoint)
	if err != nil {
		return CampaignCheckpoint{}, err
	}
	checkpoint.Digest = digest
	return checkpoint, nil
}

func campaignCheckpointFromConfig(config CampaignConfig) CampaignCheckpoint {
	return CampaignCheckpoint{
		SchemaVersion: CampaignCheckpointVersion,
		ConfigID:      config.ID, ConfigDigest: config.Digest, TargetID: config.TargetID,
		TargetIdentityDigest: config.TargetIdentityDigest,
		ExperimentSpecDigest: config.ExperimentSpecDigest,
		Budget:               config.Budget, WallClockCeilingMillis: config.WallClockCeilingMillis,
		CheckpointPolicy: config.CheckpointPolicy,
	}
}

func campaignCheckpointID(configID string, sequence int) string {
	return fmt.Sprintf("%s-checkpoint-%d", configID, sequence)
}

func campaignBudgetExceeded(budget CampaignLogicalBudget, attempts int, totals WorkLedger) bool {
	return attempts > budget.MaxAttempts ||
		totals.Primary.SchedulerDecisions > budget.MaxPrimarySchedulerDecisions ||
		totals.Primary.WorkUnits > budget.MaxPrimaryWorkUnits ||
		totals.Replay.WorkUnits > budget.MaxReplayWorkUnits ||
		totals.Model.Calls > budget.MaxModelCalls || totals.Model.TotalTokens > budget.MaxModelTokens
}

func campaignLogicalBudgetReached(budget CampaignLogicalBudget, totals WorkLedger) bool {
	return totals.Primary.SchedulerDecisions == budget.MaxPrimarySchedulerDecisions ||
		totals.Primary.WorkUnits == budget.MaxPrimaryWorkUnits ||
		totals.Replay.WorkUnits == budget.MaxReplayWorkUnits ||
		(totals.Model.Calls == budget.MaxModelCalls && budget.MaxModelCalls > 0) ||
		(totals.Model.TotalTokens == budget.MaxModelTokens && budget.MaxModelTokens > 0)
}

func campaignStopReason(
	config CampaignConfig,
	attempts int,
	totals WorkLedger,
	elapsedMillis int64,
) string {
	if attempts == config.Budget.MaxAttempts {
		return CampaignStopAttemptLimit
	}
	if campaignLogicalBudgetReached(config.Budget, totals) {
		return CampaignStopLogicalBudget
	}
	if elapsedMillis >= config.WallClockCeilingMillis {
		return CampaignStopWallClock
	}
	return CampaignStopRunning
}

func campaignStopReasonFromCheckpoint(checkpoint CampaignCheckpoint) string {
	config := CampaignConfig{
		Budget: checkpoint.Budget, WallClockCeilingMillis: checkpoint.WallClockCeilingMillis,
	}
	return campaignStopReason(config, checkpoint.Sequence, checkpoint.Totals, checkpoint.ElapsedMillis)
}

func cloneCampaignRecord(record *CampaignAttemptRecord) *CampaignAttemptRecord {
	if record == nil {
		return nil
	}
	cloned := *record
	if cloned.Failure != nil {
		failure := *cloned.Failure
		cloned.Failure = &failure
	}
	return &cloned
}
