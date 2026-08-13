package controlexperiment

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const StatelessCampaignObservationVersion = "consensus-atlas/stateless-campaign-observation/v1"

type StatelessCampaignArtifactProjection struct {
	ArtifactDigest string
	Request        CampaignAttemptRequest
	Artifact       StatelessCampaignAttemptArtifact
}

type StatelessCampaignAttemptObservation struct {
	Ordinal        int                      `json:"ordinal"`
	RecordDigest   string                   `json:"record_digest"`
	ArtifactDigest string                   `json:"artifact_digest"`
	Method         StatelessTraversalMethod `json:"method"`
	Outcome        string                   `json:"outcome"`
	Failure        *MethodFailure           `json:"failure,omitempty"`

	DiscoveryDigest              string                    `json:"discovery_digest,omitempty"`
	CorpusBaselinePSSStates      int                       `json:"corpus_baseline_pss_states,omitempty"`
	CorpusBaselinePSSSetDigest   string                    `json:"corpus_baseline_pss_set_digest,omitempty"`
	LocalIncrementalPSSStates    int                       `json:"local_incremental_pss_states,omitempty"`
	LocalIncrementalPSSSetDigest string                    `json:"local_incremental_pss_set_digest,omitempty"`
	CorpusNovelPSSStates         int                       `json:"corpus_novel_pss_states,omitempty"`
	CorpusNovelPSSKeys           []string                  `json:"corpus_novel_pss_keys,omitempty"`
	CorpusNovelPSSSetDigest      string                    `json:"corpus_novel_pss_set_digest,omitempty"`
	QualifiedExecutionAttempts   int                       `json:"qualified_execution_attempts,omitempty"`
	SearchWork                   StatelessDFSWork          `json:"search_work"`
	QualifiedExecutionWork       WorkLedger                `json:"qualified_execution_work"`
	AgentCalls                   []StatelessAgentCallAudit `json:"agent_calls,omitempty"`
	ChargedWork                  WorkLedger                `json:"charged_work"`
}

// StatelessCampaignObservation is a compact denominator-free index over
// durable search attempt artifacts. It reports discovered state sets and real
// work; it is not a coverage percentage or correctness verdict.
type StatelessCampaignObservation struct {
	SchemaVersion        string `json:"schema_version"`
	CampaignID           string `json:"campaign_id"`
	ConfigDigest         string `json:"config_digest"`
	TargetID             string `json:"target_id"`
	TargetIdentityDigest string `json:"target_identity_digest"`
	SpecDigest           string `json:"spec_digest"`
	SummaryDigest        string `json:"summary_digest"`
	HeadCheckpointDigest string `json:"head_checkpoint_digest"`
	CorpusDigest         string `json:"corpus_digest"`
	Strategy             string `json:"strategy"`

	Status        string      `json:"status"`
	StopReason    string      `json:"stop_reason"`
	AttemptCount  int         `json:"attempt_count"`
	Completed     int         `json:"completed"`
	Failed        int         `json:"failed"`
	ElapsedMillis int64       `json:"elapsed_ms"`
	Work          WorkLedger  `json:"work"`
	FailureDigest string      `json:"failure_digest,omitempty"`
	FailureWork   *WorkLedger `json:"failure_work,omitempty"`

	UnionCorpusNovelPSSStates    int                                   `json:"union_corpus_novel_pss_states"`
	UnionCorpusNovelPSSKeys      []string                              `json:"union_corpus_novel_pss_keys"`
	UnionCorpusNovelPSSSetDigest string                                `json:"union_corpus_novel_pss_set_digest"`
	Attempts                     []StatelessCampaignAttemptObservation `json:"attempts"`
	Digest                       string                                `json:"digest"`
}

func NewStatelessCampaignObservation(
	summary CampaignSummary,
	spec StatelessCampaignSpec,
	projections []StatelessCampaignArtifactProjection,
) (StatelessCampaignObservation, error) {
	if summary.Validate() != nil || spec.Validate() != nil || len(projections) != len(summary.Attempts) ||
		summary.CampaignID == "" || summary.TargetID != spec.TargetID ||
		summary.TargetIdentityDigest != spec.TargetIdentityDigest ||
		summary.ExperimentSpecDigest != spec.Digest {
		return StatelessCampaignObservation{}, errors.New("EXPERIMENT_STATELESS_OBSERVATION_INPUT_INVALID")
	}
	observation := StatelessCampaignObservation{
		SchemaVersion: StatelessCampaignObservationVersion,
		CampaignID:    summary.CampaignID, ConfigDigest: summary.ConfigDigest,
		TargetID: spec.TargetID, TargetIdentityDigest: spec.TargetIdentityDigest,
		SpecDigest: spec.Digest, SummaryDigest: summary.Digest,
		HeadCheckpointDigest: summary.HeadCheckpointDigest,
		CorpusDigest:         spec.CorpusDigest, Strategy: spec.Methods[0].Strategy,
		Status: summary.Status, StopReason: summary.StopReason,
		AttemptCount: summary.Sequence, ElapsedMillis: summary.ElapsedMillis, Work: summary.Totals,
	}
	if summary.Failure != nil {
		failureWork := summary.Failure.Work
		observation.FailureDigest = summary.Failure.Digest
		observation.FailureWork = &failureWork
	}
	novel := make(map[string]bool)
	for index, projection := range projections {
		source := summary.Attempts[index]
		artifact := projection.Artifact
		if source.Ordinal != index+1 || projection.ArtifactDigest != source.Record.ArtifactDigest ||
			projection.Request.CampaignID != summary.CampaignID ||
			projection.Request.ConfigDigest != summary.ConfigDigest ||
			projection.Request.Ordinal != source.Ordinal || artifact.ValidateInputs(projection.Request) != nil ||
			artifact.Spec.Digest != spec.Digest ||
			artifact.Method.Digest != spec.Methods[index].Digest || artifact.Outcome != source.Record.Outcome ||
			artifact.Work != source.Record.Work || source.Record.InputDigest != spec.Digest ||
			!sameStatelessCampaignFailure(artifact.Failure, source.Record.Failure) {
			return StatelessCampaignObservation{}, errors.New("EXPERIMENT_STATELESS_OBSERVATION_ATTEMPT_MISMATCH")
		}
		attempt := StatelessCampaignAttemptObservation{
			Ordinal: source.Ordinal, RecordDigest: source.Record.Digest,
			ArtifactDigest: projection.ArtifactDigest, Method: artifact.Method,
			Outcome: artifact.Outcome, Failure: cloneMethodFailure(artifact.Failure),
			SearchWork: artifact.SearchWork, QualifiedExecutionWork: artifact.QualifiedExecutionWork,
			AgentCalls:  cloneStatelessAgentCallAudits(artifact.AgentCalls),
			ChargedWork: artifact.Work,
		}
		if artifact.Outcome == CampaignAttemptCompleted {
			observation.Completed++
			discovery := artifact.Discovery
			attempt.DiscoveryDigest = discovery.Digest
			attempt.CorpusBaselinePSSStates = discovery.CorpusBaselinePSSStates
			attempt.CorpusBaselinePSSSetDigest = discovery.CorpusBaselinePSSSetDigest
			attempt.LocalIncrementalPSSStates = discovery.LocalIncrementalPSSStates
			attempt.LocalIncrementalPSSSetDigest = discovery.LocalIncrementalPSSSetDigest
			attempt.CorpusNovelPSSStates = discovery.CorpusNovelPSSStates
			attempt.CorpusNovelPSSKeys = append([]string(nil), discovery.CorpusNovelPSSKeys...)
			attempt.CorpusNovelPSSSetDigest = discovery.CorpusNovelPSSSetDigest
			attempt.QualifiedExecutionAttempts = discovery.QualifiedExecutionAttempts
			for _, key := range discovery.CorpusNovelPSSKeys {
				novel[key] = true
			}
		} else {
			observation.Failed++
		}
		observation.Attempts = append(observation.Attempts, attempt)
	}
	observation.UnionCorpusNovelPSSKeys = make([]string, 0, len(novel))
	for key := range novel {
		observation.UnionCorpusNovelPSSKeys = append(observation.UnionCorpusNovelPSSKeys, key)
	}
	sort.Strings(observation.UnionCorpusNovelPSSKeys)
	if observation.UnionCorpusNovelPSSKeys == nil {
		observation.UnionCorpusNovelPSSKeys = []string{}
	}
	observation.UnionCorpusNovelPSSStates = len(observation.UnionCorpusNovelPSSKeys)
	var err error
	observation.UnionCorpusNovelPSSSetDigest, err = control.CanonicalDigest(
		observation.UnionCorpusNovelPSSKeys,
	)
	if err != nil {
		return StatelessCampaignObservation{}, err
	}
	sealed, err := observation.seal()
	if err != nil {
		return StatelessCampaignObservation{}, errors.New("EXPERIMENT_STATELESS_OBSERVATION_SEAL_FAILED")
	}
	if err := sealed.Validate(); err != nil {
		return StatelessCampaignObservation{}, fmt.Errorf("EXPERIMENT_STATELESS_OBSERVATION_INVALID: %w", err)
	}
	return sealed, nil
}

func (observation StatelessCampaignObservation) Validate() error {
	if observation.SchemaVersion != StatelessCampaignObservationVersion ||
		!validMethodToken(observation.CampaignID) || !validSHA256(observation.ConfigDigest) ||
		!validMethodToken(observation.TargetID) || !validSHA256(observation.TargetIdentityDigest) ||
		!validSHA256(observation.SpecDigest) || !validSHA256(observation.SummaryDigest) ||
		!validSHA256(observation.HeadCheckpointDigest) || !validSHA256(observation.CorpusDigest) ||
		observation.Strategy == "" || !validStatelessObservationStatus(observation.Status, observation.StopReason) ||
		observation.AttemptCount != len(observation.Attempts) ||
		observation.Completed+observation.Failed != observation.AttemptCount ||
		observation.ElapsedMillis < 0 || validateMethodWork(observation.Work) != nil ||
		observation.UnionCorpusNovelPSSStates != len(observation.UnionCorpusNovelPSSKeys) ||
		!canonicalStrings(observation.UnionCorpusNovelPSSKeys, false) ||
		!validSHA256(observation.UnionCorpusNovelPSSSetDigest) {
		return errors.New("EXPERIMENT_STATELESS_OBSERVATION_INVALID")
	}
	completed, failed := 0, 0
	union := make(map[string]bool)
	total := emptyWork()
	for index, attempt := range observation.Attempts {
		if attempt.Ordinal != index+1 || !validSHA256(attempt.RecordDigest) ||
			!validSHA256(attempt.ArtifactDigest) || attempt.Method.Validate() != nil ||
			attempt.Method.Strategy != observation.Strategy ||
			!validStatelessDFSWork(attempt.SearchWork, int(^uint(0)>>1)) ||
			validateMethodWork(attempt.QualifiedExecutionWork) != nil ||
			attempt.QualifiedExecutionWork.Model != (ModelWork{}) || validateMethodWork(attempt.ChargedWork) != nil {
			return errors.New("EXPERIMENT_STATELESS_OBSERVATION_ATTEMPT_INVALID")
		}
		if !validStatelessCampaignAgentCalls(attempt.AgentCalls, attempt.ChargedWork.Model, attempt.Method) {
			return errors.New("EXPERIMENT_STATELESS_OBSERVATION_AGENT_CALL_INVALID")
		}
		if attempt.Outcome == CampaignAttemptCompleted {
			completed++
			if attempt.Failure != nil || !validSHA256(attempt.DiscoveryDigest) ||
				!validSHA256(attempt.CorpusBaselinePSSSetDigest) ||
				!validSHA256(attempt.LocalIncrementalPSSSetDigest) ||
				!validSHA256(attempt.CorpusNovelPSSSetDigest) || attempt.QualifiedExecutionAttempts <= 0 ||
				attempt.CorpusNovelPSSStates != len(attempt.CorpusNovelPSSKeys) ||
				!canonicalStrings(attempt.CorpusNovelPSSKeys, false) {
				return errors.New("EXPERIMENT_STATELESS_OBSERVATION_COMPLETED_INVALID")
			}
			wantNovel, err := control.CanonicalDigest(attempt.CorpusNovelPSSKeys)
			if err != nil || wantNovel != attempt.CorpusNovelPSSSetDigest {
				return errors.New("EXPERIMENT_STATELESS_OBSERVATION_NOVEL_DIGEST_MISMATCH")
			}
			for _, key := range attempt.CorpusNovelPSSKeys {
				union[key] = true
			}
		} else if attempt.Outcome != CampaignAttemptFailed || attempt.Failure == nil {
			return errors.New("EXPERIMENT_STATELESS_OBSERVATION_FAILED_INVALID")
		} else {
			failed++
		}
		total = addWorkLedgers(total, attempt.ChargedWork)
	}
	if observation.Completed != completed || observation.Failed != failed {
		return errors.New("EXPERIMENT_STATELESS_OBSERVATION_OUTCOME_COUNT_MISMATCH")
	}
	if observation.Status == CampaignSummaryStatusFailed {
		if !validSHA256(observation.FailureDigest) || observation.FailureWork == nil ||
			validateMethodWork(*observation.FailureWork) != nil {
			return errors.New("EXPERIMENT_STATELESS_OBSERVATION_FAILURE_INVALID")
		}
		total = addWorkLedgers(total, *observation.FailureWork)
	} else if observation.FailureDigest != "" || observation.FailureWork != nil {
		return errors.New("EXPERIMENT_STATELESS_OBSERVATION_FAILURE_INVALID")
	}
	if total != observation.Work {
		return errors.New("EXPERIMENT_STATELESS_OBSERVATION_WORK_MISMATCH")
	}
	wantUnionKeys := make([]string, 0, len(union))
	for key := range union {
		wantUnionKeys = append(wantUnionKeys, key)
	}
	sort.Strings(wantUnionKeys)
	if len(wantUnionKeys) != len(observation.UnionCorpusNovelPSSKeys) {
		return errors.New("EXPERIMENT_STATELESS_OBSERVATION_UNION_MISMATCH")
	}
	for index := range wantUnionKeys {
		if wantUnionKeys[index] != observation.UnionCorpusNovelPSSKeys[index] {
			return errors.New("EXPERIMENT_STATELESS_OBSERVATION_UNION_MISMATCH")
		}
	}
	wantUnion, err := control.CanonicalDigest(observation.UnionCorpusNovelPSSKeys)
	if err != nil || wantUnion != observation.UnionCorpusNovelPSSSetDigest {
		return errors.New("EXPERIMENT_STATELESS_OBSERVATION_UNION_DIGEST_MISMATCH")
	}
	want, err := observation.seal()
	if err != nil || !validSHA256(observation.Digest) || want.Digest != observation.Digest {
		return errors.New("EXPERIMENT_STATELESS_OBSERVATION_DIGEST_MISMATCH")
	}
	return nil
}

func DecodeStatelessCampaignObservation(encoded []byte) (StatelessCampaignObservation, error) {
	var observation StatelessCampaignObservation
	if len(encoded) == 0 || len(encoded) > campaignMaxJSONBytes {
		return StatelessCampaignObservation{}, errors.New("EXPERIMENT_STATELESS_OBSERVATION_JSON_INVALID")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&observation); err != nil || observation.Validate() != nil {
		return StatelessCampaignObservation{}, errors.New("EXPERIMENT_STATELESS_OBSERVATION_JSON_INVALID")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return StatelessCampaignObservation{}, errors.New("EXPERIMENT_STATELESS_OBSERVATION_JSON_INVALID")
	}
	return observation, nil
}

func validStatelessObservationStatus(status string, stopReason string) bool {
	switch status {
	case CampaignSummaryStatusRunning:
		return stopReason == CampaignStopRunning
	case CampaignSummaryStatusStopped:
		return stopReason == CampaignStopAttemptLimit || stopReason == CampaignStopLogicalBudget ||
			stopReason == CampaignStopWallClock
	case CampaignSummaryStatusFailed:
		return stopReason == CampaignStopRunning
	default:
		return false
	}
}

func sameStatelessCampaignFailure(left *MethodFailure, right *MethodFailure) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (observation StatelessCampaignObservation) seal() (StatelessCampaignObservation, error) {
	if len(observation.UnionCorpusNovelPSSKeys) == 0 {
		observation.UnionCorpusNovelPSSKeys = []string{}
	} else {
		observation.UnionCorpusNovelPSSKeys = append([]string(nil), observation.UnionCorpusNovelPSSKeys...)
	}
	observation.Attempts = append([]StatelessCampaignAttemptObservation(nil), observation.Attempts...)
	for index := range observation.Attempts {
		observation.Attempts[index].CorpusNovelPSSKeys = append(
			[]string(nil), observation.Attempts[index].CorpusNovelPSSKeys...,
		)
		observation.Attempts[index].Failure = cloneMethodFailure(observation.Attempts[index].Failure)
	}
	if observation.FailureWork != nil {
		work := *observation.FailureWork
		observation.FailureWork = &work
	}
	observation.Digest = ""
	digest, err := control.CanonicalDigest(observation)
	observation.Digest = digest
	return observation, err
}
