package controlexperiment

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sort"
	"strings"
)

const (
	CrossTargetAttemptArtifactVersion = "consensus-atlas/cross-target-attempt-artifact/v1"
	CrossTargetCampaignLedgerVersion  = "consensus-atlas/cross-target-campaign-ledger/v1"
)

// CrossTargetAttemptArtifact is target-local evidence. It is stored once in
// that target's existing Campaign artifact store; the outer ledger only keeps
// its content digest.
type CrossTargetAttemptArtifact struct {
	SchemaVersion        string          `json:"schema_version"`
	CommonViewDigest     string          `json:"common_view_digest"`
	ParentIntentDigest   string          `json:"parent_intent_digest"`
	PlannedAttemptDigest string          `json:"planned_attempt_digest"`
	Report               Report          `json:"report"`
	Bundle               ExecutionBundle `json:"bundle"`
	Outcome              IntentOutcome   `json:"outcome"`
}

func NewCrossTargetAttemptArtifact(
	common CrossTargetPlannerView,
	parent GuardedTestIntent,
	planned CampaignPlannedAttempt,
	report Report,
	bundle ExecutionBundle,
	outcome IntentOutcome,
) (CrossTargetAttemptArtifact, error) {
	artifact := CrossTargetAttemptArtifact{
		SchemaVersion:    CrossTargetAttemptArtifactVersion,
		CommonViewDigest: common.Digest, ParentIntentDigest: parent.Digest,
		PlannedAttemptDigest: planned.Digest, Report: report, Bundle: bundle, Outcome: outcome,
	}
	if err := artifact.ValidateInputs(common, parent, planned); err != nil {
		return CrossTargetAttemptArtifact{}, err
	}
	return artifact, nil
}

func (artifact CrossTargetAttemptArtifact) Validate() error {
	if artifact.SchemaVersion != CrossTargetAttemptArtifactVersion ||
		!validSHA256(artifact.CommonViewDigest) || !validSHA256(artifact.ParentIntentDigest) ||
		!validSHA256(artifact.PlannedAttemptDigest) {
		return errors.New("EXPERIMENT_CROSS_TARGET_ARTIFACT_INVALID")
	}
	if err := artifact.Report.Validate(); err != nil {
		return err
	}
	if err := artifact.Bundle.Validate(); err != nil {
		return err
	}
	if err := artifact.Outcome.Validate(); err != nil {
		return err
	}
	if artifact.Report.Work != artifact.Bundle.Work ||
		artifact.Bundle.Identity.ReportDigest != artifact.Report.Digest ||
		artifact.Outcome.ReportDigest != artifact.Report.Digest ||
		artifact.Outcome.BundleDigest != artifact.Bundle.Digest {
		return errors.New("EXPERIMENT_CROSS_TARGET_ARTIFACT_WORK_MISMATCH")
	}
	return nil
}

func (artifact CrossTargetAttemptArtifact) ValidateInputs(
	common CrossTargetPlannerView,
	parent GuardedTestIntent,
	planned CampaignPlannedAttempt,
) error {
	if err := artifact.Validate(); err != nil {
		return err
	}
	if err := common.Validate(); err != nil {
		return err
	}
	if err := parent.Validate(); err != nil {
		return err
	}
	if err := planned.Validate(); err != nil {
		return err
	}
	if err := artifact.Outcome.ValidateInputs(
		planned.Plan, planned.Instance, artifact.Report, artifact.Bundle,
	); err != nil {
		return err
	}
	if artifact.CommonViewDigest != common.Digest || parent.ViewDigest != common.Digest ||
		artifact.ParentIntentDigest != parent.Digest || artifact.PlannedAttemptDigest != planned.Digest ||
		!sameCrossTargetIntentSemantics(parent, planned.Proposal) {
		return errors.New("EXPERIMENT_CROSS_TARGET_ARTIFACT_PARENT_MISMATCH")
	}
	return nil
}

func EncodeCrossTargetAttemptArtifact(artifact CrossTargetAttemptArtifact) ([]byte, error) {
	if err := artifact.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(artifact)
}

func DecodeCrossTargetAttemptArtifact(encoded []byte) (CrossTargetAttemptArtifact, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var artifact CrossTargetAttemptArtifact
	if err := decoder.Decode(&artifact); err != nil {
		return CrossTargetAttemptArtifact{}, errors.New("EXPERIMENT_CROSS_TARGET_ARTIFACT_JSON_INVALID")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return CrossTargetAttemptArtifact{}, errors.New("EXPERIMENT_CROSS_TARGET_ARTIFACT_JSON_INVALID")
	}
	if err := artifact.Validate(); err != nil {
		return CrossTargetAttemptArtifact{}, err
	}
	return artifact, nil
}

type CrossTargetCampaignReference struct {
	Slot                 string     `json:"slot"`
	CampaignID           string     `json:"campaign_id"`
	ConfigDigest         string     `json:"config_digest"`
	TargetID             string     `json:"target_id"`
	TargetIdentityDigest string     `json:"target_identity_digest"`
	HeadCheckpointDigest string     `json:"head_checkpoint_digest"`
	AttemptOrdinal       int        `json:"attempt_ordinal"`
	AttemptRecordDigest  string     `json:"attempt_record_digest"`
	PlannedAttemptDigest string     `json:"planned_attempt_digest"`
	ArtifactDigest       string     `json:"artifact_digest"`
	PSSID                string     `json:"pss_id"`
	Work                 WorkLedger `json:"work"`
	Digest               string     `json:"digest"`
}

func newCrossTargetCampaignReference(
	slot string,
	recovered *CampaignRecovery,
	common CrossTargetPlannerView,
	parent GuardedTestIntent,
) (CrossTargetCampaignReference, error) {
	if !validMethodToken(slot) || recovered == nil || recovered.Head.Sequence != 1 ||
		len(recovered.PlannedAttempts) != 1 {
		return CrossTargetCampaignReference{}, errors.New("EXPERIMENT_CROSS_TARGET_REFERENCE_INPUT_INVALID")
	}
	summary, err := NewCampaignSummary(recovered)
	if err != nil {
		return CrossTargetCampaignReference{}, err
	}
	if summary.Sequence != 1 || len(summary.Attempts) != 1 {
		return CrossTargetCampaignReference{}, errors.New("EXPERIMENT_CROSS_TARGET_REFERENCE_ATTEMPT_INVALID")
	}
	if summary.ExperimentSpecDigest != common.Digest || summary.Status != CampaignSummaryStatusStopped ||
		summary.StopReason != CampaignStopAttemptLimit {
		return CrossTargetCampaignReference{}, errors.New("EXPERIMENT_CROSS_TARGET_REFERENCE_CAMPAIGN_MISMATCH")
	}
	encoded, err := recovered.ReadAttemptArtifact(1)
	if err != nil {
		return CrossTargetCampaignReference{}, err
	}
	artifact, err := DecodeCrossTargetAttemptArtifact(encoded)
	if err != nil {
		return CrossTargetCampaignReference{}, err
	}
	planned := recovered.PlannedAttempts[0]
	if err := artifact.ValidateInputs(common, parent, planned); err != nil {
		return CrossTargetCampaignReference{}, err
	}
	record := summary.Attempts[0].Record
	wantWork, err := planned.AttachPlanningWork(artifact.Report.Work)
	if err != nil {
		return CrossTargetCampaignReference{}, err
	}
	if planned.Digest != artifact.PlannedAttemptDigest || record.InputDigest != planned.Digest ||
		record.ArtifactDigest != CampaignArtifactDigest(encoded) || record.Outcome != CampaignAttemptCompleted ||
		record.Failure != nil || record.Work != wantWork ||
		artifact.Report.ManifestDigest != summary.TargetIdentityDigest ||
		artifact.Bundle.Identity.ManifestDigest != summary.TargetIdentityDigest {
		return CrossTargetCampaignReference{}, errors.New("EXPERIMENT_CROSS_TARGET_REFERENCE_RECORD_MISMATCH")
	}
	reference := CrossTargetCampaignReference{
		Slot: slot, CampaignID: summary.CampaignID, ConfigDigest: summary.ConfigDigest,
		TargetID: summary.TargetID, TargetIdentityDigest: summary.TargetIdentityDigest,
		HeadCheckpointDigest: summary.HeadCheckpointDigest, AttemptOrdinal: 1,
		AttemptRecordDigest: record.Digest, PlannedAttemptDigest: planned.Digest,
		ArtifactDigest: record.ArtifactDigest,
		PSSID:          artifact.Bundle.Identity.PSSID, Work: record.Work,
	}
	sealed, err := reference.seal()
	if err != nil {
		return CrossTargetCampaignReference{}, err
	}
	if err := sealed.Validate(); err != nil {
		return CrossTargetCampaignReference{}, err
	}
	return sealed, nil
}

func (reference CrossTargetCampaignReference) Validate() error {
	if !validMethodToken(reference.Slot) || !validMethodToken(reference.CampaignID) ||
		!validSHA256(reference.ConfigDigest) || !validMethodToken(reference.TargetID) ||
		!validSHA256(reference.TargetIdentityDigest) || !validSHA256(reference.HeadCheckpointDigest) ||
		reference.AttemptOrdinal != 1 || !validSHA256(reference.AttemptRecordDigest) ||
		!validSHA256(reference.PlannedAttemptDigest) || !validSHA256(reference.ArtifactDigest) ||
		reference.PSSID == "" ||
		validateMethodWork(reference.Work) != nil {
		return errors.New("EXPERIMENT_CROSS_TARGET_REFERENCE_INVALID")
	}
	sealed, err := reference.seal()
	if err != nil || !validSHA256(reference.Digest) || sealed.Digest != reference.Digest {
		return errors.New("EXPERIMENT_CROSS_TARGET_REFERENCE_DIGEST_MISMATCH")
	}
	return nil
}

func (reference CrossTargetCampaignReference) seal() (CrossTargetCampaignReference, error) {
	reference.Digest = ""
	digest, err := portableJSONDigest(reference)
	if err != nil {
		return CrossTargetCampaignReference{}, err
	}
	reference.Digest = digest
	return reference, nil
}

type CrossTargetCampaignSource struct {
	Slot     string
	Campaign *CampaignRecovery
}

// CrossTargetCampaignLedger is a composition index, not a new Campaign. It
// adds no scheduling authority and never combines target-local PSS states.
type CrossTargetCampaignLedger struct {
	SchemaVersion    string                         `json:"schema_version"`
	ID               string                         `json:"id"`
	CommonViewDigest string                         `json:"common_view_digest"`
	ParentIntent     GuardedTestIntent              `json:"parent_intent"`
	Targets          []CrossTargetCampaignReference `json:"targets"`
	Totals           WorkLedger                     `json:"totals"`
	Digest           string                         `json:"digest"`
}

func NewCrossTargetCampaignLedger(
	id string,
	common CrossTargetPlannerView,
	parent GuardedTestIntent,
	sources []CrossTargetCampaignSource,
) (CrossTargetCampaignLedger, error) {
	if !validMethodToken(id) || len(sources) < 2 || parent.ViewDigest != common.Digest {
		return CrossTargetCampaignLedger{}, errors.New("EXPERIMENT_CROSS_TARGET_LEDGER_INPUT_INVALID")
	}
	ledger := CrossTargetCampaignLedger{
		SchemaVersion: CrossTargetCampaignLedgerVersion, ID: id,
		CommonViewDigest: common.Digest, ParentIntent: parent, Totals: emptyWork(),
	}
	for _, source := range sources {
		reference, err := newCrossTargetCampaignReference(source.Slot, source.Campaign, common, parent)
		if err != nil {
			return CrossTargetCampaignLedger{}, err
		}
		ledger.Targets = append(ledger.Targets, reference)
		ledger.Totals = addWorkLedgers(ledger.Totals, reference.Work)
	}
	sort.Slice(ledger.Targets, func(i, j int) bool { return ledger.Targets[i].Slot < ledger.Targets[j].Slot })
	sealed, err := ledger.seal()
	if err != nil {
		return CrossTargetCampaignLedger{}, err
	}
	if err := sealed.Validate(); err != nil {
		return CrossTargetCampaignLedger{}, err
	}
	return sealed, nil
}

func (ledger CrossTargetCampaignLedger) Validate() error {
	if ledger.SchemaVersion != CrossTargetCampaignLedgerVersion || !validMethodToken(ledger.ID) ||
		!validSHA256(ledger.CommonViewDigest) || len(ledger.Targets) < 2 ||
		ledger.ParentIntent.Validate() != nil || ledger.ParentIntent.ViewDigest != ledger.CommonViewDigest {
		return errors.New("EXPERIMENT_CROSS_TARGET_LEDGER_INVALID")
	}
	totals := emptyWork()
	seenSlots, seenTargets, seenCampaigns := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for index, reference := range ledger.Targets {
		if reference.Validate() != nil || seenSlots[reference.Slot] || seenTargets[reference.TargetID] ||
			seenCampaigns[reference.CampaignID] || (index > 0 && ledger.Targets[index-1].Slot >= reference.Slot) {
			return errors.New("EXPERIMENT_CROSS_TARGET_LEDGER_TARGET_INVALID")
		}
		seenSlots[reference.Slot], seenTargets[reference.TargetID], seenCampaigns[reference.CampaignID] = true, true, true
		totals = addWorkLedgers(totals, reference.Work)
	}
	if totals != ledger.Totals {
		return errors.New("EXPERIMENT_CROSS_TARGET_LEDGER_TOTAL_MISMATCH")
	}
	sealed, err := ledger.seal()
	if err != nil || !validSHA256(ledger.Digest) || sealed.Digest != ledger.Digest {
		return errors.New("EXPERIMENT_CROSS_TARGET_LEDGER_DIGEST_MISMATCH")
	}
	return nil
}

func (ledger CrossTargetCampaignLedger) ValidateRecoveries(
	common CrossTargetPlannerView,
	sources []CrossTargetCampaignSource,
) error {
	if err := ledger.Validate(); err != nil {
		return err
	}
	want, err := NewCrossTargetCampaignLedger(ledger.ID, common, ledger.ParentIntent, sources)
	if err != nil {
		return err
	}
	if want.Digest != ledger.Digest {
		return errors.New("EXPERIMENT_CROSS_TARGET_LEDGER_RECOVERY_MISMATCH")
	}
	return nil
}

func (ledger CrossTargetCampaignLedger) seal() (CrossTargetCampaignLedger, error) {
	ledger.Targets = append([]CrossTargetCampaignReference(nil), ledger.Targets...)
	ledger.Digest = ""
	digest, err := portableJSONDigest(ledger)
	if err != nil {
		return CrossTargetCampaignLedger{}, err
	}
	ledger.Digest = digest
	return ledger, nil
}

func sameCrossTargetIntentSemantics(parent, child GuardedTestIntent) bool {
	wantPrefix := parent.ID + "-target-"
	return strings.HasPrefix(child.ID, wantPrefix) && len(child.ID) == len(wantPrefix)+12 &&
		child.ID == wantPrefix+child.ViewDigest[:12] &&
		parent.RiskID == child.RiskID &&
		parent.Must.Decisions == child.Must.Decisions &&
		parent.Must.FaultEnvelope == child.Must.FaultEnvelope &&
		slices.Equal(parent.Must.RequiredCapabilities, child.Must.RequiredCapabilities) &&
		slices.Equal(parent.Must.RequiredActions, child.Must.RequiredActions) &&
		slices.Equal(parent.Prefer.BackendIDs, child.Prefer.BackendIDs) &&
		slices.Equal(parent.Prefer.Actions, child.Prefer.Actions)
}
