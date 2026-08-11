package controlexperiment

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

func TestPolicyUsesExactRuleBeforeFallbackPriority(t *testing.T) {
	policy := Policy{
		Version: PolicyVersion, ID: "lifecycle",
		Rules:    []DecisionRule{{Decision: 1, Kind: control.ActionCrash, Node: "n2"}},
		Priority: []control.ActionKind{control.ActionDeliverMessage, control.ActionFireTemporal},
	}
	if err := policy.Validate(2); err != nil {
		t.Fatal(err)
	}
	actions := []control.Action{
		{ID: "timer", Kind: control.ActionFireTemporal},
		{ID: "crash-n1", Kind: control.ActionCrash, Node: control.NodeRef{Node: "n1", Incarnation: 1}},
		{ID: "crash-n2", Kind: control.ActionCrash, Node: control.NodeRef{Node: "n2", Incarnation: 1}},
	}
	selected, err := policy.selectAction(1, actions)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != "crash-n2" {
		t.Fatalf("rule selected %s, want crash-n2", selected.ID)
	}
	selected, err = policy.selectAction(2, actions)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != "timer" {
		t.Fatalf("fallback selected %s, want timer", selected.ID)
	}
}

func TestPolicyExactActionIDCannotSubstituteSameClassAction(t *testing.T) {
	policy := Policy{
		Version: PolicyVersion, ID: "exact-action",
		Rules: []DecisionRule{{
			Decision: 1, Kind: control.ActionCrash, Node: "n1", ActionID: "crash-n1-incarnation-1",
		}},
		Priority: []control.ActionKind{control.ActionDeliverMessage},
	}
	if err := policy.Validate(1); err != nil {
		t.Fatal(err)
	}
	_, err := policy.selectAction(1, []control.Action{
		{ID: "crash-n1-incarnation-2", Kind: control.ActionCrash, Node: control.NodeRef{Node: "n1", Incarnation: 2}},
		{ID: "deliver", Kind: control.ActionDeliverMessage},
	})
	var selection *policySelectionError
	if !errors.As(err, &selection) || selection.failureCode() != "EXPERIMENT_POLICY_RULE_NOT_ENABLED" {
		t.Fatalf("same-class substitution error = %v, want exact-action rejection", err)
	}
}

func TestConfigRejectsOptionalReplayAndDuplicateRun(t *testing.T) {
	config := Config{
		SchemaVersion: SchemaVersion, ID: "test", PSSID: "pss", DecisionsPerRun: 2,
		Runtime: RuntimeConfig{SeedHex: "01", MaxClones: 1},
		Runs: []RunPlan{{Run: 1, Policy: Policy{
			Version: PolicyVersion, ID: "progress", Priority: []control.ActionKind{control.ActionFireTemporal},
		}}},
	}
	if err := config.Validate(); err == nil || err.Error() != "EXPERIMENT_REPLAY_REQUIRED" {
		t.Fatalf("Validate() error = %v, want replay requirement", err)
	}
	config.RequireReplay = true
	config.Runs = append(config.Runs, config.Runs[0])
	if err := config.Validate(); err == nil || err.Error() != "EXPERIMENT_RUN_INVALID: 1" {
		t.Fatalf("Validate() error = %v, want duplicate run", err)
	}
}

func TestRandomPolicyIsSeededAndDeterministic(t *testing.T) {
	actions := []control.Action{
		{ID: "a", Kind: control.ActionCrash},
		{ID: "b", Kind: control.ActionDeliverMessage},
		{ID: "c", Kind: control.ActionFireTemporal},
	}
	left := Policy{Version: RandomPolicyVersion, ID: "left", SeedHex: "01"}
	right := Policy{Version: RandomPolicyVersion, ID: "right", SeedHex: "02"}
	if err := left.Validate(32); err != nil {
		t.Fatal(err)
	}
	invalid := left
	invalid.Priority = []control.ActionKind{control.ActionCrash}
	if err := invalid.Validate(32); err == nil || err.Error() != "EXPERIMENT_RANDOM_POLICY_INVALID" {
		t.Fatalf("Validate() error = %v, want mixed-policy rejection", err)
	}
	var leftIDs, replayIDs, rightIDs []control.ActionID
	for decision := 1; decision <= 32; decision++ {
		leftAction, err := left.selectAction(decision, actions)
		if err != nil {
			t.Fatal(err)
		}
		replayAction, _ := left.selectAction(decision, actions)
		rightAction, _ := right.selectAction(decision, actions)
		leftIDs = append(leftIDs, leftAction.ID)
		replayIDs = append(replayIDs, replayAction.ID)
		rightIDs = append(rightIDs, rightAction.ID)
	}
	if !reflect.DeepEqual(leftIDs, replayIDs) {
		t.Fatal("same random policy seed changed the selection sequence")
	}
	if reflect.DeepEqual(leftIDs, rightIDs) {
		t.Fatal("different random policy seeds produced the same selection sequence")
	}
}

func TestActionClassRandomDoesNotWeightClassesByActionMultiplicity(t *testing.T) {
	actions := []control.Action{{ID: "deliver", Kind: control.ActionDeliverMessage}}
	for index := 0; index < 15; index++ {
		actions = append(actions, control.Action{
			ID: control.ActionID(fmt.Sprintf("drop-%02d", index)), Kind: control.ActionDropMessage,
		})
	}
	policy := Policy{Version: ActionClassPolicyVersion, ID: "class-random", SeedHex: "01"}
	if err := policy.Validate(256); err != nil {
		t.Fatal(err)
	}
	deliveries := 0
	var first []control.ActionID
	for decision := 1; decision <= 256; decision++ {
		selected, err := policy.selectAction(decision, actions)
		if err != nil {
			t.Fatal(err)
		}
		repeated, _ := policy.selectAction(decision, actions)
		if repeated.ID != selected.ID {
			t.Fatalf("decision %d changed from %s to %s", decision, selected.ID, repeated.ID)
		}
		if selected.Kind == control.ActionDeliverMessage {
			deliveries++
		}
		if decision <= 8 {
			first = append(first, selected.ID)
		}
	}
	if deliveries < 96 || deliveries > 160 {
		t.Fatalf("deliver class selected %d/256 times, want approximately half", deliveries)
	}
	reordered := append([]control.Action(nil), actions...)
	slices.Reverse(reordered)
	for decision, want := range first {
		selected, err := policy.selectAction(decision+1, reordered)
		if err != nil || selected.ID != want {
			t.Fatalf("reordered decision %d = %s/%v, want %s", decision+1, selected.ID, err, want)
		}
	}
}

func TestActionClassRandomHonorsFrozenWorkloadPriority(t *testing.T) {
	policy := Policy{
		Version: ActionClassPolicyVersion, ID: "workload-class-random", SeedHex: "01",
		Priority: []control.ActionKind{control.ActionInvoke},
	}
	if err := policy.Validate(1); err != nil {
		t.Fatal(err)
	}
	selected, err := policy.selectAction(1, []control.Action{
		{ID: "drop", Kind: control.ActionDropMessage},
		{ID: "invoke", Kind: control.ActionInvoke},
	})
	if err != nil || selected.ID != "invoke" {
		t.Fatalf("selected = %s/%v, want frozen invoke", selected.ID, err)
	}
}

func TestAdmissibleUniformIsOrderIndependentAndHonorsPreparationPriority(t *testing.T) {
	policy := Policy{
		Version: AdmissibleUniformPolicyVersion, ID: "qualified-uniform", SeedHex: "01",
		Priority: []control.ActionKind{control.ActionInvoke},
	}
	if err := policy.Validate(64); err != nil {
		t.Fatal(err)
	}
	actions := []control.Action{
		{ID: "timer", Kind: control.ActionFireTemporal},
		{ID: "deliver", Kind: control.ActionDeliverMessage},
		{ID: "drop", Kind: control.ActionDropMessage},
	}
	reordered := append([]control.Action(nil), actions...)
	slices.Reverse(reordered)
	seen := make(map[control.ActionID]bool)
	for decision := 1; decision <= 64; decision++ {
		selected, err := policy.selectAction(decision, actions)
		if err != nil {
			t.Fatal(err)
		}
		repeated, err := policy.selectAction(decision, reordered)
		if err != nil || repeated.ID != selected.ID {
			t.Fatalf("reordered decision %d = %s/%v, want %s", decision, repeated.ID, err, selected.ID)
		}
		seen[selected.ID] = true
	}
	if len(seen) != len(actions) {
		t.Fatalf("uniform policy did not reach all fixture actions: %#v", seen)
	}
	selected, err := policy.selectAction(1, append(actions,
		control.Action{ID: "invoke", Kind: control.ActionInvoke},
	))
	if err != nil || selected.ID != "invoke" {
		t.Fatalf("preparation selection = %s/%v, want invoke", selected.ID, err)
	}
	invalid := policy
	invalid.Rules = []DecisionRule{{Decision: 1, Kind: control.ActionCrash}}
	if err := invalid.Validate(64); err == nil || err.Error() != "EXPERIMENT_ADMISSIBLE_UNIFORM_POLICY_INVALID" {
		t.Fatalf("mixed policy error = %v", err)
	}
}

func TestBoundedStochasticPolicyFiltersBeforeSelectionAndBindsSurface(t *testing.T) {
	policy := Policy{
		Version: BoundedActionClassPolicyVersion, ID: "bounded-class", SeedHex: "01",
		Priority: []control.ActionKind{control.ActionInvoke},
		SelectableActions: []control.ActionKind{
			control.ActionDeliverMessage, control.ActionDropMessage,
			control.ActionFireTemporal, control.ActionInvoke,
		},
	}
	if err := policy.Validate(64); err != nil {
		t.Fatal(err)
	}
	actions := []control.Action{
		{ID: "crash", Kind: control.ActionCrash},
		{ID: "deliver", Kind: control.ActionDeliverMessage},
		{ID: "duplicate", Kind: control.ActionDuplicateMessage},
		{ID: "timer", Kind: control.ActionFireTemporal},
	}
	for decision := 1; decision <= 64; decision++ {
		selected, err := policy.selectAction(decision, actions)
		if err != nil {
			t.Fatal(err)
		}
		if selected.Kind != control.ActionDeliverMessage && selected.Kind != control.ActionFireTemporal {
			t.Fatalf("bounded decision %d selected %s", decision, selected.Kind)
		}
	}
	leftDigest, err := policy.Digest()
	if err != nil {
		t.Fatal(err)
	}
	changed := policy
	changed.SelectableActions = []control.ActionKind{
		control.ActionDeliverMessage, control.ActionFireTemporal, control.ActionInvoke,
	}
	rightDigest, err := changed.Digest()
	if err != nil || leftDigest == rightDigest {
		t.Fatalf("selection surface digest was not bound: %s/%s/%v", leftDigest, rightDigest, err)
	}
	invalid := policy
	invalid.Priority = []control.ActionKind{control.ActionCrash}
	if err := invalid.Validate(64); err == nil ||
		err.Error() != "EXPERIMENT_BOUNDED_POLICY_PRIORITY_OUTSIDE_SURFACE" {
		t.Fatalf("outside priority error=%v", err)
	}
}

func TestBoundedPolicyRequiresCanonicalSurfaceAndWorkloadInvoke(t *testing.T) {
	payload, err := control.NewPayload("input", "opaque", []byte("value"))
	if err != nil {
		t.Fatal(err)
	}
	policy := Policy{
		Version: BoundedUniformPolicyVersion, ID: "bounded-uniform", SeedHex: "01",
		SelectableActions: []control.ActionKind{control.ActionInvoke, control.ActionDeliverMessage},
	}
	if err := policy.Validate(8); err == nil || err.Error() != "EXPERIMENT_BOUNDED_UNIFORM_POLICY_INVALID" {
		t.Fatalf("non-canonical surface error=%v", err)
	}
	policy.SelectableActions = []control.ActionKind{control.ActionDeliverMessage}
	legacyConfig := Config{
		SchemaVersion: SchemaVersion, ID: "legacy-bounded", PSSID: "pss",
		Runtime: RuntimeConfig{SeedHex: "01"}, DecisionsPerRun: 8, RequireReplay: true,
		Runs: []RunPlan{{Run: 1, Policy: policy}},
	}
	if err := legacyConfig.Validate(); err == nil ||
		err.Error() != "run 1: EXPERIMENT_BOUNDED_POLICY_REQUIRES_V2" {
		t.Fatalf("legacy experiment error=%v", err)
	}
	admission, err := BindExecutionAdmission(admissionQualification(t), ExecutionRequirements{
		Capabilities: []string{"strict-replay"},
	})
	if err != nil {
		t.Fatal(err)
	}
	config := Config{
		SchemaVersion: SchemaVersionV2, ID: "bounded-workload", PSSID: "pss",
		WorkloadRouterID: "router", Runtime: RuntimeConfig{SeedHex: "01"},
		Admission: &admission, DecisionsPerRun: 8, RequireReplay: true,
		Runs: []RunPlan{{Run: 1, Policy: policy, Workload: &WorkloadPlan{
			SchemaVersion: WorkloadPlanVersion, ID: "workload",
			TargetSelector: TargetSingleCoordinatingMember,
			Invocations: []WorkloadInvocation{{
				ID: "request", Input: payload, ExpectedStatus: "complete",
			}},
		}}},
	}
	if err := config.Validate(); err == nil ||
		err.Error() != "run 1: EXPERIMENT_BOUNDED_POLICY_INVOKE_REQUIRED" {
		t.Fatalf("missing invoke error=%v", err)
	}
}

func TestBoundedPolicyRejectsTraceActionOutsideSurface(t *testing.T) {
	policy := Policy{
		Version: BoundedUniformPolicyVersion,
		ID:      "bounded-trace",
		SeedHex: "01",
		SelectableActions: []control.ActionKind{
			control.ActionDeliverMessage,
			control.ActionInvoke,
		},
	}
	trace := controlruntime.Trace{Records: []controlruntime.ActionRecord{{
		Action: control.Action{ID: "crash-1", Kind: control.ActionCrash},
	}}}

	err := policy.validateTraceSurface(trace)
	if err == nil || err.Error() !=
		"EXPERIMENT_BOUNDED_POLICY_TRACE_ACTION_OUTSIDE_SURFACE: decision=1 kind=crash" {
		t.Fatalf("unexpected validation result: %v", err)
	}

	policy.Version = AdmissibleUniformPolicyVersion
	if err := policy.validateTraceSurface(trace); err != nil {
		t.Fatalf("legacy policy should retain its historical open semantics: %v", err)
	}
}

func TestAdjacentTraceMutationUsesExactSpliceAndDeclaredSuffix(t *testing.T) {
	trace, err := (controlruntime.Trace{Records: []controlruntime.ActionRecord{
		{Action: control.Action{ID: "a", Kind: control.ActionCompleteEffect}},
		{Action: control.Action{ID: "b", Kind: control.ActionDeliverMessage}},
		{Action: control.Action{ID: "c", Kind: control.ActionCompleteEffect}},
		{Action: control.Action{ID: "d", Kind: control.ActionFireTemporal}},
	}}).Seal()
	if err != nil {
		t.Fatal(err)
	}
	source := Policy{
		Version: PolicyVersion, ID: "source",
		Priority: []control.ActionKind{control.ActionDeliverMessage, control.ActionFireTemporal},
	}
	plan, err := NewAdjacentTraceMutation("swap-2-3", trace, source, 2)
	if err != nil {
		t.Fatal(err)
	}
	policy := Policy{Version: TraceMutationPolicyVersion, ID: "mutation", TraceMutation: &plan}
	if err := policy.Validate(4); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		decision int
		enabled  []control.Action
		want     control.ActionID
	}{
		{1, []control.Action{{ID: "a", Kind: control.ActionCompleteEffect}}, "a"},
		{2, []control.Action{{ID: "b", Kind: control.ActionDeliverMessage}, {ID: "c", Kind: control.ActionCompleteEffect}}, "c"},
		{3, []control.Action{{ID: "b", Kind: control.ActionDeliverMessage}}, "b"},
		{4, []control.Action{{ID: "x", Kind: control.ActionFireTemporal}, {ID: "y", Kind: control.ActionDeliverMessage}}, "y"},
	}
	for _, test := range tests {
		selected, err := policy.selectAction(test.decision, test.enabled)
		if err != nil || selected.ID != test.want {
			t.Fatalf("decision %d selected %s/%v, want %s", test.decision, selected.ID, err, test.want)
		}
	}
	_, err = policy.selectAction(2, []control.Action{{ID: "b", Kind: control.ActionDeliverMessage}})
	var selection *policySelectionError
	if !errors.As(err, &selection) || selection.failureCode() != "EXPERIMENT_TRACE_MUTATION_ACTION_NOT_ENABLED" {
		t.Fatalf("missing swapped action error = %v", err)
	}
	tampered := plan
	tampered.FirstActionID = "changed"
	if err := tampered.Validate(4); err == nil || err.Error() != "EXPERIMENT_TRACE_MUTATION_DIGEST_MISMATCH" {
		t.Fatalf("tampered plan error = %v", err)
	}
}

func TestOccurrenceAwareMutationAcceptsRepeatedSourceActionID(t *testing.T) {
	trace, err := (controlruntime.Trace{
		ManifestDigest: strings.Repeat("a", 64),
		Records: []controlruntime.ActionRecord{
			{Action: control.Action{ID: "repeat", Kind: control.ActionCompleteEffect}},
			{Action: control.Action{ID: "middle", Kind: control.ActionDeliverMessage}},
			{Action: control.Action{ID: "repeat", Kind: control.ActionCompleteEffect}},
			{Action: control.Action{ID: "tail", Kind: control.ActionFireTemporal}},
		},
	}).Seal()
	if err != nil {
		t.Fatal(err)
	}
	entry := MutationSourceEntry{
		SchemaVersion: MutationSourceEntryVersion, ID: "source-1",
		BundleDigest: strings.Repeat("b", 64), ReportDigest: strings.Repeat("c", 64),
		ConfigDigest: strings.Repeat("d", 64), TraceDigest: trace.Digest,
		ManifestDigest: trace.ManifestDigest, PSSID: "test/core-pss-v1",
		PolicyID: "any-qualified-policy", PolicyDigest: strings.Repeat("e", 64),
		TraceSchema: trace.SchemaVersion, Decisions: len(trace.Records),
		CorePSSDigest: strings.Repeat("f", 64), QualificationID: strings.Repeat("1", 64),
	}
	entry, err = entry.seal()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewOccurrenceAwareAdjacentTraceMutation(
		"swap-repeat-tail", trace, entry,
		[]control.ActionKind{control.ActionDeliverMessage}, 3,
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.SchemaVersion != TraceMutationPlanVersionV2 ||
		plan.FirstActionRef == nil || plan.FirstActionRef.Occurrence != 2 ||
		plan.SecondActionRef == nil || plan.SecondActionRef.Occurrence != 1 ||
		len(plan.PrefixActionRefs) != 2 || plan.PrefixActionRefs[0].Occurrence != 1 {
		t.Fatalf("unexpected occurrence plan: %#v", plan)
	}
	policy := Policy{
		Version: TraceMutationPolicyVersionV2, ID: "occurrence-mutation", TraceMutation: &plan,
	}
	if err := policy.Validate(4); err != nil {
		t.Fatal(err)
	}
	wants := []control.ActionID{"repeat", "middle", "tail", "repeat"}
	for decision, want := range wants {
		action, err := policy.selectAction(decision+1, []control.Action{{
			ID: want, Kind: trace.Records[decision].Action.Kind,
		}})
		if err != nil || action.ID != want {
			t.Fatalf("decision %d selected %s/%v, want %s", decision+1, action.ID, err, want)
		}
	}
	tampered := plan
	ref := *tampered.FirstActionRef
	ref.Occurrence = 1
	tampered.FirstActionRef = &ref
	tampered, err = tampered.seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := tampered.Validate(4); err == nil || !strings.Contains(err.Error(), "OCCURRENCE_INVALID") {
		t.Fatalf("tampered occurrence error = %v", err)
	}
}

func TestMutationSourceCorpusPreservesExplicitOrderAndRejectsDuplicates(t *testing.T) {
	base := MutationSourceEntry{
		SchemaVersion: MutationSourceEntryVersion, ID: "first",
		BundleDigest: strings.Repeat("a", 64), ReportDigest: strings.Repeat("b", 64),
		ConfigDigest: strings.Repeat("c", 64), TraceDigest: strings.Repeat("d", 64),
		ManifestDigest: strings.Repeat("e", 64), PSSID: "test/core-pss-v1",
		PolicyID: "policy-1", PolicyDigest: strings.Repeat("f", 64),
		TraceSchema: controlruntime.TraceSchemaVersion, Decisions: 4,
		CorePSSDigest: strings.Repeat("1", 64), QualificationID: strings.Repeat("2", 64),
	}
	first, err := base.seal()
	if err != nil {
		t.Fatal(err)
	}
	base.ID = "second"
	base.BundleDigest = strings.Repeat("3", 64)
	base.TraceDigest = strings.Repeat("4", 64)
	second, err := base.seal()
	if err != nil {
		t.Fatal(err)
	}
	corpus, err := NewMutationSourceCorpus("ordered-sources", []MutationSourceEntry{second, first})
	if err != nil {
		t.Fatal(err)
	}
	if corpus.Entries[0].ID != "second" || corpus.Entries[1].ID != "first" {
		t.Fatalf("corpus order changed: %#v", corpus.Entries)
	}
	if _, err := NewMutationSourceCorpus("duplicates", []MutationSourceEntry{first, first}); err == nil || !strings.Contains(err.Error(), "SOURCE_DUPLICATE") {
		t.Fatalf("duplicate corpus error = %v", err)
	}
	renamed := first
	renamed.ID = "renamed-same-bundle"
	renamed, err = renamed.seal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewMutationSourceCorpus("renamed-duplicate", []MutationSourceEntry{first, renamed}); err == nil || !strings.Contains(err.Error(), "SOURCE_DUPLICATE") {
		t.Fatalf("renamed duplicate bundle error = %v", err)
	}
}
