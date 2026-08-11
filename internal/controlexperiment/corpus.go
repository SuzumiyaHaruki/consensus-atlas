package controlexperiment

import (
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

const (
	MutationSourceEntryVersion  = "consensus-atlas/mutation-source-entry/v1"
	MutationSourceCorpusVersion = "consensus-atlas/mutation-source-corpus/v1"
	MaxMutationSources          = 64
)

// MutationSourceEntry is the small, digest-bound identity of one already
// validated ExecutionBundle. It supports any qualified source policy; the
// mutation operator's suffix policy is supplied separately.
type MutationSourceEntry struct {
	SchemaVersion   string `json:"schema_version"`
	ID              string `json:"id"`
	BundleDigest    string `json:"bundle_digest"`
	ReportDigest    string `json:"report_digest"`
	ConfigDigest    string `json:"config_digest"`
	TraceDigest     string `json:"trace_digest"`
	ManifestDigest  string `json:"manifest_digest"`
	PSSID           string `json:"pss_id"`
	PolicyID        string `json:"policy_id"`
	PolicyDigest    string `json:"policy_digest"`
	TraceSchema     string `json:"trace_schema"`
	Decisions       int    `json:"decisions"`
	CorePSSDigest   string `json:"core_pss_digest"`
	QualificationID string `json:"qualification_digest"`
	Digest          string `json:"digest"`
}

func NewMutationSourceEntry(id string, bundle ExecutionBundle) (MutationSourceEntry, error) {
	if id == "" {
		return MutationSourceEntry{}, errors.New("EXPERIMENT_MUTATION_SOURCE_ID_REQUIRED")
	}
	if err := bundle.Validate(); err != nil {
		return MutationSourceEntry{}, fmt.Errorf("EXPERIMENT_MUTATION_SOURCE_BUNDLE_INVALID: %w", err)
	}
	entry := MutationSourceEntry{
		SchemaVersion: MutationSourceEntryVersion, ID: id,
		BundleDigest: bundle.Digest, ReportDigest: bundle.Identity.ReportDigest,
		ConfigDigest: bundle.Identity.ConfigDigest, TraceDigest: bundle.Trace.Digest,
		ManifestDigest: bundle.Identity.ManifestDigest, PSSID: bundle.Identity.PSSID,
		PolicyID: bundle.Run.PolicyID, PolicyDigest: bundle.Run.PolicyDigest,
		TraceSchema: bundle.Trace.SchemaVersion, Decisions: len(bundle.Trace.Records),
		CorePSSDigest:   bundle.Run.CorePSSSamplesDigest,
		QualificationID: bundle.Qualification.Qualification.Digest,
	}
	entry, err := entry.seal()
	if err != nil {
		return MutationSourceEntry{}, err
	}
	if err := entry.Validate(); err != nil {
		return MutationSourceEntry{}, err
	}
	return entry, nil
}

func (entry MutationSourceEntry) Validate() error {
	if entry.SchemaVersion != MutationSourceEntryVersion || entry.ID == "" ||
		!validSHA256(entry.BundleDigest) || !validSHA256(entry.ReportDigest) ||
		!validSHA256(entry.ConfigDigest) || !validSHA256(entry.TraceDigest) ||
		!validSHA256(entry.ManifestDigest) || entry.PSSID == "" || entry.PolicyID == "" ||
		!validSHA256(entry.PolicyDigest) || entry.TraceSchema != controlruntime.TraceSchemaVersion ||
		entry.Decisions < 0 || !validSHA256(entry.CorePSSDigest) ||
		!validSHA256(entry.QualificationID) {
		return errors.New("EXPERIMENT_MUTATION_SOURCE_INVALID")
	}
	sealed, err := entry.seal()
	if err != nil || sealed.Digest != entry.Digest {
		return errors.New("EXPERIMENT_MUTATION_SOURCE_DIGEST_MISMATCH")
	}
	return nil
}

func (entry MutationSourceEntry) ValidateBundle(bundle ExecutionBundle) error {
	if err := entry.Validate(); err != nil {
		return err
	}
	want, err := NewMutationSourceEntry(entry.ID, bundle)
	if err != nil {
		return err
	}
	if want.Digest != entry.Digest {
		return errors.New("EXPERIMENT_MUTATION_SOURCE_BUNDLE_MISMATCH")
	}
	return nil
}

func (entry MutationSourceEntry) ValidateTrace(trace controlruntime.Trace) error {
	if err := entry.Validate(); err != nil {
		return err
	}
	if err := trace.Validate(); err != nil {
		return err
	}
	if trace.Digest != entry.TraceDigest || trace.ManifestDigest != entry.ManifestDigest ||
		trace.SchemaVersion != entry.TraceSchema || len(trace.Records) != entry.Decisions {
		return errors.New("EXPERIMENT_MUTATION_SOURCE_TRACE_MISMATCH")
	}
	return nil
}

func (entry MutationSourceEntry) seal() (MutationSourceEntry, error) {
	entry.Digest = ""
	digest, err := portableJSONDigest(entry)
	if err != nil {
		return MutationSourceEntry{}, err
	}
	entry.Digest = digest
	return entry, nil
}

// MutationSourceCorpus is an explicitly ordered source set. Order is part of
// its identity so a method cannot try multiple sources and later present the
// most convenient one as the first attempt.
type MutationSourceCorpus struct {
	SchemaVersion string                `json:"schema_version"`
	ID            string                `json:"id"`
	Entries       []MutationSourceEntry `json:"entries"`
	Digest        string                `json:"digest"`
}

func NewMutationSourceCorpus(id string, entries []MutationSourceEntry) (MutationSourceCorpus, error) {
	corpus := MutationSourceCorpus{
		SchemaVersion: MutationSourceCorpusVersion, ID: id,
		Entries: append([]MutationSourceEntry(nil), entries...),
	}
	sealed, err := corpus.seal()
	if err != nil {
		return MutationSourceCorpus{}, err
	}
	if err := sealed.Validate(); err != nil {
		return MutationSourceCorpus{}, err
	}
	return sealed, nil
}

func (corpus MutationSourceCorpus) Validate() error {
	if corpus.SchemaVersion != MutationSourceCorpusVersion || corpus.ID == "" ||
		len(corpus.Entries) == 0 || len(corpus.Entries) > MaxMutationSources {
		return errors.New("EXPERIMENT_MUTATION_CORPUS_INVALID")
	}
	seenIDs := make(map[string]bool, len(corpus.Entries))
	seenDigests := make(map[string]bool, len(corpus.Entries))
	seenBundles := make(map[string]bool, len(corpus.Entries))
	seenTraces := make(map[string]bool, len(corpus.Entries))
	for _, entry := range corpus.Entries {
		if err := entry.Validate(); err != nil {
			return err
		}
		if seenIDs[entry.ID] || seenDigests[entry.Digest] ||
			seenBundles[entry.BundleDigest] || seenTraces[entry.TraceDigest] {
			return errors.New("EXPERIMENT_MUTATION_CORPUS_SOURCE_DUPLICATE")
		}
		seenIDs[entry.ID] = true
		seenDigests[entry.Digest] = true
		seenBundles[entry.BundleDigest] = true
		seenTraces[entry.TraceDigest] = true
	}
	sealed, err := corpus.seal()
	if err != nil || sealed.Digest != corpus.Digest {
		return errors.New("EXPERIMENT_MUTATION_CORPUS_DIGEST_MISMATCH")
	}
	return nil
}

func (corpus MutationSourceCorpus) seal() (MutationSourceCorpus, error) {
	corpus.Entries = append([]MutationSourceEntry(nil), corpus.Entries...)
	corpus.Digest = ""
	digest, err := portableJSONDigest(corpus)
	if err != nil {
		return MutationSourceCorpus{}, err
	}
	corpus.Digest = digest
	return corpus, nil
}
