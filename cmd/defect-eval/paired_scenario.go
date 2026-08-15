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
	"sort"
	"strings"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
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

func (config pairedScenarioLaunchConfig) validate() error {
	model := strings.TrimSpace(config.AgentModel)
	if config.SemanticInputPath == "" || config.AgentKeyFile == "" || model == "" ||
		model != config.AgentModel || strings.ContainsAny(model, " \t\r\n") || config.Timeout <= 0 {
		return errors.New("FORMAL_PAIRED_SCENARIO_LAUNCH_CONFIG_INVALID")
	}
	return nil
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

type pairedScenarioBinaryRunner func(
	context.Context,
	loadedFreshTrial,
	pairedScenarioLaunchConfig,
	string,
) (pairedScenarioFreshEvidence, error)

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
	if ctx == nil || current.trialID == "" || current.audit.Validate() != nil ||
		current.audit.TrialID != current.trialID || !pairedScenarioDigest(current.auditDigest) ||
		current.binaryDigest != current.audit.BinaryDigest ||
		current.binaryDigest != digestBytes(current.binary) || config.validate() != nil ||
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
		"-agent-model", config.AgentModel,
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

func executePairedScenarioTrials(
	ctx context.Context,
	inputs []loadedFreshTrial,
	variants map[string]defectbench.FormalVariant,
	config pairedScenarioLaunchConfig,
	artifactDir string,
	runner pairedScenarioBinaryRunner,
) (map[string]pairedScenarioFreshEvidence, error) {
	clean := filepath.Clean(artifactDir)
	if ctx == nil || len(inputs) == 0 || len(inputs) != len(variants) || runner == nil || config.validate() != nil ||
		artifactDir == "" || clean == "." || clean == string(filepath.Separator) {
		return nil, errors.New("FORMAL_PAIRED_SCENARIO_BATCH_INPUT_INVALID")
	}
	ordered := append([]loadedFreshTrial(nil), inputs...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].trialID < ordered[j].trialID })
	seen := make(map[string]bool, len(ordered))
	for _, current := range ordered {
		variant, ok := variants[current.trialID]
		if !ok || seen[current.trialID] || current.audit.Validate() != nil ||
			current.audit.TrialID != current.trialID || current.audit.SUTBuildIdentity != variant.ExpectedBuildID ||
			current.auditDigest != variant.ExpectedBuildAuditDigest ||
			current.binaryDigest != variant.ExpectedBinaryDigest ||
			current.audit.BinaryDigest != current.binaryDigest ||
			current.binaryDigest != digestBytes(current.binary) {
			return nil, errors.New("FORMAL_PAIRED_SCENARIO_BATCH_PREFLIGHT_FAILED")
		}
		seen[current.trialID] = true
	}
	if _, err := os.Lstat(clean); err == nil {
		return nil, errors.New("FORMAL_PAIRED_SCENARIO_OUTPUT_EXISTS")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(clean, 0o755); err != nil {
		return nil, err
	}
	result := make(map[string]pairedScenarioFreshEvidence, len(ordered))
	for _, current := range ordered {
		fresh, err := runner(ctx, current, config, filepath.Join(clean, current.trialID))
		if err != nil {
			return nil, fmt.Errorf("paired Scenario trial %s: %w", current.trialID, err)
		}
		variant := variants[current.trialID]
		if fresh.TrialID != current.trialID || fresh.BuildID != variant.ExpectedBuildID ||
			fresh.BuildAuditDigest != variant.ExpectedBuildAuditDigest ||
			fresh.BinaryDigest != variant.ExpectedBinaryDigest ||
			fresh.Root.RootMode != pairedScenarioFreshRootMode ||
			fresh.Root.RootRule != pairedScenarioFreshRootRule ||
			fresh.Scenario.Deterministic.Bundle.Qualification.Manifest.BuildID != variant.ExpectedBuildID ||
			fresh.Scenario.Agent.Bundle.Qualification.Manifest.BuildID != variant.ExpectedBuildID {
			return nil, errors.New("FORMAL_PAIRED_SCENARIO_BATCH_EVIDENCE_MISMATCH")
		}
		result[current.trialID] = fresh
	}
	return result, nil
}
