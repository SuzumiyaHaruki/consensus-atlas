package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	etcdqualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/etcdraftv2"
)

const (
	etcdraftM523gStrategy = "workload-stateless-agent-m5.23g"
	m523gCorpusPath       = "benchmarks/experiments/etcdraft-v2-root-corpus-m5.23e/root-corpus.json"
	m523gBaselinePath     = "benchmarks/experiments/etcdraft-v2-root-corpus-m5.23e/summary.json"
)

type m523gBaselineMethod struct {
	ID                        string `json:"id"`
	Strategy                  string `json:"strategy"`
	MethodDigest              string `json:"method_digest"`
	CorpusNovelPSSStates      int    `json:"corpus_novel_pss_states"`
	CorpusNovelPSSSetDigest   string `json:"corpus_novel_pss_set_digest"`
	MarginalEvidenceWorkUnits int    `json:"marginal_evidence_work_units"`
}

type m523gFrozenBaseline struct {
	SchemaVersion              string                `json:"schema_version"`
	Stage                      string                `json:"stage"`
	CorpusDigest               string                `json:"corpus_digest"`
	SourceBundleDigest         string                `json:"source_bundle_digest"`
	SharedSourceWorkUnits      int                   `json:"shared_source_work_units"`
	RootCount                  int                   `json:"root_count"`
	MaxDepth                   int                   `json:"max_depth"`
	MaxWorkItemsPerRoot        int                   `json:"max_work_items_per_root"`
	MaxWorkUnitsPerRoot        int                   `json:"max_work_units_per_root"`
	CorpusBaselinePSSStates    int                   `json:"corpus_baseline_pss_states"`
	CorpusBaselinePSSSetDigest string                `json:"corpus_baseline_pss_set_digest"`
	Methods                    []m523gBaselineMethod `json:"methods"`
}

type m523gMethodComparison struct {
	ID                        string                      `json:"id"`
	Strategy                  string                      `json:"strategy"`
	EvidenceSource            string                      `json:"evidence_source"`
	MethodDigest              string                      `json:"method_digest"`
	CorpusNovelPSSStates      int                         `json:"corpus_novel_pss_states"`
	CorpusNovelPSSSetDigest   string                      `json:"corpus_novel_pss_set_digest"`
	MarginalEvidenceWorkUnits int                         `json:"marginal_evidence_work_units"`
	EndToEndWorkUnits         int                         `json:"end_to_end_work_units"`
	ModelWork                 controlexperiment.ModelWork `json:"model_work"`
}

type m523gRootSummary struct {
	Ordinal                    int    `json:"ordinal"`
	RootID                     string `json:"root_id"`
	RootDecisions              int    `json:"root_decisions"`
	SpecDigest                 string `json:"spec_digest"`
	AgentResultDigest          string `json:"agent_result_digest"`
	SearchDigest               string `json:"search_digest"`
	ActionOrderDigest          string `json:"action_order_digest"`
	PlannerInvocations         int    `json:"planner_invocations"`
	AcceptedProposals          int    `json:"accepted_proposals"`
	CompletedHistoryEntries    int    `json:"completed_history_entries"`
	CompletedHistoryDigest     string `json:"completed_history_digest"`
	RootDiscoveryDigest        string `json:"root_discovery_digest"`
	CorpusNovelPSSStates       int    `json:"corpus_novel_pss_states"`
	CorpusNovelPSSSetDigest    string `json:"corpus_novel_pss_set_digest"`
	SearchWorkUnits            int    `json:"search_work_units"`
	PrimaryWorkUnits           int    `json:"primary_work_units"`
	ReplayWorkUnits            int    `json:"replay_work_units"`
	QualifiedExecutionAttempts int    `json:"qualified_execution_attempts"`
}

type m523gSummary struct {
	SchemaVersion              string                      `json:"schema_version"`
	Stage                      string                      `json:"stage"`
	Date                       string                      `json:"date"`
	Provider                   string                      `json:"provider"`
	Model                      string                      `json:"model"`
	PromptVersion              string                      `json:"prompt_version"`
	MaxRetries                 int                         `json:"max_retries"`
	MaxModelCalls              int                         `json:"max_model_calls"`
	ActualModelCalls           int                         `json:"actual_model_calls"`
	AcceptedProposals          int                         `json:"accepted_proposals"`
	ModelWork                  controlexperiment.ModelWork `json:"model_work"`
	CorpusDigest               string                      `json:"corpus_digest"`
	SourceBundleDigest         string                      `json:"source_bundle_digest"`
	FrozenBaselineFileDigest   string                      `json:"frozen_baseline_file_digest"`
	KnowledgeDigest            string                      `json:"knowledge_digest"`
	MethodDigest               string                      `json:"method_digest"`
	AgentDiscoveryDigest       string                      `json:"agent_discovery_digest"`
	CorpusBaselinePSSStates    int                         `json:"corpus_baseline_pss_states"`
	CorpusBaselinePSSSetDigest string                      `json:"corpus_baseline_pss_set_digest"`
	RootCount                  int                         `json:"root_count"`
	Roots                      []m523gRootSummary          `json:"roots"`
	Methods                    []m523gMethodComparison     `json:"methods"`
	RootSelectionUsesAgent     bool                        `json:"root_selection_uses_agent"`
	ReadOnlyPSSProjection      bool                        `json:"read_only_pss_projection"`
	StrictReplayRequired       bool                        `json:"strict_replay_required"`
	Classification             string                      `json:"classification"`
	Digest                     string                      `json:"digest"`
}

func runEtcdraftM523gOptIn(
	ctx context.Context,
	keyFile string,
	directory string,
	stdout io.Writer,
) error {
	clean := filepath.Clean(directory)
	if keyFile == "" || directory == "" || clean == "." || clean == string(filepath.Separator) {
		return errors.New("M5_23G_OPTIONS_INVALID")
	}
	if _, err := os.Lstat(clean); err == nil || !os.IsNotExist(err) {
		return errors.New("M5_23G_OUTPUT_DIRECTORY_NOT_NEW")
	}
	// Finish all deterministic preflight work before reading a credential.
	_, source, err := etcdraftExecution(ctx, "workload-risk-witness-calibration", 64, 1, true)
	if err != nil {
		return err
	}
	var corpus controlexperiment.StatelessRootCorpus
	if err := readM523gJSON(m523gCorpusPath, 64<<10, &corpus, true); err != nil || corpus.Validate(source) != nil {
		return errors.New("M5_23G_FROZEN_CORPUS_INVALID")
	}
	baselineBytes, baseline, err := readM523gBaseline(m523gBaselinePath, corpus, source)
	if err != nil {
		return err
	}
	qualification, admission, workload, err := etcdraftQualifiedWorkload(ctx)
	if err != nil {
		return err
	}
	key, err := readAgentKey(keyFile)
	if err != nil {
		return err
	}
	journal, err := newStatelessAgentCallJournal(clean, defaultDeepSeekIntentClient(), key)
	key = ""
	if err != nil {
		return err
	}
	summary, agentResults, rootDiscoveries, discovery, runErr := executeEtcdraftM523g(
		ctx, source, corpus, baseline, controlexperiment.AgentInvocationDigest(baselineBytes),
		journal, qualification, admission, workload,
	)
	if runErr != nil {
		_ = writeStatelessAgentJSON(clean, "failure.json", struct {
			SchemaVersion string `json:"schema_version"`
			Stage         string `json:"stage"`
			ModelCalls    int    `json:"model_calls"`
			FailureCode   string `json:"failure_code"`
		}{"consensus-atlas/stateless-agent-pilot-failure/v1", "M5.23g", journal.Calls(), "pilot-terminal-failure"})
		return runErr
	}
	rootDirectory := filepath.Join(clean, "roots")
	if err := os.Mkdir(rootDirectory, 0o700); err != nil {
		return err
	}
	for index := range agentResults {
		rootID := corpus.Roots[index].ID
		if err := writeStatelessAgentJSON(rootDirectory, rootID+"-agent-result.json", agentResults[index]); err != nil {
			return err
		}
		if err := writeStatelessAgentJSON(rootDirectory, rootID+"-discovery.json", rootDiscoveries[index]); err != nil {
			return err
		}
	}
	if err := writeStatelessAgentJSON(clean, "agent-discovery.json", discovery); err != nil {
		return err
	}
	if err := writeStatelessAgentJSON(clean, "summary.json", summary); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s\nstatus=completed model_calls=%d corpus_novel_pss=%d summary=%s\n",
		clean, summary.ActualModelCalls, discovery.CorpusNovelPSSStates, summary.Digest)
	return nil
}

func executeEtcdraftM523g(
	ctx context.Context,
	source controlexperiment.ExecutionBundle,
	corpus controlexperiment.StatelessRootCorpus,
	baseline m523gFrozenBaseline,
	baselineFileDigest string,
	journal *statelessAgentCallJournal,
	qualification etcdqualification.Bundle,
	admission controlexperiment.ExecutionAdmission,
	workload controlexperiment.WorkloadPlan,
) (m523gSummary, []controlexperiment.StatelessAgentTraversalResult, []controlexperiment.StatelessTraversalDiscovery, controlexperiment.StatelessCorpusDiscovery, error) {
	if source.Validate() != nil || corpus.Validate(source) != nil || journal == nil ||
		qualification.Qualification.Validate() != nil || admission.Validate() != nil || workload.Validate() != nil {
		return m523gSummary{}, nil, nil, controlexperiment.StatelessCorpusDiscovery{},
			errors.New("M5_23G_EXECUTION_INPUT_INVALID")
	}
	knowledge, err := etcdraftStatelessAgentKnowledge()
	if err != nil {
		return m523gSummary{}, nil, nil, controlexperiment.StatelessCorpusDiscovery{}, err
	}
	method, err := controlexperiment.NewStatelessAgentTraversalMethod(
		"etcdraft-m5-23g-deepseek", "deepseek-v4-flash-frontier-order", knowledge,
	)
	if err != nil {
		return m523gSummary{}, nil, nil, controlexperiment.StatelessCorpusDiscovery{}, err
	}
	envelope := &controlexperiment.FaultEnvelope{
		MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
		MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	baselineSet := make(map[string]bool)
	maxRootDecisions := corpus.Roots[len(corpus.Roots)-1].Decisions
	if maxRootDecisions >= len(source.CorePSS) {
		return m523gSummary{}, nil, nil, controlexperiment.StatelessCorpusDiscovery{},
			errors.New("M5_23G_BASELINE_PSS_INPUT_INVALID")
	}
	for _, sample := range source.CorePSS[:maxRootDecisions+1] {
		baselineSet[sample.Key] = true
	}
	baselineKeys := make([]string, 0, len(baselineSet))
	for key := range baselineSet {
		baselineKeys = append(baselineKeys, key)
	}
	slices.Sort(baselineKeys)
	baselineDigest, err := control.CanonicalDigest(baselineKeys)
	if err != nil || len(baselineKeys) != baseline.CorpusBaselinePSSStates ||
		baselineDigest != baseline.CorpusBaselinePSSSetDigest {
		return m523gSummary{}, nil, nil, controlexperiment.StatelessCorpusDiscovery{},
			errors.New("M5_23G_BASELINE_PSS_DRIFT")
	}

	history := make([]controlexperiment.StatelessSearchHistoryEntry, 0, len(corpus.Roots))
	agentResults := make([]controlexperiment.StatelessAgentTraversalResult, 0, len(corpus.Roots))
	rootDiscoveries := make([]controlexperiment.StatelessTraversalDiscovery, 0, len(corpus.Roots))
	evidence := make([]controlexperiment.StatelessCorpusRootEvidence, 0, len(corpus.Roots))
	rootSummaries := make([]m523gRootSummary, 0, len(corpus.Roots))
	var modelWork controlexperiment.ModelWork
	accepted := 0
	const discoveryID = "etcdraft-m5-23g-agent"
	for rootIndex, entry := range corpus.Roots {
		root, prefixErr := corpus.Prefix(source, entry.ID)
		if prefixErr != nil {
			return m523gSummary{}, nil, nil, controlexperiment.StatelessCorpusDiscovery{}, prefixErr
		}
		spec, specErr := controlexperiment.NewStatelessDFSSpec(
			fmt.Sprintf("etcdraft-m5-23e-root-%d", rootIndex+1), root,
			etcdraftCampaignRuntimeConfig(), envelope, baseline.MaxDepth,
			baseline.MaxWorkItemsPerRoot, baseline.MaxWorkUnitsPerRoot,
		)
		if specErr != nil {
			return m523gSummary{}, nil, nil, controlexperiment.StatelessCorpusDiscovery{}, specErr
		}
		historyInput := append([]controlexperiment.StatelessSearchHistoryEntry(nil), history...)
		historyDigest, digestErr := control.CanonicalDigest(historyInput)
		if digestErr != nil || journal.SetRoot(entry.ID) != nil {
			return m523gSummary{}, nil, nil, controlexperiment.StatelessCorpusDiscovery{},
				errors.New("M5_23G_HISTORY_INPUT_INVALID")
		}
		agent, exploreErr := controlexperiment.ExploreBoundedStatelessDFSWithAgent(
			ctx, method, knowledge, historyInput, journal.Planner, spec, root, factory,
		)
		if exploreErr != nil {
			return m523gSummary{}, nil, nil, controlexperiment.StatelessCorpusDiscovery{}, exploreErr
		}
		if len(agent.Traversal.Search.Items) != spec.MaxWorkItems ||
			agent.Traversal.Search.StopReason != controlexperiment.StatelessDFSStopItems ||
			agent.PlannerInvocations != agent.AcceptedProposals || agent.ModelWork.Calls <= 0 {
			return m523gSummary{}, nil, nil, controlexperiment.StatelessCorpusDiscovery{},
				errors.New("M5_23G_AGENT_RESULT_INVALID")
		}
		bundles := make([]controlexperiment.ExecutionBundle, 0, len(agent.Traversal.Search.Items))
		for _, item := range agent.Traversal.Search.Items {
			executionID := fmt.Sprintf(
				"etcdraft-m5-23g-root-%d-item-%d", rootIndex+1, item.Ordinal,
			)
			_, bundle, executionErr := executeEtcdraftStatelessPrefix(
				ctx, executionID, agent.Traversal, root, item, envelope,
				qualification, admission, workload,
			)
			if executionErr != nil {
				return m523gSummary{}, nil, nil, controlexperiment.StatelessCorpusDiscovery{}, executionErr
			}
			bundles = append(bundles, bundle)
		}
		rootDiscovery, discoveryErr := controlexperiment.NewStatelessTraversalDiscovery(
			discoveryID+"-"+entry.ID, agent.Traversal, root, bundles,
			etcdraftv2.CorePSSMapper{},
		)
		if discoveryErr != nil {
			return m523gSummary{}, nil, nil, controlexperiment.StatelessCorpusDiscovery{}, discoveryErr
		}
		rootNovel, rootNovelDigest, novelErr := sortedNovelM523g(
			rootDiscovery.IncrementalPSSKeys, baselineSet,
		)
		if novelErr != nil {
			return m523gSummary{}, nil, nil, controlexperiment.StatelessCorpusDiscovery{}, novelErr
		}
		history = append(history, controlexperiment.StatelessSearchHistoryEntry{
			Ordinal: len(history) + 1, RootID: entry.ID,
			CorpusNovelPSSStates: len(rootNovel), CorpusNovelPSSSetDigest: rootNovelDigest,
			DiscoveryDigest: rootDiscovery.Digest,
		})
		actionOrderDigest, digestErr := control.CanonicalDigest(m523gActionOrder(agent.Traversal.Search))
		if digestErr != nil {
			return m523gSummary{}, nil, nil, controlexperiment.StatelessCorpusDiscovery{}, digestErr
		}
		rootSummaries = append(rootSummaries, m523gRootSummary{
			Ordinal: rootIndex + 1, RootID: entry.ID, RootDecisions: entry.Decisions,
			SpecDigest: spec.Digest, AgentResultDigest: agent.Digest,
			SearchDigest: agent.Traversal.Search.Digest, ActionOrderDigest: actionOrderDigest,
			PlannerInvocations: agent.PlannerInvocations, AcceptedProposals: agent.AcceptedProposals,
			CompletedHistoryEntries: len(historyInput), CompletedHistoryDigest: historyDigest,
			RootDiscoveryDigest:  rootDiscovery.Digest,
			CorpusNovelPSSStates: len(rootNovel), CorpusNovelPSSSetDigest: rootNovelDigest,
			SearchWorkUnits:            agent.Traversal.Search.Work.TotalWorkUnits,
			PrimaryWorkUnits:           rootDiscovery.QualifiedExecutionWork.Primary.WorkUnits,
			ReplayWorkUnits:            rootDiscovery.QualifiedExecutionWork.Replay.WorkUnits,
			QualifiedExecutionAttempts: rootDiscovery.QualifiedExecutionAttempts,
		})
		modelWork.Calls += agent.ModelWork.Calls
		modelWork.InputTokens += agent.ModelWork.InputTokens
		modelWork.OutputTokens += agent.ModelWork.OutputTokens
		modelWork.TotalTokens += agent.ModelWork.TotalTokens
		accepted += agent.AcceptedProposals
		agentResults = append(agentResults, agent)
		rootDiscoveries = append(rootDiscoveries, rootDiscovery)
		evidence = append(evidence, controlexperiment.StatelessCorpusRootEvidence{
			RootID: entry.ID, Result: agent.Traversal, Bundles: bundles,
		})
	}
	discovery, err := controlexperiment.NewStatelessCorpusDiscovery(
		discoveryID, corpus, source, evidence, etcdraftv2.CorePSSMapper{},
	)
	if err != nil || discovery.Validate(corpus, source, evidence, etcdraftv2.CorePSSMapper{}) != nil ||
		discovery.CorpusBaselinePSSStates != baseline.CorpusBaselinePSSStates ||
		discovery.CorpusBaselinePSSSetDigest != baseline.CorpusBaselinePSSSetDigest ||
		journal.Calls() != modelWork.Calls || accepted != modelWork.Calls ||
		journal.Calls() != statelessAgentMaxCalls {
		return m523gSummary{}, nil, nil, controlexperiment.StatelessCorpusDiscovery{},
			errors.New("M5_23G_CORPUS_DISCOVERY_INVALID")
	}
	methods := make([]m523gMethodComparison, 0, len(baseline.Methods)+1)
	for _, method := range baseline.Methods {
		methods = append(methods, m523gMethodComparison{
			ID: method.ID, Strategy: method.Strategy, EvidenceSource: "frozen-m5.23e",
			MethodDigest:              method.MethodDigest,
			CorpusNovelPSSStates:      method.CorpusNovelPSSStates,
			CorpusNovelPSSSetDigest:   method.CorpusNovelPSSSetDigest,
			MarginalEvidenceWorkUnits: method.MarginalEvidenceWorkUnits,
			EndToEndWorkUnits:         baseline.SharedSourceWorkUnits + method.MarginalEvidenceWorkUnits,
			ModelWork:                 controlexperiment.ModelWork{},
		})
	}
	methods = append(methods, m523gMethodComparison{
		ID: method.ID, Strategy: method.Strategy, EvidenceSource: "live-m5.23g",
		MethodDigest:              method.Digest,
		CorpusNovelPSSStates:      discovery.CorpusNovelPSSStates,
		CorpusNovelPSSSetDigest:   discovery.CorpusNovelPSSSetDigest,
		MarginalEvidenceWorkUnits: discovery.MarginalEvidenceWorkUnits,
		EndToEndWorkUnits:         baseline.SharedSourceWorkUnits + discovery.MarginalEvidenceWorkUnits,
		ModelWork:                 modelWork,
	})
	summary := m523gSummary{
		SchemaVersion: "consensus-atlas/stateless-agent-multi-root-pilot-summary/v1",
		Stage:         "M5.23g", Date: "2026-08-12", Provider: deepSeekProvider, Model: deepSeekV4Flash,
		PromptVersion: statelessAgentPromptVersion, MaxRetries: 0,
		MaxModelCalls: statelessAgentMaxCalls, ActualModelCalls: journal.Calls(),
		AcceptedProposals: accepted, ModelWork: modelWork,
		CorpusDigest: corpus.Digest, SourceBundleDigest: source.Digest,
		FrozenBaselineFileDigest: baselineFileDigest, KnowledgeDigest: knowledge.Digest,
		MethodDigest: method.Digest, AgentDiscoveryDigest: discovery.Digest,
		CorpusBaselinePSSStates:    discovery.CorpusBaselinePSSStates,
		CorpusBaselinePSSSetDigest: discovery.CorpusBaselinePSSSetDigest,
		RootCount:                  len(corpus.Roots), Roots: rootSummaries, Methods: methods,
		RootSelectionUsesAgent: false, ReadOnlyPSSProjection: true, StrictReplayRequired: true,
		Classification: "bounded-single-agent-pilot-not-general-agent-superiority-coverage-completeness-defect-or-significance",
	}
	summary, err = summary.seal()
	return summary, agentResults, rootDiscoveries, discovery, err
}

func m523gActionOrder(result controlexperiment.StatelessDFSResult) []string {
	order := make([]string, 0, len(result.Items))
	for _, item := range result.Items {
		order = append(order, string(item.Action.Kind)+":"+string(item.Action.ActionID))
	}
	return order
}

func etcdraftStatelessAgentKnowledge() (controlexperiment.ProtocolKnowledgePack, error) {
	return controlexperiment.NewProtocolKnowledgePack(controlexperiment.ProtocolKnowledgePack{
		ID: "etcdraft-search-knowledge-m5-23f", Family: "raft", Protocol: "etcdraft",
		Knowledge: []controlexperiment.KnowledgeStatement{
			{ID: "current-frontier-only", Text: "Prioritize only Action IDs supplied by the current trusted frontier."},
			{ID: "natural-progress", Text: "Leadership and progress arise from controlled peer delivery and natural temporal progress."},
		},
		Risks: []controlexperiment.ProtocolRisk{{
			ID: "leader-change-progress", Summary: "Explore ordering around leader change and workload progress.",
			RequiredCapabilities: []string{"runtime-owned-message-control", "strict-replay"},
			RequiredActions: []control.ActionKind{
				control.ActionDeliverMessage, control.ActionFireTemporal, control.ActionInvoke,
			},
			AllowedBackendIDs: []string{"validated-frontier-order"},
		}},
	})
}

func readM523gBaseline(
	path string,
	corpus controlexperiment.StatelessRootCorpus,
	source controlexperiment.ExecutionBundle,
) ([]byte, m523gFrozenBaseline, error) {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 || len(data) > 256<<10 {
		return nil, m523gFrozenBaseline{}, errors.New("M5_23G_BASELINE_FILE_INVALID")
	}
	var baseline m523gFrozenBaseline
	if json.Unmarshal(data, &baseline) != nil || baseline.SchemaVersion != "consensus-atlas/stateless-root-corpus-calibration-summary/v1" ||
		baseline.Stage != "M5.23e" || baseline.CorpusDigest != corpus.Digest ||
		baseline.SourceBundleDigest != source.Digest || baseline.RootCount != len(corpus.Roots) ||
		baseline.RootCount != 3 || baseline.MaxDepth != 2 || baseline.MaxWorkItemsPerRoot != 6 ||
		baseline.MaxWorkUnitsPerRoot != 1500 || baseline.SharedSourceWorkUnits <= 0 ||
		baseline.CorpusBaselinePSSStates <= 0 || len(baseline.Methods) != 4 {
		return nil, m523gFrozenBaseline{}, errors.New("M5_23G_BASELINE_BINDING_INVALID")
	}
	wantMethods := []string{
		"etcdraft-m5-23c-canonical", "etcdraft-m5-23c-uniform-seed-1",
		"etcdraft-m5-23c-uniform-seed-2", "etcdraft-m5-23c-uniform-seed-3",
	}
	for index, method := range baseline.Methods {
		if method.ID != wantMethods[index] || method.Strategy == "" || method.MethodDigest == "" ||
			method.CorpusNovelPSSStates < 0 || method.CorpusNovelPSSSetDigest == "" ||
			method.MarginalEvidenceWorkUnits <= 0 {
			return nil, m523gFrozenBaseline{}, errors.New("M5_23G_BASELINE_METHOD_INVALID")
		}
	}
	return data, baseline, nil
}

func readM523gJSON(path string, limit int64, target any, strict bool) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > limit {
		return errors.New("M5_23G_JSON_FILE_INVALID")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, limit+1))
	if strict {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("M5_23G_JSON_TRAILING_DATA")
	}
	return nil
}

func (summary m523gSummary) seal() (m523gSummary, error) {
	summary.Roots = append([]m523gRootSummary(nil), summary.Roots...)
	summary.Methods = append([]m523gMethodComparison(nil), summary.Methods...)
	summary.Digest = ""
	digest, err := control.CanonicalDigest(summary)
	summary.Digest = digest
	return summary, err
}

func sortedNovelM523g(values []string, baseline map[string]bool) ([]string, string, error) {
	set := make(map[string]bool)
	for _, value := range values {
		if !baseline[value] {
			set[value] = true
		}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	digest, err := control.CanonicalDigest(keys)
	return keys, digest, err
}
