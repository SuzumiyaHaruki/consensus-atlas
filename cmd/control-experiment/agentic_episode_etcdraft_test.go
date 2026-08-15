package main

import (
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
	composition, err := prepareAgenticEpisodeComposition(ctx, controlExperimentOptions{
		Target:        "etcdraft-v2",
		SemanticInput: "../../plans/agent/etcdraft-agentic-calibration-v1.json",
		AgentKeyFile:  "fixture-key.txt", AgentModel: openRouterFixtureModel,
	})
	if err != nil || composition.Target.ID != "etcdraft-v2" ||
		composition.Budget.MaxRiskCalls != 3 || composition.Budget.MaxScenarioCalls != 1 ||
		composition.Budget.MaxTotalCalls != 4 || composition.Budget.MaxObservedTokens != 25000 ||
		composition.Client.Model != openRouterFixtureModel ||
		composition.Client.ReasoningEffort != "low" || composition.Client.MaxOutputTokens != 4096 ||
		composition.Client.MaxRetries != inputs.experiment.ModelMaxRetries {
		t.Fatalf("etcd/raft registry composition drifted: %#v err=%v", composition, err)
	}
}
