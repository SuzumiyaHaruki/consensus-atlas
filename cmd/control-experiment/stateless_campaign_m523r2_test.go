package main

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestM523R2FrozenAgentEvidenceFitsPlannerFreeStatelessCampaign(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	_, source, err := etcdraftExecution(ctx, "workload-risk-witness-calibration", 64, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	corpus := readM521hJSONFile[controlexperiment.StatelessRootCorpus](
		t, "../../"+m523gCorpusPath, 64<<10,
	)
	if corpus.Validate(source) != nil {
		t.Fatal("frozen root corpus no longer binds its source")
	}
	summary := readM521hJSONFile[m523gSummary](
		t, m523gExperimentDirectory+"/summary.json", 64<<10,
	)
	discovery := readM521hJSONFile[controlexperiment.StatelessCorpusDiscovery](
		t, m523gExperimentDirectory+"/agent-discovery.json", 64<<10,
	)
	if discovery.ValidateStructure() != nil || discovery.Digest != summary.AgentDiscoveryDigest ||
		discovery.CorpusDigest != corpus.Digest {
		t.Fatal("frozen Agent discovery is not self-consistent")
	}
	knowledge, err := etcdraftStatelessAgentKnowledge()
	if err != nil {
		t.Fatal(err)
	}
	method, err := controlexperiment.NewStatelessAgentTraversalMethod(
		"etcdraft-m5-23g-deepseek", "deepseek-v4-flash-frontier-order", knowledge,
	)
	if err != nil || method.Digest != summary.MethodDigest || discovery.MethodDigest != method.Digest {
		t.Fatalf("frozen Agent method drifted: %#v/%v", method, err)
	}
	want := m523r2ExpectedWork(source.Work, discovery, summary.ModelWork)
	spec, err := controlexperiment.NewStatelessCampaignSpec(controlexperiment.StatelessCampaignSpec{
		ID: "etcdraft-m5-23r2-agent-migration", TargetID: "etcdraft-v2",
		TargetIdentityDigest: source.Identity.ManifestDigest,
		Methods:              []controlexperiment.StatelessTraversalMethod{method},
		CorpusDigest:         corpus.Digest, SourceBundleDigest: source.Digest,
		SourceManifestDigest: source.Identity.ManifestDigest, SourceWork: source.Work,
		RootCount: len(corpus.Roots), MaxDepth: 2, MaxWorkItemsPerRoot: 6,
		MaxSearchWorkUnitsPerRoot: 1500,
		Budget: controlexperiment.StatelessCampaignAttemptBudget{
			MaxPrimarySchedulerDecisions: want.Primary.SchedulerDecisions,
			MaxPrimaryWorkUnits:          want.Primary.WorkUnits,
			MaxReplayWorkUnits:           want.Replay.WorkUnits,
			MaxModelCalls:                summary.ModelWork.Calls,
			MaxModelTokens:               summary.ModelWork.TotalTokens,
		},
	})
	if err != nil || spec.ValidateInputs([]controlexperiment.StatelessTraversalMethod{method}, corpus, source) != nil {
		t.Fatalf("frozen Agent inputs do not fit the Campaign contract: %#v/%v", spec, err)
	}
	config, err := controlexperiment.NewStatelessCampaignConfig(
		"etcdraft-m5-23r2-agent-campaign", spec, 60_000,
	)
	if err != nil || config.PlannerMode != controlexperiment.CampaignPlannerNone ||
		config.AttemptInputMode != controlexperiment.CampaignInputBaseRequest ||
		config.Budget.MaxModelCalls != summary.ModelWork.Calls {
		t.Fatalf("Stateless Campaign retained legacy Planner mode: %#v/%v", config, err)
	}
	head, err := controlexperiment.NewCampaignCheckpoint(config)
	if err != nil {
		t.Fatal(err)
	}
	request, err := controlexperiment.NewCampaignAttemptRequest(config, head)
	if err != nil {
		t.Fatal(err)
	}
	audits := make([]controlexperiment.StatelessAgentCallAudit, 0, statelessAgentMaxCalls)
	callOrdinal := 0
	for _, root := range corpus.Roots {
		agent := readM521hJSONFile[controlexperiment.StatelessAgentTraversalResult](
			t, fmt.Sprintf("%s/roots/%s-agent-result.json", m523gExperimentDirectory, root.ID), 128<<10,
		)
		for range agent.Records {
			callOrdinal++
			callDirectory := fmt.Sprintf(
				"%s/model-calls/%03d-%s", m523gExperimentDirectory, callOrdinal, root.ID,
			)
			intent := readM521hJSONFile[controlexperiment.StatelessAgentCallIntent](
				t, callDirectory+"/intent.json", 128<<10,
			)
			dispatch := readM521hJSONFile[controlexperiment.StatelessAgentCallDispatch](
				t, callDirectory+"/dispatch.json", 16<<10,
			)
			result := readM521hJSONFile[controlexperiment.StatelessAgentCallResult](
				t, callDirectory+"/result.json", 32<<10,
			)
			audit, auditErr := controlexperiment.NewStatelessAgentCallAudit(intent, &dispatch, &result)
			if auditErr != nil {
				t.Fatal(auditErr)
			}
			audits = append(audits, audit)
		}
	}
	artifact, err := controlexperiment.NewStatelessCampaignAttemptArtifact(
		request, spec, controlexperiment.StatelessCampaignExecution{
			Discovery: &discovery, SearchWork: discovery.SearchWork,
			QualifiedExecutionWork: discovery.QualifiedExecutionWork,
			ModelWork:              summary.ModelWork, AgentCalls: audits,
		},
	)
	if err != nil || artifact.ValidateInputs(request) != nil || artifact.Work != want ||
		artifact.Discovery == nil || artifact.Discovery.Digest != summary.AgentDiscoveryDigest {
		t.Fatalf("frozen Agent evidence did not migrate exactly: %#v/%v", artifact, err)
	}
}

func m523r2ExpectedWork(
	source controlexperiment.WorkLedger,
	discovery controlexperiment.StatelessCorpusDiscovery,
	model controlexperiment.ModelWork,
) controlexperiment.WorkLedger {
	total := source
	addPhaseM523r2 := func(target *controlexperiment.PhaseWork, delta controlexperiment.PhaseWork) {
		target.SetupAttempts += delta.SetupAttempts
		target.RuntimeInitializations += delta.RuntimeInitializations
		target.PrepareActions += delta.PrepareActions
		target.SchedulerDecisions += delta.SchedulerDecisions
		target.WorkUnits = target.SetupAttempts + target.PrepareActions + target.SchedulerDecisions
	}
	addPhaseM523r2(&total.Primary, discovery.SearchWork.FrontierReconstruction)
	addPhaseM523r2(&total.Primary, discovery.SearchWork.ChildMaterialization)
	addPhaseM523r2(&total.Primary, discovery.SearchWork.ChildVerification)
	addPhaseM523r2(&total.Primary, discovery.QualifiedExecutionWork.Primary)
	addPhaseM523r2(&total.Replay, discovery.QualifiedExecutionWork.Replay)
	total.Model = model
	if !reflect.DeepEqual(total.Resources, source.Resources) {
		panic("resource accounting drift")
	}
	return total
}
