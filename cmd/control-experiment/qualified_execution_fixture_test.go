package main

import (
	"context"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const etcdraftTestRootCorpusPath = "../../benchmarks/experiments/etcdraft-v2-root-corpus-m5.23e/root-corpus.json"

func executeEtcdraftFrontierPolicy(
	t *testing.T,
	ctx context.Context,
	id string,
	policy controlexperiment.Policy,
	envelope *controlexperiment.FaultEnvelope,
) (controlexperiment.Report, controlexperiment.ExecutionBundle) {
	t.Helper()
	qualification, admission, workload, err := etcdraftQualifiedWorkload(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config := controlexperiment.Config{
		SchemaVersion:    controlexperiment.SchemaVersionV2,
		ID:               "public-etcdraft-v2-frontier-" + id,
		PSSID:            etcdraftv2.CorePSSMappingID,
		Runtime:          etcdraftCampaignRuntimeConfig(),
		Admission:        &admission,
		FaultEnvelope:    envelope,
		WorkloadRouterID: etcdraftv2.WorkloadRouterID,
		DecisionsPerRun:  64,
		RequireReplay:    true,
		Runs:             []controlexperiment.RunPlan{{Run: 1, Policy: policy, Workload: &workload}},
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	report, bundle, err := controlexperiment.ExecuteQualifiedBundle(
		ctx, config, qualification, factory, etcdraftv2.CorePSSMapper{},
		etcdraftv2.DecisionProjector{}, etcdraftv2.WorkloadRouter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	return report, bundle
}
