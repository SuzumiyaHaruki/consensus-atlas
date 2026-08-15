package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	pairedScenarioPlannerDeterministic = "deterministic"
	pairedScenarioPlannerAgent         = "agent"
	pairedScenarioBinaryStrategy       = "etcdraft-a8-paired-scenario-v1"
	pairedScenarioFreshRootMode        = "sut-local-fresh"
	pairedScenarioFreshRootRule        = "first-invoke-crash-restart-actions-v1"
)

type pairedScenarioLaunchConfig struct {
	SemanticInputPath string
	AgentKeyFile      string
	AgentModel        string
	Timeout           time.Duration
}

type pairedScenarioRootSummary struct {
	Classification string          `json:"classification"`
	TargetID       string          `json:"target_id"`
	TargetIdentity string          `json:"target_identity_digest"`
	SemanticMode   string          `json:"semantic_exposure"`
	RootMode       string          `json:"root_mode"`
	SourceDigest   string          `json:"source_bundle_digest"`
	CorpusDigest   string          `json:"root_corpus_digest"`
	RootRule       string          `json:"root_selection_rule"`
	RootDigest     string          `json:"root_trace_digest"`
	RootDecisions  int             `json:"root_decisions"`
	SameTrace      bool            `json:"same_trace"`
	Deterministic  json.RawMessage `json:"deterministic"`
	Agent          json.RawMessage `json:"agent"`
}

type pairedScenarioFreshEvidence struct {
	TrialID          string
	BuildID          string
	BuildAuditDigest string
	BinaryDigest     string
	Root             pairedScenarioRootSummary
	Scenario         pairedScenarioTrialEvidence
}

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

func runPairedScenarioBinary(
	ctx context.Context,
	current loadedFreshTrial,
	config pairedScenarioLaunchConfig,
	directory string,
) (pairedScenarioFreshEvidence, error) {
	clean := filepath.Clean(directory)
	model := strings.TrimSpace(config.AgentModel)
	if ctx == nil || current.trialID == "" || current.audit.Validate() != nil ||
		current.audit.TrialID != current.trialID || !pairedScenarioDigest(current.auditDigest) ||
		current.binaryDigest != current.audit.BinaryDigest ||
		current.binaryDigest != digestBytes(current.binary) ||
		config.SemanticInputPath == "" || config.AgentKeyFile == "" || model == "" ||
		model != config.AgentModel || strings.ContainsAny(model, " \t\r\n") || config.Timeout <= 0 ||
		directory == "" || clean == "." || clean == string(filepath.Separator) {
		return pairedScenarioFreshEvidence{}, errors.New("FORMAL_PAIRED_SCENARIO_LAUNCH_INPUT_INVALID")
	}
	if _, err := os.Lstat(clean); err == nil {
		return pairedScenarioFreshEvidence{}, errors.New("FORMAL_PAIRED_SCENARIO_OUTPUT_EXISTS")
	} else if !errors.Is(err, os.ErrNotExist) {
		return pairedScenarioFreshEvidence{}, err
	}
	temporary, err := os.MkdirTemp("", "consensus-atlas-paired-eval-*")
	if err != nil {
		return pairedScenarioFreshEvidence{}, err
	}
	defer os.RemoveAll(temporary)
	binaryPath := filepath.Join(temporary, "sut")
	if err := os.WriteFile(binaryPath, current.binary, 0o700); err != nil {
		return pairedScenarioFreshEvidence{}, err
	}
	runContext, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()
	command := exec.CommandContext(runContext, binaryPath,
		"-strategy", pairedScenarioBinaryStrategy,
		"-campaign-dir", clean,
		"-semantic-input", config.SemanticInputPath,
		"-agent-key-file", config.AgentKeyFile,
		"-agent-model", model,
	)
	command.Env = []string{"TZ=UTC"}
	combined, err := command.CombinedOutput()
	if err != nil {
		if runContext.Err() != nil {
			return pairedScenarioFreshEvidence{}, fmt.Errorf(
				"paired Scenario for %s timed out: %w", current.trialID, runContext.Err(),
			)
		}
		return pairedScenarioFreshEvidence{}, fmt.Errorf(
			"paired Scenario for %s failed: %w: %s",
			current.trialID, err, strings.TrimSpace(string(combined)),
		)
	}
	scenario, root, err := loadFreshPairedScenarioTrialEvidence(clean)
	if err != nil {
		return pairedScenarioFreshEvidence{}, err
	}
	for _, arm := range []pairedScenarioArmEvidence{scenario.Deterministic, scenario.Agent} {
		if arm.Bundle.Qualification.Manifest.BuildID != current.audit.SUTBuildIdentity {
			return pairedScenarioFreshEvidence{}, errors.New("FORMAL_PAIRED_SCENARIO_BUILD_IDENTITY_MISMATCH")
		}
	}
	return pairedScenarioFreshEvidence{
		TrialID: current.trialID, BuildID: current.audit.SUTBuildIdentity,
		BuildAuditDigest: current.auditDigest, BinaryDigest: current.binaryDigest,
		Root: root, Scenario: scenario,
	}, nil
}

func loadFreshPairedScenarioTrialEvidence(
	directory string,
) (pairedScenarioTrialEvidence, pairedScenarioRootSummary, error) {
	scenario, err := loadPairedScenarioTrialEvidence(directory)
	if err != nil {
		return pairedScenarioTrialEvidence{}, pairedScenarioRootSummary{}, err
	}
	var root pairedScenarioRootSummary
	if err := readStrictJSON(filepath.Join(directory, "summary.json"), &root); err != nil {
		return pairedScenarioTrialEvidence{}, pairedScenarioRootSummary{}, err
	}
	if root.Classification != "public-calibration-not-agent-effectiveness-holdout-or-correctness" ||
		root.TargetID != scenario.TargetID || root.TargetIdentity != scenario.TargetIdentity ||
		root.SemanticMode != "full" || root.RootMode != pairedScenarioFreshRootMode ||
		root.RootRule != pairedScenarioFreshRootRule || root.RootDecisions <= 0 ||
		!pairedScenarioDigest(root.SourceDigest) || !pairedScenarioDigest(root.CorpusDigest) ||
		!pairedScenarioDigest(root.RootDigest) || len(root.Deterministic) == 0 || len(root.Agent) == 0 ||
		root.SameTrace != (scenario.Deterministic.Bundle.Trace.Digest == scenario.Agent.Bundle.Trace.Digest) {
		return pairedScenarioTrialEvidence{}, pairedScenarioRootSummary{},
			errors.New("FORMAL_PAIRED_SCENARIO_FRESH_ROOT_INVALID")
	}
	return scenario, root, nil
}

func pairedScenarioDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && hex.EncodeToString(decoded) == value
}
