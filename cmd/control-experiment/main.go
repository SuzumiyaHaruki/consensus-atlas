package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	qualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/etcdraftv2"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "control-experiment:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	options := controlExperimentOptions{
		Strategy:               "workload",
		Decisions:              96,
		PolicySeed:             1,
		InvestigationEpisodes:  1,
		AgentProvider:          openRouterProvider,
		CapabilityFeedbackMode: controlexperiment.AgenticCapabilityFeedbackStructuredGaps,
	}
	flags := flag.NewFlagSet("control-experiment", flag.ContinueOnError)
	flags.StringVar(&options.Out, "out", "", "report output path")
	flags.StringVar(&options.BundleOut, "bundle-out", "", "optional execution bundle output path")
	flags.IntVar(&options.BundleEvidenceVersion, "bundle-evidence-version", 0, "optional trusted bundle evidence version")
	flags.StringVar(&options.MethodSpecDigest, "method-spec-digest", "", "MethodSpec digest required by bundle evidence v3")
	flags.StringVar(&options.AgentKeyFile, "agent-key-file", "", "key file for an explicit opt-in Agent strategy")
	flags.StringVar(&options.AgentProvider, "agent-provider", options.AgentProvider, "Agent provider: openrouter or deepseek")
	flags.StringVar(&options.AgentModel, "agent-model", "", "provider model ID for an explicit opt-in Agent strategy")
	flags.StringVar(&options.WorkerPath, "worker", "", "target worker executable for a worker-backed Agent strategy")
	flags.StringVar(&options.Target, "target", "", "target composition for an Agentic Episode")
	flags.StringVar(&options.SemanticInput, "semantic-input", "", "editable protocol and Agent-planning JSON")
	flags.StringVar(&options.FixedRiskInput, "fixed-risk-input", "", "optional public accepted Risk candidate JSON for Scenario-only calibration")
	var knowledgeSourceMounts repeatableStringFlag
	flags.Var(&knowledgeSourceMounts, "knowledge-source-mount", "repeatable repo=<directory> or <reference-prefix>=<directory> read-only Agent source mount")
	flags.StringVar(&options.CampaignDirectory, "campaign-dir", "", "Campaign directory")
	flags.BoolVar(&options.CampaignResume, "campaign-resume", false, "resume an exact Campaign")
	flags.IntVar(&options.InvestigationEpisodes, "investigation-episodes", options.InvestigationEpisodes, "Agentic Investigation episode limit")
	flags.StringVar(&options.ClosureMode, "closure-mode", "", "Agent progress mode: public-fixed or target-local (default: Target composition)")
	flags.StringVar(&options.CapabilityFeedbackMode, "capability-feedback", options.CapabilityFeedbackMode, "Agent capability feedback: reason-codes or structured-gaps")
	flags.StringVar(&options.CapabilityFeedbackProbe, "capability-feedback-probe", "", "optional public mechanical capability-probe JSON")
	flags.StringVar(&options.Strategy, "strategy", options.Strategy, "qualified or explicit opt-in Agent strategy")
	flags.IntVar(&options.Decisions, "decisions", options.Decisions, "charged decisions per run")
	flags.Uint64Var(&options.PolicySeed, "policy-seed", options.PolicySeed, "public random-policy seed")
	if err := flags.Parse(args); err != nil {
		return err
	}
	options.KnowledgeSourceMounts = append([]string(nil), knowledgeSourceMounts...)
	switch options.Strategy {
	case agenticEpisodeStrategy:
		return runAgenticEpisodeCLI(ctx, options, stdout)
	default:
		return runEtcdraftQualifiedCLI(ctx, options, stdout)
	}
}

func etcdraftBundle(
	ctx context.Context,
	strategy string,
	decisions int,
	policySeed uint64,
) (controlexperiment.Report, controlexperiment.ExecutionBundle, error) {
	return etcdraftExecution(ctx, strategy, decisions, policySeed, true)
}

func etcdraftBundleV3(
	ctx context.Context,
	strategy string,
	decisions int,
	policySeed uint64,
	methodSpecDigest string,
) (controlexperiment.Report, controlexperiment.ExecutionBundle, error) {
	return etcdraftExecutionWithMethodSpec(
		ctx, strategy, decisions, policySeed, true, methodSpecDigest,
	)
}

func writeReport(path string, encoded []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

func etcdraftReport(
	ctx context.Context,
	strategy string,
	decisions int,
	policySeed uint64,
) (controlexperiment.Report, error) {
	report, _, err := etcdraftExecution(ctx, strategy, decisions, policySeed, false)
	return report, err
}

func supportsQualifiedBundleOutput(strategy string) bool {
	switch strategy {
	case "workload", "workload-evaluation-v3":
		return true
	default:
		return false
	}
}

func etcdraftExecution(
	ctx context.Context,
	strategy string,
	decisions int,
	policySeed uint64,
	captureBundle bool,
) (controlexperiment.Report, controlexperiment.ExecutionBundle, error) {
	return etcdraftExecutionWithMethodSpec(ctx, strategy, decisions, policySeed, captureBundle, "")
}

func etcdraftExecutionWithMethodSpec(
	ctx context.Context,
	strategy string,
	decisions int,
	policySeed uint64,
	captureBundle bool,
	methodSpecDigest string,
) (controlexperiment.Report, controlexperiment.ExecutionBundle, error) {
	if methodSpecDigest != "" && !captureBundle {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{},
			errors.New("method spec requires bundle capture")
	}
	if strategy != "workload" && strategy != "workload-evaluation-v3" {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, fmt.Errorf("unsupported -strategy %q", strategy)
	}
	qualificationReport, admission, workload, err := etcdraftQualifiedWorkload(ctx)
	if err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	policy := controlexperiment.Policy{
		Version: controlexperiment.PolicyVersion, ID: "semantic-workload-progress-v1",
		Priority: []control.ActionKind{
			control.ActionInvoke, control.ActionCompleteEffect,
			control.ActionDeliverMessage, control.ActionFireTemporal,
		},
	}
	schemaVersion := controlexperiment.SchemaVersion
	experimentID := "public-etcdraft-v2-semantic-workload-m5.15"
	workloadRouterID := ""
	if strategy == "workload-evaluation-v3" {
		schemaVersion = controlexperiment.SchemaVersionV2
		experimentID = "public-etcdraft-v2-method-evaluation-m5.18a"
		workloadRouterID = etcdraftv2.WorkloadRouterID
	}
	faultEnvelope := &controlexperiment.FaultEnvelope{
		MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
		MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
	}
	config := controlexperiment.Config{
		SchemaVersion: schemaVersion,
		ID:            experimentID,
		PSSID:         etcdraftv2.CorePSSMappingID,
		Runtime:       etcdraftCampaignRuntimeConfig(),
		Admission:     &admission, FaultEnvelope: faultEnvelope, WorkloadRouterID: workloadRouterID,
		DecisionsPerRun: decisions, RequireReplay: true,
		Runs: []controlexperiment.RunPlan{{Run: 1, Policy: policy, Workload: &workload}},
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	_ = policySeed
	if captureBundle {
		if methodSpecDigest != "" {
			return controlexperiment.ExecuteQualifiedBundleV3(
				ctx, config, qualificationReport, factory, etcdraftv2.CorePSSMapper{},
				etcdraftv2.DecisionProjector{}, etcdraftv2.WorkloadRouter{}, methodSpecDigest,
			)
		}
		return controlexperiment.ExecuteQualifiedBundle(
			ctx, config, qualificationReport, factory, etcdraftv2.CorePSSMapper{},
			etcdraftv2.DecisionProjector{}, etcdraftv2.WorkloadRouter{},
		)
	}
	report, err := controlexperiment.ExecuteQualified(
		ctx, config, qualificationReport.Qualification, factory, etcdraftv2.CorePSSMapper{},
		etcdraftv2.WorkloadRouter{},
	)
	return report, controlexperiment.ExecutionBundle{}, err
}

func etcdraftQualifiedWorkload(
	ctx context.Context,
) (qualification.Bundle, controlexperiment.ExecutionAdmission, controlexperiment.WorkloadPlan, error) {
	bundle, err := qualification.Run(ctx)
	if err != nil {
		return qualification.Bundle{}, controlexperiment.ExecutionAdmission{}, controlexperiment.WorkloadPlan{}, err
	}
	bound, err := controlexperiment.BindExecutionAdmission(
		bundle.Qualification,
		controlexperiment.ExecutionRequirements{Capabilities: bundle.Profile.RequiredCapabilityIDs()},
	)
	if err != nil {
		return qualification.Bundle{}, controlexperiment.ExecutionAdmission{}, controlexperiment.WorkloadPlan{}, err
	}
	workload, err := etcdraftCampaignWorkload()
	if err != nil {
		return qualification.Bundle{}, controlexperiment.ExecutionAdmission{}, controlexperiment.WorkloadPlan{}, err
	}
	return bundle, bound, workload, nil
}

func etcdraftCampaignWorkload() (controlexperiment.WorkloadPlan, error) {
	const requestID = "m5.15-write-1"
	payload, err := etcdraftv2.InputPayload(etcdraftv2.Input{
		Operation: etcdraftv2.OperationPropose, RequestID: requestID, Value: []byte("alpha"),
	})
	if err != nil {
		return controlexperiment.WorkloadPlan{}, err
	}
	return controlexperiment.WorkloadPlan{
		SchemaVersion: controlexperiment.WorkloadPlanVersion, ID: "single-write-v1",
		TargetSelector: controlexperiment.TargetSingleCoordinatingMember,
		Invocations: []controlexperiment.WorkloadInvocation{{
			ID: requestID, Input: payload, ExpectedStatus: "committed",
		}},
	}, nil
}

func allReplayStable(report controlexperiment.Report) bool {
	for _, run := range report.Runs {
		if !run.Replay.Stable {
			return false
		}
	}
	return true
}
