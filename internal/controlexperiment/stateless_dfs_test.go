package controlexperiment

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

type closeCountingAdapter struct {
	control.Adapter
	closed *int
}

func (adapter *closeCountingAdapter) Close() error {
	*adapter.closed = *adapter.closed + 1
	return nil
}

func TestStatelessDFSClosesEveryTemporaryAdapter(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "613768312d616461707465722d6c6966656379636c65", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	spec, err := NewStatelessDFSSpec(
		"fixture-a7h1-adapter-lifecycle", root, runtimeConfig,
		&FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1}, 2, 5, 400,
	)
	if err != nil {
		t.Fatal(err)
	}
	opened := 0
	closed := 0
	factory := func() (control.Adapter, error) {
		opened++
		return &closeCountingAdapter{Adapter: fixture.New(), closed: &closed}, nil
	}
	if _, err := ExploreBoundedStatelessDFS(ctx, spec, root, factory); err != nil {
		t.Fatal(err)
	}
	if opened == 0 || closed != opened {
		t.Fatalf("temporary adapters: opened=%d closed=%d", opened, closed)
	}
}

func TestSearchKernelCanonicalCompatibility(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "61312d7365617263682d73706c6974", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	spec, err := NewStatelessDFSSpec(
		"fixture-a1-search-split", root, runtimeConfig,
		&FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1}, 2, 5, 400,
	)
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	legacy, err := ExploreBoundedStatelessDFS(ctx, spec, root, factory)
	if err != nil {
		t.Fatal(err)
	}
	algorithm := NewBoundedStatelessDFSAlgorithm()
	separated, err := algorithm.Explore(
		ctx, spec, root, factory, NewCanonicalStatelessGuidancePolicy(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if algorithm.ID() != StatelessSearchBoundedDepthFirst || !reflect.DeepEqual(legacy, separated) {
		t.Fatalf("search/guidance split changed canonical behavior: legacy=%#v separated=%#v", legacy, separated)
	}
	leaf := legacy.Items[len(legacy.Items)-1]
	legacyPolicy, err := CompileStatelessDFSPath(
		"fixture-a1-legacy-path", legacy, root, leaf.Ordinal,
		[]control.ActionKind{control.ActionFireTemporal}, leaf.Path.Decision,
	)
	if err != nil {
		t.Fatal(err)
	}
	separatedPolicy, err := CompileStatelessDFSPath(
		"fixture-a1-separated-path", separated, root, leaf.Ordinal,
		[]control.ActionKind{control.ActionFireTemporal}, leaf.Path.Decision,
	)
	if err != nil {
		t.Fatal(err)
	}
	legacyPolicy.ID = separatedPolicy.ID
	if !reflect.DeepEqual(legacyPolicy, separatedPolicy) {
		t.Fatalf("search split changed compiled exact path: legacy=%#v separated=%#v", legacyPolicy, separatedPolicy)
	}
}

func TestSearchKernelRejectsGuidanceAuthorityExpansion(t *testing.T) {
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "61312d67756964616e63652d626f756e64617279", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	spec, err := NewStatelessDFSSpec(
		"fixture-a1-guidance-boundary", root, runtimeConfig,
		&FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1}, 1, 5, 400,
	)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]StatelessGuidancePolicyFunc{
		"typed-nil": nil,
		"missing": func(view ActionFrontierView) ([]FrontierActionRef, error) {
			return append([]FrontierActionRef(nil), view.Actions[:len(view.Actions)-1]...), nil
		},
		"duplicate": func(view ActionFrontierView) ([]FrontierActionRef, error) {
			ordered := append([]FrontierActionRef(nil), view.Actions...)
			ordered[len(ordered)-1] = ordered[0]
			return ordered, nil
		},
		"invented": func(view ActionFrontierView) ([]FrontierActionRef, error) {
			ordered := append([]FrontierActionRef(nil), view.Actions...)
			ordered[0].ActionID = "invented-action"
			return ordered, nil
		},
		"modified": func(view ActionFrontierView) ([]FrontierActionRef, error) {
			ordered := append([]FrontierActionRef(nil), view.Actions...)
			ordered[0].EffectKind = "invented-effect"
			return ordered, nil
		},
	}
	for name, guidance := range tests {
		t.Run(name, func(t *testing.T) {
			_, exploreErr := NewBoundedStatelessDFSAlgorithm().Explore(
				ctx, spec, root,
				func() (control.Adapter, error) { return fixture.New(), nil }, guidance,
			)
			var failure *StatelessDFSExecutionError
			if !errors.As(exploreErr, &failure) ||
				failure.Work.FrontierReconstruction.WorkUnits == 0 ||
				failure.Work.TotalWorkUnits != failure.Work.FrontierReconstruction.WorkUnits {
				t.Fatalf("guidance authority expansion was not rejected with charged work: %v", exploreErr)
			}
		})
	}
}

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
	// Captured from the pre-A1 implementation at branch point 0106e2c. The
	// search seam must not alter persisted DFS identity for this fixed fixture.
	const preA1Digest = "98749847e4f85748eecad2af2709838b7e976d39a0844f7dc0939893eeef3991"
	if first.Digest != preA1Digest {
		t.Fatalf("A1 search seam changed the frozen pre-A1 result digest: got=%s want=%s", first.Digest, preA1Digest)
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
	defer runtime.Close()
	trace, err := runtime.Trace()
	if err != nil {
		t.Fatal(err)
	}
	return trace
}
