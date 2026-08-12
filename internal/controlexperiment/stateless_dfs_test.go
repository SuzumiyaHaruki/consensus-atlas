package controlexperiment

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

func TestBoundedStatelessDFSReconstructsCanonicalExactPrefixWorkItems(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "73746174656c6573732d646673", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	envelope := &FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1}
	spec, err := NewStatelessDFSSpec(
		"fixture-bounded-dfs", root, runtimeConfig, envelope, 2, 5, 400,
	)
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	first, err := ExploreBoundedStatelessDFS(ctx, spec, root, factory)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExploreBoundedStatelessDFS(ctx, spec, root, factory)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || first.Digest != second.Digest ||
		len(first.Items) != 5 || first.StopReason != StatelessDFSStopItems ||
		first.Work.TotalWorkUnits <= 0 || first.StatesExpanded <= 0 {
		t.Fatalf("bounded DFS is not deterministic and budgeted: first=%#v second=%#v", first, second)
	}
	for index, item := range first.Items {
		if !item.ReplayStable || item.Ordinal != index+1 || item.Action.ActionID == "" ||
			item.ChildPrefixDigest == item.State.PrefixTraceDigest {
			t.Fatalf("invalid exact-prefix WorkItem: %#v", item)
		}
	}
	leaf := first.Items[len(first.Items)-1]
	policy, err := CompileStatelessDFSPath(
		"fixture-dfs-leaf", first, root, leaf.Ordinal,
		[]control.ActionKind{control.ActionFireTemporal}, len(root.Records)+spec.MaxDepth,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Rules) != len(root.Records)+leaf.Path.Depth ||
		policy.Rules[len(policy.Rules)-1].ActionID != leaf.Action.ActionID {
		t.Fatalf("compiled path did not preserve exact WorkItem chain: %#v", policy)
	}
}

func TestStatelessAgentOrderAcceptsOnlyCompleteFrozenFrontierPermutation(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "73746174656c6573732d6167656e74", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	spec, err := NewStatelessDFSSpec(
		"fixture-agent-order", root, runtimeConfig,
		&FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1}, 2, 5, 400,
	)
	if err != nil {
		t.Fatal(err)
	}
	knowledge := fixtureStatelessAgentKnowledge(t)
	method, err := NewStatelessAgentTraversalMethod(
		"fixture-agent-order", "fixture-reverse-frontier", knowledge,
	)
	if err != nil {
		t.Fatal(err)
	}
	planner := func(
		_ context.Context,
		view StatelessSearchAgentView,
	) ([]byte, ModelWork, error) {
		actionIDs := make([]control.ActionID, 0, len(view.Request.Frontier.Actions))
		for index := len(view.Request.Frontier.Actions) - 1; index >= 0; index-- {
			actionIDs = append(actionIDs, view.Request.Frontier.Actions[index].ActionID)
		}
		return fixtureStatelessAgentProposalJSON(t, view.Request, actionIDs), ModelWork{}, nil
	}
	result, err := ExploreBoundedStatelessDFSWithAgent(
		ctx, method, knowledge, nil, planner, spec, root,
		func() (control.Adapter, error) { return fixture.New(), nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Validate(root, knowledge) != nil || result.PlannerInvocations == 0 ||
		result.PlannerInvocations != result.AcceptedProposals || result.ModelWork.Calls != 0 {
		t.Fatalf("restricted no-model planner was not durably bound: %#v", result)
	}
	request := result.Records[0].Request
	valid := append([]control.ActionID(nil), result.Records[0].Proposal.ActionIDs...)
	rejected := [][]control.ActionID{
		valid[:len(valid)-1],
		append(append([]control.ActionID(nil), valid...), valid[0]),
		append([]control.ActionID{valid[0]}, valid...),
		append([]control.ActionID{"invented-action"}, valid[1:]...),
	}
	for index, actionIDs := range rejected {
		wire := fixtureStatelessAgentProposalJSON(t, request, actionIDs)
		proposal, parseErr := ParseStatelessFrontierOrderProposal(wire)
		if parseErr == nil {
			_, parseErr = ValidateStatelessFrontierOrderProposal(request, proposal)
		}
		if parseErr == nil {
			t.Fatalf("authority expansion %d was accepted: %v", index, actionIDs)
		}
	}
	unknownField := append(
		fixtureStatelessAgentProposalJSON(t, request, valid)[:len(fixtureStatelessAgentProposalJSON(t, request, valid))-1],
		[]byte(`,"explanation":"trust me"}`)...,
	)
	if _, err := ParseStatelessFrontierOrderProposal(unknownField); err == nil {
		t.Fatal("unknown proposal field was accepted")
	}
	tampered := result
	tampered.Records = append([]StatelessFrontierOrderRecord(nil), result.Records...)
	tampered.Records[0].Proposal.ActionIDs = append(
		[]control.ActionID(nil), result.Records[0].Proposal.ActionIDs...,
	)
	if len(tampered.Records[0].Proposal.ActionIDs) < 2 {
		t.Fatal("fixture frontier cannot exercise order tamper")
	}
	tampered.Records[0].Proposal.ActionIDs[0], tampered.Records[0].Proposal.ActionIDs[1] =
		tampered.Records[0].Proposal.ActionIDs[1], tampered.Records[0].Proposal.ActionIDs[0]
	tampered.Records[0].Proposal, err = tampered.Records[0].Proposal.seal()
	if err != nil {
		t.Fatal(err)
	}
	tampered.Records[0], err = tampered.Records[0].seal()
	if err != nil {
		t.Fatal(err)
	}
	tampered, err = tampered.seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := tampered.Validate(root, knowledge); err == nil {
		t.Fatal("WorkItem order detached from the accepted proposal was accepted")
	}
}

func fixtureStatelessAgentKnowledge(t *testing.T) ProtocolKnowledgePack {
	t.Helper()
	pack, err := NewProtocolKnowledgePack(ProtocolKnowledgePack{
		ID: "fixture-stateless-agent-knowledge", Family: "fixture-cft", Protocol: "fixture",
		Knowledge: []KnowledgeStatement{{
			ID: "order-only", Text: "The planner may prioritize only Action IDs already present in the current frontier.",
		}},
		Risks: []ProtocolRisk{{
			ID: "natural-progress", Summary: "Explore deterministic natural progress.",
			RequiredCapabilities: []string{"fixture-control"},
			RequiredActions:      []control.ActionKind{control.ActionFireTemporal},
			AllowedBackendIDs:    []string{"validated-frontier-order"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return pack
}

func fixtureStatelessAgentProposalJSON(
	t *testing.T,
	request StatelessFrontierOrderRequest,
	actionIDs []control.ActionID,
) []byte {
	t.Helper()
	wire := StatelessFrontierOrderProposal{
		SchemaVersion: StatelessFrontierOrderProposalVersion, ID: request.ID,
		RequestDigest: request.Digest, ViewDigest: request.Frontier.Digest,
		ActionIDs: actionIDs,
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestBoundedStatelessDFSStopsBeforeExceedingWorkBudgetAndRejectsTamper(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "73746174656c6573732d627564676574", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	spec, err := NewStatelessDFSSpec(
		"fixture-work-bounded-dfs", root, runtimeConfig,
		&FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1}, 3, 100, 5,
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ExploreBoundedStatelessDFS(
		ctx, spec, root, func() (control.Adapter, error) { return fixture.New(), nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != StatelessDFSStopWork || result.Work.TotalWorkUnits > spec.MaxWorkUnits {
		t.Fatalf("work budget was exceeded: %#v", result)
	}

	fullSpec, err := NewStatelessDFSSpec(
		"fixture-tamper-dfs", root, runtimeConfig,
		&FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1}, 1, 1, 100,
	)
	if err != nil {
		t.Fatal(err)
	}
	full, err := ExploreBoundedStatelessDFS(
		ctx, fullSpec, root, func() (control.Adapter, error) { return fixture.New(), nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	tampered := full
	tampered.Items = append([]StatelessDFSWorkItem(nil), full.Items...)
	tampered.Items[0].Action.ActionID = "invented-action"
	if err := tampered.Validate(root); err == nil {
		t.Fatal("tampered ActionRef was accepted")
	}
	tampered = full
	tampered.Work.ChildVerification.WorkUnits++
	if err := tampered.Validate(root); err == nil {
		t.Fatal("tampered search work ledger was accepted")
	}
}

func TestStatelessTraversalMethodsShareExecutorBoundsAndBindOrder(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "73746174656c6573732d6d6574686f64", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	spec, err := NewStatelessDFSSpec(
		"fixture-traversal-method", root, runtimeConfig,
		&FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1}, 2, 5, 400,
	)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := NewStatelessTraversalMethod(
		"fixture-canonical", StatelessTraversalCanonical, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	uniform, err := NewStatelessTraversalMethod(
		"fixture-seeded-uniform", StatelessTraversalSeededUniform, "01",
	)
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	legacy, err := ExploreBoundedStatelessDFS(ctx, spec, root, factory)
	if err != nil {
		t.Fatal(err)
	}
	ordered, err := ExploreBoundedStatelessDFSWithMethod(ctx, canonical, spec, root, factory)
	if err != nil {
		t.Fatal(err)
	}
	randomized, err := ExploreBoundedStatelessDFSWithMethod(ctx, uniform, spec, root, factory)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := ExploreBoundedStatelessDFSWithMethod(ctx, uniform, spec, root, factory)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(legacy, ordered.Search) || !reflect.DeepEqual(randomized, repeated) ||
		ordered.Search.Work != randomized.Search.Work ||
		ordered.Search.StatesExpanded != randomized.Search.StatesExpanded ||
		len(ordered.Search.Items) != len(randomized.Search.Items) ||
		ordered.Digest == randomized.Digest {
		t.Fatalf("traversal methods were not deterministic and matched: ordered=%#v randomized=%#v", ordered, randomized)
	}
	if reflect.DeepEqual(ordered.Search.Items, randomized.Search.Items) {
		t.Fatal("seeded uniform did not change the bounded traversal order")
	}
	if ordered.Validate(root) != nil || randomized.Validate(root) != nil {
		t.Fatal("valid traversal result was rejected")
	}
	tampered := randomized
	tampered.Method.SeedHex = "02"
	if err := tampered.Validate(root); err == nil {
		t.Fatal("tampered traversal method identity was accepted")
	}
}

func fixtureInitialTrace(t *testing.T, ctx context.Context, config RuntimeConfig) controlruntime.Trace {
	t.Helper()
	runtimeConfig, err := config.runtimeConfig()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := controlruntime.New(ctx, fixture.New(), runtimeConfig)
	if err != nil {
		t.Fatal(err)
	}
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	return trace
}
