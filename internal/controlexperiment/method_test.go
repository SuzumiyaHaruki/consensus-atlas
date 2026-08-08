package controlexperiment

import (
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

func TestMethodLedgerPreservesFailedAttemptAndAllSourceCost(t *testing.T) {
	entry := MutationSourceEntry{
		SchemaVersion: MutationSourceEntryVersion, ID: "source",
		BundleDigest: strings.Repeat("a", 64), ReportDigest: strings.Repeat("b", 64),
		ConfigDigest: strings.Repeat("c", 64), TraceDigest: strings.Repeat("d", 64),
		ManifestDigest: strings.Repeat("e", 64), PSSID: "test/core-pss-v1",
		PolicyID: "source-policy", PolicyDigest: strings.Repeat("f", 64),
		TraceSchema: controlruntime.TraceSchemaVersion, Decisions: 10,
		CorePSSDigest: strings.Repeat("1", 64), QualificationID: strings.Repeat("2", 64),
	}
	var err error
	entry, err = entry.seal()
	if err != nil {
		t.Fatal(err)
	}
	corpus, err := NewMutationSourceCorpus("sources", []MutationSourceEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	work := func(decisions int) WorkLedger {
		result := emptyWork()
		result.Primary = PhaseWork{
			SetupAttempts: 1, RuntimeInitializations: 1,
			SchedulerDecisions: decisions, WorkUnits: 1 + decisions,
		}
		return result
	}
	proposalDigest := strings.Repeat("3", 64)
	records := []MethodRecord{
		{
			Ordinal: 1, Kind: MethodRecordSource, Outcome: MethodOutcomeCompleted,
			InputDigest: entry.ConfigDigest, OutputDigest: entry.Digest,
			ReportDigest: entry.ReportDigest, BundleDigest: entry.BundleDigest, Work: work(10),
		},
		{
			Ordinal: 2, Kind: MethodRecordProposal, Outcome: MethodOutcomeCompleted,
			InputDigest: entry.Digest, OutputDigest: proposalDigest, Work: emptyWork(),
		},
		{
			Ordinal: 3, Kind: MethodRecordExecution, Outcome: MethodOutcomeExecutionFailed,
			InputDigest: proposalDigest,
			Failure: &MethodFailure{
				Phase: "primary-policy", Code: "EXPERIMENT_TRACE_MUTATION_ACTION_NOT_ENABLED",
				Decision: 4,
			},
			Work: work(3),
		},
	}
	ledger, err := NewMethodLedger("failed-method", "mutation/v1", corpus, nil, records)
	if err != nil {
		t.Fatal(err)
	}
	if ledger.Records[2].Outcome != MethodOutcomeExecutionFailed ||
		ledger.Totals.Primary.SetupAttempts != 2 ||
		ledger.Totals.Primary.SchedulerDecisions != 13 ||
		ledger.Totals.Primary.WorkUnits != 15 {
		t.Fatalf("failed attempt cost disappeared: %#v", ledger)
	}

	missingSource := append([]MethodRecord(nil), records[1:]...)
	missingSource[0].Ordinal = 1
	missingSource[1].Ordinal = 2
	if _, err := NewMethodLedger("missing-source", "mutation/v1", corpus, nil, missingSource); err == nil || !strings.Contains(err.Error(), "SOURCE_COST_MISSING") {
		t.Fatalf("missing source cost error = %v", err)
	}

	wrongSourceInput := append([]MethodRecord(nil), records...)
	wrongSourceInput[0].InputDigest = entry.BundleDigest
	if _, err := NewMethodLedger("wrong-source-input", "mutation/v1", corpus, nil, wrongSourceInput); err == nil || !strings.Contains(err.Error(), "SOURCE_BINDING_INVALID") {
		t.Fatalf("wrong source input error = %v", err)
	}
}
