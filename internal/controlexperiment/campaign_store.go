package controlexperiment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	campaignConfigFile       = "config.json"
	campaignFailureFile      = "failure.json"
	campaignArtifactsDir     = "artifacts"
	campaignCheckpointsDir   = "checkpoints"
	campaignPlansDir         = "plans"
	campaignModelCallsDir    = "model-calls"
	campaignArtifactSuffix   = ".artifact"
	campaignCheckpointSuffix = ".json"
	campaignPendingPrefix    = ".campaign-pending-"
	campaignMaxJSONBytes     = 4 << 20
)

type CampaignRecovery struct {
	Config                   CampaignConfig
	Checkpoints              []CampaignCheckpoint
	Head                     CampaignCheckpoint
	Failure                  *CampaignFailureMarker
	OrphanArtifactDigests    []string
	PendingRelativeFilePaths []string
	PlannedAttempts          []CampaignPlannedAttempt
	ModelCalls               []CampaignModelCallRecovery
	directory                string
	validatedConfigDigest    string
	validatedHeadDigest      string
	validatedFailureDigest   string
	activeModelDispatches    map[string]bool
}

func CampaignArtifactDigest(artifact []byte) string {
	digest := sha256.Sum256(artifact)
	return hex.EncodeToString(digest[:])
}

func CreateCampaignDirectory(
	directory string,
	config CampaignConfig,
) (CampaignRecovery, error) {
	clean, err := validateCampaignDirectoryArgument(directory)
	if err != nil {
		return CampaignRecovery{}, err
	}
	if err := config.Validate(); err != nil {
		return CampaignRecovery{}, err
	}
	initial, err := NewCampaignCheckpoint(config)
	if err != nil {
		return CampaignRecovery{}, err
	}
	configBytes, err := campaignJSONBytes(config)
	if err != nil {
		return CampaignRecovery{}, err
	}
	checkpointBytes, err := campaignJSONBytes(initial)
	if err != nil {
		return CampaignRecovery{}, err
	}
	parent := filepath.Dir(clean)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return CampaignRecovery{}, fmt.Errorf("EXPERIMENT_CAMPAIGN_STORE_PARENT_CREATE: %w", err)
	}
	if err := os.Mkdir(clean, 0o700); err != nil {
		if os.IsExist(err) {
			return CampaignRecovery{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_DIRECTORY_NOT_NEW")
		}
		return CampaignRecovery{}, fmt.Errorf("EXPERIMENT_CAMPAIGN_STORE_DIRECTORY_CREATE: %w", err)
	}
	if err := syncCampaignDirectory(parent); err != nil {
		return CampaignRecovery{}, err
	}
	for _, name := range []string{
		campaignArtifactsDir, campaignCheckpointsDir, campaignPlansDir, campaignModelCallsDir,
	} {
		if err := os.Mkdir(filepath.Join(clean, name), 0o700); err != nil {
			return CampaignRecovery{}, fmt.Errorf("EXPERIMENT_CAMPAIGN_STORE_LAYOUT_CREATE: %w", err)
		}
	}
	if err := syncCampaignDirectory(clean); err != nil {
		return CampaignRecovery{}, err
	}
	if err := writeCampaignFileNoReplace(clean, campaignConfigFile, configBytes); err != nil {
		return CampaignRecovery{}, err
	}
	if err := writeCampaignFileNoReplace(
		filepath.Join(clean, campaignCheckpointsDir), campaignCheckpointFile(0), checkpointBytes,
	); err != nil {
		return CampaignRecovery{}, err
	}
	return RecoverCampaignDirectory(clean, config)
}

func (recovered *CampaignRecovery) PrepareModelCall(
	intent CampaignModelCallIntent,
) (CampaignModelCallIntent, error) {
	if !recovered.validModelCallToken() || recovered.Config.PlannerMode != CampaignPlannerDurableCall {
		return CampaignModelCallIntent{}, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_RECOVERY_INVALID")
	}
	request, err := NewCampaignAttemptRequest(recovered.Config, recovered.Head)
	if err != nil {
		return CampaignModelCallIntent{}, err
	}
	if err := intent.ValidateRequest(request); err != nil {
		return CampaignModelCallIntent{}, err
	}
	if len(recovered.ModelCalls) == recovered.Head.Sequence+1 {
		existing := recovered.ModelCalls[len(recovered.ModelCalls)-1].Intent
		if existing.Digest != intent.Digest {
			return CampaignModelCallIntent{}, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_INTENT_CONFLICT")
		}
		return existing, nil
	}
	if len(recovered.ModelCalls) != recovered.Head.Sequence {
		return CampaignModelCallIntent{}, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_SEQUENCE_INVALID")
	}
	if err := recovered.writeModelCall(intent.Ordinal, "intent", intent); err != nil {
		return CampaignModelCallIntent{}, err
	}
	recovered.ModelCalls = append(recovered.ModelCalls, CampaignModelCallRecovery{
		Status: CampaignModelCallPrepared, Intent: intent,
	})
	return intent, nil
}

func (recovered *CampaignRecovery) DispatchModelCall() (CampaignModelCallDispatch, error) {
	if !recovered.validModelCallToken() || len(recovered.ModelCalls) != recovered.Head.Sequence+1 {
		return CampaignModelCallDispatch{}, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_RECOVERY_INVALID")
	}
	call := &recovered.ModelCalls[len(recovered.ModelCalls)-1]
	if call.Dispatch != nil {
		if recovered.activeModelDispatches[call.Dispatch.Digest] {
			return *call.Dispatch, nil
		}
		return CampaignModelCallDispatch{}, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_AMBIGUOUS")
	}
	dispatch, err := NewCampaignModelCallDispatch(call.Intent)
	if err != nil {
		return CampaignModelCallDispatch{}, err
	}
	if err := recovered.writeModelCall(call.Intent.Ordinal, "dispatch", dispatch); err != nil {
		return CampaignModelCallDispatch{}, err
	}
	call.Dispatch, call.Status = &dispatch, CampaignModelCallAmbiguous
	if recovered.activeModelDispatches == nil {
		recovered.activeModelDispatches = make(map[string]bool)
	}
	recovered.activeModelDispatches[dispatch.Digest] = true
	return dispatch, nil
}

func (recovered *CampaignRecovery) CommitModelCallResult(
	result CampaignModelCallResult,
) (CampaignModelCallResult, error) {
	if !recovered.validModelCallToken() || len(recovered.ModelCalls) != recovered.Head.Sequence+1 {
		return CampaignModelCallResult{}, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_RECOVERY_INVALID")
	}
	call := &recovered.ModelCalls[len(recovered.ModelCalls)-1]
	if call.Result != nil {
		if call.Result.Digest != result.Digest {
			return CampaignModelCallResult{}, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_RESULT_CONFLICT")
		}
		return *call.Result, nil
	}
	if call.Dispatch == nil || !recovered.activeModelDispatches[call.Dispatch.Digest] {
		return CampaignModelCallResult{}, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_AMBIGUOUS")
	}
	if err := result.ValidateInputs(call.Intent, *call.Dispatch); err != nil {
		return CampaignModelCallResult{}, err
	}
	if err := recovered.writeModelCall(call.Intent.Ordinal, "result", result); err != nil {
		return CampaignModelCallResult{}, err
	}
	call.Result, call.Status = &result, result.Status
	delete(recovered.activeModelDispatches, call.Dispatch.Digest)
	return result, nil
}

func (recovered *CampaignRecovery) validModelCallToken() bool {
	return recovered != nil && recovered.directory != "" && recovered.Failure == nil &&
		recovered.Config.Digest == recovered.validatedConfigDigest &&
		recovered.Head.Digest == recovered.validatedHeadDigest
}

func (recovered *CampaignRecovery) writeModelCall(ordinal int, stage string, value any) error {
	encoded, err := campaignJSONBytes(value)
	if err != nil {
		return err
	}
	return writeCampaignFileNoReplace(
		filepath.Join(recovered.directory, campaignModelCallsDir), campaignModelCallFile(ordinal, stage), encoded,
	)
}

// PreparePlannedAttempt binds the exact next request before target execution; repeating it is idempotent.
func (recovered *CampaignRecovery) PreparePlannedAttempt(
	planned CampaignPlannedAttempt,
) (CampaignPlannedAttempt, error) {
	if recovered == nil || recovered.directory == "" || recovered.Failure != nil ||
		recovered.Config.Digest != recovered.validatedConfigDigest ||
		recovered.Head.Digest != recovered.validatedHeadDigest {
		return CampaignPlannedAttempt{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_RECOVERY_TOKEN_INVALID")
	}
	if recovered.Config.AttemptInputMode != CampaignInputPlanned {
		return CampaignPlannedAttempt{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_PLAN_MODE_REQUIRED")
	}
	if recovered.Config.PlannerMode == CampaignPlannerDurableCall {
		if len(recovered.ModelCalls) != recovered.Head.Sequence+1 {
			return CampaignPlannedAttempt{}, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_RESULT_REQUIRED")
		}
		call := recovered.ModelCalls[len(recovered.ModelCalls)-1]
		if call.Result == nil || call.Result.Status != CampaignModelCallCompleted ||
			planned.PlanningWork != call.Result.Work {
			return CampaignPlannedAttempt{}, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_PLAN_MISMATCH")
		}
	}
	request, err := NewCampaignAttemptRequest(recovered.Config, recovered.Head)
	if err != nil {
		return CampaignPlannedAttempt{}, err
	}
	if err := planned.ValidateRequest(request); err != nil {
		return CampaignPlannedAttempt{}, err
	}
	if len(recovered.PlannedAttempts) == recovered.Head.Sequence+1 {
		existing := recovered.PlannedAttempts[len(recovered.PlannedAttempts)-1]
		if existing.Digest != planned.Digest {
			return CampaignPlannedAttempt{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_PLAN_CONFLICT")
		}
		return existing, nil
	}
	if len(recovered.PlannedAttempts) != recovered.Head.Sequence {
		return CampaignPlannedAttempt{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_PLAN_SEQUENCE_INVALID")
	}
	encoded, err := campaignJSONBytes(planned)
	if err != nil {
		return CampaignPlannedAttempt{}, err
	}
	directory := filepath.Join(recovered.directory, campaignPlansDir)
	name := campaignCheckpointFile(request.Ordinal)
	if err := writeCampaignFileNoReplace(directory, name, encoded); err != nil {
		return CampaignPlannedAttempt{}, err
	}
	var checked CampaignPlannedAttempt
	if err := readCampaignJSON(filepath.Join(directory, name), &checked); err != nil {
		return CampaignPlannedAttempt{}, err
	}
	if checked.Digest != planned.Digest || checked.ValidateRequest(request) != nil {
		return CampaignPlannedAttempt{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_PLAN_COMMIT_MISMATCH")
	}
	recovered.PlannedAttempts = append(recovered.PlannedAttempts, checked)
	return checked, nil
}

func (recovered *CampaignRecovery) CommitAttempt(
	record CampaignAttemptRecord,
	artifact []byte,
	elapsedMillis int64,
) (CampaignCheckpoint, error) {
	if recovered == nil || recovered.directory == "" || recovered.Failure != nil ||
		recovered.validatedFailureDigest != "" ||
		recovered.Config.Digest != recovered.validatedConfigDigest ||
		recovered.Head.Digest != recovered.validatedHeadDigest {
		return CampaignCheckpoint{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_RECOVERY_TOKEN_INVALID")
	}
	if recovered.Config.AttemptInputMode == CampaignInputPlanned &&
		len(recovered.PlannedAttempts) != recovered.Head.Sequence+1 {
		return CampaignCheckpoint{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_PLAN_REQUIRED")
	}
	if recovered.Config.PlannerMode == CampaignPlannerDurableCall {
		if len(recovered.ModelCalls) != recovered.Head.Sequence+1 ||
			recovered.ModelCalls[len(recovered.ModelCalls)-1].Status != CampaignModelCallCompleted {
			return CampaignCheckpoint{}, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_RESULT_REQUIRED")
		}
	}
	if len(recovered.PlannedAttempts) == recovered.Head.Sequence+1 &&
		record.InputDigest != recovered.PlannedAttempts[len(recovered.PlannedAttempts)-1].Digest {
		return CampaignCheckpoint{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_PLAN_RECORD_MISMATCH")
	}
	next, err := commitCampaignAttempt(
		recovered.directory, recovered.Config, recovered.Head, record, artifact, elapsedMillis,
	)
	if err != nil {
		return CampaignCheckpoint{}, err
	}
	recovered.Checkpoints = append(recovered.Checkpoints, next)
	recovered.Head = next
	recovered.Head.Record = cloneCampaignRecord(next.Record)
	recovered.validatedHeadDigest = next.Digest
	recovered.OrphanArtifactDigests = removeCampaignDigest(
		recovered.OrphanArtifactDigests, record.ArtifactDigest,
	)
	return recovered.Head, nil
}

// FailAttempt durably closes the exact next request without inventing an
// artifact or WorkLedger. It is only for provider/coordinator failures whose
// executed cost cannot be represented as a terminal attempt result.
func (recovered *CampaignRecovery) FailAttempt(
	request CampaignAttemptRequest,
	code string,
) (CampaignFailureMarker, error) {
	if recovered == nil || recovered.directory == "" || recovered.Failure != nil ||
		recovered.validatedFailureDigest != "" ||
		recovered.Config.Digest != recovered.validatedConfigDigest ||
		recovered.Head.Digest != recovered.validatedHeadDigest {
		return CampaignFailureMarker{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_RECOVERY_TOKEN_INVALID")
	}
	marker, err := NewCampaignFailureMarker(recovered.Config, recovered.Head, request, code)
	if err != nil {
		return CampaignFailureMarker{}, err
	}
	checked, err := commitCampaignFailureMarker(
		recovered.directory, recovered.Config, recovered.Head, marker,
	)
	if err != nil {
		return CampaignFailureMarker{}, err
	}
	recovered.Failure = &checked
	recovered.validatedFailureDigest = checked.Digest
	return checked, nil
}

func commitCampaignAttempt(
	directory string,
	config CampaignConfig,
	previous CampaignCheckpoint,
	record CampaignAttemptRecord,
	artifact []byte,
	elapsedMillis int64,
) (CampaignCheckpoint, error) {
	clean, err := validateCampaignDirectoryArgument(directory)
	if err != nil {
		return CampaignCheckpoint{}, err
	}
	if err := config.Validate(); err != nil {
		return CampaignCheckpoint{}, err
	}
	if err := previous.ValidateInputs(config); err != nil {
		return CampaignCheckpoint{}, err
	}
	if err := validateCampaignRoot(clean, new([]string)); err != nil {
		return CampaignCheckpoint{}, err
	}
	var storedConfig CampaignConfig
	if err := readCampaignJSON(filepath.Join(clean, campaignConfigFile), &storedConfig); err != nil {
		return CampaignCheckpoint{}, err
	}
	if storedConfig.Validate() != nil || storedConfig.Digest != config.Digest {
		return CampaignCheckpoint{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_IDENTITY_MISMATCH")
	}
	var storedHead CampaignCheckpoint
	if err := readCampaignJSON(
		filepath.Join(clean, campaignCheckpointsDir, campaignCheckpointFile(previous.Sequence)), &storedHead,
	); err != nil {
		return CampaignCheckpoint{}, err
	}
	if storedHead.ValidateInputs(config) != nil || storedHead.Digest != previous.Digest {
		return CampaignCheckpoint{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_HEAD_MISMATCH")
	}
	if _, err := os.Lstat(filepath.Join(clean, campaignFailureFile)); err == nil {
		return CampaignCheckpoint{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_FAILURE_LOCKED")
	} else if !os.IsNotExist(err) {
		return CampaignCheckpoint{}, fmt.Errorf("EXPERIMENT_CAMPAIGN_STORE_FAILURE_STAT: %w", err)
	}
	nextPath := filepath.Join(
		clean, campaignCheckpointsDir, campaignCheckpointFile(previous.Sequence+1),
	)
	if _, err := os.Lstat(nextPath); err == nil {
		return CampaignCheckpoint{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_HEAD_STALE")
	} else if !os.IsNotExist(err) {
		return CampaignCheckpoint{}, fmt.Errorf("EXPERIMENT_CAMPAIGN_STORE_HEAD_STAT: %w", err)
	}
	if CampaignArtifactDigest(artifact) != record.ArtifactDigest {
		return CampaignCheckpoint{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_ARTIFACT_INPUT_MISMATCH")
	}
	next, err := previous.AppendAttempt(config, record, elapsedMillis)
	if err != nil {
		return CampaignCheckpoint{}, err
	}
	artifactDirectory := filepath.Join(clean, campaignArtifactsDir)
	artifactName := record.ArtifactDigest + campaignArtifactSuffix
	if err := ensureCampaignArtifact(artifactDirectory, artifactName, record.ArtifactDigest, artifact); err != nil {
		return CampaignCheckpoint{}, err
	}
	checkpointBytes, err := campaignJSONBytes(next)
	if err != nil {
		return CampaignCheckpoint{}, err
	}
	if err := writeCampaignFileNoReplace(
		filepath.Join(clean, campaignCheckpointsDir), campaignCheckpointFile(next.Sequence), checkpointBytes,
	); err != nil {
		return CampaignCheckpoint{}, err
	}
	var checked CampaignCheckpoint
	if err := readCampaignJSON(nextPath, &checked); err != nil {
		return CampaignCheckpoint{}, err
	}
	if checked.Digest != next.Digest || checked.ValidatePrevious(config, previous) != nil {
		return CampaignCheckpoint{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_COMMIT_MISMATCH")
	}
	return checked, nil
}

func commitCampaignFailureMarker(
	directory string,
	config CampaignConfig,
	head CampaignCheckpoint,
	marker CampaignFailureMarker,
) (CampaignFailureMarker, error) {
	clean, err := validateCampaignDirectoryArgument(directory)
	if err != nil {
		return CampaignFailureMarker{}, err
	}
	if err := marker.ValidateInputs(config, head); err != nil {
		return CampaignFailureMarker{}, err
	}
	if err := validateCampaignRoot(clean, new([]string)); err != nil {
		return CampaignFailureMarker{}, err
	}
	var storedConfig CampaignConfig
	if err := readCampaignJSON(filepath.Join(clean, campaignConfigFile), &storedConfig); err != nil {
		return CampaignFailureMarker{}, err
	}
	if storedConfig.Validate() != nil || storedConfig.Digest != config.Digest {
		return CampaignFailureMarker{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_IDENTITY_MISMATCH")
	}
	var storedHead CampaignCheckpoint
	if err := readCampaignJSON(
		filepath.Join(clean, campaignCheckpointsDir, campaignCheckpointFile(head.Sequence)), &storedHead,
	); err != nil {
		return CampaignFailureMarker{}, err
	}
	if storedHead.ValidateInputs(config) != nil || storedHead.Digest != head.Digest {
		return CampaignFailureMarker{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_HEAD_MISMATCH")
	}
	nextPath := filepath.Join(clean, campaignCheckpointsDir, campaignCheckpointFile(head.Sequence+1))
	if _, err := os.Lstat(nextPath); err == nil {
		return CampaignFailureMarker{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_HEAD_STALE")
	} else if !os.IsNotExist(err) {
		return CampaignFailureMarker{}, fmt.Errorf("EXPERIMENT_CAMPAIGN_STORE_HEAD_STAT: %w", err)
	}
	encoded, err := campaignJSONBytes(marker)
	if err != nil {
		return CampaignFailureMarker{}, err
	}
	if err := writeCampaignFileNoReplace(clean, campaignFailureFile, encoded); err != nil {
		return CampaignFailureMarker{}, err
	}
	var checked CampaignFailureMarker
	if err := readCampaignJSON(filepath.Join(clean, campaignFailureFile), &checked); err != nil {
		return CampaignFailureMarker{}, err
	}
	if checked.Digest != marker.Digest || checked.ValidateInputs(config, head) != nil {
		return CampaignFailureMarker{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_FAILURE_COMMIT_MISMATCH")
	}
	return checked, nil
}

func RecoverCampaignDirectory(
	directory string,
	expected CampaignConfig,
) (CampaignRecovery, error) {
	clean, err := validateCampaignDirectoryArgument(directory)
	if err != nil {
		return CampaignRecovery{}, err
	}
	if err := expected.Validate(); err != nil {
		return CampaignRecovery{}, err
	}
	pending := make([]string, 0)
	if err := validateCampaignRoot(clean, &pending); err != nil {
		return CampaignRecovery{}, err
	}
	var stored CampaignConfig
	if err := readCampaignJSON(filepath.Join(clean, campaignConfigFile), &stored); err != nil {
		return CampaignRecovery{}, err
	}
	if err := stored.Validate(); err != nil {
		return CampaignRecovery{}, err
	}
	if stored.Digest != expected.Digest {
		return CampaignRecovery{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_IDENTITY_MISMATCH")
	}
	checkpoints, err := readCampaignCheckpoints(clean, stored, &pending)
	if err != nil {
		return CampaignRecovery{}, err
	}
	artifacts, err := readCampaignArtifacts(clean, &pending)
	if err != nil {
		return CampaignRecovery{}, err
	}
	plans, err := readCampaignPlans(clean, stored, checkpoints, &pending)
	if err != nil {
		return CampaignRecovery{}, err
	}
	modelCalls, err := readCampaignModelCalls(clean, stored, checkpoints, plans, &pending)
	if err != nil {
		return CampaignRecovery{}, err
	}
	referenced := make(map[string]bool, len(checkpoints)-1)
	for _, checkpoint := range checkpoints[1:] {
		digest := checkpoint.Record.ArtifactDigest
		if !artifacts[digest] {
			return CampaignRecovery{}, errors.New("EXPERIMENT_CAMPAIGN_STORE_ARTIFACT_MISSING")
		}
		referenced[digest] = true
	}
	orphans := make([]string, 0)
	for digest := range artifacts {
		if !referenced[digest] {
			orphans = append(orphans, digest)
		}
	}
	sort.Strings(orphans)
	sort.Strings(pending)
	head := checkpoints[len(checkpoints)-1]
	head.Record = cloneCampaignRecord(head.Record)
	failure, err := readCampaignFailure(clean, stored, head)
	if err != nil {
		return CampaignRecovery{}, err
	}
	validatedFailureDigest := ""
	if failure != nil {
		validatedFailureDigest = failure.Digest
	}
	return CampaignRecovery{
		Config: stored, Checkpoints: checkpoints, Head: head, Failure: failure,
		OrphanArtifactDigests: orphans, PendingRelativeFilePaths: pending,
		PlannedAttempts: plans, ModelCalls: modelCalls,
		directory: clean, validatedConfigDigest: stored.Digest, validatedHeadDigest: head.Digest,
		validatedFailureDigest: validatedFailureDigest,
	}, nil
}

func readCampaignFailure(
	root string,
	config CampaignConfig,
	head CampaignCheckpoint,
) (*CampaignFailureMarker, error) {
	path := filepath.Join(root, campaignFailureFile)
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("EXPERIMENT_CAMPAIGN_STORE_FAILURE_STAT: %w", err)
	}
	var marker CampaignFailureMarker
	if err := readCampaignJSON(path, &marker); err != nil {
		return nil, err
	}
	if err := marker.ValidateInputs(config, head); err != nil {
		return nil, err
	}
	return &marker, nil
}

func removeCampaignDigest(digests []string, removed string) []string {
	filtered := make([]string, 0, len(digests))
	for _, digest := range digests {
		if digest != removed {
			filtered = append(filtered, digest)
		}
	}
	return filtered
}

func validateCampaignDirectoryArgument(directory string) (string, error) {
	clean := filepath.Clean(directory)
	if directory == "" || clean == "." || clean == string(filepath.Separator) {
		return "", errors.New("EXPERIMENT_CAMPAIGN_STORE_DIRECTORY_INVALID")
	}
	absolute, err := filepath.Abs(clean)
	if err != nil {
		return "", errors.New("EXPERIMENT_CAMPAIGN_STORE_DIRECTORY_INVALID")
	}
	return absolute, nil
}

func validateCampaignRoot(root string, pending *[]string) error {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("EXPERIMENT_CAMPAIGN_STORE_LAYOUT_INVALID")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("EXPERIMENT_CAMPAIGN_STORE_LAYOUT_READ: %w", err)
	}
	seen := make(map[string]bool, 6)
	for _, entry := range entries {
		name := entry.Name()
		switch name {
		case campaignConfigFile, campaignFailureFile:
			if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
				return errors.New("EXPERIMENT_CAMPAIGN_STORE_LAYOUT_INVALID")
			}
			seen[name] = true
		case campaignArtifactsDir, campaignCheckpointsDir, campaignPlansDir, campaignModelCallsDir:
			if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
				return errors.New("EXPERIMENT_CAMPAIGN_STORE_LAYOUT_INVALID")
			}
			seen[name] = true
		default:
			if !campaignPendingFile(entry) {
				return errors.New("EXPERIMENT_CAMPAIGN_STORE_UNKNOWN_FILE")
			}
			*pending = append(*pending, name)
		}
	}
	if !seen[campaignConfigFile] || !seen[campaignArtifactsDir] || !seen[campaignCheckpointsDir] ||
		!seen[campaignPlansDir] || !seen[campaignModelCallsDir] {
		return errors.New("EXPERIMENT_CAMPAIGN_STORE_LAYOUT_INVALID")
	}
	return nil
}

func readCampaignModelCalls(
	root string,
	config CampaignConfig,
	checkpoints []CampaignCheckpoint,
	plans []CampaignPlannedAttempt,
	pending *[]string,
) ([]CampaignModelCallRecovery, error) {
	directory := filepath.Join(root, campaignModelCallsDir)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("EXPERIMENT_CAMPAIGN_MODEL_CALL_READ: %w", err)
	}
	if len(entries) == 0 {
		if config.PlannerMode == CampaignPlannerDurableCall && len(checkpoints) > 1 {
			return nil, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_MISSING")
		}
		return nil, nil
	}
	if config.PlannerMode != CampaignPlannerDurableCall {
		return nil, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_MODE_INVALID")
	}
	byOrdinal := make(map[int]*CampaignModelCallRecovery)
	for _, entry := range entries {
		if campaignPendingFile(entry) {
			*pending = append(*pending, filepath.Join(campaignModelCallsDir, entry.Name()))
			continue
		}
		ordinal, stage, ok := parseCampaignModelCallFile(entry.Name())
		if !ok || ordinal <= 0 || entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return nil, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_FILE_INVALID")
		}
		call := byOrdinal[ordinal]
		if call == nil {
			call = &CampaignModelCallRecovery{}
			byOrdinal[ordinal] = call
		}
		path := filepath.Join(directory, entry.Name())
		switch stage {
		case "intent":
			if call.Intent.Digest != "" || readCampaignJSON(path, &call.Intent) != nil {
				return nil, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_INTENT_READ_INVALID")
			}
		case "dispatch":
			if call.Dispatch != nil {
				return nil, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_DISPATCH_DUPLICATE")
			}
			call.Dispatch = new(CampaignModelCallDispatch)
			if err := readCampaignJSON(path, call.Dispatch); err != nil {
				return nil, err
			}
		case "result":
			if call.Result != nil {
				return nil, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_RESULT_DUPLICATE")
			}
			call.Result = new(CampaignModelCallResult)
			if err := readCampaignJSON(path, call.Result); err != nil {
				return nil, err
			}
		}
	}
	headSequence := len(checkpoints) - 1
	if len(byOrdinal) > headSequence+1 {
		return nil, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_SEQUENCE_INVALID")
	}
	calls := make([]CampaignModelCallRecovery, 0, len(byOrdinal))
	for ordinal := 1; ordinal <= len(byOrdinal); ordinal++ {
		call := byOrdinal[ordinal]
		if call == nil || ordinal > len(checkpoints) || call.Intent.Digest == "" {
			return nil, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_SEQUENCE_INVALID")
		}
		request, err := NewCampaignAttemptRequest(config, checkpoints[ordinal-1])
		if err != nil || call.Intent.ValidateRequest(request) != nil {
			return nil, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_INPUT_MISMATCH")
		}
		call.Status = CampaignModelCallPrepared
		if call.Dispatch != nil {
			if err := call.Dispatch.ValidateInputs(call.Intent); err != nil {
				return nil, err
			}
			call.Status = CampaignModelCallAmbiguous
		}
		if call.Result != nil {
			if call.Dispatch == nil || call.Result.ValidateInputs(call.Intent, *call.Dispatch) != nil {
				return nil, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_RESULT_INPUT_MISMATCH")
			}
			call.Status = call.Result.Status
		}
		if ordinal <= headSequence && call.Status != CampaignModelCallCompleted {
			return nil, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_COMMITTED_INCOMPLETE")
		}
		if ordinal <= len(plans) && (call.Status != CampaignModelCallCompleted ||
			plans[ordinal-1].PlanningWork != call.Result.Work) {
			return nil, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_PLAN_MISMATCH")
		}
		calls = append(calls, *call)
	}
	if len(calls) < headSequence {
		return nil, errors.New("EXPERIMENT_CAMPAIGN_MODEL_CALL_MISSING")
	}
	return calls, nil
}

func campaignModelCallFile(ordinal int, stage string) string {
	return fmt.Sprintf("%020d.%s.json", ordinal, stage)
}

func parseCampaignModelCallFile(name string) (int, string, bool) {
	for _, stage := range []string{"intent", "dispatch", "result"} {
		suffix := "." + stage + campaignCheckpointSuffix
		if strings.HasSuffix(name, suffix) {
			ordinal, ok := parseCampaignCheckpointFile(
				strings.TrimSuffix(name, suffix) + campaignCheckpointSuffix,
			)
			return ordinal, stage, ok
		}
	}
	return 0, "", false
}

func readCampaignPlans(
	root string,
	config CampaignConfig,
	checkpoints []CampaignCheckpoint,
	pending *[]string,
) ([]CampaignPlannedAttempt, error) {
	directory := filepath.Join(root, campaignPlansDir)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("EXPERIMENT_CAMPAIGN_STORE_PLAN_READ: %w", err)
	}
	plans := make([]CampaignPlannedAttempt, 0, len(entries))
	for _, entry := range entries {
		if campaignPendingFile(entry) {
			*pending = append(*pending, filepath.Join(campaignPlansDir, entry.Name()))
			continue
		}
		ordinal, ok := parseCampaignCheckpointFile(entry.Name())
		if !ok || ordinal <= 0 || entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return nil, errors.New("EXPERIMENT_CAMPAIGN_STORE_PLAN_FILE_INVALID")
		}
		var planned CampaignPlannedAttempt
		if err := readCampaignJSON(filepath.Join(directory, entry.Name()), &planned); err != nil {
			return nil, err
		}
		if planned.View.Request.Ordinal != ordinal {
			return nil, errors.New("EXPERIMENT_CAMPAIGN_STORE_PLAN_NAME_MISMATCH")
		}
		plans = append(plans, planned)
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].View.Request.Ordinal < plans[j].View.Request.Ordinal })
	headSequence := len(checkpoints) - 1
	if len(plans) > headSequence+1 ||
		(config.AttemptInputMode == CampaignInputPlanned && len(plans) < headSequence) ||
		(config.AttemptInputMode != CampaignInputPlanned && len(plans) > 0) {
		return nil, errors.New("EXPERIMENT_CAMPAIGN_STORE_PLAN_SEQUENCE_INVALID")
	}
	for index := range plans {
		if plans[index].View.Request.Ordinal != index+1 || index >= len(checkpoints) {
			return nil, errors.New("EXPERIMENT_CAMPAIGN_STORE_PLAN_SEQUENCE_INVALID")
		}
		request, err := NewCampaignAttemptRequest(config, checkpoints[index])
		if err != nil || plans[index].ValidateRequest(request) != nil {
			return nil, errors.New("EXPERIMENT_CAMPAIGN_STORE_PLAN_INPUT_MISMATCH")
		}
		if index < headSequence && checkpoints[index+1].Record.InputDigest != plans[index].Digest {
			return nil, errors.New("EXPERIMENT_CAMPAIGN_STORE_PLAN_RECORD_MISMATCH")
		}
	}
	return plans, nil
}

func readCampaignCheckpoints(
	root string,
	config CampaignConfig,
	pending *[]string,
) ([]CampaignCheckpoint, error) {
	directory := filepath.Join(root, campaignCheckpointsDir)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("EXPERIMENT_CAMPAIGN_STORE_CHECKPOINT_READ: %w", err)
	}
	checkpoints := make([]CampaignCheckpoint, 0, len(entries))
	for _, entry := range entries {
		if campaignPendingFile(entry) {
			*pending = append(*pending, filepath.Join(campaignCheckpointsDir, entry.Name()))
			continue
		}
		sequence, ok := parseCampaignCheckpointFile(entry.Name())
		if !ok || entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return nil, errors.New("EXPERIMENT_CAMPAIGN_STORE_CHECKPOINT_FILE_INVALID")
		}
		var checkpoint CampaignCheckpoint
		if err := readCampaignJSON(filepath.Join(directory, entry.Name()), &checkpoint); err != nil {
			return nil, err
		}
		if checkpoint.Sequence != sequence {
			return nil, errors.New("EXPERIMENT_CAMPAIGN_STORE_CHECKPOINT_NAME_MISMATCH")
		}
		checkpoints = append(checkpoints, checkpoint)
	}
	sort.Slice(checkpoints, func(left, right int) bool {
		return checkpoints[left].Sequence < checkpoints[right].Sequence
	})
	if len(checkpoints) == 0 {
		return nil, errors.New("EXPERIMENT_CAMPAIGN_STORE_CHECKPOINT_MISSING")
	}
	for index := range checkpoints {
		if checkpoints[index].Sequence != index {
			return nil, errors.New("EXPERIMENT_CAMPAIGN_STORE_CHECKPOINT_GAP")
		}
	}
	if err := ValidateCampaignCheckpointChain(config, checkpoints); err != nil {
		return nil, err
	}
	return checkpoints, nil
}

func readCampaignArtifacts(root string, pending *[]string) (map[string]bool, error) {
	directory := filepath.Join(root, campaignArtifactsDir)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("EXPERIMENT_CAMPAIGN_STORE_ARTIFACT_READ: %w", err)
	}
	artifacts := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if campaignPendingFile(entry) {
			*pending = append(*pending, filepath.Join(campaignArtifactsDir, entry.Name()))
			continue
		}
		name := entry.Name()
		digest := strings.TrimSuffix(name, campaignArtifactSuffix)
		if name != digest+campaignArtifactSuffix || !validSHA256(digest) ||
			entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return nil, errors.New("EXPERIMENT_CAMPAIGN_STORE_ARTIFACT_FILE_INVALID")
		}
		actual, err := campaignFileDigest(filepath.Join(directory, name))
		if err != nil || actual != digest {
			return nil, errors.New("EXPERIMENT_CAMPAIGN_STORE_ARTIFACT_DIGEST_MISMATCH")
		}
		artifacts[digest] = true
	}
	return artifacts, nil
}

func ensureCampaignArtifact(directory string, name string, digest string, artifact []byte) error {
	path := filepath.Join(directory, name)
	if _, err := os.Lstat(path); err == nil {
		actual, digestErr := campaignFileDigest(path)
		if digestErr != nil || actual != digest {
			return errors.New("EXPERIMENT_CAMPAIGN_STORE_ARTIFACT_CONFLICT")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("EXPERIMENT_CAMPAIGN_STORE_ARTIFACT_STAT: %w", err)
	}
	if err := writeCampaignFileNoReplace(directory, name, artifact); err != nil {
		return err
	}
	actual, err := campaignFileDigest(path)
	if err != nil || actual != digest {
		return errors.New("EXPERIMENT_CAMPAIGN_STORE_ARTIFACT_COMMIT_MISMATCH")
	}
	return nil
}

func campaignPendingFile(entry os.DirEntry) bool {
	return strings.HasPrefix(entry.Name(), campaignPendingPrefix) &&
		entry.Type().IsRegular()
}

func campaignCheckpointFile(sequence int) string {
	return fmt.Sprintf("%020d%s", sequence, campaignCheckpointSuffix)
}

func parseCampaignCheckpointFile(name string) (int, bool) {
	if len(name) != 20+len(campaignCheckpointSuffix) || !strings.HasSuffix(name, campaignCheckpointSuffix) {
		return 0, false
	}
	sequence, err := strconv.ParseInt(strings.TrimSuffix(name, campaignCheckpointSuffix), 10, 64)
	if err != nil || sequence < 0 || int64(int(sequence)) != sequence {
		return 0, false
	}
	return int(sequence), campaignCheckpointFile(int(sequence)) == name
}

func campaignJSONBytes(value any) ([]byte, error) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func readCampaignJSON(path string, target any) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() > campaignMaxJSONBytes {
		return errors.New("EXPERIMENT_CAMPAIGN_STORE_JSON_FILE_INVALID")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("EXPERIMENT_CAMPAIGN_STORE_JSON_OPEN: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, campaignMaxJSONBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("EXPERIMENT_CAMPAIGN_STORE_JSON_INVALID")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("EXPERIMENT_CAMPAIGN_STORE_JSON_TRAILING_DATA")
	}
	return nil
}

func campaignFileDigest(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("EXPERIMENT_CAMPAIGN_STORE_FILE_INVALID")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeCampaignFileNoReplace(directory string, name string, data []byte) error {
	if filepath.Base(name) != name || name == "." || name == "" {
		return errors.New("EXPERIMENT_CAMPAIGN_STORE_FILE_NAME_INVALID")
	}
	temporary, err := os.CreateTemp(directory, campaignPendingPrefix)
	if err != nil {
		return fmt.Errorf("EXPERIMENT_CAMPAIGN_STORE_TEMP_CREATE: %w", err)
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
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	closed = true
	target := filepath.Join(directory, name)
	if err := os.Link(temporaryPath, target); err != nil {
		if os.IsExist(err) {
			return errors.New("EXPERIMENT_CAMPAIGN_STORE_FILE_EXISTS")
		}
		return fmt.Errorf("EXPERIMENT_CAMPAIGN_STORE_FILE_COMMIT: %w", err)
	}
	if err := syncCampaignDirectory(directory); err != nil {
		return err
	}
	if err := os.Remove(temporaryPath); err != nil {
		return fmt.Errorf("EXPERIMENT_CAMPAIGN_STORE_TEMP_REMOVE: %w", err)
	}
	if err := syncCampaignDirectory(directory); err != nil {
		return err
	}
	return nil
}

func syncCampaignDirectory(directory string) error {
	opened, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("EXPERIMENT_CAMPAIGN_STORE_DIRECTORY_OPEN: %w", err)
	}
	defer opened.Close()
	if err := opened.Sync(); err != nil {
		return fmt.Errorf("EXPERIMENT_CAMPAIGN_STORE_DIRECTORY_SYNC: %w", err)
	}
	return nil
}
