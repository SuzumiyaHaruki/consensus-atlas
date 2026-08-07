package conformance_test

import (
	"context"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestEtcdRaftDeclaredOpaqueInvokeConformance(t *testing.T) {
	input, err := etcdraftv2.InputPayload(etcdraftv2.Input{
		Operation: etcdraftv2.OperationPropose,
		RequestID: "conformance-request-1",
		Value:     []byte("opaque-conformance-value"),
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := conformance.EvaluateOpaqueInvoke(
		context.Background(),
		func() control.Adapter {
			adapter, err := etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
			if err != nil {
				t.Fatal(err)
			}
			return adapter
		},
		conformance.OpaqueInvokePlan{
			Seed: []byte("etcdraft-v2-opaque-invoke-conformance"), Node: "n1",
			Input: input, DecisionBound: 256,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Cases) != 1 || len(report.ValidatedCapabilities) != 1 ||
		report.ValidatedCapabilities[0] != "opaque-invoke-boundary" {
		t.Fatalf("opaque invoke conformance report = %+v", report)
	}
}
