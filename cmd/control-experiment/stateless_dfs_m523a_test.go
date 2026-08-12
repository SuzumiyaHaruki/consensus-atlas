package main

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const m523aExperimentDirectory = "../../benchmarks/experiments/etcdraft-v2-stateless-dfs-m5.23a"

func TestM523aEtcdraftBoundedStatelessDFSIsDeterministicAndExecutable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	_, source, err := etcdraftExecution(ctx, "workload-risk-witness-calibration", 64, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	root, err := controlexperiment.ExecutionTracePrefix(source.Trace, 28)
	if err != nil {
		t.Fatal(err)
	}
	envelope := &controlexperiment.FaultEnvelope{
		MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
		MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
	}
	spec, err := controlexperiment.NewStatelessDFSSpec(
		"etcdraft-m5-23a", root, etcdraftCampaignRuntimeConfig(), envelope,
		2, 6, 1000,
	)
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	first, err := controlexperiment.ExploreBoundedStatelessDFS(ctx, spec, root, factory)
	if err != nil {
		t.Fatal(err)
	}
	second, err := controlexperiment.ExploreBoundedStatelessDFS(ctx, spec, root, factory)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || first.Digest != second.Digest ||
		len(first.Items) != spec.MaxWorkItems ||
		first.StopReason != controlexperiment.StatelessDFSStopItems {
		t.Fatalf("real-target DFS did not retain frozen order/bounds: first=%#v second=%#v", first, second)
	}
	leaf := first.Items[len(first.Items)-1]
	if leaf.Path.Depth != spec.MaxDepth {
		t.Fatalf("frozen DFS did not reach the requested depth: %#v", leaf)
	}
	fallback := []control.ActionKind{
		control.ActionInvoke, control.ActionCompleteEffect,
		control.ActionDeliverMessage, control.ActionFireTemporal,
	}
	policy, err := controlexperiment.CompileStatelessDFSPath(
		"etcdraft-m5-23a-leaf", first, root, leaf.Ordinal, fallback, 64,
	)
	if err != nil {
		t.Fatal(err)
	}
	report, bundle := executeEtcdraftFrontierPolicy(t, ctx, "m5-23a-dfs-leaf", policy, envelope)
	executedPrefix, err := controlexperiment.ExecutionTracePrefix(bundle.Trace, leaf.Path.Decision)
	if err != nil {
		t.Fatal(err)
	}
	if executedPrefix.Digest != leaf.ChildPrefixDigest ||
		executedPrefix.FinalStateDigest != leaf.ChildStateDigest ||
		!report.Runs[0].Replay.Stable || report.Runs[0].ChargedDecisions != 64 ||
		first.Work.TotalWorkUnits > spec.MaxWorkUnits {
		t.Fatalf("DFS WorkItem did not execute through the qualified path: result=%#v run=%#v", first, report.Runs[0])
	}
	summary := statelessDFSCalibrationSummary{
		SchemaVersion: "consensus-atlas/stateless-dfs-calibration-summary/v1",
		Stage:         "M5.23a", Date: "2026-08-12",
		SourceBundleDigest: source.Digest, SourceTraceDigest: source.Trace.Digest,
		RootPrefixDigest: root.Digest, RootDecisions: len(root.Records),
		SpecDigest: spec.Digest, ResultDigest: first.Digest,
		MaxDepth: spec.MaxDepth, MaxWorkItems: spec.MaxWorkItems, MaxWorkUnits: spec.MaxWorkUnits,
		StatesExpanded: first.StatesExpanded, WorkItems: len(first.Items),
		StopReason: first.StopReason, SearchWork: first.Work,
		LeafOrdinal: leaf.Ordinal, LeafDigest: leaf.Digest,
		LeafPrefixDigest: leaf.ChildPrefixDigest, LeafStateDigest: leaf.ChildStateDigest,
		PolicyDigest: mustPolicyDigest(t, policy), ReportDigest: report.Digest,
		BundleDigest: bundle.Digest, ExecutionTraceDigest: bundle.Trace.Digest,
		ExecutionWork: report.Work, ExecutionReplayStable: report.Runs[0].Replay.Stable,
		DeterministicRepeat:  reflect.DeepEqual(first, second),
		LeafPrefixReproduced: executedPrefix.Digest == leaf.ChildPrefixDigest,
		ModelCalls:           0,
		Classification:       "bounded-exact-prefix-stateless-dfs-calibration-not-state-complete-or-method-ranking",
		Items:                make([]statelessDFSItemSummary, 0, len(first.Items)),
	}
	for _, item := range first.Items {
		summary.Items = append(summary.Items, statelessDFSItemSummary{
			Ordinal: item.Ordinal, ParentOrdinal: item.Path.ParentOrdinal,
			Depth: item.Path.Depth, Decision: item.Path.Decision,
			Kind: item.Action.Kind, ActionID: item.Action.ActionID,
			Digest: item.Digest, ChildPrefixDigest: item.ChildPrefixDigest,
		})
	}
	checked := readM521hJSONFile[statelessDFSCalibrationSummary](
		t, m523aExperimentDirectory+"/summary.json", 96<<10,
	)
	if !reflect.DeepEqual(checked, summary) {
		t.Fatalf("checked-in M5.23a summary drifted:\n got: %#v\nwant: %#v", checked, summary)
	}
	t.Logf(
		"source=%s/%s spec=%s result=%s root=%s items=%d states=%d stop=%s work=%#v leaf=%s/%s policy=%s report=%s bundle=%s execution=%s execution_work=%#v",
		source.Digest, source.Trace.Digest,
		spec.Digest, first.Digest, root.Digest, len(first.Items), first.StatesExpanded,
		first.StopReason, first.Work, leaf.Digest, leaf.ChildPrefixDigest,
		mustPolicyDigest(t, policy), report.Digest, bundle.Digest, bundle.Trace.Digest, report.Work,
	)
	for _, item := range first.Items {
		t.Logf("item=%d parent=%d depth=%d decision=%d kind=%s action=%s digest=%s child=%s",
			item.Ordinal, item.Path.ParentOrdinal, item.Path.Depth, item.Path.Decision,
			item.Action.Kind, item.Action.ActionID, item.Digest, item.ChildPrefixDigest)
	}
}

type statelessDFSItemSummary struct {
	Ordinal           int                `json:"ordinal"`
	ParentOrdinal     int                `json:"parent_ordinal"`
	Depth             int                `json:"depth"`
	Decision          int                `json:"decision"`
	Kind              control.ActionKind `json:"kind"`
	ActionID          control.ActionID   `json:"action_id"`
	Digest            string             `json:"digest"`
	ChildPrefixDigest string             `json:"child_prefix_digest"`
}

type statelessDFSCalibrationSummary struct {
	SchemaVersion         string                             `json:"schema_version"`
	Stage                 string                             `json:"stage"`
	Date                  string                             `json:"date"`
	SourceBundleDigest    string                             `json:"source_bundle_digest"`
	SourceTraceDigest     string                             `json:"source_trace_digest"`
	RootPrefixDigest      string                             `json:"root_prefix_digest"`
	RootDecisions         int                                `json:"root_decisions"`
	SpecDigest            string                             `json:"spec_digest"`
	ResultDigest          string                             `json:"result_digest"`
	MaxDepth              int                                `json:"max_depth"`
	MaxWorkItems          int                                `json:"max_work_items"`
	MaxWorkUnits          int                                `json:"max_work_units"`
	StatesExpanded        int                                `json:"states_expanded"`
	WorkItems             int                                `json:"work_items"`
	StopReason            string                             `json:"stop_reason"`
	SearchWork            controlexperiment.StatelessDFSWork `json:"search_work"`
	Items                 []statelessDFSItemSummary          `json:"items"`
	LeafOrdinal           int                                `json:"leaf_ordinal"`
	LeafDigest            string                             `json:"leaf_digest"`
	LeafPrefixDigest      string                             `json:"leaf_prefix_digest"`
	LeafStateDigest       string                             `json:"leaf_state_digest"`
	PolicyDigest          string                             `json:"policy_digest"`
	ReportDigest          string                             `json:"report_digest"`
	BundleDigest          string                             `json:"bundle_digest"`
	ExecutionTraceDigest  string                             `json:"execution_trace_digest"`
	ExecutionWork         controlexperiment.WorkLedger       `json:"execution_work"`
	ExecutionReplayStable bool                               `json:"execution_replay_stable"`
	DeterministicRepeat   bool                               `json:"deterministic_repeat"`
	LeafPrefixReproduced  bool                               `json:"leaf_prefix_reproduced"`
	ModelCalls            int                                `json:"model_calls"`
	Classification        string                             `json:"classification"`
}

func mustPolicyDigest(t *testing.T, policy controlexperiment.Policy) string {
	t.Helper()
	digest, err := policy.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return digest
}
