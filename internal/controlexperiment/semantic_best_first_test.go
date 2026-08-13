package controlexperiment

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

type fixtureSemanticPrefixProjector struct {
	preferred control.ActionID
}

func (fixtureSemanticPrefixProjector) ID() string { return "fixture-semantic-prefix-projector-v1" }

func (projector fixtureSemanticPrefixProjector) Project(
	id string,
	spec semantic.RiskWitnessSpec,
	trace controlruntime.Trace,
) (semantic.RiskWitnessResult, error) {
	milestones := make([]semantic.RiskWitnessMilestoneEvidence, 0, 1)
	for _, record := range trace.Records {
		if record.Action.ID != projector.preferred {
			continue
		}
		digest, err := control.CanonicalDigest(record)
		if err != nil {
			return semantic.RiskWitnessResult{}, err
		}
		milestones = append(milestones, semantic.RiskWitnessMilestoneEvidence{
			MilestoneID: "preferred-prefix", Step: record.Step,
			Kind: "trace-action", EvidenceDigest: digest,
		})
		break
	}
	return semantic.NewRiskWitnessResult(
		id, spec, trace.ManifestDigest, trace.Digest, projector.ID(), milestones,
	)
}

type actionKindSemanticProjector struct{}

func (actionKindSemanticProjector) ID() string { return "fixture-action-kind-projector-v1" }

func (projector actionKindSemanticProjector) Project(
	id string,
	spec semantic.RiskWitnessSpec,
	trace controlruntime.Trace,
) (semantic.RiskWitnessResult, error) {
	seen := make(map[string]bool)
	milestones := make([]semantic.RiskWitnessMilestoneEvidence, 0, 2)
	for _, record := range trace.Records {
		milestoneID := ""
		switch record.Action.Kind {
		case control.ActionFireTemporal:
			milestoneID = "temporal-prefix"
		case control.ActionCrash:
			milestoneID = "crash-prefix"
		}
		if milestoneID == "" || seen[milestoneID] {
			continue
		}
		digest, err := control.CanonicalDigest(record)
		if err != nil {
			return semantic.RiskWitnessResult{}, err
		}
		seen[milestoneID] = true
		milestones = append(milestones, semantic.RiskWitnessMilestoneEvidence{
			MilestoneID: milestoneID, Step: record.Step,
			Kind: "trace-action", EvidenceDigest: digest,
		})
	}
	return semantic.NewRiskWitnessResult(
		id, spec, trace.ManifestDigest, trace.Digest, projector.ID(), milestones,
	)
}

type recordingSemanticGuidance struct {
	views []SemanticQueueView
}

func (*recordingSemanticGuidance) ID() string { return SemanticBestFirstGuidanceID }

func (guidance *recordingSemanticGuidance) Order(ctx context.Context, view SemanticQueueView) ([]string, error) {
	guidance.views = append(guidance.views, view)
	return NewDeterministicSemanticBestFirstGuidance().Order(ctx, view)
}

type recordingCandidateIDGuidance struct {
	views []SemanticQueueView
}

func (*recordingCandidateIDGuidance) ID() string { return "candidate-id-lexicographic-v1" }

func (guidance *recordingCandidateIDGuidance) Order(
	_ context.Context,
	view SemanticQueueView,
) ([]string, error) {
	if err := view.Validate(); err != nil {
		return nil, err
	}
	guidance.views = append(guidance.views, view)
	ordered := make([]string, len(view.Candidates))
	for index, candidate := range view.Candidates {
		ordered[index] = candidate.CandidateID
	}
	return ordered, nil
}

type invalidSemanticGuidance struct {
	mode string
}

func (invalidSemanticGuidance) ID() string { return "fixture-invalid-semantic-guidance-v1" }

func (guidance invalidSemanticGuidance) Order(_ context.Context, view SemanticQueueView) ([]string, error) {
	ordered := make([]string, len(view.Candidates))
	for index, candidate := range view.Candidates {
		ordered[index] = candidate.CandidateID
	}
	switch guidance.mode {
	case "missing":
		return ordered[:len(ordered)-1], nil
	case "duplicate":
		ordered[len(ordered)-1] = ordered[0]
	case "invented":
		ordered[0] = "semantic-candidate-999999"
	}
	return ordered, nil
}

func TestSemanticBestFirstLazilyChangesExpansionAndTrace(t *testing.T) {
	ctx, runtimeConfig, root, frontier := semanticBestFirstFixtureRoot(t)
	preferred := frontier.Actions[len(frontier.Actions)-1].ActionID
	riskSpec := semanticBestFirstRiskSpec(t)
	searchSpec, err := NewStatelessDFSSpec(
		"fixture-a2a-semantic-search", root, runtimeConfig,
		&FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1},
		2, len(frontier.Actions)+1, 4000,
	)
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	projector := fixtureSemanticPrefixProjector{preferred: preferred}
	guidance := &recordingSemanticGuidance{}
	first, err := ExploreBoundedSemanticBestFirst(
		ctx, searchSpec, root, riskSpec, factory, projector, guidance,
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExploreBoundedSemanticBestFirst(
		ctx, searchSpec, root, riskSpec, factory, projector,
		NewDeterministicSemanticBestFirstGuidance(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || first.AlgorithmID != SemanticBestFirstAlgorithmID ||
		first.GuidanceID != SemanticBestFirstGuidanceID || first.Search.StopReason != StatelessDFSStopItems ||
		len(first.Search.Items) != searchSpec.MaxWorkItems || len(first.ExpansionOrder) != 1 {
		t.Fatalf("semantic best-first is not deterministic and bounded: %#v", first)
	}
	preferredOrdinal := 0
	for index, item := range first.Search.Items[:len(frontier.Actions)] {
		if item.Action.ActionID == preferred {
			preferredOrdinal = index + 1
			break
		}
	}
	if preferredOrdinal == 0 || first.ExpansionOrder[0] != semanticCandidateID(preferredOrdinal) ||
		first.Search.Items[len(first.Search.Items)-1].Path.ParentOrdinal != preferredOrdinal {
		t.Fatalf("semantic target did not determine the first global expansion: preferred=%d result=%#v",
			preferredOrdinal, first)
	}
	if len(guidance.views) != 1 || len(guidance.views[0].Candidates) != len(frontier.Actions) {
		t.Fatalf("search precomputed more than the root queue before guidance: %#v", guidance.views)
	}
	preferredView := semanticQueueCandidateByID(t, guidance.views[0], semanticCandidateID(preferredOrdinal))
	if !preferredView.TargetUnreached || len(preferredView.SatisfiedMilestones) != 1 ||
		preferredView.SatisfiedMilestones[0] != "preferred-prefix" ||
		preferredView.FirstMissingMilestone != "confirmed-prefix" || preferredView.SemanticBucketVisits != 0 {
		t.Fatalf("candidate semantics were not projected from the exact prefix: %#v", preferredView)
	}
	candidateIDGuidance := &recordingCandidateIDGuidance{}
	candidateIDResult, err := ExploreBoundedSemanticBestFirst(
		ctx, searchSpec, root, riskSpec, factory, projector, candidateIDGuidance,
	)
	if err != nil {
		t.Fatal(err)
	}
	if candidateIDResult.AlgorithmID != first.AlgorithmID ||
		candidateIDResult.GuidanceID == first.GuidanceID || len(candidateIDGuidance.views) != 1 ||
		candidateIDGuidance.views[0].Digest != guidance.views[0].Digest ||
		candidateIDResult.ExpansionOrder[0] != semanticCandidateID(1) ||
		candidateIDResult.Search.Items[len(candidateIDResult.Search.Items)-1].ChildPrefixDigest ==
			first.Search.Items[len(first.Search.Items)-1].ChildPrefixDigest {
		t.Fatalf("same semantic candidate set did not produce a guidance-caused trace delta: semantic=%#v candidate-id=%#v",
			first, candidateIDResult)
	}

	canonical, err := ExploreBoundedStatelessDFS(ctx, searchSpec, root, factory)
	if err != nil {
		t.Fatal(err)
	}
	canonicalChild := StatelessDFSWorkItem{}
	for _, item := range canonical.Items {
		if item.Path.Depth == 2 {
			canonicalChild = item
			break
		}
	}
	semanticChild := first.Search.Items[len(first.Search.Items)-1]
	if canonicalChild.Ordinal == 0 || canonicalChild.Path.ParentOrdinal != 1 ||
		semanticChild.Path.ParentOrdinal != preferredOrdinal ||
		canonicalChild.ChildPrefixDigest == semanticChild.ChildPrefixDigest {
		t.Fatalf("semantic and depth-first searches did not produce a behavior delta: dfs=%#v semantic=%#v",
			canonical.Items, first.Search.Items)
	}
	if err := first.ValidateSources(
		ctx, root, riskSpec, factory, projector, NewDeterministicSemanticBestFirstGuidance(),
	); err != nil {
		t.Fatalf("semantic result is not independently source-verifiable: %v", err)
	}
	var persisted SemanticBestFirstResult
	roundTripJSON(t, first, &persisted)
	if err := persisted.ValidateSources(
		ctx, root, riskSpec, factory, projector, NewDeterministicSemanticBestFirstGuidance(),
	); err != nil {
		t.Fatalf("persisted semantic result lost source verification: %v", err)
	}
	assertSemanticQueueViewAllowlist(t, guidance.views[0])
}

func TestSemanticBestFirstRejectsGuidanceAndProjectionAuthorityExpansion(t *testing.T) {
	ctx, runtimeConfig, root, frontier := semanticBestFirstFixtureRoot(t)
	riskSpec := semanticBestFirstRiskSpec(t)
	searchSpec, err := NewStatelessDFSSpec(
		"fixture-a2a-authority", root, runtimeConfig,
		&FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1},
		2, len(frontier.Actions)+1, 4000,
	)
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	projector := fixtureSemanticPrefixProjector{preferred: frontier.Actions[len(frontier.Actions)-1].ActionID}
	var nilGuidance *recordingSemanticGuidance
	if _, exploreErr := ExploreBoundedSemanticBestFirst(
		ctx, searchSpec, root, riskSpec, factory, projector, nilGuidance,
	); exploreErr == nil {
		t.Fatal("typed-nil semantic guidance was accepted")
	}
	var nilProjector *fixtureSemanticPrefixProjector
	if _, exploreErr := ExploreBoundedSemanticBestFirst(
		ctx, searchSpec, root, riskSpec, factory, nilProjector,
		NewDeterministicSemanticBestFirstGuidance(),
	); exploreErr == nil {
		t.Fatal("typed-nil semantic projector was accepted")
	}
	for _, mode := range []string{"missing", "duplicate", "invented"} {
		t.Run(mode, func(t *testing.T) {
			_, exploreErr := ExploreBoundedSemanticBestFirst(
				ctx, searchSpec, root, riskSpec, factory, projector, invalidSemanticGuidance{mode: mode},
			)
			var failure *StatelessDFSExecutionError
			if !errors.As(exploreErr, &failure) || failure.Work.ChildMaterialization.WorkUnits == 0 {
				t.Fatalf("invalid global guidance was not rejected with charged work: %v", exploreErr)
			}
		})
	}

	result, err := ExploreBoundedSemanticBestFirst(
		ctx, searchSpec, root, riskSpec, factory, projector,
		NewDeterministicSemanticBestFirstGuidance(),
	)
	if err != nil {
		t.Fatal(err)
	}
	differentProjector := fixtureSemanticPrefixProjector{preferred: frontier.Actions[0].ActionID}
	if err := result.ValidateSources(
		ctx, root, riskSpec, factory, differentProjector, NewDeterministicSemanticBestFirstGuidance(),
	); err == nil {
		t.Fatal("result detached from the trusted semantic projector was accepted")
	}
	tampered := result
	tampered.ExpansionOrder = []string{semanticCandidateID(1)}
	tampered, err = tampered.seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := tampered.Validate(root, riskSpec); err == nil {
		t.Fatal("resealed semantic expansion order detached from the executed tree was accepted")
	}
}

func TestSemanticBestFirstVisitAccountingDiversifiesEqualProgress(t *testing.T) {
	ctx, runtimeConfig, root, frontier := semanticBestFirstFixtureRoot(t)
	riskSpec, err := semantic.NewRiskWitnessSpec(
		"fixture-a2a-bucket-risk", "fixture-cft", "semantic-bucket-risk",
		[]string{"temporal-prefix", "crash-prefix"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	searchSpec, err := NewStatelessDFSSpec(
		"fixture-a2a-bucket-search", root, runtimeConfig,
		&FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1},
		2, len(frontier.Actions)*3, 12000,
	)
	if err != nil {
		t.Fatal(err)
	}
	factory := func() (control.Adapter, error) { return fixture.New(), nil }
	projector := actionKindSemanticProjector{}
	guidance := &recordingSemanticGuidance{}
	result, err := ExploreBoundedSemanticBestFirst(
		ctx, searchSpec, root, riskSpec, factory, projector, guidance,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(guidance.views) < 2 || len(result.ExpansionOrder) < 2 {
		t.Fatalf("fixture did not exercise a second global queue decision: %#v", result)
	}
	firstOrder, err := NewDeterministicSemanticBestFirstGuidance().Order(ctx, guidance.views[0])
	if err != nil {
		t.Fatal(err)
	}
	secondOrder, err := NewDeterministicSemanticBestFirstGuidance().Order(ctx, guidance.views[1])
	if err != nil {
		t.Fatal(err)
	}
	first := semanticQueueCandidateByID(t, guidance.views[0], firstOrder[0])
	second := semanticQueueCandidateByID(t, guidance.views[1], secondOrder[0])
	visitedBucket := SemanticQueueCandidate{}
	for _, candidate := range guidance.views[1].Candidates {
		if reflect.DeepEqual(candidate.SatisfiedMilestones, first.SatisfiedMilestones) {
			visitedBucket = candidate
			break
		}
	}
	if visitedBucket.CandidateID == "" || visitedBucket.SemanticBucketVisits != 1 ||
		second.SemanticBucketVisits != 0 ||
		len(first.SatisfiedMilestones) != len(second.SatisfiedMilestones) ||
		reflect.DeepEqual(first.SatisfiedMilestones, second.SatisfiedMilestones) ||
		result.ExpansionOrder[0] != firstOrder[0] || result.ExpansionOrder[1] != secondOrder[0] {
		t.Fatalf("visit accounting did not diversify equal-progress buckets: first=%#v visited=%#v second=%#v order=%#v",
			first, visitedBucket, second, result.ExpansionOrder)
	}
}

func TestSemanticQueueRepresentsOrderViolationWithoutInventingMissingMilestone(t *testing.T) {
	_, _, root, frontier := semanticBestFirstFixtureRoot(t)
	spec := semanticBestFirstRiskSpec(t)
	beforeDigest, err := control.CanonicalDigest("before-evidence")
	if err != nil {
		t.Fatal(err)
	}
	afterDigest, err := control.CanonicalDigest("after-evidence")
	if err != nil {
		t.Fatal(err)
	}
	risk, err := semantic.NewRiskWitnessResult(
		"fixture-a2a-order-violation", spec, root.ManifestDigest, root.Digest,
		"fixture-order-projector-v1", []semantic.RiskWitnessMilestoneEvidence{
			{MilestoneID: "preferred-prefix", Step: 2, Kind: "fixture-evidence", EvidenceDigest: beforeDigest},
			{MilestoneID: "confirmed-prefix", Step: 1, Kind: "fixture-evidence", EvidenceDigest: afterDigest},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	view, err := newSemanticQueueView("fixture-order-view", spec, []semanticQueueEntry{{
		item: StatelessDFSWorkItem{
			Path:   StatelessDFSPathMetadata{Decision: 2},
			Action: frontier.Actions[0],
		},
		candidate: SemanticCandidateRecord{CandidateID: semanticCandidateID(1), RiskResult: risk},
	}}, map[string]int{})
	if err != nil {
		t.Fatal(err)
	}
	candidate := view.Candidates[0]
	if !candidate.TargetUnreached || candidate.FirstMissingMilestone != "" ||
		len(candidate.SatisfiedMilestones) != 1 || len(risk.OrderViolations) != 1 {
		t.Fatalf("order-only near miss was misrepresented as a missing milestone: %#v", candidate)
	}
}

func semanticBestFirstFixtureRoot(
	t *testing.T,
) (context.Context, RuntimeConfig, controlruntime.Trace, ActionFrontierView) {
	t.Helper()
	ctx := context.Background()
	runtimeConfig := RuntimeConfig{SeedHex: "6132612d73656d616e7469632d626573742d6669727374", MaxClones: 1}
	root := fixtureInitialTrace(t, ctx, runtimeConfig)
	frontier, _, err := ReconstructActionFrontierView(
		ctx, "fixture-a2a-root-frontier", root, len(root.Records), runtimeConfig,
		&FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1},
		func() (control.Adapter, error) { return fixture.New(), nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(frontier.Actions) < 2 {
		t.Fatalf("fixture requires a multi-candidate root frontier: %#v", frontier)
	}
	return ctx, runtimeConfig, root, frontier
}

func semanticBestFirstRiskSpec(t *testing.T) semantic.RiskWitnessSpec {
	t.Helper()
	spec, err := semantic.NewRiskWitnessSpec(
		"fixture-a2a-risk", "fixture-cft", "semantic-prefix-risk",
		[]string{"preferred-prefix", "confirmed-prefix"},
		[]semantic.RiskWitnessOrder{{Before: "preferred-prefix", After: "confirmed-prefix"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func semanticQueueCandidateByID(
	t *testing.T,
	view SemanticQueueView,
	id string,
) SemanticQueueCandidate {
	t.Helper()
	for _, candidate := range view.Candidates {
		if candidate.CandidateID == id {
			return candidate
		}
	}
	t.Fatalf("semantic candidate %q not found", id)
	return SemanticQueueCandidate{}
}

func assertSemanticQueueViewAllowlist(t *testing.T, view SemanticQueueView) {
	t.Helper()
	assertJSONObjectKeys(t, view, map[string]bool{
		"schema_version": true, "id": true, "risk_spec_digest": true,
		"candidates": true, "digest": true,
	})
	assertJSONObjectKeys(t, view.Candidates[0], map[string]bool{
		"candidate_id": true, "target_unreached": true, "satisfied_milestones": true,
		"first_missing_milestone": true, "semantic_bucket_visits": true,
		"prefix_decisions": true, "action_kind": true,
	})
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range [][]byte{
		[]byte("action_id"), []byte("work_item_digest"), []byte("evidence_digest"),
		[]byte("target_identity"), []byte("oracle"), []byte("verdict"),
	} {
		if jsonContains(encoded, forbidden) {
			t.Fatalf("semantic planner view leaked forbidden field %q", forbidden)
		}
	}
}

func jsonContains(data []byte, pattern []byte) bool {
	for index := 0; index+len(pattern) <= len(data); index++ {
		if reflect.DeepEqual(data[index:index+len(pattern)], pattern) {
			return true
		}
	}
	return false
}
