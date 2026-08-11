package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestM522dBlindMockAndBaselineUseOneDurableCommonProposal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	result, err := runPortableM522dPreferenceComparison(
		ctx, buildPortableM522bWorker(t), filepath.Join(t.TempDir(), "comparison"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Agent.ModelCall == nil || result.Agent.ModelResult == nil || result.Agent.Request == nil ||
		result.Agent.ModelResult.Status != controlexperiment.CampaignModelCallCompleted ||
		result.Agent.ModelResult.Work != (controlexperiment.ModelWork{
			Calls: 1, InputTokens: 4, OutputTokens: 3, TotalTokens: 7,
		}) {
		t.Fatalf("blind mock did not cross the durable model boundary once: %#v", result.Agent)
	}
	request := strings.ToLower(string(result.Agent.Request.RequestBytes))
	for _, forbidden := range []string{
		"etcd", "raft", "omnipaxos", "paxos", "adapter_id", "profile_id", "pss", "oracle",
		"target-a", "target-b", "campaign_id", "campaign_request_digest",
	} {
		if strings.Contains(request, forbidden) {
			t.Fatalf("blind common request exposed target/campaign token %q", forbidden)
		}
	}
	if result.Agent.ModelCall.ViewDigest != result.Inputs.CommonView.Digest ||
		result.Baseline.Planning.Intent.Digest == result.Agent.Planning.Intent.Digest ||
		!reflect.DeepEqual(result.Baseline.Planning.Intent.Must, result.Agent.Planning.Intent.Must) ||
		!portableM522dPlansExecutionEquivalent(result.Baseline.Planning.EtcdPlan, result.Agent.Planning.EtcdPlan) ||
		!portableM522dPlansExecutionEquivalent(result.Baseline.Planning.OmniPlan, result.Agent.Planning.OmniPlan) ||
		result.Baseline.Planning.EtcdPlan.Digest == result.Agent.Planning.EtcdPlan.Digest ||
		result.Baseline.Planning.OmniPlan.Digest == result.Agent.Planning.OmniPlan.Digest {
		t.Fatalf("proposal authority or trusted lowering drifted: baseline=%#v agent=%#v",
			result.Baseline.Planning, result.Agent.Planning)
	}
	if result.Baseline.Etcd.Report.Digest != result.Agent.Etcd.Report.Digest ||
		result.Baseline.Etcd.Bundle.Digest != result.Agent.Etcd.Bundle.Digest ||
		result.Baseline.Etcd.Bundle.Trace.Digest != result.Agent.Etcd.Bundle.Trace.Digest ||
		result.Baseline.Omni.Report.Digest != result.Agent.Omni.Report.Digest ||
		result.Baseline.Omni.Bundle.Digest != result.Agent.Omni.Bundle.Digest ||
		result.Baseline.Omni.Bundle.Trace.Digest != result.Agent.Omni.Bundle.Trace.Digest {
		t.Fatal("preference-only mock changed execution on the one-backend common surface")
	}
	if result.Baseline.Ledger.Totals.Primary.WorkUnits != 75 ||
		result.Baseline.Ledger.Totals.Replay.WorkUnits != 75 ||
		result.Baseline.Ledger.Totals.Model != (controlexperiment.ModelWork{}) ||
		result.Agent.Ledger.Totals.Primary.WorkUnits != 75 ||
		result.Agent.Ledger.Totals.Replay.WorkUnits != 75 ||
		result.Agent.Ledger.Totals.Model != result.Agent.ModelResult.Work {
		t.Fatalf("comparison accounting drifted: baseline=%#v agent=%#v",
			result.Baseline.Ledger.Totals, result.Agent.Ledger.Totals)
	}
	if len(result.Agent.Campaigns.Etcd.Recovery.ModelCalls) != 1 ||
		result.Agent.Campaigns.Etcd.Recovery.ModelCalls[0].Status !=
			controlexperiment.CampaignModelCallCompleted ||
		len(result.Agent.Campaigns.Omni.Recovery.ModelCalls) != 0 ||
		result.Agent.Campaigns.Etcd.Config.PlannerMode != controlexperiment.CampaignPlannerDurableCall ||
		result.Agent.Campaigns.Omni.Config.PlannerMode != controlexperiment.CampaignPlannerZeroModel {
		t.Fatalf("model work was not owned exactly once: %#v", result.Agent.Campaigns)
	}
	encodedLedger, err := json.Marshal(result.Agent.Ledger)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"\"trace\"", "\"qualification\"", "\"core_pss\""} {
		if strings.Contains(string(encodedLedger), forbidden) {
			t.Fatalf("comparison ledger copied target artifact field %s", forbidden)
		}
	}
	if result.Baseline.Ledger.Digest != "2aa1365af08189c097a46d9d34b5437dff7bfe45daee1479ea3ccbfe2c10f536" ||
		result.Agent.Ledger.Digest != "3388d340576cf57b3b90ea4b72beedf73829242d196bdb9632db080cfb7142da" ||
		result.Agent.ModelCall.Digest != "98a42ed72e42a7397a1c60e3901fe2138c126583e4ed54405d267ff50ad6cddd" ||
		result.Agent.ModelResult.Digest != "55fa91138417f052ebe3ef0328c0a086d591ec782d5e2376f4442b080e6793d1" ||
		result.Agent.Planning.Intent.Digest != "301b11731eeb07aa23bd2f6c89da89af2f66401720094d01d7b722e650ebe81e" ||
		result.Agent.Planning.EtcdPlan.Digest != "3518f0a51c9b9752c65628d97d2518d626713a4b93e6891adeee2d1e837fc129" ||
		result.Agent.Planning.OmniPlan.Digest != "2bcab96ceef127548bae242ab583e2b95b009bb624cb31ff84bf02d54a355a2d" {
		t.Fatal("M5.22d durable comparison identity drift")
	}
	t.Logf("baseline_ledger=%s agent_ledger=%s model_call=%s model_result=%s baseline_parent=%s agent_parent=%s etcd_plan=%s/%s omni_plan=%s/%s etcd_report=%s omni_report=%s",
		result.Baseline.Ledger.Digest, result.Agent.Ledger.Digest,
		result.Agent.ModelCall.Digest, result.Agent.ModelResult.Digest,
		result.Baseline.Planning.Intent.Digest, result.Agent.Planning.Intent.Digest,
		result.Baseline.Planning.EtcdPlan.Digest, result.Agent.Planning.EtcdPlan.Digest,
		result.Baseline.Planning.OmniPlan.Digest, result.Agent.Planning.OmniPlan.Digest,
		result.Agent.Etcd.Report.Digest, result.Agent.Omni.Report.Digest)
}

func portableM522dPlansExecutionEquivalent(
	left controlexperiment.CompiledIntentPlanV2,
	right controlexperiment.CompiledIntentPlanV2,
) bool {
	left.IntentDigest, left.Digest, left.CompilerWork = "", "", controlexperiment.IntentCompilerWork{}
	right.IntentDigest, right.Digest, right.CompilerWork = "", "", controlexperiment.IntentCompilerWork{}
	return reflect.DeepEqual(left, right)
}
