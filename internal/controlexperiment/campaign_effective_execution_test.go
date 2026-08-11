package controlexperiment

import (
	"strings"
	"testing"
)

func TestCampaignEffectiveExecutionExcludesAuditOnlyPlanIdentity(t *testing.T) {
	planned := campaignPlannedAttemptFixture(t)
	plan, instance := planned.Plan, planned.Instance
	identity, err := NewCampaignEffectiveExecution(
		"fixture-target", strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64),
		plan, instance,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.Validate(); err != nil {
		t.Fatal(err)
	}

	changedPlan := plan
	changedPlan.IntentDigest = strings.Repeat("d", 64)
	changedPlan.CompilerWork.PreferenceChecks++
	changedPlan.CompilerWork.WorkUnits++
	changedPlan, err = changedPlan.seal()
	if err != nil || changedPlan.Validate() != nil {
		t.Fatalf("audit-only plan fixture is invalid: %#v/%v", changedPlan, err)
	}
	changedInstance, err := NewIntentExecutionInstance(
		instance.ID, changedPlan, instance.PolicySeed, instance.Budget,
	)
	if err != nil {
		t.Fatal(err)
	}
	changedIdentity, err := NewCampaignEffectiveExecution(
		identity.TargetID, identity.TargetIdentityDigest, identity.ExecutionEnvironmentDigest,
		identity.PolicyDigest, changedPlan, changedInstance,
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Digest == changedPlan.Digest || instance.Digest == changedInstance.Digest ||
		identity.Digest != changedIdentity.Digest {
		t.Fatalf("audit metadata changed effective identity: before=%#v after=%#v", identity, changedIdentity)
	}

	changedPolicy, err := NewCampaignEffectiveExecution(
		identity.TargetID, identity.TargetIdentityDigest, identity.ExecutionEnvironmentDigest,
		strings.Repeat("e", 64), changedPlan, changedInstance,
	)
	if err != nil {
		t.Fatal(err)
	}
	if changedPolicy.Digest == identity.Digest {
		t.Fatal("effective policy change did not change execution identity")
	}
}
