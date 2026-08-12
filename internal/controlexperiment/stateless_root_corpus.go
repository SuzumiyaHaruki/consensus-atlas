package controlexperiment

import (
	"errors"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

const (
	StatelessRootCorpusSchemaVersion = "consensus-atlas/stateless-root-corpus/v1"
	StatelessRootEntrySchemaVersion  = "consensus-atlas/stateless-root-entry/v1"
)

// StatelessRootSpec is curated before a traversal method runs. PhaseID is
// descriptive metadata only and grants no scheduling or evaluation authority.
type StatelessRootSpec struct {
	ID        string `json:"id"`
	PhaseID   string `json:"phase_id"`
	Decisions int    `json:"decisions"`
}

// StatelessRootEntry binds a predeclared source position to its exact prefix.
type StatelessRootEntry struct {
	SchemaVersion    string `json:"schema_version"`
	ID               string `json:"id"`
	PhaseID          string `json:"phase_id"`
	Decisions        int    `json:"decisions"`
	PrefixDigest     string `json:"prefix_digest"`
	FinalStateDigest string `json:"final_state_digest"`
	Digest           string `json:"digest"`
}

// StatelessRootCorpus is a versioned, method-independent selection of exact
// prefixes from one strict-Replay ExecutionBundle.
type StatelessRootCorpus struct {
	SchemaVersion      string               `json:"schema_version"`
	ID                 string               `json:"id"`
	SelectionRuleID    string               `json:"selection_rule_id"`
	SourceBundleDigest string               `json:"source_bundle_digest"`
	SourceTraceDigest  string               `json:"source_trace_digest"`
	ManifestDigest     string               `json:"manifest_digest"`
	Roots              []StatelessRootEntry `json:"roots"`
	Digest             string               `json:"digest"`
}

func NewStatelessRootCorpus(
	id string,
	selectionRuleID string,
	source ExecutionBundle,
	specs []StatelessRootSpec,
) (StatelessRootCorpus, error) {
	if !validMethodToken(id) || !validMethodToken(selectionRuleID) || source.Validate() != nil ||
		len(specs) == 0 {
		return StatelessRootCorpus{}, errors.New("EXPERIMENT_STATELESS_ROOT_CORPUS_INPUT_INVALID")
	}
	corpus := StatelessRootCorpus{
		SchemaVersion: StatelessRootCorpusSchemaVersion, ID: id,
		SelectionRuleID: selectionRuleID, SourceBundleDigest: source.Digest,
		SourceTraceDigest: source.Trace.Digest, ManifestDigest: source.Identity.ManifestDigest,
	}
	seenIDs := make(map[string]bool, len(specs))
	seenDecisions := make(map[int]bool, len(specs))
	previousDecision := -1
	for _, spec := range specs {
		if !validMethodToken(spec.ID) || !validMethodToken(spec.PhaseID) ||
			spec.Decisions < 0 || spec.Decisions > len(source.Trace.Records) ||
			spec.Decisions <= previousDecision || seenIDs[spec.ID] || seenDecisions[spec.Decisions] {
			return StatelessRootCorpus{}, errors.New("EXPERIMENT_STATELESS_ROOT_SPEC_INVALID")
		}
		prefix, err := ExecutionTracePrefix(source.Trace, spec.Decisions)
		if err != nil {
			return StatelessRootCorpus{}, err
		}
		entry, err := (StatelessRootEntry{
			SchemaVersion: StatelessRootEntrySchemaVersion,
			ID:            spec.ID, PhaseID: spec.PhaseID, Decisions: spec.Decisions,
			PrefixDigest: prefix.Digest, FinalStateDigest: prefix.FinalStateDigest,
		}).seal()
		if err != nil {
			return StatelessRootCorpus{}, err
		}
		corpus.Roots = append(corpus.Roots, entry)
		seenIDs[spec.ID], seenDecisions[spec.Decisions] = true, true
		previousDecision = spec.Decisions
	}
	return corpus.seal()
}

func (corpus StatelessRootCorpus) Validate(source ExecutionBundle) error {
	if corpus.SchemaVersion != StatelessRootCorpusSchemaVersion ||
		corpus.SourceBundleDigest != source.Digest || corpus.SourceTraceDigest != source.Trace.Digest ||
		corpus.ManifestDigest != source.Identity.ManifestDigest {
		return errors.New("EXPERIMENT_STATELESS_ROOT_CORPUS_SOURCE_MISMATCH")
	}
	specs := make([]StatelessRootSpec, 0, len(corpus.Roots))
	for _, root := range corpus.Roots {
		specs = append(specs, StatelessRootSpec{
			ID: root.ID, PhaseID: root.PhaseID, Decisions: root.Decisions,
		})
	}
	want, err := NewStatelessRootCorpus(corpus.ID, corpus.SelectionRuleID, source, specs)
	if err != nil || !reflect.DeepEqual(want, corpus) {
		return errors.New("EXPERIMENT_STATELESS_ROOT_CORPUS_MISMATCH")
	}
	return nil
}

func (corpus StatelessRootCorpus) Prefix(
	source ExecutionBundle,
	rootID string,
) (controlruntime.Trace, error) {
	if corpus.Validate(source) != nil || !validMethodToken(rootID) {
		return controlruntime.Trace{}, errors.New("EXPERIMENT_STATELESS_ROOT_PREFIX_INPUT_INVALID")
	}
	for _, root := range corpus.Roots {
		if root.ID != rootID {
			continue
		}
		prefix, err := ExecutionTracePrefix(source.Trace, root.Decisions)
		if err != nil || prefix.Digest != root.PrefixDigest ||
			prefix.FinalStateDigest != root.FinalStateDigest {
			return controlruntime.Trace{}, errors.New("EXPERIMENT_STATELESS_ROOT_PREFIX_MISMATCH")
		}
		return prefix, nil
	}
	return controlruntime.Trace{}, errors.New("EXPERIMENT_STATELESS_ROOT_UNKNOWN")
}

func (entry StatelessRootEntry) seal() (StatelessRootEntry, error) {
	entry.Digest = ""
	digest, err := control.CanonicalDigest(entry)
	if err != nil {
		return StatelessRootEntry{}, err
	}
	entry.Digest = digest
	return entry, nil
}

func (corpus StatelessRootCorpus) seal() (StatelessRootCorpus, error) {
	corpus.Roots = append([]StatelessRootEntry(nil), corpus.Roots...)
	corpus.Digest = ""
	digest, err := control.CanonicalDigest(corpus)
	if err != nil {
		return StatelessRootCorpus{}, err
	}
	corpus.Digest = digest
	return corpus, nil
}
