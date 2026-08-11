package main

import (
	"reflect"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
	raftfamily "github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic/raft"
)

const m521jExperimentDirectory = "../../benchmarks/experiments/etcdraft-v2-risk-witness-m5.21j"

func TestEtcdraftM521jReprojectsFrozenBundlesThroughRiskWitness(t *testing.T) {
	const archive = "../../benchmarks/experiments/etcdraft-v2-planner-gate-m5.21f/campaign.tar.gz"
	members := []string{
		"campaign/artifacts/4b8fb115689d2dee079e813780b640aa2d0b357af3e471b7385b1b078d7d8472.artifact",
		"campaign/artifacts/6e496ffe7d354b6b181306fc02444aa20344a7c2bda21b3eb1e32d158a73cdc3.artifact",
	}
	bundles := make([]controlexperiment.ExecutionBundle, 0, 3)
	for _, member := range members {
		artifact, err := decodeEtcdraftCampaignArtifact(
			readArchivedCampaignMember(t, archive, member, 2<<20),
		)
		if err != nil || artifact.Bundle == nil {
			t.Fatalf("decode %s: %v", member, err)
		}
		bundles = append(bundles, *artifact.Bundle)
	}
	bundles = append(bundles, readM521hGzipJSON[controlexperiment.ExecutionBundle](
		t, m521hExperimentDirectory+"/uniform-bundle.json.gz", 2<<20,
	))
	ids := []string{"action-class-attempt-1", "action-class-attempt-2", "uniform-attempt-2"}
	results := make([]semantic.RiskWitnessResult, len(bundles))
	for index, bundle := range bundles {
		result, err := newEtcdraftLeaderChangeRiskWitness(ids[index], bundle)
		if err != nil {
			t.Fatalf("project %s: %v", ids[index], err)
		}
		results[index] = result
	}
	spec, err := raftfamily.LeaderChangeWithInflightProposalWitness()
	if err != nil {
		t.Fatal(err)
	}
	comparison := m521jComparison{
		SchemaVersion:               "consensus-atlas/risk-witness-corpus/v1",
		SourceCampaignArchiveSHA256: "a0ccca92d064f78cc07f3d0a3edb63d77a9ef1b866b9283ee5cdead965534afd",
		Spec:                        spec,
		ProjectorID:                 etcdraftLeaderChangeRiskWitnessProjectorID,
		NewModelCalls:               0,
		NewSUTExecutions:            0,
		Results:                     results,
		Summary: m521jSummary{
			Executions: 3, Reached: 0, NotReached: 3,
			Missing: []m521jMissingCount{
				{MilestoneID: raftfamily.MilestoneWorkloadInvokedAtCoordinator, Executions: 3},
				{MilestoneID: raftfamily.MilestoneCoordinatorChangedInflight, Executions: 3},
				{MilestoneID: raftfamily.MilestoneOldCoordinatorRestarted, Executions: 3},
			},
		},
		Classification: "existing-replay-stable-evidence-all-witness-not-reached-no-method-advantage-claim",
	}
	for index, result := range comparison.Results {
		if err := result.Validate(spec); err != nil {
			t.Fatalf("result %d invalid: %v", index+1, err)
		}
		if result.Status != semantic.RiskWitnessNotReached ||
			result.ReasonCode != semantic.RiskWitnessReasonMilestoneMissing ||
			len(result.Milestones) != 0 || len(result.SatisfiedMilestones) != 0 ||
			len(result.MissingMilestones) != 3 {
			t.Fatalf("result %d unexpectedly reached semantic risk: %#v", index+1, result)
		}
	}
	checked := readM521hJSONFile[m521jComparison](
		t, m521jExperimentDirectory+"/comparison.json", 1<<20,
	)
	if !reflect.DeepEqual(checked, comparison) {
		t.Fatalf("checked M5.21j comparison drifted: checked=%#v fresh=%#v", checked, comparison)
	}
}

type m521jMissingCount struct {
	MilestoneID string `json:"milestone_id"`
	Executions  int    `json:"executions"`
}

type m521jSummary struct {
	Executions int                 `json:"executions"`
	Reached    int                 `json:"reached"`
	NotReached int                 `json:"not_reached"`
	Missing    []m521jMissingCount `json:"missing"`
}

type m521jComparison struct {
	SchemaVersion               string                       `json:"schema_version"`
	SourceCampaignArchiveSHA256 string                       `json:"source_campaign_archive_sha256"`
	Spec                        semantic.RiskWitnessSpec     `json:"spec"`
	ProjectorID                 string                       `json:"projector_id"`
	NewModelCalls               int                          `json:"new_model_calls"`
	NewSUTExecutions            int                          `json:"new_sut_executions"`
	Results                     []semantic.RiskWitnessResult `json:"results"`
	Summary                     m521jSummary                 `json:"summary"`
	Classification              string                       `json:"classification"`
}
