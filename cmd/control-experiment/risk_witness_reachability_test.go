package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const m521pExperimentDirectory = "../../benchmarks/experiments/etcdraft-v2-risk-witness-reachability-m5.21p"

func TestEtcdraftM521pRealRuntimeReachesFrozenRiskWitness(t *testing.T) {
	if _, _, err := etcdraftExecution(
		context.Background(), "workload-risk-witness-calibration", 65, 1, true,
	); err == nil {
		t.Fatal("calibration accepted an unregistered decision budget")
	}
	if _, _, err := etcdraftExecution(
		context.Background(), "workload-risk-witness-calibration", 64, 2, true,
	); err == nil {
		t.Fatal("calibration accepted an unregistered policy seed")
	}
	report, bundle, err := etcdraftExecution(
		context.Background(), "workload-risk-witness-calibration", 64, 1, true,
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := newEtcdraftLeaderChangeRiskWitness("public-calibration-reached", bundle)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Runs) != 1 || report.Runs[0].Workload == nil || report.Runs[0].Faults == nil {
		t.Fatalf("calibration run evidence missing: %#v", report.Runs)
	}
	run := report.Runs[0]
	if result.Status != semantic.RiskWitnessReached || len(result.Milestones) != 3 ||
		result.Milestones[0].Step != 28 || result.Milestones[1].Step != 53 ||
		result.Milestones[2].Step != 54 || run.ChargedDecisions != 64 || !run.Replay.Stable ||
		run.Workload.Offered != 1 || run.Workload.Completed != 0 || run.Workload.Pending != 1 ||
		run.Faults.Crashes != 1 || report.Work.Primary.WorkUnits != 66 ||
		report.Work.Replay.WorkUnits != 66 {
		t.Fatalf("real Runtime did not retain the frozen calibration semantics: result=%#v run=%#v", result, run)
	}
	calibration := m521pReachabilityCalibration{
		SchemaVersion:         "consensus-atlas/risk-witness-reachability-calibration/v1",
		Strategy:              "workload-risk-witness-calibration",
		DecisionBudget:        64,
		PolicySeed:            1,
		ConfigDigest:          report.ConfigDigest,
		PolicyDigest:          run.PolicyDigest,
		ReportDigest:          report.Digest,
		BundleDigest:          bundle.Digest,
		TraceDigest:           bundle.Trace.Digest,
		DecisionHistoryDigest: bundle.Decisions.Digest,
		ReplayStable:          run.Replay.Stable,
		Work:                  report.Work,
		Workload:              *run.Workload,
		Faults:                *run.Faults,
		UniquePSSStates:       report.StateDiscovery.UniqueStates,
		RiskWitness:           result,
		NewModelCalls:         0,
		NewSUTExecutions:      1,
		Classification:        "real-runtime-risk-witness-reached-calibration-not-method-ranking-or-oracle-verdict",
	}
	checked := readM521hJSONFile[m521pReachabilityCalibration](
		t, m521pExperimentDirectory+"/summary.json", 1<<20,
	)
	if !reflect.DeepEqual(checked, calibration) {
		t.Fatalf("checked M5.21p calibration drifted: checked=%#v fresh=%#v", checked, calibration)
	}
}

type m521pReachabilityCalibration struct {
	SchemaVersion         string                              `json:"schema_version"`
	Strategy              string                              `json:"strategy"`
	DecisionBudget        int                                 `json:"decision_budget"`
	PolicySeed            uint64                              `json:"policy_seed"`
	ConfigDigest          string                              `json:"config_digest"`
	PolicyDigest          string                              `json:"policy_digest"`
	ReportDigest          string                              `json:"report_digest"`
	BundleDigest          string                              `json:"bundle_digest"`
	TraceDigest           string                              `json:"trace_digest"`
	DecisionHistoryDigest string                              `json:"decision_history_digest"`
	ReplayStable          bool                                `json:"replay_stable"`
	Work                  controlexperiment.WorkLedger        `json:"work"`
	Workload              controlexperiment.WorkloadRunReport `json:"workload"`
	Faults                controlexperiment.FaultUsage        `json:"faults"`
	UniquePSSStates       int                                 `json:"unique_pss_states"`
	RiskWitness           semantic.RiskWitnessResult          `json:"risk_witness"`
	NewModelCalls         int                                 `json:"new_model_calls"`
	NewSUTExecutions      int                                 `json:"new_sut_executions"`
	Classification        string                              `json:"classification"`
}
