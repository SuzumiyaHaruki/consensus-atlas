package controlexperiment

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestStatelessCampaignProviderRunsMatchedMethodsThroughDurableCampaign(t *testing.T) {
	canonical, err := NewStatelessTraversalMethod(
		"fixture-canonical-repeat", StatelessTraversalCanonical, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	methods := []StatelessTraversalMethod{canonical, canonical}
	sourceWork := campaignTestWork(2, 2, 0, 0)
	discovery := fixtureStatelessCampaignDiscovery(t, canonical)
	spec, err := NewStatelessCampaignSpec(StatelessCampaignSpec{
		ID: "fixture-stateless-campaign", TargetID: "fixture-target",
		TargetIdentityDigest: strings.Repeat("a", 64), Methods: methods,
		CorpusDigest: discovery.CorpusDigest, SourceBundleDigest: strings.Repeat("b", 64),
		SourceManifestDigest: strings.Repeat("c", 64), SourceWork: sourceWork,
		RootCount: 1, MaxDepth: 1, MaxWorkItemsPerRoot: 1, MaxSearchWorkUnitsPerRoot: 10,
		Budget: StatelessCampaignAttemptBudget{
			MaxPrimarySchedulerDecisions: 10, MaxPrimaryWorkUnits: 20, MaxReplayWorkUnits: 20,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	config, err := NewStatelessCampaignConfig("fixture-search-campaign", spec, 1_000)
	if err != nil {
		t.Fatal(err)
	}
	if config.ExperimentSpecDigest != spec.Digest || config.Budget.MaxAttempts != len(methods) ||
		config.AttemptInputMode != CampaignInputBaseRequest || config.PlannerMode != CampaignPlannerNone {
		t.Fatalf("search Campaign retained legacy planner authority: %#v", config)
	}
	calls := 0
	provider, err := NewStatelessCampaignAttemptProvider(
		spec,
		func(_ context.Context, request CampaignAttemptRequest) (StatelessCampaignExecution, error) {
			calls++
			if request.Ordinal != calls || spec.Methods[request.Ordinal-1].Digest != canonical.Digest {
				t.Fatalf("attempt method sequence drifted: %#v", request)
			}
			copy := discovery
			return StatelessCampaignExecution{
				Discovery: &copy, SearchWork: copy.SearchWork,
				QualifiedExecutionWork: copy.QualifiedExecutionWork,
			}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "search-campaign")
	recovered, err := CreateCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := newCampaignCoordinator(
		&recovered, provider, newCampaignTestClock(0, 0, 10).Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	first, err := coordinator.Step(context.Background())
	if err != nil || first.Sequence != 1 {
		t.Fatalf("first search attempt failed: %#v/%v", first, err)
	}
	resumed, err := RecoverCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err = newCampaignCoordinator(
		&resumed, provider, newCampaignTestClock(0, 0, 10).Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	head, err := coordinator.Run(context.Background())
	if err != nil || head.Sequence != 2 || head.StopReason != CampaignStopAttemptLimit || calls != 2 {
		t.Fatalf("resumed search Campaign failed: %#v calls=%d err=%v", head, calls, err)
	}
	checked, err := RecoverCampaignDirectory(directory, config)
	if err != nil {
		t.Fatal(err)
	}
	for ordinal := 1; ordinal <= 2; ordinal++ {
		encoded, readErr := checked.ReadAttemptArtifact(ordinal)
		if readErr != nil {
			t.Fatal(readErr)
		}
		request, requestErr := NewCampaignAttemptRequest(config, checked.Checkpoints[ordinal-1])
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		artifact, decodeErr := DecodeStatelessCampaignAttemptArtifact(encoded, request)
		if decodeErr != nil || artifact.Method.Digest != methods[ordinal-1].Digest ||
			artifact.Discovery == nil || artifact.Discovery.Digest != discovery.Digest {
			t.Fatalf("attempt %d artifact drifted: %#v/%v", ordinal, artifact, decodeErr)
		}
	}
	wantAttemptWork := statelessCampaignWork(sourceWork, discovery.SearchWork, discovery.QualifiedExecutionWork, ModelWork{})
	wantTotal := addWorkLedgers(wantAttemptWork, wantAttemptWork)
	if head.Totals != wantTotal {
		t.Fatalf("source/search/execution cost was not charged per attempt: got=%#v want=%#v", head.Totals, wantTotal)
	}
}

func TestStatelessCampaignRejectsMethodDriftUnknownJSONAndInventedDiscovery(t *testing.T) {
	canonical, err := NewStatelessTraversalMethod("fixture-canonical", StatelessTraversalCanonical, "")
	if err != nil {
		t.Fatal(err)
	}
	uniform, err := NewStatelessTraversalMethod("fixture-uniform", StatelessTraversalSeededUniform, "01")
	if err != nil {
		t.Fatal(err)
	}
	discovery := fixtureStatelessCampaignDiscovery(t, canonical)
	spec := fixtureStatelessCampaignSpec(t, []StatelessTraversalMethod{canonical}, discovery)
	config, err := NewStatelessCampaignConfig("fixture-tamper-campaign", spec, 1_000)
	if err != nil {
		t.Fatal(err)
	}
	head, err := NewCampaignCheckpoint(config)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewCampaignAttemptRequest(config, head)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := NewStatelessCampaignAttemptArtifact(request, spec, StatelessCampaignExecution{
		Discovery: &discovery, SearchWork: discovery.SearchWork,
		QualifiedExecutionWork: discovery.QualifiedExecutionWork,
	})
	if err != nil {
		t.Fatal(err)
	}
	tampered := artifact
	tampered.Method = uniform
	tampered, err = tampered.seal()
	if err != nil {
		t.Fatal(err)
	}
	if tampered.ValidateInputs(request) == nil {
		t.Fatal("attempt method drift was accepted")
	}
	encoded, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded[:len(encoded)-1], []byte(`,"invented_verdict":"pass"}`)...)
	if _, err := DecodeStatelessCampaignAttemptArtifact(encoded, request); err == nil {
		t.Fatal("unknown evaluator field was accepted")
	}
	invented := discovery
	invented.CorpusNovelPSSStates++
	if _, err := NewStatelessCampaignAttemptArtifact(request, spec, StatelessCampaignExecution{
		Discovery: &invented, SearchWork: invented.SearchWork,
		QualifiedExecutionWork: invented.QualifiedExecutionWork,
	}); err == nil {
		t.Fatal("unsealed discovery claim was accepted")
	}
	smuggled := discovery
	smuggled.QualifiedExecutionWork.Model = ModelWork{
		Calls: 1, InputTokens: 1, TotalTokens: 1,
	}
	if _, err := NewStatelessCampaignAttemptArtifact(request, spec, StatelessCampaignExecution{
		Discovery: &smuggled, SearchWork: smuggled.SearchWork,
		QualifiedExecutionWork: smuggled.QualifiedExecutionWork,
	}); err == nil {
		t.Fatal("qualified execution smuggled model work")
	}
}

func fixtureStatelessCampaignSpec(
	t *testing.T,
	methods []StatelessTraversalMethod,
	discovery StatelessCorpusDiscovery,
) StatelessCampaignSpec {
	t.Helper()
	spec, err := NewStatelessCampaignSpec(StatelessCampaignSpec{
		ID: "fixture-stateless-spec", TargetID: "fixture-target",
		TargetIdentityDigest: strings.Repeat("a", 64), Methods: methods,
		CorpusDigest: discovery.CorpusDigest, SourceBundleDigest: strings.Repeat("b", 64),
		SourceManifestDigest: strings.Repeat("c", 64), SourceWork: campaignTestWork(2, 2, 0, 0),
		RootCount: 1, MaxDepth: 1, MaxWorkItemsPerRoot: 1, MaxSearchWorkUnitsPerRoot: 10,
		Budget: StatelessCampaignAttemptBudget{
			MaxPrimarySchedulerDecisions: 10, MaxPrimaryWorkUnits: 20, MaxReplayWorkUnits: 20,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func fixtureStatelessCampaignDiscovery(
	t *testing.T,
	method StatelessTraversalMethod,
) StatelessCorpusDiscovery {
	t.Helper()
	search := StatelessDFSWork{
		FrontierReconstruction: PhaseWork{SetupAttempts: 1, RuntimeInitializations: 1, WorkUnits: 1},
		TotalWorkUnits:         1,
	}
	qualified := campaignTestWork(1, 1, 0, 0)
	root, err := (StatelessCorpusRootResult{
		SchemaVersion: StatelessCorpusRootResultVersion, RootID: "fixture-root",
		RootPrefixDigest: strings.Repeat("d", 64), SearchDigest: strings.Repeat("e", 64),
		DiscoveryDigest: strings.Repeat("f", 64), RootPSSStates: 1,
		LocalIncrementalPSSStates: 1, SearchWork: search, QualifiedExecutionWork: qualified,
	}).seal()
	if err != nil {
		t.Fatal(err)
	}
	baseline := []string{"base-state"}
	local := []string{"new-state"}
	baselineDigest, _ := control.CanonicalDigest(baseline)
	localDigest, _ := control.CanonicalDigest(local)
	discovery, err := (StatelessCorpusDiscovery{
		SchemaVersion: StatelessCorpusDiscoveryVersion, ID: "fixture-corpus-discovery",
		CorpusDigest: strings.Repeat("1", 64), MethodDigest: method.Digest, Roots: []StatelessCorpusRootResult{root},
		CorpusBaselinePSSStates: 1, CorpusBaselinePSSKeys: baseline,
		CorpusBaselinePSSSetDigest: baselineDigest,
		LocalIncrementalPSSStates:  1, LocalIncrementalPSSKeys: local,
		LocalIncrementalPSSSetDigest: localDigest,
		CorpusNovelPSSStates:         1, CorpusNovelPSSKeys: local, CorpusNovelPSSSetDigest: localDigest,
		SearchWork: search, QualifiedExecutionWork: qualified, QualifiedExecutionAttempts: 1,
		MarginalEvidenceWorkUnits: search.TotalWorkUnits + qualified.Primary.WorkUnits + qualified.Replay.WorkUnits,
	}).seal()
	if err != nil || discovery.ValidateStructure() != nil {
		t.Fatalf("invalid discovery fixture: %#v/%v", discovery, err)
	}
	return discovery
}

func TestStatelessCampaignSpecBindsPredeclaredUniformSequence(t *testing.T) {
	first, _ := NewStatelessTraversalMethod("uniform-one", StatelessTraversalSeededUniform, "01")
	second, _ := NewStatelessTraversalMethod("uniform-two", StatelessTraversalSeededUniform, "02")
	discovery := fixtureStatelessCampaignDiscovery(t, first)
	spec := fixtureStatelessCampaignSpec(t, []StatelessTraversalMethod{first, second}, discovery)
	if !reflect.DeepEqual(spec.Methods, []StatelessTraversalMethod{first, second}) {
		t.Fatal("predeclared method sequence drifted")
	}
	mixed := spec
	canonical, _ := NewStatelessTraversalMethod("canonical", StatelessTraversalCanonical, "")
	mixed.Methods[1] = canonical
	mixed, _ = mixed.seal()
	if mixed.Validate() == nil {
		t.Fatal("mixed-strategy Campaign was accepted")
	}
}
