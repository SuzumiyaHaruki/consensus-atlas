package controlexperiment

import (
	"errors"
	"fmt"
)

const (
	MethodLedgerVersion          = "consensus-atlas/method-ledger/v1"
	MethodRecordSource           = "source"
	MethodRecordProposal         = "proposal"
	MethodRecordExecution        = "execution"
	MethodOutcomeCompleted       = "completed"
	MethodOutcomeRejected        = "rejected"
	MethodOutcomeExecutionFailed = "execution-failed"
)

type MethodFailure struct {
	Phase    string `json:"phase"`
	Code     string `json:"code"`
	Decision int    `json:"decision,omitempty"`
}

type MethodRecord struct {
	Ordinal       int            `json:"ordinal"`
	Kind          string         `json:"kind"`
	Outcome       string         `json:"outcome"`
	InputDigest   string         `json:"input_digest"`
	ContextDigest string         `json:"context_digest,omitempty"`
	OutputDigest  string         `json:"output_digest,omitempty"`
	ReportDigest  string         `json:"report_digest,omitempty"`
	BundleDigest  string         `json:"bundle_digest,omitempty"`
	Failure       *MethodFailure `json:"failure,omitempty"`
	Work          WorkLedger     `json:"work"`
}

type MethodLedger struct {
	SchemaVersion              string               `json:"schema_version"`
	ID                         string               `json:"id"`
	MethodID                   string               `json:"method_id"`
	SourceCorpusDigest         string               `json:"source_corpus_digest"`
	SourceCorpus               MutationSourceCorpus `json:"source_corpus"`
	SourceEntryDigests         []string             `json:"source_entry_digests"`
	FeedbackDigest             string               `json:"feedback_digest,omitempty"`
	FeedbackSourceBundleDigest string               `json:"feedback_source_bundle_digest,omitempty"`
	Feedback                   *PSSFeedback         `json:"feedback,omitempty"`
	Records                    []MethodRecord       `json:"records"`
	Totals                     WorkLedger           `json:"totals"`
	Digest                     string               `json:"digest"`
}

func NewMethodLedger(
	id string,
	methodID string,
	corpus MutationSourceCorpus,
	feedback *PSSFeedback,
	records []MethodRecord,
) (MethodLedger, error) {
	if err := corpus.Validate(); err != nil {
		return MethodLedger{}, err
	}
	ledger := MethodLedger{
		SchemaVersion: MethodLedgerVersion, ID: id, MethodID: methodID,
		SourceCorpusDigest: corpus.Digest, SourceCorpus: corpus,
		Records: append([]MethodRecord(nil), records...),
		Totals:  sumMethodWork(records),
	}
	for _, entry := range corpus.Entries {
		ledger.SourceEntryDigests = append(ledger.SourceEntryDigests, entry.Digest)
	}
	if feedback != nil {
		if err := feedback.Validate(); err != nil {
			return MethodLedger{}, err
		}
		ledger.FeedbackDigest = feedback.Digest
		ledger.FeedbackSourceBundleDigest = feedback.SourceBundleDigest
		copyFeedback := *feedback
		ledger.Feedback = &copyFeedback
	}
	sealed, err := ledger.seal()
	if err != nil {
		return MethodLedger{}, err
	}
	if err := sealed.Validate(); err != nil {
		return MethodLedger{}, err
	}
	return sealed, nil
}

func (ledger MethodLedger) Validate() error {
	if ledger.SchemaVersion != MethodLedgerVersion || ledger.ID == "" || ledger.MethodID == "" ||
		!validSHA256(ledger.SourceCorpusDigest) || len(ledger.SourceEntryDigests) == 0 ||
		len(ledger.Records) == 0 {
		return errors.New("EXPERIMENT_METHOD_LEDGER_INVALID")
	}
	if err := ledger.SourceCorpus.Validate(); err != nil {
		return err
	}
	if ledger.SourceCorpus.Digest != ledger.SourceCorpusDigest ||
		len(ledger.SourceCorpus.Entries) != len(ledger.SourceEntryDigests) {
		return errors.New("EXPERIMENT_METHOD_LEDGER_CORPUS_MISMATCH")
	}
	seenSources := make(map[string]bool, len(ledger.SourceEntryDigests))
	for index, digest := range ledger.SourceEntryDigests {
		if !validSHA256(digest) || seenSources[digest] {
			return errors.New("EXPERIMENT_METHOD_LEDGER_SOURCE_INVALID")
		}
		if digest != ledger.SourceCorpus.Entries[index].Digest {
			return errors.New("EXPERIMENT_METHOD_LEDGER_CORPUS_MISMATCH")
		}
		seenSources[digest] = true
	}
	completedProposals := make(map[string]bool)
	completedBundles := make(map[string]bool)
	sourceIndex := 0
	sourcePhase := true
	for index, record := range ledger.Records {
		if err := record.validate(index + 1); err != nil {
			return err
		}
		switch record.Kind {
		case MethodRecordSource:
			if !sourcePhase || sourceIndex >= len(ledger.SourceEntryDigests) {
				return errors.New("EXPERIMENT_METHOD_LEDGER_SOURCE_ORDER_INVALID")
			}
			source := ledger.SourceCorpus.Entries[sourceIndex]
			if record.OutputDigest != source.Digest || record.InputDigest != source.ConfigDigest ||
				record.ReportDigest != source.ReportDigest || record.BundleDigest != source.BundleDigest {
				return errors.New("EXPERIMENT_METHOD_LEDGER_SOURCE_BINDING_INVALID")
			}
			sourceIndex++
		case MethodRecordProposal:
			sourcePhase = false
			if !seenSources[record.InputDigest] {
				return errors.New("EXPERIMENT_METHOD_LEDGER_PROPOSAL_SOURCE_INVALID")
			}
			if record.Outcome == MethodOutcomeCompleted {
				completedProposals[record.OutputDigest] = true
			}
		case MethodRecordExecution:
			sourcePhase = false
			if !completedProposals[record.InputDigest] {
				return errors.New("EXPERIMENT_METHOD_LEDGER_EXECUTION_PROPOSAL_INVALID")
			}
			if record.Outcome == MethodOutcomeCompleted {
				completedBundles[record.BundleDigest] = true
			}
		}
	}
	if sourceIndex != len(ledger.SourceEntryDigests) {
		return errors.New("EXPERIMENT_METHOD_LEDGER_SOURCE_COST_MISSING")
	}
	if ledger.FeedbackDigest == "" {
		if ledger.FeedbackSourceBundleDigest != "" || ledger.Feedback != nil {
			return errors.New("EXPERIMENT_METHOD_LEDGER_FEEDBACK_INVALID")
		}
	} else if !validSHA256(ledger.FeedbackDigest) ||
		!validSHA256(ledger.FeedbackSourceBundleDigest) ||
		!completedBundles[ledger.FeedbackSourceBundleDigest] || ledger.Feedback == nil ||
		ledger.Feedback.Validate() != nil || ledger.Feedback.Digest != ledger.FeedbackDigest ||
		ledger.Feedback.SourceBundleDigest != ledger.FeedbackSourceBundleDigest {
		return errors.New("EXPERIMENT_METHOD_LEDGER_FEEDBACK_INVALID")
	}
	if err := validateMethodWork(ledger.Totals); err != nil {
		return err
	}
	if want := sumMethodWork(ledger.Records); want != ledger.Totals {
		return errors.New("EXPERIMENT_METHOD_LEDGER_TOTAL_MISMATCH")
	}
	sealed, err := ledger.seal()
	if err != nil || sealed.Digest != ledger.Digest {
		return errors.New("EXPERIMENT_METHOD_LEDGER_DIGEST_MISMATCH")
	}
	return nil
}

func (record MethodRecord) validate(ordinal int) error {
	if record.Ordinal != ordinal || !validSHA256(record.InputDigest) ||
		(record.ContextDigest != "" && !validSHA256(record.ContextDigest)) {
		return fmt.Errorf("EXPERIMENT_METHOD_RECORD_INVALID: %d", ordinal)
	}
	if err := validateMethodWork(record.Work); err != nil {
		return err
	}
	switch record.Kind {
	case MethodRecordSource:
		if record.Outcome != MethodOutcomeCompleted || !validSHA256(record.OutputDigest) ||
			!validSHA256(record.ReportDigest) || !validSHA256(record.BundleDigest) || record.Failure != nil {
			return errors.New("EXPERIMENT_METHOD_SOURCE_RECORD_INVALID")
		}
	case MethodRecordProposal:
		if record.Outcome == MethodOutcomeCompleted {
			if !validSHA256(record.OutputDigest) || record.Failure != nil {
				return errors.New("EXPERIMENT_METHOD_PROPOSAL_RECORD_INVALID")
			}
		} else if record.Outcome != MethodOutcomeRejected || record.OutputDigest != "" ||
			record.Failure == nil {
			return errors.New("EXPERIMENT_METHOD_PROPOSAL_RECORD_INVALID")
		}
		if record.ReportDigest != "" || record.BundleDigest != "" {
			return errors.New("EXPERIMENT_METHOD_PROPOSAL_RECORD_INVALID")
		}
	case MethodRecordExecution:
		if record.Outcome == MethodOutcomeCompleted {
			if !validSHA256(record.OutputDigest) || !validSHA256(record.ReportDigest) ||
				!validSHA256(record.BundleDigest) || record.OutputDigest != record.BundleDigest ||
				record.Failure != nil {
				return errors.New("EXPERIMENT_METHOD_EXECUTION_RECORD_INVALID")
			}
		} else if record.Outcome != MethodOutcomeExecutionFailed || record.OutputDigest != "" ||
			record.ReportDigest != "" || record.BundleDigest != "" || record.Failure == nil {
			return errors.New("EXPERIMENT_METHOD_EXECUTION_RECORD_INVALID")
		}
	default:
		return errors.New("EXPERIMENT_METHOD_RECORD_KIND_INVALID")
	}
	if record.Failure != nil && (record.Failure.Phase == "" || record.Failure.Code == "" ||
		record.Failure.Decision < 0) {
		return errors.New("EXPERIMENT_METHOD_FAILURE_INVALID")
	}
	return nil
}

func validateMethodWork(work WorkLedger) error {
	for _, phase := range []PhaseWork{work.Primary, work.Replay} {
		if phase.SetupAttempts < 0 || phase.RuntimeInitializations < 0 || phase.PrepareActions < 0 ||
			phase.SchedulerDecisions < 0 ||
			phase.WorkUnits != phase.SetupAttempts+phase.PrepareActions+phase.SchedulerDecisions {
			return errors.New("EXPERIMENT_METHOD_WORK_INVALID")
		}
	}
	if work.Model.Calls < 0 || work.Model.InputTokens < 0 || work.Model.OutputTokens < 0 ||
		work.Model.TotalTokens != work.Model.InputTokens+work.Model.OutputTokens ||
		work.Resources.WallTime == "" || work.Resources.CPUTime == "" || work.Resources.PeakRSS == "" {
		return errors.New("EXPERIMENT_METHOD_WORK_INVALID")
	}
	return nil
}

func sumMethodWork(records []MethodRecord) WorkLedger {
	total := emptyWork()
	for _, record := range records {
		addPhaseWork := func(target *PhaseWork, source PhaseWork) {
			target.SetupAttempts += source.SetupAttempts
			target.RuntimeInitializations += source.RuntimeInitializations
			target.PrepareActions += source.PrepareActions
			target.SchedulerDecisions += source.SchedulerDecisions
			updateWorkUnits(target)
		}
		addPhaseWork(&total.Primary, record.Work.Primary)
		addPhaseWork(&total.Replay, record.Work.Replay)
		total.Model.Calls += record.Work.Model.Calls
		total.Model.InputTokens += record.Work.Model.InputTokens
		total.Model.OutputTokens += record.Work.Model.OutputTokens
		total.Model.TotalTokens = total.Model.InputTokens + total.Model.OutputTokens
	}
	return total
}

func (ledger MethodLedger) seal() (MethodLedger, error) {
	ledger.SourceEntryDigests = append([]string(nil), ledger.SourceEntryDigests...)
	ledger.Records = append([]MethodRecord(nil), ledger.Records...)
	if ledger.Feedback != nil {
		feedback := *ledger.Feedback
		feedback.States = append([]PSSFeedbackState(nil), ledger.Feedback.States...)
		ledger.Feedback = &feedback
	}
	ledger.Digest = ""
	digest, err := portableJSONDigest(ledger)
	if err != nil {
		return MethodLedger{}, err
	}
	ledger.Digest = digest
	return ledger, nil
}
