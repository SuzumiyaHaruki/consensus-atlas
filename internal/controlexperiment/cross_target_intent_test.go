package controlexperiment

import (
	"reflect"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestCrossTargetPlannerViewIntersectsAndProjectsOneIntent(t *testing.T) {
	pack, sources := crossTargetIntentFixture(t)
	view, err := NewCrossTargetPlannerView("portable-view", sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.ValidatedCapabilities) != 2 || len(view.AvailableActions) != 3 ||
		len(view.EligibleBackends) != 1 || view.KnowledgePack.Digest != pack.Digest {
		t.Fatalf("unexpected common view: %#v", view)
	}
	intent, err := NewGuardedTestIntent(GuardedTestIntent{
		ID: "portable-intent", ViewDigest: view.Digest, RiskID: "natural-progress",
		Must:   IntentMust{Decisions: 8},
		Prefer: IntentPrefer{BackendIDs: []string{"bounded-action-class"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	left, err := ProjectCrossTargetIntent(view, sources, intent, sources[0])
	if err != nil {
		t.Fatal(err)
	}
	right, err := ProjectCrossTargetIntent(view, sources, intent, sources[1])
	if err != nil {
		t.Fatal(err)
	}
	if left.Digest == right.Digest || left.ViewDigest == right.ViewDigest ||
		left.RiskID != intent.RiskID || !reflect.DeepEqual(right.Must, intent.Must) ||
		left.Prefer.BackendIDs[0] != intent.Prefer.BackendIDs[0] {
		t.Fatalf("target projection changed shared intent semantics: left=%#v right=%#v", left, right)
	}
	if err := view.ValidateInputs([]AgentSemanticView{sources[1], sources[0]}); err != nil {
		t.Fatalf("source ordering changed common identity: %v", err)
	}
}

func TestCrossTargetPlannerViewRejectsNonCommonInputs(t *testing.T) {
	_, sources := crossTargetIntentFixture(t)
	changed := sources[1]
	changed.CatalogDigest = strings.Repeat("f", 64)
	changed, _ = changed.seal()
	if _, err := NewCrossTargetPlannerView("portable-view", []AgentSemanticView{sources[0], changed}); err == nil ||
		err.Error() != "EXPERIMENT_CROSS_TARGET_VIEW_SOURCE_MISMATCH" {
		t.Fatalf("catalog mismatch error=%v", err)
	}

	view, err := NewCrossTargetPlannerView("portable-view", sources)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := NewGuardedTestIntent(GuardedTestIntent{
		ID: "portable-intent", ViewDigest: view.Digest, RiskID: "natural-progress",
		Must: IntentMust{Decisions: 8, RequiredActions: []control.ActionKind{control.ActionCrash},
			FaultEnvelope: FaultEnvelope{MaxCrashes: 1, MaxConcurrentCrashes: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ProjectCrossTargetIntent(view, sources, intent, sources[0]); err == nil ||
		err.Error() != "EXPERIMENT_CROSS_TARGET_INTENT_HARD_CONSTRAINT_UNSATISFIED" {
		t.Fatalf("non-common action error=%v", err)
	}
}

func TestCrossTargetPlannerProposalIsPreferenceOnlyAndTargetBlind(t *testing.T) {
	_, sources := crossTargetIntentFixture(t)
	view, err := NewCrossTargetPlannerView("portable-view", sources)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := NewGuardedTestIntent(GuardedTestIntent{
		ID: "portable-intent", ViewDigest: view.Digest, RiskID: "natural-progress",
		Must: IntentMust{Decisions: 8},
	})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := NewCrossTargetPlannerProposalContract(view, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if len(contract.AllowedBackendIDs) != 1 || len(contract.AllowedActions) != 3 ||
		contract.Template.ID != baseline.ID || contract.Template.ViewDigest != view.Digest ||
		len(contract.Template.Prefer.BackendIDs) != 0 || len(contract.Template.Prefer.Actions) != 0 {
		t.Fatalf("unexpected cross-target proposal contract: %#v", contract)
	}
	proposal := baseline
	proposal.Prefer = IntentPrefer{
		BackendIDs: []string{"bounded-action-class"},
		Actions: []control.ActionKind{
			control.ActionFireTemporal, control.ActionDeliverMessage, control.ActionInvoke,
		},
	}
	proposal, err = NewGuardedTestIntent(proposal)
	if err != nil || ValidateCrossTargetPlannerProposal(view, baseline, proposal) != nil {
		t.Fatalf("valid preference-only proposal rejected: %#v/%v", proposal, err)
	}

	hardChange := proposal
	hardChange.Must.Decisions++
	hardChange, _ = NewGuardedTestIntent(hardChange)
	if err := ValidateCrossTargetPlannerProposal(view, baseline, hardChange); err == nil {
		t.Fatal("cross-target proposal changed the decision budget")
	}
	targetOnly := proposal
	targetOnly.Prefer.Actions = []control.ActionKind{control.ActionCrash}
	targetOnly, _ = NewGuardedTestIntent(targetOnly)
	if err := ValidateCrossTargetPlannerProposal(view, baseline, targetOnly); err == nil {
		t.Fatal("cross-target proposal selected a target-only action")
	}
}

func crossTargetIntentFixture(t *testing.T) (ProtocolKnowledgePack, []AgentSemanticView) {
	t.Helper()
	pack, err := NewProtocolKnowledgePack(ProtocolKnowledgePack{
		ID: "portable-pack", Family: "leader-based-cft", Protocol: "portable-control",
		Knowledge: []KnowledgeStatement{{ID: "natural", Text: "Use natural progress and controlled delivery."}},
		Risks: []ProtocolRisk{{
			ID: "natural-progress", Summary: "Exercise one opaque input under natural progress.",
			RequiredCapabilities: []string{"message", "temporal"},
			RequiredActions: []control.ActionKind{
				control.ActionDeliverMessage, control.ActionFireTemporal, control.ActionInvoke,
			},
			AllowedBackendIDs: []string{"bounded-action-class"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	backend := IntentBackendView{
		ID: "bounded-action-class", SupportedActions: []control.ActionKind{
			control.ActionDeliverMessage, control.ActionFireTemporal, control.ActionInvoke,
		}, MinDecisions: 1, MaxDecisions: 16,
	}
	makeView := func(
		id, profile, profileDigest, adapter string,
		capabilities []string,
		actions []control.ActionKind,
	) AgentSemanticView {
		view, sealErr := (AgentSemanticView{
			SchemaVersion: AgentSemanticViewVersion, ID: id, KnowledgePack: pack,
			CatalogDigest: strings.Repeat("a", 64), ProfileID: profile,
			ProfileDigest: profileDigest, AdapterID: adapter,
			ValidatedCapabilities: capabilities, AvailableActions: actions,
			EligibleBackends: []IntentBackendView{backend},
		}).seal()
		if sealErr != nil || view.Validate() != nil {
			t.Fatalf("source view invalid: %v", sealErr)
		}
		return view
	}
	left := makeView("target-left", "profile-left", strings.Repeat("b", 64), "adapter-left",
		[]string{"durability", "message", "temporal"},
		[]control.ActionKind{control.ActionCrash, control.ActionDeliverMessage, control.ActionFireTemporal, control.ActionInvoke})
	right := makeView("target-right", "profile-right", strings.Repeat("c", 64), "adapter-right",
		[]string{"message", "temporal"},
		[]control.ActionKind{control.ActionDeliverMessage, control.ActionFireTemporal, control.ActionInvoke})
	return pack, []AgentSemanticView{left, right}
}
