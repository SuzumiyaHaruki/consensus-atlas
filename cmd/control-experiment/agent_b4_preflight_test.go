package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestEtcdraftM518b4PreflightSeparatesActionSurfaceSeedAndIntentOutcome(t *testing.T) {
	result, err := newEtcdraftAgentB4PreflightFromInputsAndMethods(
		context.Background(), 96, 1,
		sharedEtcdraftB4IntentInputsFixture(t),
		sharedEtcdraftActionV2MethodFixture(t), sharedEtcdraftUniformMethodFixture(t),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Plan.ValidateInputs(
		result.Inputs.View, result.Inputs.Knowledge, result.Inputs.Catalog,
		result.Inputs.Qualification.Manifest, result.Inputs.Qualification.Qualification,
		result.Intent,
	); err != nil {
		t.Fatal(err)
	}
	if err := result.Instance.ValidatePlan(result.Plan); err != nil {
		t.Fatal(err)
	}
	if err := result.Outcome.ValidateInputs(
		result.Plan, result.Instance, result.Report, result.Bundle,
	); err != nil {
		t.Fatal(err)
	}
	for _, template := range result.Inputs.Catalog.Templates {
		if template.PolicySeed != 0 || template.MaxFaultEnvelope.MaxPartitions != 0 ||
			template.MaxFaultEnvelope.MaxActivePartitions != 0 ||
			actionKindPresent(template.SupportedActions, control.ActionPartition) ||
			actionKindPresent(template.SupportedActions, control.ActionHeal) {
			t.Fatalf("b4 backend overstates its action surface: %#v", template)
		}
	}
	if !actionKindPresent(result.Inputs.View.AvailableActions, control.ActionPartition) {
		t.Fatal("Runtime-supported Partition disappeared from the manifest-derived view")
	}
	for _, method := range []etcdraftMethodExecution{result.Batch.ActionClass, result.Batch.Uniform} {
		for _, bundle := range method.Bundles {
			for _, record := range bundle.Trace.Records {
				if record.Action.Kind == control.ActionPartition || record.Action.Kind == control.ActionHeal {
					t.Fatalf("historical source contains an action unavailable to the b4 experiment: %#v", record.Action)
				}
			}
		}
	}
	encodedPlan, err := json.Marshal(result.Plan)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encodedPlan, []byte("policy_seed")) {
		t.Fatalf("macro plan still contains an execution seed: %s", encodedPlan)
	}
	if result.Instance.PolicySeed != 4 || result.Plan.Strategy != "workload-action-class-random-b4" ||
		result.Report.Config.FaultEnvelope == nil ||
		result.Report.Config.FaultEnvelope.MaxPartitions != 0 ||
		result.Report.Config.FaultEnvelope.MaxActivePartitions != 0 ||
		result.Outcome.ExecutionStatus != controlexperiment.IntentExecutionValid ||
		result.Outcome.IntentStatus != controlexperiment.IntentReachabilityNotReached ||
		result.Outcome.ReasonCode != controlexperiment.IntentReasonRequiredActionMissing ||
		len(result.Outcome.MissingActions) == 0 ||
		result.Outcome.OracleStatus != controlexperiment.IntentOracleNotEvaluated ||
		result.Summary.ModelCalls != 0 {
		t.Fatalf("unexpected b4 preflight result: plan=%#v instance=%#v outcome=%#v",
			result.Plan, result.Instance, result.Outcome)
	}
	if err := validateEtcdraftB4ExecutionInstance(result.Instance, result.Report); err != nil {
		t.Fatal(err)
	}
	checkedDirectory := "../../benchmarks/experiments/etcdraft-v2-agent-b4-preflight-m5.18b4-pre"
	var checkedView controlexperiment.AgentSemanticView
	readCheckedB4JSON(t, checkedDirectory+"/view.json", &checkedView)
	if err := checkedView.ValidateInputs(
		result.Inputs.Knowledge, result.Inputs.Catalog, result.Inputs.Qualification.Manifest,
		result.Inputs.Qualification.Qualification,
	); err != nil || checkedView.Digest != result.Inputs.View.Digest {
		t.Fatalf("checked b4 view drifted: %v", err)
	}
	var checkedFeedback controlexperiment.AgentBatchFeedbackView
	readCheckedB4JSON(t, checkedDirectory+"/feedback.json", &checkedFeedback)
	if err := checkedFeedback.ValidateInputs(result.Inputs.View, result.Batch.Sources); err != nil ||
		checkedFeedback.Digest != result.Batch.Feedback.Digest {
		t.Fatalf("checked b4 feedback drifted: %v", err)
	}
	var checkedIntent controlexperiment.GuardedTestIntent
	readCheckedB4JSON(t, checkedDirectory+"/intent.json", &checkedIntent)
	if err := checkedIntent.Validate(); err != nil || checkedIntent.Digest != result.Intent.Digest {
		t.Fatalf("checked b4 intent drifted: %v", err)
	}
	var checkedPlan controlexperiment.CompiledIntentPlanV2
	readCheckedB4JSON(t, checkedDirectory+"/plan.json", &checkedPlan)
	if err := checkedPlan.ValidateInputs(
		result.Inputs.View, result.Inputs.Knowledge, result.Inputs.Catalog,
		result.Inputs.Qualification.Manifest, result.Inputs.Qualification.Qualification,
		result.Intent,
	); err != nil || checkedPlan.Digest != result.Plan.Digest {
		t.Fatalf("checked b4 plan drifted: %v", err)
	}
	var checkedInstance controlexperiment.IntentExecutionInstance
	readCheckedB4JSON(t, checkedDirectory+"/execution-instance.json", &checkedInstance)
	if err := checkedInstance.ValidatePlan(result.Plan); err != nil ||
		checkedInstance.Digest != result.Instance.Digest {
		t.Fatalf("checked b4 execution instance drifted: %v", err)
	}
	var checkedOutcome controlexperiment.IntentOutcome
	readCheckedB4JSON(t, checkedDirectory+"/intent-outcome.json", &checkedOutcome)
	if err := checkedOutcome.ValidateInputs(
		result.Plan, result.Instance, result.Report, result.Bundle,
	); err != nil || checkedOutcome.Digest != result.Outcome.Digest {
		t.Fatalf("checked b4 intent outcome drifted: %v", err)
	}
	var checkedSummary etcdraftAgentB4PreflightSummary
	readCheckedB4JSON(t, checkedDirectory+"/summary.json", &checkedSummary)
	checkedSummaryJSON, err := json.Marshal(checkedSummary)
	if err != nil {
		t.Fatal(err)
	}
	resultSummaryJSON, err := json.Marshal(result.Summary)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(checkedSummaryJSON, resultSummaryJSON) {
		t.Fatal("checked b4 summary drifted")
	}

	partitionIntent, err := controlexperiment.NewGuardedTestIntent(controlexperiment.GuardedTestIntent{
		ID: "unproducible-partition", ViewDigest: result.Inputs.View.Digest,
		RiskID: etcdraftIntentRiskLeaderChange,
		Must: controlexperiment.IntentMust{
			Decisions: 96, RequiredActions: []control.ActionKind{control.ActionPartition},
			FaultEnvelope: controlexperiment.FaultEnvelope{
				MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
				MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controlexperiment.CompileGuardedTestIntentV2(
		result.Inputs.View, result.Inputs.Knowledge, result.Inputs.Catalog,
		result.Inputs.Qualification.Manifest, result.Inputs.Qualification.Qualification,
		partitionIntent,
	); err == nil || !strings.Contains(err.Error(), "HARD_CONSTRAINT_UNSATISFIED") {
		t.Fatalf("unproducible Partition was accepted: %v", err)
	}

	tampered := result.Instance
	tampered.PolicySeed = 5
	tampered, err = controlexperiment.NewIntentExecutionInstance(
		tampered.ID, result.Plan, tampered.PolicySeed, tampered.Budget,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateEtcdraftB4ExecutionInstance(tampered, result.Report); err == nil ||
		!strings.Contains(err.Error(), "SEED_MISMATCH") {
		t.Fatalf("execution-instance/report seed drift was accepted: %v", err)
	}
	t.Logf("view=%s catalog=%s feedback=%s plan=%s instance=%s outcome=%s report=%s bundle=%s summary=%s missing=%v",
		result.Inputs.View.Digest, result.Inputs.Catalog.Digest, result.Batch.Feedback.Digest,
		result.Plan.Digest, result.Instance.Digest, result.Outcome.Digest,
		result.Report.Digest, result.Bundle.Digest, result.Summary.Digest,
		result.Outcome.MissingActions)
}

func actionKindPresent(actions []control.ActionKind, want control.ActionKind) bool {
	for _, action := range actions {
		if action == want {
			return true
		}
	}
	return false
}

func readCheckedB4JSON(t *testing.T, path string, destination any) {
	t.Helper()
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, destination); err != nil {
		t.Fatal(err)
	}
}
