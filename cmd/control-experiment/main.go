package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
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
	flags := flag.NewFlagSet("control-experiment", flag.ContinueOnError)
	out := flags.String("out", "", "report output path")
	bundleOut := flags.String("bundle-out", "", "optional execution bundle output path (qualified workload strategies only)")
	bundleEvidenceVersion := flags.Int("bundle-evidence-version", 0, "optional trusted bundle evidence version (3 only)")
	methodSpecDigest := flags.String("method-spec-digest", "", "frozen MethodSpec digest required by bundle evidence v3")
	agentKeyFile := flags.String("agent-key-file", "", "key file for an explicit opt-in Agent strategy")
	agentModel := flags.String("agent-model", "", "OpenRouter model ID for an explicit opt-in Agent strategy")
	semanticInput := flags.String("semantic-input", "", "editable protocol knowledge and hypothesis JSON for an Agent strategy")
	campaignDirectory := flags.String("campaign-dir", "", "Campaign directory for an explicit Campaign strategy")
	campaignObservationOut := flags.String("campaign-observation-out", "", "Campaign Observation output path")
	campaignAttempts := flags.Int("campaign-attempts", 0, "attempt limit for an explicit Campaign strategy")
	campaignWallClock := flags.Int64("campaign-wall-clock-ms", 0, "wall-clock ceiling for an explicit Campaign strategy")
	campaignModelTokens := flags.Int("campaign-model-tokens-per-attempt", 0, "model-token allowance per Agent Campaign attempt")
	campaignResume := flags.Bool("campaign-resume", false, "resume an existing exact Campaign config")
	statelessCorpus := flags.String("stateless-corpus", "", "frozen root corpus for a Stateless Campaign strategy")
	strategy := flags.String("strategy", "workload", "qualified strategy, including explicit opt-in Agent strategies")
	decisions := flags.Int("decisions", 96, "charged decisions per run")
	policySeed := flags.Uint64("policy-seed", 1, "public random-policy seed")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *strategy == etcdraftScenarioSessionStrategy {
		if *campaignDirectory == "" || *statelessCorpus == "" || *semanticInput == "" ||
			*agentKeyFile == "" || *agentModel == "" ||
			*out != "" || *bundleOut != "" || *campaignObservationOut != "" || *campaignAttempts != 0 ||
			*campaignWallClock != 0 || *campaignModelTokens != 0 || *bundleEvidenceVersion != 0 ||
			*methodSpecDigest != "" || *decisions != 96 || *policySeed != 1 {
			return errors.New("OpenRouter Scenario session requires -campaign-dir, -stateless-corpus, -semantic-input, -agent-key-file, -agent-model, and optional -campaign-resume")
		}
		summary, err := runEtcdraftScenarioSession(ctx, etcdraftScenarioSessionOptions{
			Directory: *campaignDirectory, CorpusPath: *statelessCorpus, SemanticInputPath: *semanticInput,
			Resume: *campaignResume, AgentKeyFile: *agentKeyFile,
			Client: newOpenRouterIntentClient(*agentModel), ReadKey: readAgentKey,
		})
		if summary.Campaign.CampaignID != "" {
			fmt.Fprintf(
				stdout, "session=%s status=%s episodes=%d stop=%s primary_work=%d replay_work=%d model_calls=%d model_tokens=%d\n",
				*campaignDirectory, summary.Campaign.Status, summary.Campaign.Sequence, summary.Campaign.StopReason,
				summary.Campaign.Totals.Primary.WorkUnits, summary.Campaign.Totals.Replay.WorkUnits,
				summary.Campaign.Totals.Model.Calls, summary.Campaign.Totals.Model.TotalTokens,
			)
			fmt.Fprintf(
				stdout, "testing_episodes=%d replay_stable=%d pss_states=%d risk=%s oracle_violations=%d\n",
				summary.TestingEpisodes, summary.ReplayStableEpisodes, summary.UniqueCorePSSStates,
				summary.BestRiskStatus, summary.OracleViolations,
			)
		}
		return err
	}
	if *strategy == etcdraftScenarioCalibrationStrategy {
		if *campaignDirectory == "" || *statelessCorpus == "" || *semanticInput == "" ||
			*agentKeyFile == "" || *agentModel == "" ||
			*out != "" || *bundleOut != "" || *campaignObservationOut != "" || *campaignAttempts != 0 ||
			*campaignWallClock != 0 || *campaignModelTokens != 0 || *bundleEvidenceVersion != 0 ||
			*methodSpecDigest != "" || *decisions != 96 || *policySeed != 1 {
			return errors.New("OpenRouter Scenario calibration requires -campaign-dir, -stateless-corpus, -semantic-input, -agent-key-file, -agent-model, and optional -campaign-resume")
		}
		summary, err := runEtcdraftScenarioCalibration(ctx, etcdraftScenarioCalibrationRunOptions{
			Directory: *campaignDirectory, CorpusPath: *statelessCorpus, SemanticInputPath: *semanticInput,
			Resume: *campaignResume, AgentKeyFile: *agentKeyFile,
			Client: newOpenRouterIntentClient(*agentModel), ReadKey: readAgentKey,
		})
		if summary.AgentStatus != "" {
			if summary.Testing != nil {
				fmt.Fprintf(
					stdout, "summary=%s status=%s attempts=%d pss_states=%d replay=%t oracle_violations=%d model_calls=%d model_tokens=%d\n",
					filepath.Join(*campaignDirectory, "summary.json"), summary.AgentStatus, summary.Attempts,
					summary.Testing.UniqueCorePSSStates, summary.Testing.ReplayStable,
					summary.Testing.OracleViolations, summary.ModelWork.Calls, summary.ModelWork.TotalTokens,
				)
			} else {
				fmt.Fprintf(
					stdout, "summary=%s status=%s attempts=%d model_calls=%d model_tokens=%d\n",
					filepath.Join(*campaignDirectory, "summary.json"), summary.AgentStatus,
					summary.Attempts, summary.ModelWork.Calls, summary.ModelWork.TotalTokens,
				)
			}
		}
		return err
	}
	if *strategy == etcdraftSemanticCalibrationStrategy {
		if *campaignDirectory == "" || *statelessCorpus == "" || *semanticInput == "" ||
			*agentKeyFile == "" || *agentModel == "" ||
			*out != "" || *bundleOut != "" || *campaignObservationOut != "" || *campaignAttempts != 0 ||
			*campaignWallClock != 0 || *campaignModelTokens != 0 || *bundleEvidenceVersion != 0 ||
			*methodSpecDigest != "" || *decisions != 96 || *policySeed != 1 {
			return errors.New("semantic Explorer calibration requires -campaign-dir, -stateless-corpus, -semantic-input, -agent-key-file, -agent-model, and optional -campaign-resume")
		}
		artifact, err := runEtcdraftSemanticCalibration(ctx, etcdraftSemanticCalibrationRunOptions{
			Directory: *campaignDirectory, CorpusPath: *statelessCorpus, SemanticInputPath: *semanticInput,
			Resume: *campaignResume, AgentKeyFile: *agentKeyFile,
			Client: newOpenRouterIntentClient(*agentModel), ReadKey: readAgentKey,
		})
		if artifact.Digest != "" {
			if artifact.Testing != nil {
				fmt.Fprintf(
					stdout, "artifact=%s status=%s selected=%s pss_states=%d replay=%t oracle_violations=%d model_calls=%d model_tokens=%d digest=%s\n",
					filepath.Join(*campaignDirectory, "artifact.json"), artifact.Status,
					artifact.Testing.SelectedCandidateID, artifact.Testing.UniqueCorePSSStates,
					artifact.Testing.Replay.Stable, len(artifact.Testing.Oracle.Violations),
					artifact.ModelWork.Calls, artifact.ModelWork.TotalTokens, artifact.Digest,
				)
			} else {
				fmt.Fprintf(
					stdout, "artifact=%s status=%s model_calls=%d model_tokens=%d digest=%s\n",
					filepath.Join(*campaignDirectory, "artifact.json"), artifact.Status,
					artifact.ModelWork.Calls, artifact.ModelWork.TotalTokens, artifact.Digest,
				)
			}
		}
		return err
	}
	if *strategy == etcdraftStatelessCanonicalCampaignStrategy ||
		*strategy == etcdraftStatelessUniformCampaignStrategy {
		if *out == "" || *campaignObservationOut == "" || *campaignDirectory == "" ||
			*campaignAttempts <= 0 || *campaignWallClock <= 0 || *statelessCorpus == "" ||
			*decisions != 96 || *bundleOut != "" || *bundleEvidenceVersion != 0 ||
			*methodSpecDigest != "" || *agentKeyFile != "" || *agentModel != "" || *semanticInput != "" ||
			*campaignModelTokens != 0 ||
			(*strategy == etcdraftStatelessCanonicalCampaignStrategy && *policySeed != 1) {
			return errors.New("Stateless Campaign strategy requires only -stateless-corpus, -out, Campaign, and uniform seed flags")
		}
		return runEtcdraftStatelessCampaign(ctx, etcdraftStatelessCampaignRunOptions{
			Directory: *campaignDirectory, SummaryOut: *out,
			ObservationOut: *campaignObservationOut, Resume: *campaignResume,
			Strategy: *strategy, Attempts: *campaignAttempts, FirstSeed: *policySeed,
			WallClockCeilingMillis: *campaignWallClock, CorpusPath: *statelessCorpus,
		}, stdout)
	}
	if *campaignDirectory != "" || *campaignObservationOut != "" || *campaignAttempts != 0 ||
		*campaignWallClock != 0 || *campaignModelTokens != 0 || *campaignResume {
		return errors.New("Campaign flags require an explicit Campaign strategy")
	}
	if *statelessCorpus != "" {
		return errors.New("-stateless-corpus requires a Stateless Campaign strategy")
	}
	if *agentKeyFile != "" {
		return errors.New("Agent flags require an explicit opt-in Agent strategy")
	}
	if *agentModel != "" {
		return errors.New("-agent-model requires an explicit opt-in Agent strategy")
	}
	if *semanticInput != "" {
		return errors.New("-semantic-input requires an explicit opt-in Agent strategy")
	}
	if *out == "" {
		return errors.New("-out is required")
	}
	if (*bundleEvidenceVersion != 0 || *methodSpecDigest != "") && *bundleOut == "" {
		return errors.New("bundle evidence flags require -bundle-out")
	}
	var report controlexperiment.Report
	var bundle *controlexperiment.ExecutionBundle
	var err error
	if *bundleOut != "" {
		if !supportsQualifiedBundleOutput(*strategy) {
			return errors.New("-bundle-out requires a qualified workload strategy")
		}
		var captured controlexperiment.ExecutionBundle
		if *bundleEvidenceVersion == 3 {
			if *methodSpecDigest == "" {
				return errors.New("bundle evidence v3 requires -method-spec-digest")
			}
			report, captured, err = etcdraftBundleV3(
				ctx, *strategy, *decisions, *policySeed, *methodSpecDigest,
			)
		} else {
			if *bundleEvidenceVersion != 0 || *methodSpecDigest != "" {
				return errors.New("only -bundle-evidence-version 3 is supported")
			}
			report, captured, err = etcdraftBundle(ctx, *strategy, *decisions, *policySeed)
		}
		bundle = &captured
	} else {
		report, err = etcdraftReport(ctx, *strategy, *decisions, *policySeed)
	}
	if err != nil {
		return err
	}
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
	if err := writeReport(*out, encoded); err != nil {
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
		if err := writeReport(*bundleOut, bundleBytes); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "wrote %s\nruns=%d decisions=%d states=%d replay=%t digest=%s\n",
		*out, len(report.Runs), report.StateDiscovery.TotalDecisions,
		report.StateDiscovery.UniqueStates, allReplayStable(report), report.Digest)
	return nil
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
