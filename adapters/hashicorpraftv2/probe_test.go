package hashicorpraftv2

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestRunProbeFreezesStableOutboundVotes(t *testing.T) {
	first := run(t)
	second := run(t)
	if first.Digest != second.Digest {
		t.Fatalf("probe digest changed: %s != %s", first.Digest, second.Digest)
	}
	if first.Qualified || first.StrictReplay {
		t.Fatal("capture-only probe must not qualify strict replay")
	}
	if len(first.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(first.Items))
	}
	for index, target := range []control.NodeID{"n2", "n3"} {
		item := first.Items[index]
		if err := item.Validate(); err != nil {
			t.Fatalf("item %d invalid: %v", index, err)
		}
		if item.Kind != control.ItemMessage || item.Message.Target != target || item.Message.TypeHint != "request-vote" {
			t.Fatalf("item %d is not the expected vote for %s: %+v", index, target, item)
		}
	}
}

func TestFrozenProbeArtifactMatchesFreshRun(t *testing.T) {
	encoded, err := os.ReadFile("../../benchmarks/probes/hashicorp-raft-v1.7.3-m5.4a/report.json")
	if err != nil {
		t.Fatal(err)
	}
	var frozen Report
	if err := json.Unmarshal(encoded, &frozen); err != nil {
		t.Fatal(err)
	}
	wantDigest := frozen.Digest
	frozen.Digest = ""
	gotDigest, err := control.CanonicalDigest(frozen)
	if err != nil {
		t.Fatal(err)
	}
	if gotDigest != wantDigest {
		t.Fatalf("frozen artifact digest is invalid: %s != %s", gotDigest, wantDigest)
	}
	if fresh := run(t); fresh.Digest != wantDigest {
		t.Fatalf("fresh probe digest changed: %s != %s", fresh.Digest, wantDigest)
	}
}

func run(t *testing.T) Report {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	report, err := RunProbe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return report
}
