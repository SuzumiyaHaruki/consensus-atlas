package main

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const m523eExperimentDirectory = "../../benchmarks/experiments/etcdraft-v2-root-corpus-m5.23e"

func TestM523eEtcdraftRootCorpusIsFrozenBeforeMatchedDiscovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 360*time.Second)
	defer cancel()
	_, source, err := etcdraftExecution(ctx, "workload-risk-witness-calibration", 64, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	corpus, err := controlexperiment.NewStatelessRootCorpus(
		"etcdraft-m5-23e-roots", "fixed-source-milestone-anchors-v1", source,
		[]controlexperiment.StatelessRootSpec{
			{ID: "initial", PhaseID: "source-initial-state", Decisions: 0},
			{ID: "invoked", PhaseID: "workload-invoked-milestone", Decisions: 28},
			{ID: "restarted", PhaseID: "old-coordinator-restarted-milestone", Decisions: 54},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	checkedCorpus := readM521hJSONFile[controlexperiment.StatelessRootCorpus](
		t, m523eExperimentDirectory+"/root-corpus.json", 32<<10,
	)
	if !reflect.DeepEqual(checkedCorpus, corpus) {
		t.Fatalf("checked-in M5.23e root corpus drifted:\n%s", mustJSONM523e(t, corpus))
	}

	envelope := &controlexperiment.FaultEnvelope{
		MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
		MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
	}
	qualification, admission, workload, err := etcdraftQualifiedWorkload(ctx)
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	methods := m523dTraversalMethods(t)
	discoveries := make([]controlexperiment.StatelessCorpusDiscovery, 0, len(methods))
	for methodIndex, method := range methods {
		evidence := make([]controlexperiment.StatelessCorpusRootEvidence, 0, len(corpus.Roots))
		for rootIndex, entry := range corpus.Roots {
			root, prefixErr := corpus.Prefix(source, entry.ID)
			if prefixErr != nil {
				t.Fatal(prefixErr)
			}
			spec, specErr := controlexperiment.NewStatelessDFSSpec(
				fmt.Sprintf("etcdraft-m5-23e-root-%d", rootIndex+1), root,
				etcdraftCampaignRuntimeConfig(), envelope, 2, 6, 1500,
			)
			if specErr != nil {
				t.Fatal(specErr)
			}
			result, exploreErr := controlexperiment.ExploreBoundedStatelessDFSWithMethod(
				ctx, method, spec, root, factory,
			)
			if exploreErr != nil {
				t.Fatal(exploreErr)
			}
			if len(result.Search.Items) != spec.MaxWorkItems ||
				result.Search.StopReason != controlexperiment.StatelessDFSStopItems {
				t.Fatalf("root %s did not expose the frozen bounded corpus: %#v", entry.ID, result.Search)
			}
			bundles := make([]controlexperiment.ExecutionBundle, 0, len(result.Search.Items))
			for _, item := range result.Search.Items {
				_, bundle := executeEtcdraftDiscoveryPrefix(
					t, ctx, "etcdraft-m5-23e-root-"+entry.ID, methodIndex+1,
					result, root, item, envelope, qualification, admission, workload,
				)
				bundles = append(bundles, bundle)
			}
			evidence = append(evidence, controlexperiment.StatelessCorpusRootEvidence{
				RootID: entry.ID, Result: result, Bundles: bundles,
			})
		}
		discovery, discoveryErr := controlexperiment.NewStatelessCorpusDiscovery(
			fmt.Sprintf("etcdraft-m5-23e-method-%d", methodIndex+1),
			corpus, source, evidence, etcdraftv2.CorePSSMapper{},
		)
		if discoveryErr != nil || discovery.Validate(
			corpus, source, evidence, etcdraftv2.CorePSSMapper{},
		) != nil {
			t.Fatalf("corpus discovery failed validation: %v", discoveryErr)
		}
		discoveries = append(discoveries, discovery)
	}
	for index := 1; index < len(discoveries); index++ {
		if discoveries[index].QualifiedExecutionAttempts != discoveries[0].QualifiedExecutionAttempts ||
			discoveries[index].CorpusBaselinePSSSetDigest != discoveries[0].CorpusBaselinePSSSetDigest {
			t.Fatalf("method %d lost common corpus baseline or attempt count", index+1)
		}
	}
	tampered := corpus
	tampered.Roots = append([]controlexperiment.StatelessRootEntry(nil), tampered.Roots...)
	tampered.Roots[0].PhaseID = "selected-after-search"
	if tampered.Validate(source) == nil {
		t.Fatal("tampered root corpus was accepted")
	}

	unionNovel := make(map[string]bool)
	for _, discovery := range discoveries {
		for _, key := range discovery.CorpusNovelPSSKeys {
			unionNovel[key] = true
		}
	}
	unionNovelKeys := make([]string, 0, len(unionNovel))
	for key := range unionNovel {
		unionNovelKeys = append(unionNovelKeys, key)
	}
	slices.Sort(unionNovelKeys)
	unionNovelDigest, err := control.CanonicalDigest(unionNovelKeys)
	if err != nil {
		t.Fatal(err)
	}
	sharedSourceWork := source.Work.Primary.WorkUnits + source.Work.Replay.WorkUnits
	summary := m523eSummary{
		SchemaVersion: "consensus-atlas/stateless-root-corpus-calibration-summary/v1",
		Stage:         "M5.23e", Date: "2026-08-12", CorpusDigest: corpus.Digest,
		SourceBundleDigest: source.Digest, SourceTraceDigest: source.Trace.Digest,
		SharedSourceWork: source.Work, SharedSourceWorkUnits: sharedSourceWork,
		RootCount: len(corpus.Roots), MethodCount: len(methods), MaxDepth: 2,
		MaxWorkItemsPerRoot: 6, MaxWorkUnitsPerRoot: 1500,
		QualifiedExecutionAttemptsPerMethod: discoveries[0].QualifiedExecutionAttempts,
		CorpusBaselinePSSStates:             discoveries[0].CorpusBaselinePSSStates,
		CorpusBaselinePSSSetDigest:          discoveries[0].CorpusBaselinePSSSetDigest,
		UnionMethodCorpusNovelPSSStates:     len(unionNovel),
		UnionMethodCorpusNovelPSSSetDigest:  unionNovelDigest,
		RootSelectionUsesMethodOutput:       false, ReadOnlyProjection: true, ModelCalls: 0,
		Classification: "predeclared-multi-root-discovery-calibration-not-coverage-defect-significance-or-method-ranking",
	}
	for index, discovery := range discoveries {
		summary.CampaignEvidenceWorkUnits += discovery.MarginalEvidenceWorkUnits
		if summary.MinMarginalEvidenceWorkUnits == 0 ||
			discovery.MarginalEvidenceWorkUnits < summary.MinMarginalEvidenceWorkUnits {
			summary.MinMarginalEvidenceWorkUnits = discovery.MarginalEvidenceWorkUnits
		}
		if discovery.MarginalEvidenceWorkUnits > summary.MaxMarginalEvidenceWorkUnits {
			summary.MaxMarginalEvidenceWorkUnits = discovery.MarginalEvidenceWorkUnits
		}
		methodSummary := m523eMethodSummary{
			ID: methods[index].ID, Strategy: methods[index].Strategy, SeedHex: methods[index].SeedHex,
			MethodDigest: methods[index].Digest, DiscoveryDigest: discovery.Digest,
			LocalIncrementalPSSStates:    discovery.LocalIncrementalPSSStates,
			LocalIncrementalPSSSetDigest: discovery.LocalIncrementalPSSSetDigest,
			CorpusNovelPSSStates:         discovery.CorpusNovelPSSStates,
			CorpusNovelPSSSetDigest:      discovery.CorpusNovelPSSSetDigest,
			SearchWork:                   discovery.SearchWork,
			QualifiedExecutionWork:       discovery.QualifiedExecutionWork,
			MarginalEvidenceWorkUnits:    discovery.MarginalEvidenceWorkUnits,
			EndToEndWorkUnitsIfRunAlone:  sharedSourceWork + discovery.MarginalEvidenceWorkUnits,
		}
		for _, root := range discovery.Roots {
			methodSummary.Roots = append(methodSummary.Roots, m523eMethodRootSummary{
				RootID: root.RootID, RootPSSStates: root.RootPSSStates,
				LocalIncrementalPSSStates: root.LocalIncrementalPSSStates,
				SearchWorkUnits:           root.SearchWork.TotalWorkUnits,
				PrimaryWorkUnits:          root.QualifiedExecutionWork.Primary.WorkUnits,
				ReplayWorkUnits:           root.QualifiedExecutionWork.Replay.WorkUnits,
				DiscoveryDigest:           root.DiscoveryDigest,
			})
		}
		summary.Methods = append(summary.Methods, methodSummary)
	}
	summary.CampaignEvidenceWorkUnits += sharedSourceWork
	checked := readM521hJSONFile[m523eSummary](
		t, m523eExperimentDirectory+"/summary.json", 128<<10,
	)
	if !reflect.DeepEqual(checked, summary) {
		t.Fatalf("checked-in M5.23e summary drifted:\n%s", mustJSONM523e(t, summary))
	}
}

func mustJSONM523e(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

type m523eMethodRootSummary struct {
	RootID                    string `json:"root_id"`
	RootPSSStates             int    `json:"root_pss_states"`
	LocalIncrementalPSSStates int    `json:"local_incremental_pss_states"`
	SearchWorkUnits           int    `json:"search_work_units"`
	PrimaryWorkUnits          int    `json:"primary_work_units"`
	ReplayWorkUnits           int    `json:"replay_work_units"`
	DiscoveryDigest           string `json:"discovery_digest"`
}

type m523eMethodSummary struct {
	ID                           string                             `json:"id"`
	Strategy                     string                             `json:"strategy"`
	SeedHex                      string                             `json:"seed_hex,omitempty"`
	MethodDigest                 string                             `json:"method_digest"`
	DiscoveryDigest              string                             `json:"discovery_digest"`
	LocalIncrementalPSSStates    int                                `json:"local_incremental_pss_states"`
	LocalIncrementalPSSSetDigest string                             `json:"local_incremental_pss_set_digest"`
	CorpusNovelPSSStates         int                                `json:"corpus_novel_pss_states"`
	CorpusNovelPSSSetDigest      string                             `json:"corpus_novel_pss_set_digest"`
	SearchWork                   controlexperiment.StatelessDFSWork `json:"search_work"`
	QualifiedExecutionWork       controlexperiment.WorkLedger       `json:"qualified_execution_work"`
	MarginalEvidenceWorkUnits    int                                `json:"marginal_evidence_work_units"`
	EndToEndWorkUnitsIfRunAlone  int                                `json:"end_to_end_work_units_if_run_alone"`
	Roots                        []m523eMethodRootSummary           `json:"roots"`
}

type m523eSummary struct {
	SchemaVersion                       string                       `json:"schema_version"`
	Stage                               string                       `json:"stage"`
	Date                                string                       `json:"date"`
	CorpusDigest                        string                       `json:"corpus_digest"`
	SourceBundleDigest                  string                       `json:"source_bundle_digest"`
	SourceTraceDigest                   string                       `json:"source_trace_digest"`
	SharedSourceWork                    controlexperiment.WorkLedger `json:"shared_source_work"`
	SharedSourceWorkUnits               int                          `json:"shared_source_work_units"`
	RootCount                           int                          `json:"root_count"`
	MethodCount                         int                          `json:"method_count"`
	MaxDepth                            int                          `json:"max_depth"`
	MaxWorkItemsPerRoot                 int                          `json:"max_work_items_per_root"`
	MaxWorkUnitsPerRoot                 int                          `json:"max_work_units_per_root"`
	QualifiedExecutionAttemptsPerMethod int                          `json:"qualified_execution_attempts_per_method"`
	MinMarginalEvidenceWorkUnits        int                          `json:"min_marginal_evidence_work_units"`
	MaxMarginalEvidenceWorkUnits        int                          `json:"max_marginal_evidence_work_units"`
	CampaignEvidenceWorkUnits           int                          `json:"campaign_evidence_work_units"`
	CorpusBaselinePSSStates             int                          `json:"corpus_baseline_pss_states"`
	CorpusBaselinePSSSetDigest          string                       `json:"corpus_baseline_pss_set_digest"`
	UnionMethodCorpusNovelPSSStates     int                          `json:"union_method_corpus_novel_pss_states"`
	UnionMethodCorpusNovelPSSSetDigest  string                       `json:"union_method_corpus_novel_pss_set_digest"`
	RootSelectionUsesMethodOutput       bool                         `json:"root_selection_uses_method_output"`
	ReadOnlyProjection                  bool                         `json:"read_only_projection"`
	ModelCalls                          int                          `json:"model_calls"`
	Classification                      string                       `json:"classification"`
	Methods                             []m523eMethodSummary         `json:"methods"`
}
