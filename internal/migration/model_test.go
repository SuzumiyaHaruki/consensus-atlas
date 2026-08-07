package migration_test

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/migration"
)

func TestCompareRequiresExternalEquivalenceAndStableReplay(t *testing.T) {
	left := summary(t, "v1")
	right := summary(t, "v2")
	comparison, err := migration.Compare(left, right)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Status != migration.StatusPassed || len(comparison.ReasonCodes) != 0 {
		t.Fatalf("comparison = %+v", comparison)
	}

	right.Commands[0].ValueDigest = migration.ValueDigest([]byte("different"))
	right, err = migration.SealSummary(right)
	if err != nil {
		t.Fatal(err)
	}
	comparison, err = migration.Compare(left, right)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Status != migration.StatusMismatch ||
		!contains(comparison.ReasonCodes, "MIGRATION_COMMAND_RESULT_MISMATCH") {
		t.Fatalf("mismatch comparison = %+v", comparison)
	}
}

func TestSuiteDoesNotCallDeferredCaseQualified(t *testing.T) {
	passed, err := migration.Compare(summary(t, "v1"), summary(t, "v2"))
	if err != nil {
		t.Fatal(err)
	}
	deferred, err := migration.Defer("natural-leader-change", "V1_NATURAL_TIMEOUT_UNSUPPORTED")
	if err != nil {
		t.Fatal(err)
	}
	suite, err := migration.SealSuite("etcdraft-v1-v2", []migration.Case{passed, deferred})
	if err != nil {
		t.Fatal(err)
	}
	if suite.Qualified || suite.Passed != 1 || suite.Deferred != 1 || suite.Mismatched != 0 {
		t.Fatalf("suite = %+v", suite)
	}
}

func TestFrozenExpectationRejectsEqualButIncompleteResults(t *testing.T) {
	left := summary(t, "v1")
	right := summary(t, "v2")
	left.Commands[0].AppliedNodes = []string{"n1", "n2"}
	right.Commands[0].AppliedNodes = []string{"n1", "n2"}
	var err error
	left, err = migration.SealSummary(left)
	if err != nil {
		t.Fatal(err)
	}
	right, err = migration.SealSummary(right)
	if err != nil {
		t.Fatal(err)
	}
	comparison, err := migration.CompareExpected(left, right, migration.Expectation{
		Commands: []migration.Command{{
			Ordinal: 1, ValueDigest: migration.ValueDigest([]byte("alpha")),
			AppliedNodes: []string{"n1", "n2", "n3"},
		}},
		Witnesses: []string{"commit-all-nodes"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Status != migration.StatusMismatch ||
		!contains(comparison.ReasonCodes, "MIGRATION_LEFT_EXPECTED_COMMAND_MISMATCH") ||
		!contains(comparison.ReasonCodes, "MIGRATION_RIGHT_EXPECTED_COMMAND_MISMATCH") {
		t.Fatalf("comparison = %+v", comparison)
	}
}

func TestSummaryDigestRejectsMutation(t *testing.T) {
	summary := summary(t, "v1")
	summary.Commands[0].AppliedNodes[0] = "changed"
	if err := summary.Validate(); err == nil {
		t.Fatal("mutated summary passed digest validation")
	}
}

func TestSuiteDigestRejectsMutation(t *testing.T) {
	comparison, err := migration.Compare(summary(t, "v1"), summary(t, "v2"))
	if err != nil {
		t.Fatal(err)
	}
	suite, err := migration.SealSuite("suite", []migration.Case{comparison})
	if err != nil {
		t.Fatal(err)
	}
	if err := suite.Validate(); err != nil {
		t.Fatal(err)
	}
	suite.Passed++
	if err := suite.Validate(); err == nil {
		t.Fatal("mutated suite passed validation")
	}
}

func summary(t *testing.T, path string) migration.Summary {
	t.Helper()
	summary, err := migration.SealSummary(migration.Summary{
		ScenarioID: "normal-commit", PathID: path,
		ImplementationID: "fixture@v1", ConfigurationID: migration.ValueDigest([]byte("fixture-config")),
		Commands: []migration.Command{{
			Ordinal: 1, ValueDigest: migration.ValueDigest([]byte("alpha")),
			AppliedNodes: []string{"n1", "n2", "n3"},
		}},
		Safety: migration.Safety{}, Replay: migration.Replay{Mode: path + "-replay", Stable: true},
		Witnesses: []string{"commit-all-nodes"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return summary
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
