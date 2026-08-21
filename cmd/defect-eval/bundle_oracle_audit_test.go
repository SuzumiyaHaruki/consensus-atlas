package main

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/targetoracles"
)

func TestSavedBundleOracleAuditRecomputesTargetRegistry(t *testing.T) {
	_, _, bundle := formalCLITestExecution(t)
	root := t.TempDir()
	bundlePath := filepath.Join(root, "bundle.json")
	outPath := filepath.Join(root, "oracle-audit.json")
	if err := writeJSON(bundlePath, bundle); err != nil {
		t.Fatal(err)
	}
	if err := runSavedBundleOracleAudit(
		targetoracles.EtcdraftV2TargetID, bundlePath, outPath,
	); err != nil {
		t.Fatal(err)
	}
	var audit savedBundleOracleAudit
	if err := readStrictJSON(outPath, &audit); err != nil {
		t.Fatal(err)
	}
	want := targetoracles.EtcdraftV2Registry().Check(bundle)
	if audit.SchemaVersion != savedBundleOracleAuditSchemaVersion ||
		audit.TargetID != targetoracles.EtcdraftV2TargetID ||
		audit.BundleDigest != bundle.Digest || audit.TraceDigest != bundle.Trace.Digest ||
		audit.RecordedReplayStable != bundle.Run.Replay.Stable ||
		audit.Decisions != len(bundle.Trace.Records) ||
		audit.PrimaryWork != bundle.Work.Primary.WorkUnits ||
		audit.ReplayWork != bundle.Work.Replay.WorkUnits ||
		!reflect.DeepEqual(audit.Oracle, want) {
		t.Fatalf("saved Bundle Oracle audit drifted: %#v want=%#v", audit, want)
	}
	encoded, err := json.Marshal(audit)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(encoded, &fields) != nil || fields["replay_stable"] == nil ||
		fields["recorded_replay_stable"] != nil {
		t.Fatalf("v1 replay field compatibility drifted: %s", encoded)
	}
}

func TestSavedBundleOracleAuditRejectsMixedModeFlags(t *testing.T) {
	err := run([]string{
		"-oracle-bundle", "bundle.json", "-target", targetoracles.EtcdraftV2TargetID,
		"-out", "audit.json", "-manifest", "unexpected.json",
	})
	if err == nil {
		t.Fatal("saved Bundle audit accepted unrelated evaluation flags")
	}
}
