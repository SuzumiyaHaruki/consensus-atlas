package controlexperiment

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	CampaignAttemptRequestVersion = "consensus-atlas/campaign-attempt-request/v1"
	campaignMaxArtifactBytes      = 64 << 20
)

type CampaignAllowance struct {
	RemainingAttempts         int `json:"remaining_attempts"`
	PrimarySchedulerDecisions int `json:"primary_scheduler_decisions"`
	PrimaryWorkUnits          int `json:"primary_work_units"`
	ReplayWorkUnits           int `json:"replay_work_units"`
	ModelCalls                int `json:"model_calls"`
	ModelTokens               int `json:"model_tokens"`
}

func (allowance CampaignAllowance) validate() error {
	if allowance.RemainingAttempts <= 0 || allowance.PrimarySchedulerDecisions < 0 ||
		allowance.PrimaryWorkUnits < 0 || allowance.ReplayWorkUnits < 0 ||
		allowance.ModelCalls < 0 || allowance.ModelTokens < 0 {
		return errors.New("EXPERIMENT_CAMPAIGN_ALLOWANCE_INVALID")
	}
	return nil
}

type CampaignAttemptRequest struct {
	SchemaVersion string `json:"schema_version"`
	CampaignID    string `json:"campaign_id"`
	ConfigDigest  string `json:"config_digest"`

	TargetID             string            `json:"target_id"`
	TargetIdentityDigest string            `json:"target_identity_digest"`
	ExperimentSpecDigest string            `json:"experiment_spec_digest"`
	Ordinal              int               `json:"ordinal"`
	PreviousDigest       string            `json:"previous_checkpoint_digest"`
	Allowance            CampaignAllowance `json:"allowance"`
	Digest               string            `json:"digest"`
}

func NewCampaignAttemptRequest(
	config CampaignConfig,
	head CampaignCheckpoint,
) (CampaignAttemptRequest, error) {
	if err := head.ValidateInputs(config); err != nil {
		return CampaignAttemptRequest{}, err
	}
	if head.StopReason != CampaignStopRunning {
		return CampaignAttemptRequest{}, errors.New("EXPERIMENT_CAMPAIGN_ALREADY_STOPPED")
	}
	request := CampaignAttemptRequest{
		SchemaVersion: CampaignAttemptRequestVersion,
		CampaignID:    config.ID, ConfigDigest: config.Digest,
		TargetID: config.TargetID, TargetIdentityDigest: config.TargetIdentityDigest,
		ExperimentSpecDigest: config.ExperimentSpecDigest,
		Ordinal:              head.Sequence + 1, PreviousDigest: head.Digest,
		Allowance: campaignRemainingAllowance(config, head),
	}
	sealed, err := request.seal()
	if err != nil {
		return CampaignAttemptRequest{}, err
	}
	if err := sealed.Validate(); err != nil {
		return CampaignAttemptRequest{}, err
	}
	return sealed, nil
}

func (request CampaignAttemptRequest) Validate() error {
	if request.SchemaVersion != CampaignAttemptRequestVersion ||
		!validMethodToken(request.CampaignID) || !validSHA256(request.ConfigDigest) ||
		!validMethodToken(request.TargetID) || !validSHA256(request.TargetIdentityDigest) ||
		!validSHA256(request.ExperimentSpecDigest) || request.Ordinal <= 0 ||
		!validSHA256(request.PreviousDigest) {
		return errors.New("EXPERIMENT_CAMPAIGN_ATTEMPT_REQUEST_INVALID")
	}
	if err := request.Allowance.validate(); err != nil {
		return err
	}
	sealed, err := request.seal()
	if err != nil || !validSHA256(request.Digest) || sealed.Digest != request.Digest {
		return errors.New("EXPERIMENT_CAMPAIGN_ATTEMPT_REQUEST_DIGEST_MISMATCH")
	}
	return nil
}

func (request CampaignAttemptRequest) seal() (CampaignAttemptRequest, error) {
	request.Digest = ""
	digest, err := portableJSONDigest(request)
	if err != nil {
		return CampaignAttemptRequest{}, err
	}
	request.Digest = digest
	return request, nil
}

func campaignRemainingAllowance(
	config CampaignConfig,
	head CampaignCheckpoint,
) CampaignAllowance {
	return CampaignAllowance{
		RemainingAttempts: config.Budget.MaxAttempts - head.Sequence,
		PrimarySchedulerDecisions: config.Budget.MaxPrimarySchedulerDecisions -
			head.Totals.Primary.SchedulerDecisions,
		PrimaryWorkUnits: config.Budget.MaxPrimaryWorkUnits - head.Totals.Primary.WorkUnits,
		ReplayWorkUnits:  config.Budget.MaxReplayWorkUnits - head.Totals.Replay.WorkUnits,
		ModelCalls:       config.Budget.MaxModelCalls - head.Totals.Model.Calls,
		ModelTokens:      config.Budget.MaxModelTokens - head.Totals.Model.TotalTokens,
	}
}

type CampaignAttemptResult struct {
	InputDigest string
	Outcome     string
	Failure     *MethodFailure
	Work        WorkLedger
	Artifact    []byte
}

type CampaignAttemptProvider interface {
	Attempt(context.Context, CampaignAttemptRequest) (CampaignAttemptResult, error)
}

type CampaignAttemptProviderFunc func(
	context.Context,
	CampaignAttemptRequest,
) (CampaignAttemptResult, error)

// CampaignAttemptDeferredError reports that a provider stopped before a
// terminal attempt artifact existed. The Coordinator leaves the trusted head
// unchanged; target-owned durable sidecars may make the exact request
// resumable, but no work or outcome is invented in the Campaign ledger.
type CampaignAttemptDeferredError struct {
	code string
	err  error
}

func NewCampaignAttemptDeferredError(code string, err error) error {
	if code == "" || err == nil {
		return errors.New("EXPERIMENT_CAMPAIGN_DEFERRED_INPUT_INVALID")
	}
	return &CampaignAttemptDeferredError{code: code, err: err}
}

func (deferred *CampaignAttemptDeferredError) Error() string {
	if deferred == nil {
		return "EXPERIMENT_CAMPAIGN_ATTEMPT_DEFERRED"
	}
	return fmt.Sprintf("EXPERIMENT_CAMPAIGN_ATTEMPT_DEFERRED[%s]: %v", deferred.code, deferred.err)
}

func (deferred *CampaignAttemptDeferredError) Unwrap() error {
	if deferred == nil {
		return nil
	}
	return deferred.err
}

func (provider CampaignAttemptProviderFunc) Attempt(
	ctx context.Context,
	request CampaignAttemptRequest,
) (CampaignAttemptResult, error) {
	return provider(ctx, request)
}

type CampaignCoordinator struct {
	recovered    *CampaignRecovery
	provider     CampaignAttemptProvider
	now          func() time.Time
	sessionStart time.Time
	baseElapsed  int64
	failed       bool
}

func NewCampaignCoordinator(
	recovered *CampaignRecovery,
	provider CampaignAttemptProvider,
) (*CampaignCoordinator, error) {
	return newCampaignCoordinator(recovered, provider, time.Now)
}

func newCampaignCoordinator(
	recovered *CampaignRecovery,
	provider CampaignAttemptProvider,
	now func() time.Time,
) (*CampaignCoordinator, error) {
	if recovered == nil || provider == nil || now == nil || recovered.directory == "" ||
		recovered.Failure != nil || recovered.validatedFailureDigest != "" ||
		recovered.Config.Digest != recovered.validatedConfigDigest ||
		recovered.Head.Digest != recovered.validatedHeadDigest ||
		recovered.Head.ValidateInputs(recovered.Config) != nil {
		return nil, errors.New("EXPERIMENT_CAMPAIGN_COORDINATOR_INPUT_INVALID")
	}
	return &CampaignCoordinator{
		recovered: recovered, provider: provider, now: now, sessionStart: now(),
		baseElapsed: recovered.Head.ElapsedMillis,
	}, nil
}

func (coordinator *CampaignCoordinator) Run(ctx context.Context) (CampaignCheckpoint, error) {
	if coordinator == nil || coordinator.recovered == nil {
		return CampaignCheckpoint{}, errors.New("EXPERIMENT_CAMPAIGN_COORDINATOR_INVALID")
	}
	for coordinator.recovered.Head.StopReason == CampaignStopRunning {
		if _, err := coordinator.Step(ctx); err != nil {
			return coordinator.recovered.Head, err
		}
	}
	return coordinator.recovered.Head, nil
}

func (coordinator *CampaignCoordinator) Step(ctx context.Context) (CampaignCheckpoint, error) {
	if coordinator == nil || coordinator.recovered == nil || coordinator.provider == nil {
		return CampaignCheckpoint{}, errors.New("EXPERIMENT_CAMPAIGN_COORDINATOR_INVALID")
	}
	if coordinator.failed {
		return CampaignCheckpoint{}, errors.New("EXPERIMENT_CAMPAIGN_COORDINATOR_FAILED")
	}
	if coordinator.recovered.Head.StopReason != CampaignStopRunning {
		return CampaignCheckpoint{}, errors.New("EXPERIMENT_CAMPAIGN_ALREADY_STOPPED")
	}
	if err := ctx.Err(); err != nil {
		return CampaignCheckpoint{}, fmt.Errorf("EXPERIMENT_CAMPAIGN_CONTEXT_DONE: %w", err)
	}
	elapsedBefore, err := coordinator.elapsedMillis()
	if err != nil {
		return CampaignCheckpoint{}, err
	}
	remainingMillis := coordinator.recovered.Config.WallClockCeilingMillis - elapsedBefore
	if remainingMillis <= 0 {
		return CampaignCheckpoint{}, errors.New("EXPERIMENT_CAMPAIGN_WALL_CLOCK_BEFORE_ATTEMPT")
	}
	request, err := NewCampaignAttemptRequest(coordinator.recovered.Config, coordinator.recovered.Head)
	if err != nil {
		return CampaignCheckpoint{}, err
	}
	attemptContext, cancel := context.WithTimeout(ctx, campaignTimeout(remainingMillis))
	result, providerErr := coordinator.provider.Attempt(attemptContext, request)
	cancel()
	if providerErr != nil {
		var deferred *CampaignAttemptDeferredError
		if errors.As(providerErr, &deferred) {
			return coordinator.recovered.Head, providerErr
		}
		failureWork := emptyWork()
		if validateMethodWork(result.Work) == nil {
			failureWork = result.Work
		}
		return coordinator.failAttemptWithWork(
			request, CampaignFailureProvider,
			failureWork,
			fmt.Errorf("EXPERIMENT_CAMPAIGN_PROVIDER_FAILED: %w", providerErr),
		)
	}
	elapsedAfter, err := coordinator.elapsedMillis()
	if err != nil || elapsedAfter < elapsedBefore {
		return coordinator.failAttempt(
			request, CampaignFailureClock, errors.New("EXPERIMENT_CAMPAIGN_CLOCK_INVALID"),
		)
	}
	if len(result.Artifact) == 0 || len(result.Artifact) > campaignMaxArtifactBytes {
		return coordinator.failAttempt(
			request, CampaignFailureResult, errors.New("EXPERIMENT_CAMPAIGN_ARTIFACT_INVALID"),
		)
	}
	if err := validateMethodWork(result.Work); err != nil {
		return coordinator.failAttempt(request, CampaignFailureResult, err)
	}
	if !campaignWorkWithinAllowance(result.Work, request.Allowance) {
		return coordinator.failAttempt(
			request, CampaignFailureResult, errors.New("EXPERIMENT_CAMPAIGN_ALLOWANCE_EXCEEDED"),
		)
	}
	inputDigest := request.Digest
	if result.InputDigest != "" {
		if !validSHA256(result.InputDigest) {
			return coordinator.failAttempt(
				request, CampaignFailureResult, errors.New("EXPERIMENT_CAMPAIGN_ATTEMPT_INPUT_INVALID"),
			)
		}
		inputDigest = result.InputDigest
	}
	record, err := NewCampaignAttemptRecord(CampaignAttemptRecord{
		Ordinal:     request.Ordinal,
		ID:          fmt.Sprintf("%s-attempt-%d", request.CampaignID, request.Ordinal),
		InputDigest: inputDigest, ArtifactDigest: CampaignArtifactDigest(result.Artifact),
		Outcome: result.Outcome, Failure: result.Failure, Work: result.Work,
	})
	if err != nil {
		return coordinator.failAttempt(request, CampaignFailureResult, err)
	}
	head, err := coordinator.recovered.CommitAttempt(record, result.Artifact, elapsedAfter)
	if err != nil {
		coordinator.failed = true
		return CampaignCheckpoint{}, err
	}
	return head, nil
}

func (coordinator *CampaignCoordinator) failAttempt(
	request CampaignAttemptRequest,
	code string,
	cause error,
) (CampaignCheckpoint, error) {
	return coordinator.failAttemptWithWork(request, code, emptyWork(), cause)
}

func (coordinator *CampaignCoordinator) failAttemptWithWork(
	request CampaignAttemptRequest,
	code string,
	work WorkLedger,
	cause error,
) (CampaignCheckpoint, error) {
	coordinator.failed = true
	if _, err := coordinator.recovered.failAttemptWithWork(request, code, work); err != nil {
		return CampaignCheckpoint{}, fmt.Errorf("%v; EXPERIMENT_CAMPAIGN_FAILURE_MARKER_FAILED: %w", cause, err)
	}
	return CampaignCheckpoint{}, cause
}

func (coordinator *CampaignCoordinator) elapsedMillis() (int64, error) {
	delta := coordinator.now().Sub(coordinator.sessionStart).Milliseconds()
	if delta < 0 || delta > int64(^uint64(0)>>1)-coordinator.baseElapsed {
		return 0, errors.New("EXPERIMENT_CAMPAIGN_CLOCK_INVALID")
	}
	return coordinator.baseElapsed + delta, nil
}

func campaignTimeout(milliseconds int64) time.Duration {
	const maxMilliseconds = int64((1<<63 - 1) / int64(time.Millisecond))
	if milliseconds > maxMilliseconds {
		return time.Duration(1<<63 - 1)
	}
	return time.Duration(milliseconds) * time.Millisecond
}

func campaignWorkWithinAllowance(work WorkLedger, allowance CampaignAllowance) bool {
	return work.Primary.SchedulerDecisions <= allowance.PrimarySchedulerDecisions &&
		work.Primary.WorkUnits <= allowance.PrimaryWorkUnits &&
		work.Replay.WorkUnits <= allowance.ReplayWorkUnits &&
		work.Model.Calls <= allowance.ModelCalls && work.Model.TotalTokens <= allowance.ModelTokens
}
