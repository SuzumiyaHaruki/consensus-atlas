package main

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const m523cExperimentDirectory = "../../benchmarks/experiments/etcdraft-v2-matched-traversal-m5.23c"

func TestM523cEtcdraftTraversalMethodsShareExactRootAndBounds(t *testing.T) {
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
		"etcdraft-m5-23c", root, etcdraftCampaignRuntimeConfig(), envelope,
		2, 6, 1000,
	)
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	methods := []controlexperiment.StatelessTraversalMethod{
		mustStatelessTraversalMethod(
			t, "etcdraft-m5-23c-canonical", controlexperiment.StatelessTraversalCanonical, "",
		),
		mustStatelessTraversalMethod(
			t, "etcdraft-m5-23c-uniform-seed-1", controlexperiment.StatelessTraversalSeededUniform, "01",
		),
		mustStatelessTraversalMethod(
			t, "etcdraft-m5-23c-uniform-seed-2", controlexperiment.StatelessTraversalSeededUniform, "02",
		),
		mustStatelessTraversalMethod(
			t, "etcdraft-m5-23c-uniform-seed-3", controlexperiment.StatelessTraversalSeededUniform, "03",
		),
	}
	results := make([]controlexperiment.StatelessTraversalResult, 0, len(methods))
	for _, method := range methods {
		first, exploreErr := controlexperiment.ExploreBoundedStatelessDFSWithMethod(
			ctx, method, spec, root, factory,
		)
		if exploreErr != nil {
			t.Fatal(exploreErr)
		}
		repeated, exploreErr := controlexperiment.ExploreBoundedStatelessDFSWithMethod(
			ctx, method, spec, root, factory,
		)
		if exploreErr != nil {
			t.Fatal(exploreErr)
		}
		if !reflect.DeepEqual(first, repeated) || first.Search.Spec.Digest != spec.Digest ||
			len(first.Search.Items) != spec.MaxWorkItems ||
			first.Search.StopReason != controlexperiment.StatelessDFSStopItems ||
			first.Search.Work.TotalWorkUnits > spec.MaxWorkUnits {
			t.Fatalf("matched traversal method was not deterministic and bounded: %#v", first)
		}
		results = append(results, first)
	}
	legacy, err := controlexperiment.ExploreBoundedStatelessDFS(ctx, spec, root, factory)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(legacy, results[0].Search) {
		t.Fatal("versioned canonical traversal drifted from the frozen DFS behavior")
	}
	orders := make(map[string]bool)
	for index, result := range results {
		if result.Search.Work != results[0].Search.Work ||
			result.Search.StatesExpanded != results[0].Search.StatesExpanded ||
			len(result.Search.Items) != len(results[0].Search.Items) ||
			result.Search.StopReason != results[0].Search.StopReason {
			t.Fatalf("method %d did not retain the matched execution bounds: %#v", index, result)
		}
		orders[actionOrderM523c(result.Search)] = true
	}
	if len(orders) < 2 {
		t.Fatalf("seeded traversal did not alter the bounded branch order: %#v", results)
	}

	uniform := results[len(results)-1]
	leaf := uniform.Search.Items[len(uniform.Search.Items)-1]
	policy, err := controlexperiment.CompileStatelessDFSPath(
		"etcdraft-m5-23c-uniform-leaf", uniform.Search, root, leaf.Ordinal,
		[]control.ActionKind{
			control.ActionInvoke, control.ActionCompleteEffect,
			control.ActionDeliverMessage, control.ActionFireTemporal,
		},
		64,
	)
	if err != nil {
		t.Fatal(err)
	}
	report, bundle := executeEtcdraftFrontierPolicy(
		t, ctx, "m5-23c-uniform-leaf", policy, envelope,
	)
	executedPrefix, err := controlexperiment.ExecutionTracePrefix(bundle.Trace, leaf.Path.Decision)
	if err != nil {
		t.Fatal(err)
	}
	if executedPrefix.Digest != leaf.ChildPrefixDigest ||
		executedPrefix.FinalStateDigest != leaf.ChildStateDigest ||
		!report.Runs[0].Replay.Stable {
		t.Fatalf("seeded traversal leaf did not reproduce through qualified execution: %#v", report.Runs[0])
	}

	summary := m523cSummary{
		SchemaVersion: "consensus-atlas/matched-stateless-traversal-summary/v1",
		Stage:         "M5.23c", Date: "2026-08-12",
		SourceBundleDigest: source.Digest, SourceTraceDigest: source.Trace.Digest,
		RootPrefixDigest: root.Digest, RootDecisions: len(root.Records),
		SpecDigest: spec.Digest, MaxDepth: spec.MaxDepth,
		MaxWorkItems: spec.MaxWorkItems, MaxWorkUnits: spec.MaxWorkUnits,
		SameRoot: true, SameLimits: true, DeterministicRepeats: true,
		DistinctOrders: len(orders), ModelCalls: 0,
		StatesExpanded:   results[0].Search.StatesExpanded,
		WorkItems:        len(results[0].Search.Items),
		StopReason:       results[0].Search.StopReason,
		SearchWork:       results[0].Search.Work,
		LeafMethodDigest: uniform.Method.Digest, LeafOrdinal: leaf.Ordinal,
		LeafPrefixDigest: leaf.ChildPrefixDigest,
		LeafPolicyDigest: mustPolicyDigest(t, policy),
		LeafReportDigest: report.Digest, LeafBundleDigest: bundle.Digest,
		LeafExecutionTraceDigest: bundle.Trace.Digest,
		LeafPrefixReproduced:     executedPrefix.Digest == leaf.ChildPrefixDigest,
		LeafReplayStable:         report.Runs[0].Replay.Stable,
		Classification:           "matched-exact-prefix-traversal-calibration-not-method-effectiveness-or-agent-ranking",
		Methods:                  make([]m523cMethodSummary, 0, len(results)),
	}
	for _, result := range results {
		order := actionOrderSliceM523c(result.Search)
		orderDigest, digestErr := control.CanonicalDigest(order)
		if digestErr != nil {
			t.Fatal(digestErr)
		}
		summary.Methods = append(summary.Methods, m523cMethodSummary{
			ID: result.Method.ID, Strategy: result.Method.Strategy,
			SeedHex: result.Method.SeedHex, MethodDigest: result.Method.Digest,
			ResultDigest: result.Digest, SearchDigest: result.Search.Digest,
			ActionOrderDigest: orderDigest, ActionKinds: actionKindOrderM523c(result.Search),
		})
	}
	checked := readM521hJSONFile[m523cSummary](
		t, m523cExperimentDirectory+"/summary.json", 128<<10,
	)
	if !reflect.DeepEqual(checked, summary) {
		encoded, marshalErr := json.MarshalIndent(summary, "", "  ")
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		t.Fatalf("checked-in M5.23c summary drifted:\n%s", encoded)
	}
}

func mustStatelessTraversalMethod(
	t *testing.T,
	id string,
	strategy string,
	seedHex string,
) controlexperiment.StatelessTraversalMethod {
	t.Helper()
	method, err := controlexperiment.NewStatelessTraversalMethod(id, strategy, seedHex)
	if err != nil {
		t.Fatal(err)
	}
	return method
}

func actionOrderM523c(result controlexperiment.StatelessDFSResult) string {
	encoded, err := json.Marshal(actionOrderSliceM523c(result))
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func actionOrderSliceM523c(result controlexperiment.StatelessDFSResult) []string {
	order := make([]string, 0, len(result.Items))
	for _, item := range result.Items {
		order = append(order, string(item.Action.Kind)+":"+string(item.Action.ActionID))
	}
	return order
}

func actionKindOrderM523c(result controlexperiment.StatelessDFSResult) []control.ActionKind {
	order := make([]control.ActionKind, 0, len(result.Items))
	for _, item := range result.Items {
		order = append(order, item.Action.Kind)
	}
	return order
}

type m523cMethodSummary struct {
	ID                string               `json:"id"`
	Strategy          string               `json:"strategy"`
	SeedHex           string               `json:"seed_hex,omitempty"`
	MethodDigest      string               `json:"method_digest"`
	ResultDigest      string               `json:"result_digest"`
	SearchDigest      string               `json:"search_digest"`
	ActionOrderDigest string               `json:"action_order_digest"`
	ActionKinds       []control.ActionKind `json:"action_kinds"`
}

type m523cSummary struct {
	SchemaVersion            string                             `json:"schema_version"`
	Stage                    string                             `json:"stage"`
	Date                     string                             `json:"date"`
	SourceBundleDigest       string                             `json:"source_bundle_digest"`
	SourceTraceDigest        string                             `json:"source_trace_digest"`
	RootPrefixDigest         string                             `json:"root_prefix_digest"`
	RootDecisions            int                                `json:"root_decisions"`
	SpecDigest               string                             `json:"spec_digest"`
	MaxDepth                 int                                `json:"max_depth"`
	MaxWorkItems             int                                `json:"max_work_items"`
	MaxWorkUnits             int                                `json:"max_work_units"`
	SameRoot                 bool                               `json:"same_root"`
	SameLimits               bool                               `json:"same_limits"`
	DeterministicRepeats     bool                               `json:"deterministic_repeats"`
	DistinctOrders           int                                `json:"distinct_orders"`
	StatesExpanded           int                                `json:"states_expanded"`
	WorkItems                int                                `json:"work_items"`
	StopReason               string                             `json:"stop_reason"`
	SearchWork               controlexperiment.StatelessDFSWork `json:"search_work"`
	Methods                  []m523cMethodSummary               `json:"methods"`
	LeafMethodDigest         string                             `json:"leaf_method_digest"`
	LeafOrdinal              int                                `json:"leaf_ordinal"`
	LeafPrefixDigest         string                             `json:"leaf_prefix_digest"`
	LeafPolicyDigest         string                             `json:"leaf_policy_digest"`
	LeafReportDigest         string                             `json:"leaf_report_digest"`
	LeafBundleDigest         string                             `json:"leaf_bundle_digest"`
	LeafExecutionTraceDigest string                             `json:"leaf_execution_trace_digest"`
	LeafPrefixReproduced     bool                               `json:"leaf_prefix_reproduced"`
	LeafReplayStable         bool                               `json:"leaf_replay_stable"`
	ModelCalls               int                                `json:"model_calls"`
	Classification           string                             `json:"classification"`
}
