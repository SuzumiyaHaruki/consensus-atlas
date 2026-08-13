package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const m523gCorpusPath = "benchmarks/experiments/etcdraft-v2-root-corpus-m5.23e/root-corpus.json"

// The M5.23g types below are read-only compatibility structures for checking
// the frozen public pilot. Its live orchestration was retired after R4b moved
// the same restricted Agent into the common durable Stateless Campaign.
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
