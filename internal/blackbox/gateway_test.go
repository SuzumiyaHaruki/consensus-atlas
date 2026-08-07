package blackbox_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/blackbox"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const gatewayReportPath = "../../benchmarks/qualifications/blackbox-gateway-m5.5c/report.json"

type gatewayReport struct {
	SchemaVersion string            `json:"schema_version"`
	ReportID      string            `json:"report_id"`
	Checks        map[string]bool   `json:"checks"`
	Counters      map[string]uint64 `json:"counters"`
	Capabilities  []boundaryResult  `json:"capabilities"`
	Limitations   []string          `json:"limitations"`
	Digest        string            `json:"digest"`
}

func TestConnectionGatewayPartitionsTwoProcesses(t *testing.T) {
	root := t.TempDir()
	n1Dir, n2Dir := filepath.Join(root, "n1"), filepath.Join(root, "n2")
	n1Socket, n2Socket, gatewaySocket := filepath.Join(n1Dir, "client.sock"), filepath.Join(n2Dir, "client.sock"), filepath.Join(root, "gateway.sock")
	n2 := newFixtureTarget(t, "n2", n2Dir, n2Socket, "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := n2.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer n2.Close()

	gateway, err := blackbox.NewGateway(blackbox.GatewaySpec{
		ID:      "n1-to-n2",
		Listen:  blackbox.Endpoint{ID: "proxy", Network: "unix", Address: gatewaySocket},
		Forward: blackbox.Endpoint{ID: "n2", Network: "unix", Address: n2Socket},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gateway.Start(); err != nil {
		t.Fatal(err)
	}
	defer gateway.Close()

	n1 := newFixtureTarget(t, "n1", n1Dir, n1Socket, gatewaySocket)
	if _, err := n1.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer n1.Close()
	if response := deliver(t, ctx, n1, "SEND before"); response != "ACK" {
		t.Fatalf("open gateway response = %q", response)
	}
	waitGatewayIdle(t, ctx, gateway)
	if value := deliver(t, ctx, n2, "GET"); value != "before" {
		t.Fatalf("forwarded value = %q", value)
	}

	if err := gateway.Partition(); err != nil {
		t.Fatal(err)
	}
	if response := deliver(t, ctx, n1, "SEND blocked"); response != "PEER-ERROR" {
		t.Fatalf("partitioned gateway response = %q", response)
	}
	if value := deliver(t, ctx, n2, "GET"); value != "before" {
		t.Fatalf("partition changed receiver state: %q", value)
	}
	if err := gateway.Heal(); err != nil {
		t.Fatal(err)
	}
	if response := deliver(t, ctx, n1, "SEND after"); response != "ACK" {
		t.Fatalf("healed gateway response = %q", response)
	}
	waitGatewayIdle(t, ctx, gateway)
	if value := deliver(t, ctx, n2, "GET"); value != "after" {
		t.Fatalf("healed value = %q", value)
	}

	snapshot := gateway.Snapshot()
	if snapshot.Accepted != 3 || snapshot.Forwarded != 2 || snapshot.Rejected != 1 || snapshot.DialFailures != 0 || snapshot.Active != 0 || snapshot.Partitioned {
		t.Fatalf("unexpected gateway snapshot: %+v", snapshot)
	}
	if snapshot.BytesToTarget == 0 || snapshot.BytesFromTarget == 0 {
		t.Fatalf("opaque byte counters missing: %+v", snapshot)
	}
	report := gatewayReport{
		SchemaVersion: "consensus-atlas/blackbox-gateway-conformance/v1", ReportID: "two-process-connection-gateway-m5.5c",
		Checks: map[string]bool{
			"two-independent-processes": true, "open-forwarding": true, "partition-blocked-peer-write": true,
			"receiver-unchanged-during-partition": true, "heal-restored-forwarding": true, "opaque-bytes-observed": true,
		},
		Counters: map[string]uint64{"accepted": snapshot.Accepted, "forwarded": snapshot.Forwarded, "rejected": snapshot.Rejected},
		Capabilities: []boundaryResult{
			{Surface: "peer-connection", Grade: "interceptable", Evidence: "accept-forward-reject"},
			{Surface: "partition", Grade: "interceptable", Evidence: "partition-heal-api"},
			{Surface: "peer-bytes", Grade: "observable", Evidence: "bidirectional-byte-counters"},
		},
		Limitations: []string{"configurable-peer-endpoint-required", "connection-level-only", "no-message-framing", "goroutine-and-wall-clock-order", "no-strict-replay"},
	}
	report.Digest = ""
	report.Digest, err = control.CanonicalDigest(report)
	if err != nil {
		t.Fatal(err)
	}
	assertGatewayReport(t, report)
}

func waitGatewayIdle(t *testing.T, ctx context.Context, gateway *blackbox.Gateway) {
	t.Helper()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if gateway.Snapshot().Active == 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}

func assertGatewayReport(t *testing.T, fresh gatewayReport) {
	t.Helper()
	encoded, err := os.ReadFile(gatewayReportPath)
	if err != nil {
		t.Fatal(err)
	}
	var frozen gatewayReport
	if err := json.Unmarshal(encoded, &frozen); err != nil {
		t.Fatal(err)
	}
	freshJSON, _ := json.Marshal(fresh)
	frozenJSON, _ := json.Marshal(frozen)
	if string(freshJSON) != string(frozenJSON) {
		pretty, _ := json.MarshalIndent(fresh, "", "  ")
		t.Fatalf("frozen gateway report is stale; fresh report:\n%s", pretty)
	}
}
