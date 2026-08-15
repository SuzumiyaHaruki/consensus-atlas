package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
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
		Strategy:   "workload",
		Decisions:  96,
		PolicySeed: 1,
	}
	flags := flag.NewFlagSet("control-experiment", flag.ContinueOnError)
	flags.StringVar(&options.Out, "out", "", "report output path")
	flags.StringVar(&options.BundleOut, "bundle-out", "", "optional execution bundle output path")
	flags.IntVar(&options.BundleEvidenceVersion, "bundle-evidence-version", 0, "optional trusted bundle evidence version")
	flags.StringVar(&options.MethodSpecDigest, "method-spec-digest", "", "MethodSpec digest required by bundle evidence v3")
	flags.StringVar(&options.AgentKeyFile, "agent-key-file", "", "key file for an explicit opt-in Agent strategy")
	flags.StringVar(&options.AgentModel, "agent-model", "", "OpenRouter model ID for an explicit opt-in Agent strategy")
	flags.StringVar(&options.WorkerPath, "worker", "", "target worker executable for a worker-backed Agent strategy")
	flags.StringVar(&options.SemanticInput, "semantic-input", "", "editable protocol knowledge and hypothesis JSON")
	flags.StringVar(&options.ScenarioSemanticExposure, "scenario-semantic-exposure", "", "optional full or masked semantics")
	flags.StringVar(&options.CampaignDirectory, "campaign-dir", "", "Campaign directory")
	flags.StringVar(&options.CampaignObservationOut, "campaign-observation-out", "", "Campaign Observation output path")
	flags.IntVar(&options.CampaignAttempts, "campaign-attempts", 0, "Campaign attempt limit")
	flags.Int64Var(&options.CampaignWallClock, "campaign-wall-clock-ms", 0, "Campaign wall-clock ceiling")
	flags.IntVar(&options.CampaignModelTokens, "campaign-model-tokens-per-attempt", 0, "model-token allowance per attempt")
	flags.BoolVar(&options.CampaignResume, "campaign-resume", false, "resume an exact Campaign")
	flags.StringVar(&options.StatelessCorpus, "stateless-corpus", "", "root corpus (paired Scenario defaults to SUT-local fresh roots)")
	flags.StringVar(&options.Strategy, "strategy", options.Strategy, "qualified or explicit opt-in Agent strategy")
	flags.IntVar(&options.Decisions, "decisions", options.Decisions, "charged decisions per run")
	flags.Uint64Var(&options.PolicySeed, "policy-seed", options.PolicySeed, "public random-policy seed")
	if err := flags.Parse(args); err != nil {
		return err
	}
	switch options.Strategy {
	case omnipaxosScenarioSessionStrategy:
		return runOmnipaxosSessionCLI(ctx, options, stdout)
	case etcdraftA8PairedScenarioStrategy:
		return runEtcdraftA8PairedScenarioCLI(ctx, options, stdout)
	case etcdraftScenarioSessionStrategy:
		return runEtcdraftSessionCLI(ctx, options, stdout)
	case etcdraftSemanticCalibrationStrategy:
		return runEtcdraftSemanticExplorerCLI(ctx, options, stdout)
	case etcdraftStatelessCanonicalCampaignStrategy, etcdraftStatelessUniformCampaignStrategy:
		return runEtcdraftStatelessCampaignCLI(ctx, options, stdout)
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
	case "workload", "workload-semantics-v2", "workload-evaluation-v3",
		"workload-risk-witness-calibration",
		"workload-admissible-uniform", "workload-admissible-uniform-b4",
		"workload-action-class-random", "workload-action-class-random-b4":
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
	return etcdraftExecutionConfigured(
		ctx, strategy, decisions, policySeed, captureBundle, methodSpecDigest, nil,
	)
}

func etcdraftExecutionConfigured(
	ctx context.Context,
	strategy string,
	decisions int,
	policySeed uint64,
	captureBundle bool,
	methodSpecDigest string,
	workloadOverride *controlexperiment.WorkloadPlan,
) (controlexperiment.Report, controlexperiment.ExecutionBundle, error) {
	if methodSpecDigest != "" && !captureBundle {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{},
			errors.New("method spec requires bundle capture")
	}
	var experimentID string
	var runs []controlexperiment.RunPlan
	var admission *controlexperiment.ExecutionAdmission
	var qualificationReport *qualification.Bundle
	var faultEnvelope *controlexperiment.FaultEnvelope
	schemaVersion := controlexperiment.SchemaVersion
	workloadRouterID := ""
	switch strategy {
	case "workload", "workload-semantics-v2", "workload-evaluation-v3", "workload-risk-witness-calibration", "workload-admissible-uniform", "workload-admissible-uniform-b4", "workload-action-class-random", "workload-action-class-random-v2", "workload-action-class-random-b4":
		bundle, bound, workload, err := etcdraftQualifiedWorkload(ctx)
		if err != nil {
			return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
		}
		if workloadOverride != nil {
			if err := workloadOverride.Validate(); err != nil {
				return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
			}
			workload = *workloadOverride
		}
		admission = &bound
		qualificationReport = &bundle
		policy := controlexperiment.Policy{
			Version: controlexperiment.PolicyVersion, ID: "semantic-workload-progress-v1",
			Priority: []control.ActionKind{
				control.ActionInvoke, control.ActionCompleteEffect,
				control.ActionDeliverMessage, control.ActionFireTemporal,
			},
		}
		stopAfterWorkload := false
		if strategy == "workload" || strategy == "workload-semantics-v2" || strategy == "workload-evaluation-v3" || strategy == "workload-risk-witness-calibration" {
			experimentID = "public-etcdraft-v2-semantic-workload-m5.15"
			if strategy == "workload-semantics-v2" {
				experimentID = "public-etcdraft-v2-experiment-semantics-m5.17c0"
				schemaVersion = controlexperiment.SchemaVersionV2
				workloadRouterID = etcdraftv2.WorkloadRouterID
				stopAfterWorkload = true
			} else if strategy == "workload-evaluation-v3" {
				experimentID = "public-etcdraft-v2-method-evaluation-m5.18a"
				schemaVersion = controlexperiment.SchemaVersionV2
				workloadRouterID = etcdraftv2.WorkloadRouterID
			} else if strategy == "workload-risk-witness-calibration" {
				if decisions != 64 || policySeed != 1 {
					return controlexperiment.Report{}, controlexperiment.ExecutionBundle{},
						errors.New("risk witness calibration requires -decisions 64 and -policy-seed 1")
				}
				experimentID = "public-etcdraft-v2-risk-witness-reachability-m5.21p"
				schemaVersion = controlexperiment.SchemaVersionV2
				workloadRouterID = etcdraftv2.WorkloadRouterID
				policy = controlexperiment.Policy{
					Version: controlexperiment.PolicyVersion,
					ID:      "etcdraft-risk-witness-reachability-calibration-v1",
					Rules: []controlexperiment.DecisionRule{
						{Decision: 29, Kind: control.ActionCrash, Node: "n1"},
						{Decision: 54, Kind: control.ActionRestart, Node: "n1"},
					},
					Priority: []control.ActionKind{
						control.ActionInvoke, control.ActionCompleteEffect,
						control.ActionDeliverMessage, control.ActionFireTemporal,
					},
				}
			}
			faultEnvelope = &controlexperiment.FaultEnvelope{
				MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
				MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
			}
		} else if strategy == "workload-action-class-random" || strategy == "workload-action-class-random-v2" || strategy == "workload-action-class-random-b4" {
			experimentID = fmt.Sprintf("public-etcdraft-v2-action-class-random-m5.17a-seed-%d", policySeed)
			if strategy == "workload-action-class-random-v2" {
				experimentID = fmt.Sprintf("public-etcdraft-v2-action-class-random-m5.18b3-seed-%d", policySeed)
				schemaVersion = controlexperiment.SchemaVersionV2
				workloadRouterID = etcdraftv2.WorkloadRouterID
			} else if strategy == "workload-action-class-random-b4" {
				experimentID = fmt.Sprintf("public-etcdraft-v2-action-class-random-m5.18b4-pre-seed-%d", policySeed)
				schemaVersion = controlexperiment.SchemaVersionV2
				workloadRouterID = etcdraftv2.WorkloadRouterID
			}
			policy = controlexperiment.Policy{
				Version: controlexperiment.ActionClassPolicyVersion,
				ID:      "action-class-random-v1/run-1", SeedHex: randomPolicySeed(policySeed, 1),
				Priority: []control.ActionKind{control.ActionInvoke},
			}
			if strategy == "workload-action-class-random-b4" {
				policy, err = etcdraftCampaignExecutionPolicy(strategy, policySeed, decisions)
				if err != nil {
					return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
				}
			}
			faultEnvelope = &controlexperiment.FaultEnvelope{
				MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
				MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
			}
			if strategy == "workload-action-class-random-b4" {
				faultEnvelope.MaxPartitions, faultEnvelope.MaxActivePartitions = 0, 0
			}
		} else {
			experimentID = fmt.Sprintf("public-etcdraft-v2-admissible-uniform-m5.17c2-seed-%d", policySeed)
			schemaVersion = controlexperiment.SchemaVersionV2
			workloadRouterID = etcdraftv2.WorkloadRouterID
			policy = controlexperiment.Policy{
				Version: controlexperiment.AdmissibleUniformPolicyVersion,
				ID:      "admissible-uniform-v1/run-1", SeedHex: randomPolicySeed(policySeed, 1),
				Priority: []control.ActionKind{control.ActionInvoke},
			}
			if strategy == "workload-admissible-uniform-b4" {
				policy, err = etcdraftCampaignExecutionPolicy(strategy, policySeed, decisions)
				if err != nil {
					return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
				}
			}
			faultEnvelope = &controlexperiment.FaultEnvelope{
				MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
				MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
			}
			if strategy == "workload-admissible-uniform-b4" {
				experimentID = fmt.Sprintf("public-etcdraft-v2-admissible-uniform-m5.18b4-pre-seed-%d", policySeed)
				faultEnvelope.MaxPartitions, faultEnvelope.MaxActivePartitions = 0, 0
			}
		}
		runs = []controlexperiment.RunPlan{{
			Run: 1, Policy: policy, Workload: &workload, StopAfterWorkload: stopAfterWorkload,
		}}
	default:
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, fmt.Errorf("unsupported -strategy %q", strategy)
	}
	config := controlexperiment.Config{
		SchemaVersion: schemaVersion,
		ID:            experimentID,
		PSSID:         etcdraftv2.CorePSSMappingID,
		Runtime:       etcdraftCampaignRuntimeConfig(),
		Admission:     admission, FaultEnvelope: faultEnvelope, WorkloadRouterID: workloadRouterID,
		DecisionsPerRun: decisions, RequireReplay: true, Runs: runs,
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	if qualificationReport == nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{},
			errors.New("qualified workload strategy did not bind qualification")
	}
	if captureBundle {
		if methodSpecDigest != "" {
			return controlexperiment.ExecuteQualifiedBundleV3(
				ctx, config, *qualificationReport, factory, etcdraftv2.CorePSSMapper{},
				etcdraftv2.DecisionProjector{}, etcdraftv2.WorkloadRouter{}, methodSpecDigest,
			)
		}
		return controlexperiment.ExecuteQualifiedBundle(
			ctx, config, *qualificationReport, factory, etcdraftv2.CorePSSMapper{},
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

func randomPolicySeed(base uint64, run int) string {
	var input [16]byte
	binary.BigEndian.PutUint64(input[:8], base)
	binary.BigEndian.PutUint64(input[8:], uint64(run))
	sum := sha256.Sum256(append([]byte("consensus-atlas/policy-seed/v1\x00"), input[:]...))
	return hex.EncodeToString(sum[:])
}

func allReplayStable(report controlexperiment.Report) bool {
	for _, run := range report.Runs {
		if !run.Replay.Stable {
			return false
		}
	}
	return true
}
