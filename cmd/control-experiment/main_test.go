package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestEtcdraftReportIsEqualBudgetReplayStableAndSelfValidating(t *testing.T) {
	report, err := etcdraftReport(context.Background(), "fixed", 32, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Runs) != 2 || report.StateDiscovery.TotalDecisions != 64 ||
		report.StateDiscovery.ProtocolSamples != 64 {
		t.Fatalf("unexpected budget: %#v", report.Budget)
	}
	if report.Runs[0].UniqueCoreStates != 23 || report.Runs[1].UniqueCoreStates != 23 ||
		report.StateDiscovery.UniqueStates != 45 || report.StateDiscovery.PrefixArea != 1454 {
		t.Fatalf("unexpected discovery: %#v", report.StateDiscovery)
	}
	if !allReplayStable(report) || report.Work.Primary.WorkUnits != 66 || report.Work.Replay.WorkUnits != 66 {
		t.Fatalf("unexpected replay/work: %#v", report.Work)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var persisted controlexperiment.Report
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		t.Fatal(err)
	}
	if err := persisted.Validate(); err != nil {
		t.Fatalf("persisted Validate() error = %v", err)
	}
	t.Logf("runs=2 decisions=64 states=23/23 union=45 prefix_area=1454 digest=%s", report.Digest)
}

func TestEtcdraftRandomReportIsDeterministicAndDoesNotReseedRuntime(t *testing.T) {
	ctx := context.Background()
	first, err := etcdraftReport(ctx, "random", 32, 1)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := etcdraftReport(ctx, "random", 32, 1)
	if err != nil {
		t.Fatal(err)
	}
	other, err := etcdraftReport(ctx, "random", 32, 2)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != repeated.Digest {
		t.Fatalf("same policy seed changed report digest: %s != %s", first.Digest, repeated.Digest)
	}
	if first.Digest == other.Digest {
		t.Fatal("different policy seeds produced the same report digest")
	}
	if first.Config.Runtime != other.Config.Runtime {
		t.Fatal("policy seed changed Runtime configuration")
	}
	for index := range first.Runs {
		if first.Runs[index].TraceDigest != repeated.Runs[index].TraceDigest {
			t.Fatalf("run %d trace changed under repeated policy seed", index+1)
		}
		if first.Runs[index].SeedDigest != other.Runs[index].SeedDigest ||
			first.Runs[index].SeedDigest != first.Runs[0].SeedDigest {
			t.Fatalf("run %d policy seed changed Runtime seed digest", index+1)
		}
	}
	if !allReplayStable(first) || first.StateDiscovery.TotalDecisions != 64 ||
		first.Work.Primary.WorkUnits != 66 || first.Work.Replay.WorkUnits != 66 {
		t.Fatalf("unexpected random budget/replay: %#v", first.Work)
	}
	if first.Runs[0].UniqueCoreStates != 29 || first.Runs[1].UniqueCoreStates != 26 ||
		first.StateDiscovery.UniqueStates != 53 || first.StateDiscovery.PrefixArea != 1843 {
		t.Fatalf("unexpected seed-1 discovery: %#v", first.StateDiscovery)
	}
	t.Logf("seed=1 states=29/26 union=%d prefix_area=%d digest=%s", first.StateDiscovery.UniqueStates,
		first.StateDiscovery.PrefixArea, first.Digest)
}

func TestStubPlannerAttemptUsesTheSoleExecutor(t *testing.T) {
	attempt, err := etcdraftPlannerAttempt(context.Background(), 32, stubPlannerProposal())
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Status != controlexperiment.PlannerStatusComplete || attempt.Experiment == nil ||
		attempt.Work.ProposalAttempts != 1 || attempt.Work.Model.Calls != 0 {
		t.Fatalf("unexpected attempt envelope: %#v", attempt)
	}
	report := attempt.Experiment
	if report.StateDiscovery.UniqueStates != 45 || report.StateDiscovery.PrefixArea != 1454 ||
		report.Work.Primary.WorkUnits != 66 || report.Work.Replay.WorkUnits != 66 ||
		attempt.Work.Execution != report.Work {
		t.Fatalf("unexpected compiled execution: %#v", report)
	}
	encoded, err := json.Marshal(attempt)
	if err != nil {
		t.Fatal(err)
	}
	var persisted controlexperiment.PlannerAttempt
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		t.Fatal(err)
	}
	if err := persisted.Validate(); err != nil {
		t.Fatalf("persisted Validate() error = %v", err)
	}
}

func TestStubPlannerRejectedAndUnreachableProposalsAreExplicitAndCharged(t *testing.T) {
	rejectedProposal := stubPlannerProposal()
	rejectedProposal.Policies = rejectedProposal.Policies[:1]
	rejected, err := etcdraftPlannerAttempt(context.Background(), 32, rejectedProposal)
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Status != controlexperiment.PlannerStatusRejected || rejected.Failure == nil ||
		rejected.Failure.ReasonCode != controlexperiment.ProposalRunSetInvalid ||
		rejected.Work.ProposalAttempts != 1 || rejected.Work.Execution.Primary.SetupAttempts != 0 {
		t.Fatalf("unexpected rejected attempt: %#v", rejected)
	}

	unreachableProposal := stubPlannerProposal()
	unreachableProposal.Policies[0].Rules = []controlexperiment.DecisionRule{{
		Decision: 1, Kind: control.ActionRestart, Node: "n1",
	}}
	unreachable, err := etcdraftPlannerAttempt(context.Background(), 32, unreachableProposal)
	if err != nil {
		t.Fatal(err)
	}
	if unreachable.Status != controlexperiment.PlannerStatusExecutionFailed || unreachable.Failure == nil ||
		unreachable.Failure.ReasonCode != "EXPERIMENT_POLICY_RULE_NOT_ENABLED" ||
		unreachable.Failure.Phase != "primary-policy" || unreachable.Failure.Run != 1 ||
		unreachable.Failure.Decision != 1 || unreachable.Work.ProposalAttempts != 1 ||
		unreachable.Work.Execution.Primary.SetupAttempts != 1 ||
		unreachable.Work.Execution.Primary.RuntimeInitializations != 1 ||
		unreachable.Work.Execution.Primary.SchedulerDecisions != 0 ||
		unreachable.Work.Execution.Primary.WorkUnits != 1 ||
		unreachable.Work.Execution.Replay.SetupAttempts != 0 {
		t.Fatalf("unexpected unreachable attempt: %#v", unreachable)
	}
}

func TestStubPlannerCLIWritesRejectedAttemptWithoutPanicking(t *testing.T) {
	output := filepath.Join(t.TempDir(), "attempt.json")
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{
		"-strategy", "stub-planner", "-decisions", "1", "-out", output,
	}, &stdout); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "status=proposal-rejected") ||
		!strings.Contains(stdout.String(), controlexperiment.ProposalPolicyInvalid) {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}
}

func TestCheckedInDeepSeekAttemptIsExplicitAndSelfValidating(t *testing.T) {
	encoded, err := os.ReadFile("../../benchmarks/experiments/etcdraft-v2-deepseek-planner-m5.13/attempt.json")
	if err != nil {
		t.Fatal(err)
	}
	var attempt controlexperiment.PlannerAttempt
	if err := json.Unmarshal(encoded, &attempt); err != nil {
		t.Fatal(err)
	}
	if err := attempt.Validate(); err != nil {
		t.Fatal(err)
	}
	if attempt.Status != controlexperiment.PlannerStatusExecutionFailed || attempt.Failure == nil ||
		attempt.Failure.ReasonCode != "EXPERIMENT_POLICY_RULE_NOT_ENABLED" ||
		attempt.Failure.Run != 2 || attempt.Failure.Decision != 3 ||
		attempt.Work.Model != (controlexperiment.ModelWork{
			Calls: 1, InputTokens: 555, OutputTokens: 189, TotalTokens: 744,
		}) || attempt.Work.Execution.Primary.SchedulerDecisions != 34 ||
		attempt.Work.Execution.Replay.SchedulerDecisions != 32 {
		t.Fatalf("unexpected checked-in attempt: %#v", attempt)
	}
	sum := sha256.Sum256(encoded)
	if got := hex.EncodeToString(sum[:]); got != "7d6658693ebaf3e05b6e2a130cfd65f79350a2b649276a26c01b8c2fa2689e71" {
		t.Fatalf("artifact SHA-256 = %s", got)
	}
}
