package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

type controlExperimentOptions struct {
	Out                      string
	BundleOut                string
	BundleEvidenceVersion    int
	MethodSpecDigest         string
	AgentKeyFile             string
	AgentModel               string
	WorkerPath               string
	SemanticInput            string
	ScenarioSemanticExposure string
	CampaignDirectory        string
	CampaignObservationOut   string
	CampaignAttempts         int
	CampaignWallClock        int64
	CampaignModelTokens      int
	CampaignResume           bool
	StatelessCorpus          string
	Strategy                 string
	Decisions                int
	PolicySeed               uint64
}

func (options controlExperimentOptions) hasNonSessionFlags() bool {
	return options.Out != "" || options.BundleOut != "" || options.CampaignObservationOut != "" ||
		options.CampaignAttempts != 0 || options.CampaignWallClock != 0 || options.CampaignModelTokens != 0 ||
		options.BundleEvidenceVersion != 0 || options.MethodSpecDigest != "" ||
		options.Decisions != 96 || options.PolicySeed != 1
}

func runOmnipaxosSessionCLI(
	ctx context.Context,
	options controlExperimentOptions,
	stdout io.Writer,
) error {
	if options.CampaignDirectory == "" || options.WorkerPath == "" || options.SemanticInput == "" ||
		options.AgentKeyFile == "" || options.AgentModel == "" || options.StatelessCorpus != "" ||
		options.ScenarioSemanticExposure != "" || options.hasNonSessionFlags() {
		return errors.New("OmniPaxos Scenario session requires -campaign-dir, -worker, -semantic-input, -agent-key-file, -agent-model, and optional -campaign-resume")
	}
	summary, err := runOmnipaxosScenarioSession(ctx, omnipaxosScenarioSessionOptions{
		Directory: options.CampaignDirectory, WorkerPath: options.WorkerPath,
		SemanticInputPath: options.SemanticInput, Resume: options.CampaignResume,
		AgentKeyFile: options.AgentKeyFile,
		Client:       newOpenRouterIntentClient(options.AgentModel), ReadKey: readAgentKey,
	})
	writeScenarioSessionSummary(stdout, options.CampaignDirectory, summary)
	return err
}

func runEtcdraftSessionCLI(
	ctx context.Context,
	options controlExperimentOptions,
	stdout io.Writer,
) error {
	exposure := controlexperiment.ScenarioSemanticExposureMode(options.ScenarioSemanticExposure)
	if options.CampaignDirectory == "" || options.StatelessCorpus == "" || options.SemanticInput == "" ||
		options.AgentKeyFile == "" || options.AgentModel == "" || options.WorkerPath != "" ||
		(options.ScenarioSemanticExposure != "" && exposure.Validate() != nil) || options.hasNonSessionFlags() {
		return errors.New("OpenRouter Scenario session requires -campaign-dir, -stateless-corpus, -semantic-input, -agent-key-file, -agent-model, and optional -campaign-resume")
	}
	summary, err := runEtcdraftScenarioSession(ctx, etcdraftScenarioSessionOptions{
		Directory: options.CampaignDirectory, CorpusPath: options.StatelessCorpus,
		SemanticInputPath: options.SemanticInput, Resume: options.CampaignResume,
		AgentKeyFile: options.AgentKeyFile, SemanticExposure: exposure,
		Client: newOpenRouterIntentClient(options.AgentModel), ReadKey: readAgentKey,
	})
	writeScenarioSessionSummary(stdout, options.CampaignDirectory, summary)
	return err
}

func writeScenarioSessionSummary(
	stdout io.Writer,
	directory string,
	summary scenarioSessionSummary,
) {
	if summary.Campaign.CampaignID == "" {
		return
	}
	fmt.Fprintf(
		stdout, "session=%s semantics=%s status=%s episodes=%d stop=%s primary_work=%d replay_work=%d model_calls=%d model_tokens=%d\n",
		directory, summary.SemanticExposure, summary.Campaign.Status, summary.Campaign.Sequence,
		summary.Campaign.StopReason, summary.Campaign.Totals.Primary.WorkUnits,
		summary.Campaign.Totals.Replay.WorkUnits, summary.Campaign.Totals.Model.Calls,
		summary.Campaign.Totals.Model.TotalTokens,
	)
	fmt.Fprintf(
		stdout, "testing_episodes=%d replay_stable=%d pss_states=%d risk=%s oracle_violations=%d\n",
		summary.TestingEpisodes, summary.ReplayStableEpisodes, summary.UniqueCorePSSStates,
		summary.BestRiskStatus, summary.OracleViolations,
	)
}

func runEtcdraftSemanticExplorerCLI(
	ctx context.Context,
	options controlExperimentOptions,
	stdout io.Writer,
) error {
	if options.CampaignDirectory == "" || options.StatelessCorpus == "" || options.SemanticInput == "" ||
		options.AgentKeyFile == "" || options.AgentModel == "" || options.WorkerPath != "" ||
		options.ScenarioSemanticExposure != "" || options.hasNonSessionFlags() {
		return errors.New("semantic Explorer calibration requires -campaign-dir, -stateless-corpus, -semantic-input, -agent-key-file, -agent-model, and optional -campaign-resume")
	}
	artifact, err := runEtcdraftSemanticCalibration(ctx, etcdraftSemanticCalibrationRunOptions{
		Directory: options.CampaignDirectory, CorpusPath: options.StatelessCorpus,
		SemanticInputPath: options.SemanticInput, Resume: options.CampaignResume,
		AgentKeyFile: options.AgentKeyFile,
		Client:       newOpenRouterIntentClient(options.AgentModel), ReadKey: readAgentKey,
	})
	if artifact.Digest != "" {
		if artifact.Testing != nil {
			fmt.Fprintf(
				stdout, "artifact=%s status=%s selected=%s pss_states=%d replay=%t oracle_violations=%d model_calls=%d model_tokens=%d digest=%s\n",
				filepath.Join(options.CampaignDirectory, "artifact.json"), artifact.Status,
				artifact.Testing.SelectedCandidateID, artifact.Testing.UniqueCorePSSStates,
				artifact.Testing.Replay.Stable, len(artifact.Testing.Oracle.Violations),
				artifact.ModelWork.Calls, artifact.ModelWork.TotalTokens, artifact.Digest,
			)
		} else {
			fmt.Fprintf(
				stdout, "artifact=%s status=%s model_calls=%d model_tokens=%d digest=%s\n",
				filepath.Join(options.CampaignDirectory, "artifact.json"), artifact.Status,
				artifact.ModelWork.Calls, artifact.ModelWork.TotalTokens, artifact.Digest,
			)
		}
	}
	return err
}

func runEtcdraftStatelessCampaignCLI(
	ctx context.Context,
	options controlExperimentOptions,
	stdout io.Writer,
) error {
	if options.Out == "" || options.CampaignObservationOut == "" || options.CampaignDirectory == "" ||
		options.CampaignAttempts <= 0 || options.CampaignWallClock <= 0 || options.StatelessCorpus == "" ||
		options.Decisions != 96 || options.BundleOut != "" || options.BundleEvidenceVersion != 0 ||
		options.MethodSpecDigest != "" || options.AgentKeyFile != "" || options.AgentModel != "" ||
		options.SemanticInput != "" || options.ScenarioSemanticExposure != "" || options.WorkerPath != "" ||
		options.CampaignModelTokens != 0 ||
		(options.Strategy == etcdraftStatelessCanonicalCampaignStrategy && options.PolicySeed != 1) {
		return errors.New("Stateless Campaign strategy requires only -stateless-corpus, -out, Campaign, and uniform seed flags")
	}
	return runEtcdraftStatelessCampaign(ctx, etcdraftStatelessCampaignRunOptions{
		Directory: options.CampaignDirectory, SummaryOut: options.Out,
		ObservationOut: options.CampaignObservationOut, Resume: options.CampaignResume,
		Strategy: options.Strategy, Attempts: options.CampaignAttempts, FirstSeed: options.PolicySeed,
		WallClockCeilingMillis: options.CampaignWallClock, CorpusPath: options.StatelessCorpus,
	}, stdout)
}

func runEtcdraftQualifiedCLI(
	ctx context.Context,
	options controlExperimentOptions,
	stdout io.Writer,
) error {
	if err := validateQualifiedCLIOptions(options); err != nil {
		return err
	}
	var report controlexperiment.Report
	var bundle *controlexperiment.ExecutionBundle
	var err error
	if options.BundleOut != "" {
		if !supportsQualifiedBundleOutput(options.Strategy) {
			return errors.New("-bundle-out requires a qualified workload strategy")
		}
		var captured controlexperiment.ExecutionBundle
		if options.BundleEvidenceVersion == 3 {
			if options.MethodSpecDigest == "" {
				return errors.New("bundle evidence v3 requires -method-spec-digest")
			}
			report, captured, err = etcdraftBundleV3(
				ctx, options.Strategy, options.Decisions, options.PolicySeed, options.MethodSpecDigest,
			)
		} else {
			if options.BundleEvidenceVersion != 0 || options.MethodSpecDigest != "" {
				return errors.New("only -bundle-evidence-version 3 is supported")
			}
			report, captured, err = etcdraftBundle(
				ctx, options.Strategy, options.Decisions, options.PolicySeed,
			)
		}
		bundle = &captured
	} else {
		report, err = etcdraftReport(ctx, options.Strategy, options.Decisions, options.PolicySeed)
	}
	if err != nil {
		return err
	}
	return persistQualifiedCLIResult(options, stdout, report, bundle)
}

func validateQualifiedCLIOptions(options controlExperimentOptions) error {
	if options.CampaignDirectory != "" || options.CampaignObservationOut != "" ||
		options.CampaignAttempts != 0 || options.CampaignWallClock != 0 ||
		options.CampaignModelTokens != 0 || options.CampaignResume {
		return errors.New("Campaign flags require an explicit Campaign strategy")
	}
	if options.StatelessCorpus != "" {
		return errors.New("-stateless-corpus requires a Stateless Campaign strategy")
	}
	if options.AgentKeyFile != "" {
		return errors.New("Agent flags require an explicit opt-in Agent strategy")
	}
	if options.AgentModel != "" {
		return errors.New("-agent-model requires an explicit opt-in Agent strategy")
	}
	if options.SemanticInput != "" {
		return errors.New("-semantic-input requires an explicit opt-in Agent strategy")
	}
	if options.ScenarioSemanticExposure != "" {
		return errors.New("-scenario-semantic-exposure requires the Scenario session strategy")
	}
	if options.WorkerPath != "" {
		return errors.New("-worker requires a worker-backed Agent strategy")
	}
	if options.Out == "" {
		return errors.New("-out is required")
	}
	if (options.BundleEvidenceVersion != 0 || options.MethodSpecDigest != "") && options.BundleOut == "" {
		return errors.New("bundle evidence flags require -bundle-out")
	}
	return nil
}

func persistQualifiedCLIResult(
	options controlExperimentOptions,
	stdout io.Writer,
	report controlexperiment.Report,
	bundle *controlexperiment.ExecutionBundle,
) error {
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	var persisted controlexperiment.Report
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		return err
	}
	if err := persisted.Validate(); err != nil {
		return fmt.Errorf("validate persisted report: %w", err)
	}
	if err := writeReport(options.Out, encoded); err != nil {
		return err
	}
	if bundle != nil {
		bundleBytes, err := json.MarshalIndent(bundle, "", "  ")
		if err != nil {
			return err
		}
		var persistedBundle controlexperiment.ExecutionBundle
		if err := json.Unmarshal(bundleBytes, &persistedBundle); err != nil {
			return err
		}
		if err := persistedBundle.Validate(); err != nil {
			return fmt.Errorf("validate persisted execution bundle: %w", err)
		}
		if err := persistedBundle.ValidateProjection(etcdraftv2.DecisionProjector{}); err != nil {
			return fmt.Errorf("validate persisted decision projection: %w", err)
		}
		if err := writeReport(options.BundleOut, bundleBytes); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "wrote %s\nruns=%d decisions=%d states=%d replay=%t digest=%s\n",
		options.Out, len(report.Runs), report.StateDiscovery.TotalDecisions,
		report.StateDiscovery.UniqueStates, allReplayStable(report), report.Digest)
	return nil
}
