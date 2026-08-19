package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

type controlExperimentOptions struct {
	Out                     string
	BundleOut               string
	BundleEvidenceVersion   int
	MethodSpecDigest        string
	AgentKeyFile            string
	AgentProvider           string
	AgentModel              string
	WorkerPath              string
	Target                  string
	SemanticInput           string
	RiskInput               string
	KnowledgeSourceMounts   []string
	CampaignDirectory       string
	CampaignResume          bool
	InvestigationEpisodes   int
	ClosureMode             string
	CapabilityFeedbackMode  string
	CapabilityFeedbackProbe string
	Strategy                string
	Decisions               int
	PolicySeed              uint64
}

func (options controlExperimentOptions) hasNonSessionFlags() bool {
	return options.Out != "" || options.BundleOut != "" ||
		options.BundleEvidenceVersion != 0 || options.MethodSpecDigest != "" || options.Target != "" ||
		options.RiskInput != "" ||
		len(options.KnowledgeSourceMounts) != 0 || options.InvestigationEpisodes != 1 ||
		options.Decisions != 96 || options.PolicySeed != 1
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
	if options.CampaignDirectory != "" || options.CampaignResume {
		return errors.New("Agentic Episode flags require -strategy agentic-episode-v1")
	}
	if options.AgentKeyFile != "" || options.AgentModel != "" || options.SemanticInput != "" ||
		options.RiskInput != "" ||
		options.AgentProvider != "" && options.AgentProvider != openRouterProvider ||
		options.WorkerPath != "" || options.Target != "" || len(options.KnowledgeSourceMounts) != 0 ||
		options.InvestigationEpisodes != 1 || options.ClosureMode != "" ||
		options.CapabilityFeedbackMode != "" &&
			options.CapabilityFeedbackMode != controlexperiment.AgenticCapabilityFeedbackStructuredGaps ||
		options.CapabilityFeedbackProbe != "" {
		return errors.New("Agent flags require -strategy agentic-episode-v1")
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
