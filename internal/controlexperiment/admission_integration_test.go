package controlexperiment_test

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
	qualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/etcdraftv2"
)

func TestCurrentEtcdQualificationAdmitsStrictExecution(t *testing.T) {
	ctx := context.Background()
	bundle, err := qualification.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := controlexperiment.BindExecutionAdmission(
		bundle.Qualification,
		controlexperiment.ExecutionRequirements{Capabilities: bundle.Profile.RequiredCapabilityIDs()},
	)
	if err != nil {
		t.Fatal(err)
	}
	config := controlexperiment.Config{
		SchemaVersion: controlexperiment.SchemaVersion, ID: "qualified-etcd-smoke",
		PSSID: etcdraftv2.CorePSSMappingID, Admission: &admission,
		Runtime: controlexperiment.RuntimeConfig{
			SeedHex: hex.EncodeToString([]byte("qualified-etcd-smoke-seed")), MaxClones: 1,
		},
		DecisionsPerRun: 4, RequireReplay: true,
		Runs: []controlexperiment.RunPlan{{Run: 1, Policy: controlexperiment.Policy{
			Version: controlexperiment.PolicyVersion, ID: "progress",
			Priority: []control.ActionKind{
				control.ActionCompleteEffect, control.ActionDeliverMessage, control.ActionFireTemporal,
			},
		}}},
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	report, err := controlexperiment.ExecuteQualified(
		ctx, config, bundle.Qualification, factory, etcdraftv2.CorePSSMapper{}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := report.ValidateWithQualification(bundle.Qualification); err != nil {
		t.Fatal(err)
	}
	if report.Config.Admission == nil || report.Config.Admission.Digest != admission.Digest ||
		report.ManifestDigest != bundle.Qualification.ManifestDigest || !report.Runs[0].Replay.Stable {
		t.Fatalf("unexpected admitted report: %#v", report)
	}
	if len(report.Config.Admission.RequiredCapabilities) != 8 {
		t.Fatalf("strict admitted capabilities = %d, want 8", len(report.Config.Admission.RequiredCapabilities))
	}
}

func TestQualifiedEtcdSemanticWorkloadCommitsAndReplays(t *testing.T) {
	ctx := context.Background()
	bundle, err := qualification.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := controlexperiment.BindExecutionAdmission(
		bundle.Qualification,
		controlexperiment.ExecutionRequirements{Capabilities: bundle.Profile.RequiredCapabilityIDs()},
	)
	if err != nil {
		t.Fatal(err)
	}
	const requestID = "m5.15-write-1"
	payload, err := etcdraftv2.InputPayload(etcdraftv2.Input{
		Operation: etcdraftv2.OperationPropose, RequestID: requestID, Value: []byte("alpha"),
	})
	if err != nil {
		t.Fatal(err)
	}
	config := controlexperiment.Config{
		SchemaVersion: controlexperiment.SchemaVersion, ID: "qualified-etcd-semantic-workload-m5.15",
		PSSID: etcdraftv2.CorePSSMappingID, Admission: &admission,
		Runtime: controlexperiment.RuntimeConfig{
			SeedHex: hex.EncodeToString([]byte("official-etcdraft-v2-cluster-seed")), MaxClones: 1,
		},
		FaultEnvelope: &controlexperiment.FaultEnvelope{
			MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
			MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
		},
		DecisionsPerRun: 96, RequireReplay: true,
		Runs: []controlexperiment.RunPlan{{
			Run: 1,
			Policy: controlexperiment.Policy{
				Version: controlexperiment.PolicyVersion, ID: "semantic-workload-progress-v1",
				Priority: []control.ActionKind{
					control.ActionInvoke, control.ActionCompleteEffect,
					control.ActionDeliverMessage, control.ActionFireTemporal,
				},
			},
			Workload: &controlexperiment.WorkloadPlan{
				SchemaVersion: controlexperiment.WorkloadPlanVersion, ID: "single-write-v1",
				TargetSelector: controlexperiment.TargetSingleCoordinatingMember,
				Invocations: []controlexperiment.WorkloadInvocation{{
					ID: requestID, Input: payload, ExpectedStatus: "committed",
				}},
			},
		}},
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	report, err := controlexperiment.ExecuteQualified(
		ctx, config, bundle.Qualification, factory, etcdraftv2.CorePSSMapper{},
		etcdraftv2.WorkloadRouter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := report.ValidateWithQualification(bundle.Qualification); err != nil {
		t.Fatal(err)
	}
	run := report.Runs[0]
	if run.Workload == nil || run.Workload.Offered != 1 || run.Workload.Completed != 1 ||
		len(run.Workload.Results) != 1 || run.Workload.Results[0].Status != "committed" ||
		run.Faults == nil || *run.Faults != (controlexperiment.FaultUsage{}) ||
		report.Work.Primary.PrepareActions != 1 || report.Work.Replay.PrepareActions != 1 ||
		!run.Replay.Stable {
		t.Fatalf("unexpected workload report: %#v work=%#v", run, report.Work)
	}
	if !discoveredAppliedState(report) {
		t.Fatal("workload trace did not discover an applied Core PSS decision")
	}

	blocked := config
	blocked.ID = "qualified-etcd-fault-envelope-rejection-m5.15"
	blocked.Runs = []controlexperiment.RunPlan{{Run: 1, Policy: controlexperiment.Policy{
		Version: controlexperiment.PolicyVersion, ID: "disallowed-crash",
		Priority: []control.ActionKind{control.ActionCrash},
	}}}
	blocked.FaultEnvelope = &controlexperiment.FaultEnvelope{}
	_, err = controlexperiment.ExecuteQualified(
		ctx, blocked, bundle.Qualification, factory, etcdraftv2.CorePSSMapper{}, nil,
	)
	var failure *controlexperiment.ExecutionFailure
	if !errors.As(err, &failure) || failure.Code != "EXPERIMENT_POLICY_NO_ACTION" ||
		failure.Phase != "primary-policy" || failure.Decision != 1 ||
		failure.Work.Primary.SchedulerDecisions != 0 {
		t.Fatalf("admissible-frontier rejection = %#v / %v", failure, err)
	}
}

func discoveredAppliedState(report controlexperiment.Report) bool {
	for _, witness := range report.StateDiscovery.States {
		state, ok := witness.State.(psscore.State)
		if !ok {
			continue
		}
		for _, entity := range state.Semantic.Entities {
			if entity.Kind == psscore.EntityDecision && entity.Stage == psscore.StageApplied {
				return true
			}
		}
	}
	return false
}
