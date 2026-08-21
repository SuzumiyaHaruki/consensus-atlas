package main

import (
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/targetoracles"
)

const savedBundleOracleAuditSchemaVersion = "consensus-atlas/saved-bundle-oracle-audit/v1"

// savedBundleOracleAudit is evaluator-side evidence for a public saved Bundle.
// It is not a benchmark verdict: it validates the Bundle/projection and reruns
// the existing Target registry without accepting Agent-reported findings.
type savedBundleOracleAudit struct {
	SchemaVersion string `json:"schema_version"`
	TargetID      string `json:"target_id"`
	ProjectorID   string `json:"projector_id"`
	BundleDigest  string `json:"bundle_digest"`
	TraceDigest   string `json:"trace_digest"`
	// RecordedReplayStable reports the fresh Replay result already sealed in
	// the submitted Bundle. This audit validates that evidence and reruns the
	// Oracle registry; it does not start a Runtime and execute Replay again.
	// The v1 JSON name remains replay_stable for compatibility with saved
	// M4l7 audits. The comment above defines its recorded-evidence semantics.
	RecordedReplayStable bool          `json:"replay_stable"`
	Decisions            int           `json:"decisions"`
	PrimaryWork          int           `json:"primary_work_units"`
	ReplayWork           int           `json:"replay_work_units"`
	Oracle               oracle.Result `json:"oracle"`
}

func runSavedBundleOracleAudit(targetID, bundlePath, outPath string) error {
	if targetID == "" || bundlePath == "" || outPath == "" {
		return errors.New("SAVED_BUNDLE_ORACLE_AUDIT_INPUT_REQUIRED")
	}
	projector, registry, err := targetoracles.ResolveTarget(targetID)
	if err != nil {
		return err
	}
	var bundle controlexperiment.ExecutionBundle
	if err := readStrictJSON(bundlePath, &bundle); err != nil {
		return err
	}
	if err := bundle.Validate(); err != nil {
		return err
	}
	if err := bundle.ValidateProjection(projector); err != nil {
		return err
	}
	audit := savedBundleOracleAudit{
		SchemaVersion: savedBundleOracleAuditSchemaVersion,
		TargetID:      targetID, ProjectorID: projector.ID(),
		BundleDigest: bundle.Digest, TraceDigest: bundle.Trace.Digest,
		RecordedReplayStable: bundle.Run.Replay.Stable, Decisions: len(bundle.Trace.Records),
		PrimaryWork: bundle.Work.Primary.WorkUnits, ReplayWork: bundle.Work.Replay.WorkUnits,
		Oracle: registry.Check(bundle),
	}
	return writeJSON(outPath, audit)
}
