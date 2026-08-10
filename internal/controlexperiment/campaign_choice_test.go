package controlexperiment

import (
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestCampaignExecutionChoiceBindsIntentPlanAndInstance(t *testing.T) {
	intent, err := NewGuardedTestIntent(GuardedTestIntent{
		ID: "choice-intent", ViewDigest: strings.Repeat("a", 64), RiskID: "risk",
		Must: IntentMust{Decisions: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := (CompiledIntentPlanV2{
		SchemaVersion: CompiledIntentPlanVersionV2, ID: "choice-plan",
		ViewDigest: intent.ViewDigest, IntentDigest: intent.Digest, CatalogDigest: strings.Repeat("b", 64),
		RiskID: intent.RiskID, BackendID: "backend", Strategy: "strategy", Decisions: 4,
		RequiredCapabilities: []string{"capability"}, RequiredActions: []control.ActionKind{control.ActionInvoke},
		CompilerWork: IntentCompilerWork{CandidatesEvaluated: 1, WorkUnits: 1},
	}).seal()
	if err != nil {
		t.Fatal(err)
	}
	instance, err := NewIntentExecutionInstance("choice-instance", plan, 7, MethodBudget{
		MaxExecutionAttempts: 1, MaxPrimaryWorkUnits: 6, MaxReplayWorkUnits: 6,
	})
	if err != nil {
		t.Fatal(err)
	}
	choice, err := NewCampaignExecutionChoice(intent, plan, instance)
	if err != nil || choice.BackendID != plan.BackendID || choice.PolicySeed != 7 {
		t.Fatalf("choice binding failed: %#v/%v", choice, err)
	}
	if err := choice.ValidateInputs(intent, plan, instance); err != nil {
		t.Fatal(err)
	}
	other, err := NewIntentExecutionInstance("choice-instance", plan, 8, instance.Budget)
	if err != nil {
		t.Fatal(err)
	}
	if err := choice.ValidateInputs(intent, plan, other); err == nil {
		t.Fatal("choice accepted a different execution instance")
	}
	projected := projectCampaignChoice(choice)
	if projected.ChoiceDigest != choice.Digest || projected.BackendID != choice.BackendID ||
		projected.PlanDigest != plan.Digest {
		t.Fatalf("safe choice projection drifted: %#v", projected)
	}
}
