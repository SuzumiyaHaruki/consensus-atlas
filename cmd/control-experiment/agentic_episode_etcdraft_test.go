package main

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
)

func TestEtcdraftBindingUsesCommonAgenticEpisodeContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), controlExperimentTestTimeout(180*time.Second))
	defer cancel()
	inputs, err := prepareEtcdraftAgenticEpisode(
		ctx, "", "../../plans/agent/etcdraft-agentic-calibration-v1.json",
		fixtureOpenRouterIntentClient(),
	)
	if err != nil {
		t.Fatal(err)
	}
	target, err := newEtcdraftAgenticEpisodeTarget(inputs)
	if err != nil || target.validate() != nil || target.ID != "etcdraft-v2" ||
		target.ObservationProjector.ID() != etcdraftv2.ObservationProjectionID ||
		len(target.ObservationProjector.Capabilities()) == 0 || len(target.Actions) == 0 ||
		target.ScenarioInputs == nil || target.Execute == nil {
		t.Fatalf("etcd/raft did not satisfy common Agentic Episode contract: %#v err=%v", target, err)
	}
	if len(target.Surface.Nodes) != 3 || len(target.Surface.Workload.Invocations) != 1 ||
		!bytes.Contains(target.Surface.Workload.Invocations[0].InputJSON, []byte("propose")) ||
		target.Surface.FaultAllowance.MaxCrashes != 1 ||
		len(target.Surface.TemporalKinds) == 0 || !target.Surface.Runtime.StrictReplay {
		t.Fatalf("etcd/raft dynamic Agent surface was not derived from active inputs: %#v", target.Surface)
	}
	composition, err := prepareAgenticEpisodeComposition(ctx, controlExperimentOptions{
		Target:        "etcdraft-v2",
		SemanticInput: "../../plans/agent/etcdraft-agentic-calibration-v1.json",
		AgentKeyFile:  "fixture-key.txt", AgentModel: openRouterFixtureModel,
	})
	if err != nil || composition.Target.ID != "etcdraft-v2" ||
		composition.Budget.MaxRiskCalls != 3 || composition.Budget.MaxScenarioCalls != 3 ||
		composition.Budget.MaxScenarioPlanSteps != 4 ||
		composition.Budget.MaxTotalCalls != 6 || composition.Budget.MaxObservedTokens != 50000 ||
		composition.Client.Model != openRouterFixtureModel ||
		composition.Client.ReasoningEffort != "low" || composition.Client.MaxOutputTokens != 32000 ||
		composition.Client.MaxRetries != inputs.experiment.ModelMaxRetries {
		t.Fatalf("etcd/raft registry composition drifted: %#v err=%v", composition, err)
	}
	investigation, err := agenticInvestigationBudgetFromEpisode(3, composition.Budget)
	if err != nil || investigation.MaxEpisodes != 3 ||
		investigation.MaxModelCalls != 3*composition.Budget.MaxTotalCalls ||
		investigation.MaxModelTokens != 3*composition.Budget.MaxObservedTokens ||
		investigation.MaxRuntimeDecisionAllowance != 3*composition.Budget.MaxRuntimeDecisions {
		t.Fatalf("Investigation budget did not scale the existing episode contract: %#v/%v",
			investigation, err)
	}
}
