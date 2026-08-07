package etcdraftv1v2_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/migration"
	"github.com/SuzumiyaHaruki/consensus-atlas/migrations/etcdraftv1v2"
)

func TestFrozenV1V2Comparison(t *testing.T) {
	report, err := etcdraftv1v2.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Qualified || report.Passed != 3 || report.Mismatched != 0 || report.Deferred != 1 {
		t.Fatalf("migration report = %+v", report)
	}
	for _, current := range report.Cases {
		switch current.ScenarioID {
		case etcdraftv1v2.ScenarioNormalCommit:
			if current.Status != migration.StatusPassed || current.Left == nil || current.Right == nil ||
				current.Left.Replay.Mode == current.Right.Replay.Mode {
				t.Fatalf("normal comparison = %+v", current)
			}
		case etcdraftv1v2.ScenarioTransportControl, etcdraftv1v2.ScenarioFollowerRecovery:
			if current.Status != migration.StatusPassed || current.Left == nil || current.Right == nil {
				t.Fatalf("comparison = %+v", current)
			}
		case etcdraftv1v2.ScenarioNaturalChange:
			if current.Status != migration.StatusDeferred {
				t.Fatalf("natural leader comparison = %+v", current)
			}
		default:
			t.Fatalf("unexpected migration case = %+v", current)
		}
	}
	stored := readStoredReport(t)
	if stored.Digest != report.Digest {
		t.Fatalf("checked report digest = %s, fresh = %s", stored.Digest, report.Digest)
	}
}

func readStoredReport(t *testing.T) migration.Suite {
	t.Helper()
	_, source, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("cannot locate migration test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	encoded, err := os.ReadFile(filepath.Join(
		root, "benchmarks", "migrations", "etcdraft-v1-v2-m5.2.5", "report.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var report migration.Suite
	if err := decoder.Decode(&report); err != nil {
		t.Fatal(err)
	}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	return report
}
