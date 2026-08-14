package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	pairedScenarioPlannerDeterministic = "deterministic"
	pairedScenarioPlannerAgent         = "agent"
)

// pairedScenarioArmEvidence is the evaluator-side view of one completed A8
// arm. CampaignWork deliberately remains separate from Bundle.Work: the
// former also charges source construction, frontier reconstruction and model
// planning, while the latter covers only the qualified terminal execution.
type pairedScenarioArmEvidence struct {
	Planner      string
	Config       controlexperiment.CampaignConfig
	Checkpoint   controlexperiment.CampaignCheckpoint
	CampaignWork controlexperiment.WorkLedger
	Bundle       controlexperiment.ExecutionBundle
}

type pairedScenarioTrialEvidence struct {
	TargetID       string
	TargetIdentity string
	Deterministic  pairedScenarioArmEvidence
	Agent          pairedScenarioArmEvidence
}

type pairedScenarioArtifactEnvelope struct {
	Episode          json.RawMessage `json:"episode"`
	SemanticExposure json.RawMessage `json:"semantic_exposure"`
	TestingEvidence  json.RawMessage `json:"testing_evidence"`
}

func loadPairedScenarioTrialEvidence(directory string) (pairedScenarioTrialEvidence, error) {
	deterministic, err := loadPairedScenarioArmEvidence(
		filepath.Join(directory, pairedScenarioPlannerDeterministic), pairedScenarioPlannerDeterministic,
	)
	if err != nil {
		return pairedScenarioTrialEvidence{}, err
	}
	agent, err := loadPairedScenarioArmEvidence(
		filepath.Join(directory, pairedScenarioPlannerAgent), pairedScenarioPlannerAgent,
	)
	if err != nil {
		return pairedScenarioTrialEvidence{}, err
	}
	if deterministic.Config.TargetID != agent.Config.TargetID ||
		deterministic.Config.TargetIdentityDigest != agent.Config.TargetIdentityDigest ||
		!samePairedScenarioExecutionBudget(deterministic.Config.Budget, agent.Config.Budget) {
		return pairedScenarioTrialEvidence{}, errors.New("FORMAL_PAIRED_SCENARIO_ARM_MISMATCH")
	}
	return pairedScenarioTrialEvidence{
		TargetID: deterministic.Config.TargetID, TargetIdentity: deterministic.Config.TargetIdentityDigest,
		Deterministic: deterministic, Agent: agent,
	}, nil
}

func loadPairedScenarioArmEvidence(
	directory string,
	planner string,
) (pairedScenarioArmEvidence, error) {
	recovered, err := controlexperiment.RecoverStoredCampaignDirectory(directory)
	if err != nil || recovered.Failure != nil || len(recovered.OrphanArtifactDigests) != 0 ||
		len(recovered.PendingRelativeFilePaths) != 0 || recovered.Head.Sequence != 1 ||
		recovered.Head.StopReason != controlexperiment.CampaignStopAttemptLimit ||
		recovered.Head.Record == nil || recovered.Head.Record.Outcome != controlexperiment.CampaignAttemptCompleted {
		return pairedScenarioArmEvidence{}, errors.New("FORMAL_PAIRED_SCENARIO_CAMPAIGN_INVALID")
	}
	artifact, err := recovered.ReadAttemptArtifact(1)
	if err != nil {
		return pairedScenarioArmEvidence{}, err
	}
	var envelope pairedScenarioArtifactEnvelope
	decoder := json.NewDecoder(bytes.NewReader(artifact))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return pairedScenarioArmEvidence{}, errors.New("FORMAL_PAIRED_SCENARIO_ARTIFACT_INVALID")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return pairedScenarioArmEvidence{}, errors.New("FORMAL_PAIRED_SCENARIO_ARTIFACT_INVALID")
	}
	var episode struct {
		Planner string `json:"planner"`
	}
	var testing struct {
		Bundle controlexperiment.ExecutionBundle `json:"execution_bundle"`
	}
	var exposure string
	if json.Unmarshal(envelope.Episode, &episode) != nil || episode.Planner != planner ||
		json.Unmarshal(envelope.SemanticExposure, &exposure) != nil || exposure != "full" ||
		json.Unmarshal(envelope.TestingEvidence, &testing) != nil || testing.Bundle.Validate() != nil ||
		testing.Bundle.Trace.ManifestDigest != recovered.Config.TargetIdentityDigest {
		return pairedScenarioArmEvidence{}, errors.New("FORMAL_PAIRED_SCENARIO_ARTIFACT_INVALID")
	}
	if planner == pairedScenarioPlannerDeterministic {
		if recovered.Head.Totals.Model != (controlexperiment.ModelWork{}) {
			return pairedScenarioArmEvidence{}, errors.New("FORMAL_PAIRED_SCENARIO_MODEL_WORK_INVALID")
		}
	} else if recovered.Head.Totals.Model.Calls <= 0 || recovered.Head.Totals.Model.TotalTokens <= 0 {
		return pairedScenarioArmEvidence{}, errors.New("FORMAL_PAIRED_SCENARIO_MODEL_WORK_INVALID")
	}
	return pairedScenarioArmEvidence{
		Planner: planner, Config: recovered.Config, Checkpoint: recovered.Head,
		CampaignWork: recovered.Head.Totals, Bundle: testing.Bundle,
	}, nil
}

func samePairedScenarioExecutionBudget(
	left controlexperiment.CampaignLogicalBudget,
	right controlexperiment.CampaignLogicalBudget,
) bool {
	return left.MaxAttempts == right.MaxAttempts &&
		left.MaxPrimarySchedulerDecisions == right.MaxPrimarySchedulerDecisions &&
		left.MaxPrimaryWorkUnits == right.MaxPrimaryWorkUnits &&
		left.MaxReplayWorkUnits == right.MaxReplayWorkUnits
}
