package main

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	etcdqualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/etcdraftv2"
)

const m523dExperimentDirectory = "../../benchmarks/experiments/etcdraft-v2-stateless-discovery-m5.23d"

func TestM523dEtcdraftDiscoveryIsReadOnlyRecomputedAndSeparatelyCharged(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
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
	methods := m523dTraversalMethods(t)
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	qualification, admission, workload, err := etcdraftQualifiedWorkload(ctx)
	if err != nil {
		t.Fatal(err)
	}
	discoveries := make([]controlexperiment.StatelessTraversalDiscovery, 0, len(methods))
	results := make([]controlexperiment.StatelessTraversalResult, 0, len(methods))
	allBundles := make([][]controlexperiment.ExecutionBundle, 0, len(methods))
	for methodIndex, method := range methods {
		result, exploreErr := controlexperiment.ExploreBoundedStatelessDFSWithMethod(
			ctx, method, spec, root, factory,
		)
		if exploreErr != nil {
			t.Fatal(exploreErr)
		}
		bundles := make([]controlexperiment.ExecutionBundle, 0, len(result.Search.Items))
		for _, item := range result.Search.Items {
			_, bundle := executeEtcdraftDiscoveryPrefix(
				t, ctx, "etcdraft-m5-23d", methodIndex+1, result, root, item, envelope,
				qualification, admission, workload,
			)
			bundles = append(bundles, bundle)
		}
		discovery, discoveryErr := controlexperiment.NewStatelessTraversalDiscovery(
			"etcdraft-m5-23d-method-"+string(rune('1'+methodIndex)), result, root, bundles,
			etcdraftv2.CorePSSMapper{},
		)
		if discoveryErr != nil {
			t.Fatal(discoveryErr)
		}
		if discovery.Validate(result, root, bundles, etcdraftv2.CorePSSMapper{}) != nil ||
			discovery.QualifiedExecutionAttempts != len(result.Search.Items) ||
			discovery.QualifiedExecutionWork.Primary.WorkUnits == 0 ||
			discovery.QualifiedExecutionWork.Replay.WorkUnits == 0 {
			t.Fatalf("discovery did not bind separately charged qualified evidence: %#v", discovery)
		}
		results = append(results, result)
		discoveries = append(discoveries, discovery)
		allBundles = append(allBundles, bundles)
	}
	for index := 1; index < len(results); index++ {
		if results[index].Search.Work != results[0].Search.Work ||
			discoveries[index].QualifiedExecutionWork != discoveries[0].QualifiedExecutionWork ||
			discoveries[index].QualifiedExecutionAttempts != discoveries[0].QualifiedExecutionAttempts {
			t.Fatalf("search/discovery work drifted before comparison: %d", index)
		}
	}
	if reflect.DeepEqual(discoveries[0].Items, discoveries[1].Items) {
		t.Fatal("different traversal orders produced identical ordered discovery evidence")
	}
	tampered := discoveries[0]
	tampered.Items = append([]controlexperiment.StatelessDiscoveryItem(nil), tampered.Items...)
	tampered.Items[0].FinalChildPSSKey = "invented-state"
	if err := tampered.Validate(
		results[0], root, allBundles[0], etcdraftv2.CorePSSMapper{},
	); err == nil {
		t.Fatal("tampered read-only discovery was accepted")
	}

	union := make(map[string]bool)
	for _, discovery := range discoveries {
		for _, key := range discovery.IncrementalPSSKeys {
			union[key] = true
		}
	}
	unionKeys := make([]string, 0, len(union))
	for key := range union {
		unionKeys = append(unionKeys, key)
	}
	unionDigest, err := control.CanonicalDigest(unionKeysSortedM523d(unionKeys))
	if err != nil {
		t.Fatal(err)
	}
	summary := m523dSummary{
		SchemaVersion: "consensus-atlas/stateless-discovery-calibration-summary/v1",
		Stage:         "M5.23d", Date: "2026-08-12",
		SourceBundleDigest: source.Digest, SourceTraceDigest: source.Trace.Digest,
		RootPrefixDigest: root.Digest, RootDecisions: len(root.Records),
		SpecDigest: spec.Digest, SearchWork: results[0].Search.Work,
		MethodCount: len(methods), WorkItemsPerMethod: spec.MaxWorkItems,
		QualifiedExecutionAttemptsPerMethod: discoveries[0].QualifiedExecutionAttempts,
		QualifiedExecutionWorkPerMethod:     discoveries[0].QualifiedExecutionWork,
		TotalEvidenceWorkUnitsPerMethod: results[0].Search.Work.TotalWorkUnits +
			discoveries[0].QualifiedExecutionWork.Primary.WorkUnits +
			discoveries[0].QualifiedExecutionWork.Replay.WorkUnits,
		RootPSSStates:             discoveries[0].RootPSSStates,
		RootPSSSetDigest:          discoveries[0].RootPSSSetDigest,
		UnionIncrementalPSSStates: len(union), UnionIncrementalPSSSetDigest: unionDigest,
		ReadOnlyProjection: true, ModelCalls: 0,
		Classification: "post-search-qualified-bundle-discovery-calibration-not-coverage-defect-or-method-ranking",
		Methods:        make([]m523dMethodSummary, 0, len(discoveries)),
	}
	for index, discovery := range discoveries {
		summary.Methods = append(summary.Methods, m523dMethodSummary{
			ID: methods[index].ID, Strategy: methods[index].Strategy, SeedHex: methods[index].SeedHex,
			MethodDigest: methods[index].Digest, SearchDigest: results[index].Search.Digest,
			DiscoveryDigest:            discovery.Digest,
			UniqueTracePrefixes:        discovery.UniqueTracePrefixes,
			UniqueFinalChildPSSStates:  discovery.UniqueFinalChildPSSStates,
			UniqueObservedPSSStates:    discovery.UniqueObservedPSSStates,
			UniqueIncrementalPSSStates: discovery.UniqueIncrementalPSSStates,
			TracePrefixSetDigest:       discovery.TracePrefixSetDigest,
			FinalChildPSSSetDigest:     discovery.FinalChildPSSSetDigest,
			ObservedPSSSetDigest:       discovery.ObservedPSSSetDigest,
			IncrementalPSSSetDigest:    discovery.IncrementalPSSSetDigest,
		})
	}
	checked := readM521hJSONFile[m523dSummary](
		t, m523dExperimentDirectory+"/summary.json", 96<<10,
	)
	if !reflect.DeepEqual(checked, summary) {
		encoded, marshalErr := json.MarshalIndent(summary, "", "  ")
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		t.Fatalf("checked-in M5.23d summary drifted:\n%s", encoded)
	}
}

func m523dTraversalMethods(t *testing.T) []controlexperiment.StatelessTraversalMethod {
	t.Helper()
	return []controlexperiment.StatelessTraversalMethod{
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
}

func executeEtcdraftDiscoveryPrefix(
	t *testing.T,
	ctx context.Context,
	executionIDPrefix string,
	methodOrdinal int,
	result controlexperiment.StatelessTraversalResult,
	root controlruntime.Trace,
	item controlexperiment.StatelessDFSWorkItem,
	envelope *controlexperiment.FaultEnvelope,
	qualification etcdqualification.Bundle,
	admission controlexperiment.ExecutionAdmission,
	workload controlexperiment.WorkloadPlan,
) (controlexperiment.Report, controlexperiment.ExecutionBundle) {
	t.Helper()
	executionID := executionIDPrefix + "-method-" + string(rune('1'+methodOrdinal-1)) +
		"-item-" + string(rune('1'+item.Ordinal-1))
	report, bundle, err := executeEtcdraftStatelessPrefix(
		ctx, executionID, result, root, item, envelope,
		qualification, admission, workload,
	)
	if err != nil {
		t.Fatal(err)
	}
	return report, bundle
}

func unionKeysSortedM523d(values []string) []string {
	slices.Sort(values)
	return values
}

type m523dMethodSummary struct {
	ID                         string `json:"id"`
	Strategy                   string `json:"strategy"`
	SeedHex                    string `json:"seed_hex,omitempty"`
	MethodDigest               string `json:"method_digest"`
	SearchDigest               string `json:"search_digest"`
	DiscoveryDigest            string `json:"discovery_digest"`
	UniqueTracePrefixes        int    `json:"unique_trace_prefixes"`
	UniqueFinalChildPSSStates  int    `json:"unique_final_child_pss_states"`
	UniqueObservedPSSStates    int    `json:"unique_observed_pss_states"`
	UniqueIncrementalPSSStates int    `json:"unique_incremental_pss_states"`
	TracePrefixSetDigest       string `json:"trace_prefix_set_digest"`
	FinalChildPSSSetDigest     string `json:"final_child_pss_set_digest"`
	ObservedPSSSetDigest       string `json:"observed_pss_set_digest"`
	IncrementalPSSSetDigest    string `json:"incremental_pss_set_digest"`
}

type m523dSummary struct {
	SchemaVersion                       string                             `json:"schema_version"`
	Stage                               string                             `json:"stage"`
	Date                                string                             `json:"date"`
	SourceBundleDigest                  string                             `json:"source_bundle_digest"`
	SourceTraceDigest                   string                             `json:"source_trace_digest"`
	RootPrefixDigest                    string                             `json:"root_prefix_digest"`
	RootDecisions                       int                                `json:"root_decisions"`
	SpecDigest                          string                             `json:"spec_digest"`
	SearchWork                          controlexperiment.StatelessDFSWork `json:"search_work"`
	MethodCount                         int                                `json:"method_count"`
	WorkItemsPerMethod                  int                                `json:"work_items_per_method"`
	QualifiedExecutionAttemptsPerMethod int                                `json:"qualified_execution_attempts_per_method"`
	QualifiedExecutionWorkPerMethod     controlexperiment.WorkLedger       `json:"qualified_execution_work_per_method"`
	TotalEvidenceWorkUnitsPerMethod     int                                `json:"total_evidence_work_units_per_method"`
	Methods                             []m523dMethodSummary               `json:"methods"`
	RootPSSStates                       int                                `json:"root_pss_states"`
	RootPSSSetDigest                    string                             `json:"root_pss_set_digest"`
	UnionIncrementalPSSStates           int                                `json:"union_incremental_pss_states"`
	UnionIncrementalPSSSetDigest        string                             `json:"union_incremental_pss_set_digest"`
	ReadOnlyProjection                  bool                               `json:"read_only_projection"`
	ModelCalls                          int                                `json:"model_calls"`
	Classification                      string                             `json:"classification"`
}
