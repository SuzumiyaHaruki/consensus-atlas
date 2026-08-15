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

func runFormalPairedScenarioBatch(
	ctx context.Context,
	contractPath string,
	exposurePath string,
	inputsPath string,
	config pairedScenarioLaunchConfig,
	artifactDir string,
	runner pairedScenarioBinaryRunner,
) (map[string]pairedScenarioFreshEvidence, error) {
	if ctx == nil || runner == nil || config.validate() != nil {
		return nil, errors.New("FORMAL_PAIRED_SCENARIO_BATCH_INPUT_INVALID")
	}
	var contract defectbench.FormalBenchmarkContract
	if err := readStrictJSON(contractPath, &contract); err != nil {
		return nil, err
	}
	var exposure defectbench.FormalExposureAudit
	if err := readStrictJSON(exposurePath, &exposure); err != nil {
		return nil, err
	}
	semantic, err := os.ReadFile(config.SemanticInputPath)
	if err != nil {
		return nil, err
	}
	if err := validatePairedScenarioExposure(contract, exposure, semantic); err != nil {
		return nil, err
	}
	var inputs formalFreshInputs
	if err := readStrictJSON(inputsPath, &inputs); err != nil {
		return nil, err
	}
	sources, variants, err := validateFormalFreshInputs(inputsPath, inputs, contract)
	if err != nil {
		return nil, err
	}
	loaded, err := loadFreshTrials(sources)
	if err != nil {
		return nil, err
	}
	return executePairedScenarioTrials(ctx, loaded, variants, config, artifactDir, runner)
}

func validatePairedScenarioExposure(
	contract defectbench.FormalBenchmarkContract,
	exposure defectbench.FormalExposureAudit,
	semantic []byte,
) error {
	if err := contract.Validate(); err != nil {
		return err
	}
	view, err := contract.OpaqueView()
	if err != nil {
		return err
	}
	if err := exposure.Validate(); err != nil || !exposure.Passed ||
		exposure.BenchmarkID != contract.ID || exposure.ContractDigest != contract.Digest ||
		exposure.ExpectedOpaqueViewDigest != view.Digest {
		return errors.New("FORMAL_PAIRED_SCENARIO_EXPOSURE_AUDIT_REQUIRED")
	}
	semanticDigest := digestBytes(semantic)
	for _, artifact := range exposure.PublicArtifacts {
		if artifact.Digest == semanticDigest {
			return nil
		}
	}
	return errors.New("FORMAL_PAIRED_SCENARIO_SEMANTIC_INPUT_NOT_AUDITED")
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
	resume := false
	if info, err := os.Lstat(clean); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return pairedScenarioFreshEvidence{}, errors.New("FORMAL_PAIRED_SCENARIO_RESUME_DIRECTORY_INVALID")
		}
		resume = true
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
	arguments := []string{
		"-strategy", pairedScenarioBinaryStrategy,
		"-campaign-dir", clean,
		"-semantic-input", config.SemanticInputPath,
		"-agent-key-file", config.AgentKeyFile,
		"-agent-model", config.AgentModel,
	}
	if resume {
		arguments = append(arguments, "-campaign-resume")
	}
	command := exec.CommandContext(runContext, binaryPath, arguments...)
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
	return recoverPairedScenarioFreshEvidence(current, clean)
}

func recoverPairedScenarioFreshEvidence(
	current loadedFreshTrial,
	directory string,
) (pairedScenarioFreshEvidence, error) {
	scenario, root, err := loadFreshPairedScenarioTrialEvidence(directory)
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
	if info, err := os.Lstat(clean); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("FORMAL_PAIRED_SCENARIO_BATCH_DIRECTORY_INVALID")
		}
		entries, readErr := os.ReadDir(clean)
		if readErr != nil {
			return nil, readErr
		}
		for _, entry := range entries {
			if !seen[entry.Name()] {
				return nil, errors.New("FORMAL_PAIRED_SCENARIO_BATCH_ENTRY_UNKNOWN")
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else if err := os.MkdirAll(clean, 0o755); err != nil {
		return nil, err
	}
	result := make(map[string]pairedScenarioFreshEvidence, len(ordered))
	for _, current := range ordered {
		directory := filepath.Join(clean, current.trialID)
		var fresh pairedScenarioFreshEvidence
		if info, err := os.Lstat(directory); err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return nil, errors.New("FORMAL_PAIRED_SCENARIO_TRIAL_DIRECTORY_INVALID")
			}
			fresh, err = recoverPairedScenarioFreshEvidence(current, directory)
			if err != nil {
				fresh, err = runner(ctx, current, config, directory)
			}
			if err != nil {
				return nil, fmt.Errorf("resume paired Scenario trial %s: %w", current.trialID, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		} else {
			fresh, err = runner(ctx, current, config, directory)
			if err != nil {
				return nil, fmt.Errorf("paired Scenario trial %s: %w", current.trialID, err)
			}
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
