package main

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const m523gExperimentDirectory = "../../benchmarks/experiments/etcdraft-v2-stateless-agent-m5.23g"

func TestM523gRealAgentPilotIsDurableBoundedAndComparedToCanonical(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	_, source, err := etcdraftExecution(ctx, "workload-risk-witness-calibration", 64, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	corpus := readM521hJSONFile[controlexperiment.StatelessRootCorpus](
		t, "../../"+m523gCorpusPath, 64<<10,
	)
	if corpus.Validate(source) != nil {
		t.Fatal("M5.23g corpus no longer binds the frozen source")
	}
	summary := readM521hJSONFile[m523gSummary](t, m523gExperimentDirectory+"/summary.json", 64<<10)
	wantSummary, err := summary.seal()
	if err != nil || !reflect.DeepEqual(summary, wantSummary) || summary.Stage != "M5.23g" ||
		summary.ActualModelCalls != statelessAgentMaxCalls || summary.AcceptedProposals != statelessAgentMaxCalls ||
		summary.ModelWork.Calls != statelessAgentMaxCalls || summary.ModelWork.TotalTokens != 28335 ||
		summary.AgentDiscoveryDigest == "" || summary.RootSelectionUsesAgent ||
		!summary.ReadOnlyPSSProjection || !summary.StrictReplayRequired {
		t.Fatalf("M5.23g summary is not the frozen bounded pilot: %#v/%v", summary, err)
	}
	knowledge, err := etcdraftStatelessAgentKnowledge()
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	callOrdinal := 0
	distinctRoots := 0
	for rootIndex, entry := range corpus.Roots {
		root, err := corpus.Prefix(source, entry.ID)
		if err != nil {
			t.Fatal(err)
		}
		agent := readM521hJSONFile[controlexperiment.StatelessAgentTraversalResult](
			t, fmt.Sprintf("%s/roots/%s-agent-result.json", m523gExperimentDirectory, entry.ID), 128<<10,
		)
		if agent.Validate(root, knowledge) != nil || summary.Roots[rootIndex].AgentResultDigest != agent.Digest {
			t.Fatalf("root %s Agent result is not bound", entry.ID)
		}
		canonical, err := controlexperiment.ExploreBoundedStatelessDFS(
			ctx, agent.Traversal.Search.Spec, root, factory,
		)
		if err != nil {
			t.Fatal(err)
		}
		canonicalOrder, err := control.CanonicalDigest(m523gActionOrder(canonical))
		if err != nil {
			t.Fatal(err)
		}
		if summary.Roots[rootIndex].ActionOrderDigest != canonicalOrder {
			distinctRoots++
		}
		for _, record := range agent.Records {
			callOrdinal++
			callDirectory := fmt.Sprintf(
				"%s/model-calls/%03d-%s", m523gExperimentDirectory, callOrdinal, entry.ID,
			)
			intent := readM521hJSONFile[controlexperiment.StatelessAgentCallIntent](
				t, callDirectory+"/intent.json", 128<<10,
			)
			dispatch := readM521hJSONFile[controlexperiment.StatelessAgentCallDispatch](
				t, callDirectory+"/dispatch.json", 16<<10,
			)
			result := readM521hJSONFile[controlexperiment.StatelessAgentCallResult](
				t, callDirectory+"/result.json", 32<<10,
			)
			if intent.ValidateRequest(record.Request) != nil || dispatch.ValidateIntent(intent) != nil ||
				result.ValidateInputs(intent, dispatch) != nil ||
				result.Status != controlexperiment.StatelessAgentCallCompleted ||
				result.ProposalDigest != record.Proposal.Digest || result.Work != record.ModelWork {
				t.Fatalf("call %d is not durably bound to root %s", callOrdinal, entry.ID)
			}
		}
	}
	if callOrdinal != statelessAgentMaxCalls || distinctRoots != 0 ||
		summary.Methods[len(summary.Methods)-1].CorpusNovelPSSStates != 15 ||
		summary.Methods[len(summary.Methods)-1].CorpusNovelPSSSetDigest != summary.Methods[0].CorpusNovelPSSSetDigest {
		t.Fatalf("unexpected M5.23g comparison: calls=%d distinct_roots=%d methods=%s",
			callOrdinal, distinctRoots, mustJSONM523g(t, summary.Methods))
	}
}

func mustJSONM523g(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
