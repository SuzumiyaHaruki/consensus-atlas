package main

import (
	"context"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	etcdqualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/etcdraftv2"
)

// executeEtcdraftStatelessPrefix sends one already-trusted stateless WorkItem
// through the same qualified executor used by the public etcd/raft workload.
// Search code never receives an alternate execution or PSS projection path.
func executeEtcdraftStatelessPrefix(
	ctx context.Context,
	executionID string,
	search controlexperiment.StatelessDFSResult,
	root controlruntime.Trace,
	item controlexperiment.StatelessDFSWorkItem,
	runtimeConfig controlexperiment.RuntimeConfig,
	adapterConfig etcdraftv2.Config,
	envelope *controlexperiment.FaultEnvelope,
	qualification etcdqualification.Bundle,
	admission controlexperiment.ExecutionAdmission,
	workload controlexperiment.WorkloadPlan,
) (controlexperiment.Report, controlexperiment.ExecutionBundle, error) {
	policy, err := controlexperiment.CompileStatelessDFSPath(
		executionID, search, root, item.Ordinal,
		[]control.ActionKind{
			control.ActionInvoke, control.ActionCompleteEffect,
			control.ActionDeliverMessage, control.ActionFireTemporal,
		},
		item.Path.Decision,
	)
	if err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	config := controlexperiment.Config{
		SchemaVersion:    controlexperiment.SchemaVersionV2,
		ID:               executionID,
		PSSID:            etcdraftv2.CorePSSMappingID,
		Runtime:          runtimeConfig,
		Admission:        &admission,
		FaultEnvelope:    envelope,
		WorkloadRouterID: etcdraftv2.WorkloadRouterID,
		DecisionsPerRun:  item.Path.Decision,
		RequireReplay:    true,
		Runs: []controlexperiment.RunPlan{{
			Run: 1, Policy: policy, Workload: &workload,
		}},
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(adapterConfig)
	}
	return controlexperiment.ExecuteQualifiedBundle(
		ctx, config, qualification, factory, etcdraftv2.CorePSSMapper{},
		etcdraftv2.DecisionProjector{}, etcdraftv2.WorkloadRouter{},
	)
}
