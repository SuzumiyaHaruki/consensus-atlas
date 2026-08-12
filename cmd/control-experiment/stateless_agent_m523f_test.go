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

const m523fExperimentDirectory = "../../benchmarks/experiments/etcdraft-v2-restricted-search-agent-m5.23f"

func TestM523fRestrictedSearchAgentCanOnlyReorderTrustedEtcdraftFrontiers(t *testing.T) {
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
		"etcdraft-m5-23f", root, etcdraftCampaignRuntimeConfig(), envelope, 2, 6, 1000,
	)
	if err != nil {
		t.Fatal(err)
	}
	knowledge := m523fKnowledge(t)
	method, err := controlexperiment.NewStatelessAgentTraversalMethod(
		"etcdraft-m5-23f-reverse", "no-model-reverse-frontier", knowledge,
	)
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	canonical, err := controlexperiment.ExploreBoundedStatelessDFS(ctx, spec, root, factory)
	if err != nil {
		t.Fatal(err)
	}
	planner := func(
		_ context.Context,
		view controlexperiment.StatelessSearchAgentView,
	) ([]byte, controlexperiment.ModelWork, error) {
		actionIDs := make([]control.ActionID, 0, len(view.Request.Frontier.Actions))
		for index := len(view.Request.Frontier.Actions) - 1; index >= 0; index-- {
			actionIDs = append(actionIDs, view.Request.Frontier.Actions[index].ActionID)
		}
		wire := controlexperiment.StatelessFrontierOrderProposal{
			SchemaVersion: controlexperiment.StatelessFrontierOrderProposalVersion,
			ID:            view.Request.ID, RequestDigest: view.Request.Digest,
			ViewDigest: view.Request.Frontier.Digest, ActionIDs: actionIDs,
		}
		encoded, marshalErr := json.Marshal(wire)
		return encoded, controlexperiment.ModelWork{}, marshalErr
	}
	agent, err := controlexperiment.ExploreBoundedStatelessDFSWithAgent(
		ctx, method, knowledge, nil, planner, spec, root, factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := controlexperiment.ExploreBoundedStatelessDFSWithAgent(
		ctx, method, knowledge, nil, planner, spec, root, factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(agent, repeated) || agent.Validate(root, knowledge) != nil ||
		agent.PlannerInvocations != agent.AcceptedProposals || agent.ModelWork.Calls != 0 ||
		len(agent.Traversal.Search.Items) != spec.MaxWorkItems ||
		actionOrderM523c(agent.Traversal.Search) == actionOrderM523c(canonical) {
		t.Fatalf("restricted planner did not produce deterministic effective order: %#v", agent)
	}
	qualification, admission, workload, err := etcdraftQualifiedWorkload(ctx)
	if err != nil {
		t.Fatal(err)
	}
	leaf := agent.Traversal.Search.Items[len(agent.Traversal.Search.Items)-1]
	report, bundle := executeEtcdraftDiscoveryPrefix(
		t, ctx, "etcdraft-m5-23f-leaf", 1, agent.Traversal, root, leaf, envelope,
		qualification, admission, workload,
	)
	executedPrefix, err := controlexperiment.ExecutionTracePrefix(bundle.Trace, leaf.Path.Decision)
	if err != nil {
		t.Fatal(err)
	}
	if executedPrefix.Digest != leaf.ChildPrefixDigest || !report.Runs[0].Replay.Stable {
		t.Fatalf("restricted Agent leaf did not return through qualified execution: %#v", report.Runs[0])
	}
	recordDigests := make([]string, 0, len(agent.Records))
	for _, record := range agent.Records {
		recordDigests = append(recordDigests, record.Digest)
	}
	recordSetDigest, err := control.CanonicalDigest(recordDigests)
	if err != nil {
		t.Fatal(err)
	}
	summary := m523fSummary{
		SchemaVersion: "consensus-atlas/restricted-search-agent-calibration-summary/v1",
		Stage:         "M5.23f", Date: "2026-08-12", SourceBundleDigest: source.Digest,
		RootPrefixDigest: root.Digest, RootDecisions: len(root.Records),
		SpecDigest: spec.Digest, KnowledgeDigest: knowledge.Digest, MethodDigest: method.Digest,
		AgentTraversalDigest: agent.Digest, SearchDigest: agent.Traversal.Search.Digest,
		CanonicalActionOrderDigest: mustCanonicalDigestM523f(t, actionOrderSliceM523c(canonical)),
		AgentActionOrderDigest:     mustCanonicalDigestM523f(t, actionOrderSliceM523c(agent.Traversal.Search)),
		DistinctFromCanonical:      actionOrderM523c(agent.Traversal.Search) != actionOrderM523c(canonical),
		DeterministicRepeat:        reflect.DeepEqual(agent, repeated),
		PlannerInvocations:         agent.PlannerInvocations, AcceptedProposals: agent.AcceptedProposals,
		RecordSetDigest: recordSetDigest, MutableFields: []string{"action_ids"},
		RejectedAuthorityExpansions: []string{
			"additional-action", "duplicate-action", "invented-action", "missing-action", "unknown-json-field",
		},
		SearchWork: agent.Traversal.Search.Work, ModelWork: agent.ModelWork,
		LeafOrdinal: leaf.Ordinal, LeafPrefixDigest: leaf.ChildPrefixDigest,
		LeafReportDigest: report.Digest, LeafBundleDigest: bundle.Digest,
		LeafReplayStable: report.Runs[0].Replay.Stable,
		ModelCalls:       0, ProposalSource: "deterministic-no-model-reverse-frontier-fixture",
		Classification: "restricted-frontier-order-authority-calibration-not-agent-effectiveness-coverage-or-defect-result",
	}
	checked := readM521hJSONFile[m523fSummary](
		t, m523fExperimentDirectory+"/summary.json", 64<<10,
	)
	if !reflect.DeepEqual(checked, summary) {
		encoded, marshalErr := json.MarshalIndent(summary, "", "  ")
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		t.Fatalf("checked-in M5.23f summary drifted:\n%s", encoded)
	}
}

func m523fKnowledge(t *testing.T) controlexperiment.ProtocolKnowledgePack {
	t.Helper()
	pack, err := controlexperiment.NewProtocolKnowledgePack(controlexperiment.ProtocolKnowledgePack{
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
	if err != nil {
		t.Fatal(err)
	}
	return pack
}

func mustCanonicalDigestM523f(t *testing.T, value any) string {
	t.Helper()
	digest, err := control.CanonicalDigest(value)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

type m523fSummary struct {
	SchemaVersion               string                             `json:"schema_version"`
	Stage                       string                             `json:"stage"`
	Date                        string                             `json:"date"`
	SourceBundleDigest          string                             `json:"source_bundle_digest"`
	RootPrefixDigest            string                             `json:"root_prefix_digest"`
	RootDecisions               int                                `json:"root_decisions"`
	SpecDigest                  string                             `json:"spec_digest"`
	KnowledgeDigest             string                             `json:"knowledge_digest"`
	MethodDigest                string                             `json:"method_digest"`
	AgentTraversalDigest        string                             `json:"agent_traversal_digest"`
	SearchDigest                string                             `json:"search_digest"`
	CanonicalActionOrderDigest  string                             `json:"canonical_action_order_digest"`
	AgentActionOrderDigest      string                             `json:"agent_action_order_digest"`
	DistinctFromCanonical       bool                               `json:"distinct_from_canonical"`
	DeterministicRepeat         bool                               `json:"deterministic_repeat"`
	PlannerInvocations          int                                `json:"planner_invocations"`
	AcceptedProposals           int                                `json:"accepted_proposals"`
	RecordSetDigest             string                             `json:"record_set_digest"`
	MutableFields               []string                           `json:"mutable_fields"`
	RejectedAuthorityExpansions []string                           `json:"rejected_authority_expansions"`
	SearchWork                  controlexperiment.StatelessDFSWork `json:"search_work"`
	ModelWork                   controlexperiment.ModelWork        `json:"model_work"`
	LeafOrdinal                 int                                `json:"leaf_ordinal"`
	LeafPrefixDigest            string                             `json:"leaf_prefix_digest"`
	LeafReportDigest            string                             `json:"leaf_report_digest"`
	LeafBundleDigest            string                             `json:"leaf_bundle_digest"`
	LeafReplayStable            bool                               `json:"leaf_replay_stable"`
	ModelCalls                  int                                `json:"model_calls"`
	ProposalSource              string                             `json:"proposal_source"`
	Classification              string                             `json:"classification"`
}
