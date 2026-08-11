package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

type portableM522bResult struct {
	Inputs portableM522bInputs
	Intent controlexperiment.GuardedTestIntent
	Etcd   portableM522bTargetExecution
	Omni   portableM522bTargetExecution
}

func TestM522bAndM522cPortableIntentExecutesAndRecovers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	durable, err := runPortableM522cDurableCrossTarget(
		ctx, buildPortableM522bWorker(t), filepath.Join(t.TempDir(), "cross-target"),
	)
	if err != nil {
		t.Fatal(err)
	}
	result := portableM522bResult{
		Inputs: durable.Inputs, Intent: durable.Planning.Intent,
		Etcd: durable.Etcd, Omni: durable.Omni,
	}
	encodedView, err := json.Marshal(result.Inputs.CommonView)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"etcd", "raft", "omnipaxos", "paxos", "adapter_id", "profile_id", "pss", "oracle",
	} {
		if strings.Contains(strings.ToLower(string(encodedView)), forbidden) {
			t.Fatalf("common Agent view exposed target-specific token %q: %s", forbidden, encodedView)
		}
	}
	if len(result.Inputs.CommonView.SourceViewDigests) != 2 ||
		len(result.Inputs.CommonView.ValidatedCapabilities) != 6 ||
		len(result.Inputs.CommonView.AvailableActions) != 3 ||
		len(result.Inputs.CommonView.EligibleBackends) != 1 {
		t.Fatalf("unexpected common view: %#v", result.Inputs.CommonView)
	}
	if result.Intent.ViewDigest != result.Inputs.CommonView.Digest ||
		result.Etcd.Intent.ViewDigest != result.Inputs.EtcdView.Digest ||
		result.Omni.Intent.ViewDigest != result.Inputs.OmniView.Digest ||
		!portableM522bPlansEquivalent(result.Etcd.Plan, result.Omni.Plan) ||
		result.Etcd.Instance.Budget != result.Omni.Instance.Budget ||
		result.Etcd.Instance.PolicySeed != result.Omni.Instance.PolicySeed {
		t.Fatalf("cross-target parent/projection/budget mismatch: %#v", result)
	}
	for target, execution := range map[string]portableM522bTargetExecution{
		"etcd": result.Etcd, "omni": result.Omni,
	} {
		if execution.Outcome.ExecutionStatus != controlexperiment.IntentExecutionValid ||
			execution.Outcome.IntentStatus != controlexperiment.IntentReachabilityReached ||
			len(execution.Report.Runs) != 1 || !execution.Report.Runs[0].Replay.Stable ||
			execution.Report.Runs[0].Workload == nil || execution.Report.Runs[0].Workload.Completed != 1 ||
			execution.Report.Runs[0].Termination != controlexperiment.RunTerminationConfigured ||
			execution.Report.Work.Model.Calls != 0 {
			t.Fatalf("%s execution did not complete the bounded intent: %#v", target, execution)
		}
		policy := execution.Report.Config.Runs[0].Policy
		if policy.Version != controlexperiment.BoundedActionClassPolicyVersion ||
			len(policy.SelectableActions) != 3+len(executionPlumbing(target)) {
			t.Fatalf("%s did not use the bounded policy: %#v", target, policy)
		}
		observed := make(map[control.ActionKind]int)
		for _, record := range execution.Bundle.Trace.Records {
			observed[record.Action.Kind]++
		}
		for _, action := range portableM522bActions() {
			if observed[action] == 0 {
				t.Fatalf("%s did not reach required action %s: %v", target, action, observed)
			}
		}
		allowed := make(map[control.ActionKind]bool)
		for _, action := range portableM522bActions() {
			allowed[action] = true
		}
		for _, action := range executionPlumbing(target) {
			allowed[action] = true
			if observed[action] == 0 {
				t.Fatalf("%s did not execute required target plumbing %s: %v", target, action, observed)
			}
		}
		for action := range observed {
			if !allowed[action] {
				t.Fatalf("%s selected an undeclared common/plumbing action: %v", target, observed)
			}
		}
	}
	if result.Etcd.Report.PSSID == result.Omni.Report.PSSID {
		t.Fatal("target-local PSS identities were incorrectly merged")
	}
	if result.Etcd.Report.Config.Admission == nil || result.Omni.Report.Config.Admission == nil ||
		len(result.Etcd.Report.Config.Admission.RequiredCapabilities) != 8 ||
		len(result.Omni.Report.Config.Admission.RequiredCapabilities) != 6 {
		t.Fatalf("target lowering did not retain its own capability boundary: etcd=%#v omni=%#v",
			result.Etcd.Report.Config.Admission, result.Omni.Report.Config.Admission)
	}
	withoutPlumbing, err := executePortableM522bEtcd(
		ctx, result.Inputs, result.Etcd.Intent, result.Etcd.Plan, result.Etcd.Instance, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if withoutPlumbing.Report.Runs[0].Termination != controlexperiment.RunTerminationPolicySurface ||
		withoutPlumbing.Report.Runs[0].ChargedDecisions != 0 ||
		withoutPlumbing.Outcome.IntentStatus != controlexperiment.IntentReachabilityNotReached ||
		len(withoutPlumbing.Outcome.MissingActions) != 3 {
		t.Fatalf("target-local Ready plumbing was not an explicit prerequisite: termination=%s decisions=%d outcome=%s missing=%v",
			withoutPlumbing.Report.Runs[0].Termination, withoutPlumbing.Report.Runs[0].ChargedDecisions,
			withoutPlumbing.Outcome.IntentStatus, withoutPlumbing.Outcome.MissingActions)
	}
	if result.Inputs.CommonView.Digest != "a119e7afb86c9c2144243e0e18652876d90a3ff507a68422820d572d198fed40" ||
		result.Intent.Digest != "e0dce20d88ef0a4bc35c6164b57daf4e465055a3b7c59738d0541b99a28cf5b9" ||
		result.Etcd.Plan.Digest != "3de18bfc94070ce7b15059f2b138aa5820b5c0bd324843848b29f6648bae6707" ||
		result.Omni.Plan.Digest != "dcecf35bded49a8815da54886bc117f6bddec4ddab5d741c8066fc4ba11fdee9" ||
		result.Etcd.Report.Digest != "9e644e9730e3d44333c142d3a36bc62bbbf2474ba3795cd39700802863f655d0" ||
		result.Etcd.Bundle.Digest != "1579a0f7ae11afc75b5ecbb875e4d842c02ff3cf6d72c5f4a75d205cff55f68c" ||
		result.Etcd.Bundle.Trace.Digest != "a463f0a3e8d22b2bc50cca09d8ab28ed72590dadbed662bf83dfeb0f017a38e8" ||
		result.Omni.Report.Digest != "bf10cdd5b05c2eb1e509a5c5da0e92a3880853d4e610a3cdc6a00c566cab7b91" ||
		result.Omni.Bundle.Digest != "94e0d1de99c9471b568b6a3742e889e02414182cab221be1379d134e1cb1acf6" ||
		result.Omni.Bundle.Trace.Digest != "4abe9c283e90fd0a8c275b6de4e3076f7a20176828d005c99c58514c357a72bb" {
		t.Fatal("M5.22b cross-target identity drift")
	}
	if durable.Ledger.CommonViewDigest != result.Inputs.CommonView.Digest ||
		durable.Ledger.ParentIntent.Digest != result.Intent.Digest ||
		len(durable.Ledger.Targets) != 2 ||
		durable.Ledger.Totals.Primary.WorkUnits !=
			result.Etcd.Report.Work.Primary.WorkUnits+result.Omni.Report.Work.Primary.WorkUnits ||
		durable.Ledger.Totals.Replay.WorkUnits !=
			result.Etcd.Report.Work.Replay.WorkUnits+result.Omni.Report.Work.Replay.WorkUnits {
		t.Fatalf("M5.22c ledger did not compose target-local campaigns: %#v", durable.Ledger)
	}
	encodedLedger, err := json.Marshal(durable.Ledger)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"\"trace\"", "\"qualification\"", "\"core_pss\""} {
		if strings.Contains(string(encodedLedger), forbidden) {
			t.Fatalf("composition ledger copied target artifact field %s", forbidden)
		}
	}
	for slot, campaign := range durable.Campaigns {
		if campaign.Recovery.Head.Sequence != 1 || len(campaign.Recovery.PlannedAttempts) != 1 ||
			campaign.Recovery.PlannedAttempts[0].Digest != campaign.Planned.Digest ||
			campaign.Recovery.Head.StopReason != controlexperiment.CampaignStopAttemptLimit {
			t.Fatalf("%s Campaign did not recover its planned/committed attempt: %#v", slot, campaign.Recovery)
		}
	}
	if durable.Ledger.Targets[0].PSSID == durable.Ledger.Targets[1].PSSID {
		t.Fatal("M5.22c ledger merged target-local PSS identities")
	}
	if durable.Ledger.Digest != "5ff91b9650c55046168c8c3dd4f8d30ba56749dcc60fd867164fe69058a93347" ||
		durable.Ledger.Targets[0].ArtifactDigest != "7542194c337ce33c471812940312809fefb33b12867e5ee75bae8a6e8315674b" ||
		durable.Ledger.Targets[1].ArtifactDigest != "dafb872ff2a40866ba352d48f983fd332e9dbd4a5068c115a347ed204e90a51d" {
		t.Fatalf("M5.22c durable composition identity drift: ledger=%s a=%s b=%s",
			durable.Ledger.Digest, durable.Ledger.Targets[0].ArtifactDigest,
			durable.Ledger.Targets[1].ArtifactDigest)
	}
	etcdCounts := selectedActionCounts(result.Etcd.Bundle)
	omniCounts := selectedActionCounts(result.Omni.Bundle)
	t.Logf("view=%s intent=%s etcd_plan=%s omni_plan=%s etcd_report=%s etcd_bundle=%s etcd_trace=%s etcd_decisions=%d etcd_states=%d etcd_primary=%d etcd_replay=%d etcd_invoke=%d etcd_deliver=%d etcd_temporal=%d etcd_effect=%d omni_report=%s omni_bundle=%s omni_trace=%s omni_decisions=%d omni_states=%d omni_primary=%d omni_replay=%d omni_invoke=%d omni_deliver=%d omni_temporal=%d",
		result.Inputs.CommonView.Digest, result.Intent.Digest,
		result.Etcd.Plan.Digest, result.Omni.Plan.Digest,
		result.Etcd.Report.Digest, result.Etcd.Bundle.Digest, result.Etcd.Bundle.Trace.Digest,
		result.Etcd.Report.Runs[0].ChargedDecisions, result.Etcd.Report.StateDiscovery.UniqueStates,
		result.Etcd.Report.Work.Primary.WorkUnits, result.Etcd.Report.Work.Replay.WorkUnits,
		etcdCounts[control.ActionInvoke], etcdCounts[control.ActionDeliverMessage],
		etcdCounts[control.ActionFireTemporal], etcdCounts[control.ActionCompleteEffect],
		result.Omni.Report.Digest, result.Omni.Bundle.Digest, result.Omni.Bundle.Trace.Digest,
		result.Omni.Report.Runs[0].ChargedDecisions, result.Omni.Report.StateDiscovery.UniqueStates,
		result.Omni.Report.Work.Primary.WorkUnits, result.Omni.Report.Work.Replay.WorkUnits,
		omniCounts[control.ActionInvoke], omniCounts[control.ActionDeliverMessage],
		omniCounts[control.ActionFireTemporal],
	)
	t.Logf("ledger=%s target_a_artifact=%s target_b_artifact=%s total_primary=%d total_replay=%d",
		durable.Ledger.Digest, durable.Ledger.Targets[0].ArtifactDigest,
		durable.Ledger.Targets[1].ArtifactDigest, durable.Ledger.Totals.Primary.WorkUnits,
		durable.Ledger.Totals.Replay.WorkUnits)

	tampered := durable.Campaigns["target-a"]
	artifactPath := filepath.Join(
		tampered.Directory, "artifacts", durable.Ledger.Targets[0].ArtifactDigest+".artifact",
	)
	if err := os.WriteFile(artifactPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := controlexperiment.RecoverCampaignDirectory(tampered.Directory, tampered.Config); err == nil {
		t.Fatal("M5.22c recovery accepted a modified target-local artifact")
	}
}

func selectedActionCounts(bundle controlexperiment.ExecutionBundle) map[control.ActionKind]int {
	counts := make(map[control.ActionKind]int)
	for _, record := range bundle.Trace.Records {
		counts[record.Action.Kind]++
	}
	return counts
}

func executionPlumbing(target string) []control.ActionKind {
	if target == "etcd" {
		return []control.ActionKind{control.ActionCompleteEffect}
	}
	return nil
}

func buildPortableM522bWorker(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo is required for the M5.22b cross-target integration test")
	}
	manifest := filepath.Join("..", "..", "adapters", "omnipaxosv2", "worker", "Cargo.toml")
	command := exec.Command("cargo", "build", "--locked", "--quiet", "--manifest-path", manifest)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build OmniPaxos worker: %v\n%s", err, output)
	}
	path, err := filepath.Abs(filepath.Join(
		"..", "..", "adapters", "omnipaxosv2", "worker", "target", "debug",
		"consensus-atlas-omnipaxos-worker",
	))
	if err != nil {
		t.Fatal(err)
	}
	return path
}
