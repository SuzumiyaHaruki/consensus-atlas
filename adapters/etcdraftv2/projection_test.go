package etcdraftv2_test

import (
	"context"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

func TestPublicProjectionReadsEvidenceAndCommittedResult(t *testing.T) {
	ctx := context.Background()
	runtime, err := controlruntime.New(ctx, etcdraftv2.New(), runtimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	leader := driveUntilStableLeader(t, ctx, runtime, 128)
	payload, err := etcdraftv2.InputPayload(etcdraftv2.Input{
		Operation: etcdraftv2.OperationPropose, RequestID: "projection-request", Value: []byte("projected"),
	})
	if err != nil {
		t.Fatal(err)
	}
	invoke, err := runtime.OfferInvoke(ctx, leader, payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Select(ctx, invoke); err != nil {
		t.Fatal(err)
	}
	driveUntilApplicationCommands(t, ctx, runtime, map[control.NodeID]int{"n1": 1}, 96)

	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := etcdraftv2.ProjectEvidence(*trace.Records[len(trace.Records)-1].Evidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Nodes) != 1 || evidence.Nodes[0].ApplicationCommands != 1 ||
		evidence.Nodes[0].ApplicationDigest == "" {
		t.Fatalf("projected evidence = %+v", evidence)
	}
	item := findClientResponse(t, runtime.Snapshot(), "projection-request")
	result, err := etcdraftv2.ProjectClientResult(*item.Value.Response)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "committed" || string(result.Value) != "projected" ||
		result.Index == 0 || result.Term == 0 {
		t.Fatalf("projected result = %+v", result)
	}
}
