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
	sourceBundleOut := flags.String("source-bundle-out", "", "source bundle output path (corpus mutation only)")
	methodOut := flags.String("method-out", "", "method ledger output path (corpus mutation only)")
	methodArtifacts := flags.String("method-artifacts", "", "report/bundle directory (M5.17c2 method strategies only)")
	bundleEvidenceVersion := flags.Int("bundle-evidence-version", 0, "optional trusted bundle evidence version (3 only)")
	methodSpecDigest := flags.String("method-spec-digest", "", "frozen MethodSpec digest required by bundle evidence v3")
	agentKeyFile := flags.String("agent-key-file", "", "key file for an explicit opt-in Agent strategy")
	agentArtifacts := flags.String("agent-artifacts", "", "new output directory for an explicit opt-in Agent strategy")
	campaignDirectory := flags.String("campaign-dir", "", "Campaign directory for an explicit Campaign strategy")
	campaignObservationOut := flags.String("campaign-observation-out", "", "Campaign Observation output path")
	campaignAttempts := flags.Int("campaign-attempts", 0, "attempt limit for an explicit Campaign strategy")
	campaignWallClock := flags.Int64("campaign-wall-clock-ms", 0, "wall-clock ceiling for an explicit Campaign strategy")
	campaignModelTokens := flags.Int("campaign-model-tokens-per-attempt", 0, "model-token allowance per Agent Campaign attempt")
	campaignResume := flags.Bool("campaign-resume", false, "resume an existing exact Campaign config")
	strategy := flags.String("strategy", "workload", "qualified strategy, including explicit opt-in Agent strategies")
	decisions := flags.Int("decisions", 96, "charged decisions per run")
	policySeed := flags.Uint64("policy-seed", 1, "public random-policy seed")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *strategy == etcdraftCampaignModelRunnerStrategy {
		if *out == "" || *campaignObservationOut == "" || *campaignDirectory == "" ||
			*campaignAttempts <= 0 || *campaignWallClock <= 0 || *campaignModelTokens <= 0 ||
			*agentKeyFile == "" || *bundleOut != "" || *sourceBundleOut != "" ||
			*methodOut != "" || *methodArtifacts != "" || *bundleEvidenceVersion != 0 ||
			*methodSpecDigest != "" || *agentArtifacts != "" {
			return errors.New("Agent Campaign strategy requires only -agent-key-file, -out, Campaign, decision, and seed flags")
		}
		return runEtcdraftModelCampaignOptIn(ctx, etcdraftCampaignRunOptions{
			Directory: *campaignDirectory, SummaryOut: *out,
			ObservationOut: *campaignObservationOut, Resume: *campaignResume,
			Attempts: *campaignAttempts, DecisionsPerAttempt: *decisions,
			FirstPolicySeed: *policySeed, WallClockCeilingMillis: *campaignWallClock,
			ModelTokensPerAttempt: *campaignModelTokens,
		}, *agentKeyFile, stdout)
	}
	if *strategy == etcdraftCampaignRunnerStrategy {
		if *out == "" || *campaignObservationOut == "" || *campaignDirectory == "" || *campaignAttempts <= 0 ||
			*campaignWallClock <= 0 || *bundleOut != "" || *sourceBundleOut != "" ||
			*methodOut != "" || *methodArtifacts != "" || *bundleEvidenceVersion != 0 ||
			*methodSpecDigest != "" || *agentKeyFile != "" || *agentArtifacts != "" ||
			*campaignModelTokens != 0 {
			return errors.New("Campaign strategy requires only -out, Campaign, decision, and seed flags")
		}
		return runEtcdraftCampaign(ctx, etcdraftCampaignRunOptions{
			Directory: *campaignDirectory, SummaryOut: *out,
			ObservationOut: *campaignObservationOut, Resume: *campaignResume,
			Attempts: *campaignAttempts, DecisionsPerAttempt: *decisions,
			FirstPolicySeed: *policySeed, WallClockCeilingMillis: *campaignWallClock,
		}, stdout)
	}
	if *campaignDirectory != "" || *campaignObservationOut != "" || *campaignAttempts != 0 ||
		*campaignWallClock != 0 || *campaignModelTokens != 0 || *campaignResume {
		return errors.New("Campaign flags require an explicit Campaign strategy")
	}
	if *strategy == "workload-guarded-agent-one-shot" {
		if *agentKeyFile == "" || *agentArtifacts == "" || *out != "" || *bundleOut != "" ||
			*sourceBundleOut != "" || *methodOut != "" || *methodArtifacts != "" ||
			*bundleEvidenceVersion != 0 || *methodSpecDigest != "" {
			return errors.New("one-shot Agent strategy requires only -agent-key-file and -agent-artifacts")
		}
		if _, err := os.Stat(*agentArtifacts); err == nil || !os.IsNotExist(err) {
			return errors.New("-agent-artifacts must name a new directory")
		}
		key, err := readAgentKey(*agentKeyFile)
		if err != nil {
			return err
		}
		result, stageErr := executeEtcdraftAgentOneShot(ctx, key, defaultDeepSeekIntentClient())
		key = ""
		if result.Audit.SchemaVersion == "" {
			return stageErr
		}
		if err := persistEtcdraftAgentOneShot(*agentArtifacts, result); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "wrote %s\nstatus=%s model_calls=%d audit=%s\n",
			*agentArtifacts, result.Audit.Status, result.Audit.Work.Model.Calls, result.Audit.Digest)
		return stageErr
	}
	if *strategy == etcdraftAgentB4PairStrategy {
		if *agentKeyFile == "" || *agentArtifacts == "" || *out != "" || *bundleOut != "" ||
			*sourceBundleOut != "" || *methodOut != "" || *methodArtifacts != "" ||
			*bundleEvidenceVersion != 0 || *methodSpecDigest != "" {
			return errors.New("Agent b4 pair strategy requires only -agent-key-file and -agent-artifacts")
		}
		return runEtcdraftAgentB4PairOptIn(
			ctx, *decisions, *policySeed, *agentKeyFile, *agentArtifacts, stdout,
		)
	}
	if *agentKeyFile != "" || *agentArtifacts != "" {
		return errors.New("Agent flags require an explicit opt-in Agent strategy")
	}
	if *out == "" {
		return errors.New("-out is required")
	}
	if *strategy == "workload-agent-feedback-batch" {
		if *methodArtifacts == "" || *bundleOut != "" || *sourceBundleOut != "" ||
			*methodOut != "" || *bundleEvidenceVersion != 0 || *methodSpecDigest != "" {
			return errors.New("Agent feedback batch requires only -out and -method-artifacts")
		}
		batch, err := newEtcdraftAgentFeedbackBatch(ctx, *decisions, *policySeed)
		if err != nil {
			return err
		}
		return persistEtcdraftAgentFeedbackBatch(*out, *methodArtifacts, batch, stdout)
	}
	if *strategy == "workload-agent-follow-up-baseline" {
		if *methodArtifacts == "" || *bundleOut != "" || *sourceBundleOut != "" ||
			*methodOut != "" || *bundleEvidenceVersion != 0 || *methodSpecDigest != "" {
			return errors.New("Agent follow-up baseline requires only -out and -method-artifacts")
		}
		result, err := newEtcdraftAgentFollowUpBaseline(ctx, *decisions, *policySeed)
		if err != nil {
			return err
		}
		return persistEtcdraftAgentFollowUpBaseline(*out, *methodArtifacts, result, stdout)
	}
	if *strategy == "workload-agent-b4-preflight" {
		if *methodArtifacts == "" || *bundleOut != "" || *sourceBundleOut != "" ||
			*methodOut != "" || *bundleEvidenceVersion != 0 || *methodSpecDigest != "" {
			return errors.New("Agent b4 preflight requires only -out and -method-artifacts")
		}
		result, err := newEtcdraftAgentB4Preflight(ctx, *decisions, *policySeed)
		if err != nil {
			return err
		}
		return persistEtcdraftAgentB4Preflight(*out, *methodArtifacts, result, stdout)
	}
	if *strategy == "workload-agent-b4-freeze" {
		if *methodArtifacts == "" || *bundleOut != "" || *sourceBundleOut != "" ||
			*methodOut != "" || *bundleEvidenceVersion != 0 || *methodSpecDigest != "" {
			return errors.New("Agent b4 request freeze requires only -out and -method-artifacts")
		}
		result, err := newEtcdraftAgentB4RequestFreeze(ctx, *decisions, *policySeed)
		if err != nil {
			return err
		}
		return persistEtcdraftAgentB4RequestFreeze(*out, *methodArtifacts, result, stdout)
	}
	if *strategy == "workload-admissible-uniform-method" ||
		*strategy == "workload-action-class-random-method" ||
		*strategy == "workload-pss-guided-corpus" {
		if *methodArtifacts == "" || *bundleOut != "" || *sourceBundleOut != "" || *methodOut != "" ||
			*bundleEvidenceVersion != 0 || *methodSpecDigest != "" {
			return errors.New("method strategy requires only -out and -method-artifacts")
		}
		var result etcdraftMethodExecution
		var err error
		if *strategy == "workload-admissible-uniform-method" {
			result, err = etcdraftAdmissibleUniformMethod(ctx, *decisions, *policySeed)
		} else if *strategy == "workload-action-class-random-method" {
			result, err = etcdraftActionClassMethod(ctx, *decisions, *policySeed)
		} else {
			result, err = etcdraftPSSGuidedCorpusMethod(ctx, *decisions, *policySeed)
		}
		if err != nil {
			return err
		}
		return persistEtcdraftMethodExecution(*out, *methodArtifacts, result, stdout)
	}
	if *methodArtifacts != "" {
		return errors.New("-method-artifacts requires a method or Agent evidence strategy")
	}
	if (*bundleEvidenceVersion != 0 || *methodSpecDigest != "") && *bundleOut == "" {
		return errors.New("bundle evidence flags require -bundle-out")
	}
	var report controlexperiment.Report
	var bundle *controlexperiment.ExecutionBundle
	var sourceBundle *controlexperiment.ExecutionBundle
	var method *controlexperiment.MethodLedger
	var err error
	if *strategy == "workload-trace-mutation-corpus" {
		if *bundleOut == "" || *sourceBundleOut == "" || *methodOut == "" {
			return errors.New("corpus mutation requires -source-bundle-out, -bundle-out, and -method-out")
		}
		var captured controlexperiment.ExecutionBundle
		var capturedSource controlexperiment.ExecutionBundle
		var ledger controlexperiment.MethodLedger
		report, captured, capturedSource, ledger, err = etcdraftCorpusMutationMethod(
			ctx, *decisions, *policySeed,
		)
		bundle = &captured
		sourceBundle = &capturedSource
		method = &ledger
	} else if *sourceBundleOut != "" || *methodOut != "" {
		return errors.New("-source-bundle-out and -method-out require workload-trace-mutation-corpus")
	} else if *bundleOut != "" {
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
	if method != nil {
		sourceBytes, err := json.MarshalIndent(sourceBundle, "", "  ")
		if err != nil {
			return err
		}
		var persistedSource controlexperiment.ExecutionBundle
		if err := json.Unmarshal(sourceBytes, &persistedSource); err != nil {
			return err
		}
		if err := persistedSource.Validate(); err != nil {
			return fmt.Errorf("validate persisted source bundle: %w", err)
		}
		if err := method.SourceCorpus.Entries[0].ValidateBundle(persistedSource); err != nil {
			return fmt.Errorf("validate persisted corpus source: %w", err)
		}
		if err := writeReport(*sourceBundleOut, sourceBytes); err != nil {
			return err
		}
		methodBytes, err := json.MarshalIndent(method, "", "  ")
		if err != nil {
			return err
		}
		var persistedMethod controlexperiment.MethodLedger
		if err := json.Unmarshal(methodBytes, &persistedMethod); err != nil {
			return err
		}
		if err := persistedMethod.Validate(); err != nil {
			return fmt.Errorf("validate persisted method ledger: %w", err)
		}
		if err := persistedMethod.Feedback.ValidateBundle(
			*bundle, etcdraftv2.CorePSSMapper{},
		); err != nil {
			return fmt.Errorf("validate persisted method feedback: %w", err)
		}
		if err := writeReport(*methodOut, methodBytes); err != nil {
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

func persistEtcdraftMethodExecution(
	out string,
	artifactDir string,
	result etcdraftMethodExecution,
	stdout io.Writer,
) error {
	if len(result.Reports) == 0 || len(result.Reports) != len(result.Bundles) {
		return errors.New("method report/bundle count mismatch")
	}
	persistedBundles := make([]controlexperiment.ExecutionBundle, len(result.Bundles))
	for index := range result.Reports {
		reportBytes, err := json.MarshalIndent(result.Reports[index], "", "  ")
		if err != nil {
			return err
		}
		var report controlexperiment.Report
		if err := json.Unmarshal(reportBytes, &report); err != nil {
			return err
		}
		if err := report.Validate(); err != nil {
			return fmt.Errorf("validate persisted method report %d: %w", index+1, err)
		}
		bundleBytes, err := json.MarshalIndent(result.Bundles[index], "", "  ")
		if err != nil {
			return err
		}
		if err := json.Unmarshal(bundleBytes, &persistedBundles[index]); err != nil {
			return err
		}
		if err := persistedBundles[index].Validate(); err != nil {
			return fmt.Errorf("validate persisted method bundle %d: %w", index+1, err)
		}
		if err := persistedBundles[index].ValidateProjection(etcdraftv2.DecisionProjector{}); err != nil {
			return fmt.Errorf("validate persisted method projection %d: %w", index+1, err)
		}
		if err := writeReport(
			filepath.Join(artifactDir, "reports", report.Digest+".json"), reportBytes,
		); err != nil {
			return err
		}
		if err := writeReport(
			filepath.Join(artifactDir, "bundles", persistedBundles[index].Digest+".json"), bundleBytes,
		); err != nil {
			return err
		}
	}
	observationBytes, err := json.MarshalIndent(result.Observation, "", "  ")
	if err != nil {
		return err
	}
	var observation controlexperiment.MethodObservation
	if err := json.Unmarshal(observationBytes, &observation); err != nil {
		return err
	}
	if err := observation.ValidateBundles(persistedBundles, etcdraftv2.CorePSSMapper{}); err != nil {
		return fmt.Errorf("validate persisted method observation: %w", err)
	}
	if err := writeReport(out, observationBytes); err != nil {
		return err
	}
	failure := "none"
	if result.Failure != nil {
		failure = result.Failure.Code
	}
	fmt.Fprintf(stdout, "wrote %s\nruns=%d states=%d primary=%d replay=%d failure=%s digest=%s\n",
		out, len(result.Bundles), result.Observation.Measurement.UniqueStates,
		result.Observation.Ledger.Totals.Primary.WorkUnits,
		result.Observation.Ledger.Totals.Replay.WorkUnits, failure, result.Observation.Digest)
	return nil
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
		"workload-action-class-random", "workload-action-class-random-b4",
		"workload-trace-mutation":
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
	if strategy == "workload-trace-mutation" {
		return etcdraftTraceMutationExecution(ctx, decisions, policySeed, captureBundle)
	}
	if strategy == "workload-trace-mutation-corpus" {
		report, bundle, _, _, err := etcdraftCorpusMutationMethod(ctx, decisions, policySeed)
		if !captureBundle {
			bundle = controlexperiment.ExecutionBundle{}
		}
		return report, bundle, err
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

func etcdraftCorpusMutationMethod(
	ctx context.Context,
	decisions int,
	policySeed uint64,
) (controlexperiment.Report, controlexperiment.ExecutionBundle, controlexperiment.ExecutionBundle, controlexperiment.MethodLedger, error) {
	seedReport, seedBundle, err := etcdraftExecution(ctx, "workload", decisions, policySeed, true)
	if err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{},
			controlexperiment.ExecutionBundle{}, controlexperiment.MethodLedger{}, err
	}
	source, err := controlexperiment.NewMutationSourceEntry("etcdraft-workload-source-1", seedBundle)
	if err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{},
			controlexperiment.ExecutionBundle{}, controlexperiment.MethodLedger{}, err
	}
	corpus, err := controlexperiment.NewMutationSourceCorpus(
		"etcdraft-public-source-corpus-m5.17c1", []controlexperiment.MutationSourceEntry{source},
	)
	if err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{},
			controlexperiment.ExecutionBundle{}, controlexperiment.MethodLedger{}, err
	}
	plan, err := controlexperiment.NewFirstAdjacentCorpusMutation(
		"first-adjacent-message-deliveries-v2", seedBundle.Trace, source,
		control.ActionDeliverMessage, seedReport.Config.Runs[0].Policy.Priority,
	)
	if err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{},
			controlexperiment.ExecutionBundle{}, controlexperiment.MethodLedger{}, err
	}
	config := seedReport.Config
	config.ID = "public-etcdraft-v2-corpus-mutation-m5.17c1"
	config.Runs = append([]controlexperiment.RunPlan(nil), seedReport.Config.Runs...)
	config.Runs[0].Policy = controlexperiment.Policy{
		Version: controlexperiment.TraceMutationPolicyVersionV2,
		ID:      "adjacent-message-swap-v2/run-1", TraceMutation: &plan,
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	report, bundle, err := controlexperiment.ExecuteQualifiedBundle(
		ctx, config, seedBundle.Qualification, factory, etcdraftv2.CorePSSMapper{},
		etcdraftv2.DecisionProjector{}, etcdraftv2.WorkloadRouter{},
	)
	if err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{},
			controlexperiment.ExecutionBundle{}, controlexperiment.MethodLedger{}, err
	}
	feedback, err := controlexperiment.NewPSSFeedback(
		"etcdraft-corpus-mutation-feedback-m5.17c1", bundle, etcdraftv2.CorePSSMapper{},
	)
	if err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{},
			controlexperiment.ExecutionBundle{}, controlexperiment.MethodLedger{}, err
	}
	zeroWork := controlexperiment.WorkLedger{Resources: controlexperiment.ResourceAccounting{
		WallTime: controlexperiment.ResourceNotCollected,
		CPUTime:  controlexperiment.ResourceNotCollected,
		PeakRSS:  controlexperiment.ResourceNotCollected,
	}}
	records := []controlexperiment.MethodRecord{
		{
			Ordinal: 1, Kind: controlexperiment.MethodRecordSource,
			Outcome:     controlexperiment.MethodOutcomeCompleted,
			InputDigest: source.ConfigDigest, OutputDigest: source.Digest,
			ReportDigest: seedReport.Digest, BundleDigest: seedBundle.Digest, Work: seedReport.Work,
		},
		{
			Ordinal: 2, Kind: controlexperiment.MethodRecordProposal,
			Outcome:     controlexperiment.MethodOutcomeCompleted,
			InputDigest: source.Digest, OutputDigest: plan.Digest, Work: zeroWork,
		},
		{
			Ordinal: 3, Kind: controlexperiment.MethodRecordExecution,
			Outcome:     controlexperiment.MethodOutcomeCompleted,
			InputDigest: plan.Digest, OutputDigest: bundle.Digest,
			ReportDigest: report.Digest, BundleDigest: bundle.Digest, Work: report.Work,
		},
	}
	ledger, err := controlexperiment.NewMethodLedger(
		"etcdraft-corpus-mutation-method-m5.17c1", "first-adjacent-corpus-mutation/v1",
		corpus, &feedback, records,
	)
	if err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{},
			controlexperiment.ExecutionBundle{}, controlexperiment.MethodLedger{}, err
	}
	return report, bundle, seedBundle, ledger, nil
}

func etcdraftTraceMutationExecution(
	ctx context.Context,
	decisions int,
	policySeed uint64,
	captureBundle bool,
) (controlexperiment.Report, controlexperiment.ExecutionBundle, error) {
	seedReport, seedBundle, err := etcdraftExecution(ctx, "workload", decisions, policySeed, true)
	if err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	plan, err := controlexperiment.NewFirstAdjacentTraceMutation(
		"first-adjacent-message-deliveries-v1", seedBundle.Trace,
		seedReport.Config.Runs[0].Policy, control.ActionDeliverMessage,
	)
	if err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	config := seedReport.Config
	config.ID = "public-etcdraft-v2-trace-mutation-m5.17b"
	config.Runs = append([]controlexperiment.RunPlan(nil), seedReport.Config.Runs...)
	config.Runs[0].Policy = controlexperiment.Policy{
		Version: controlexperiment.TraceMutationPolicyVersion,
		ID:      "adjacent-message-swap-v1/run-1", TraceMutation: &plan,
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	if captureBundle {
		return controlexperiment.ExecuteQualifiedBundle(
			ctx, config, seedBundle.Qualification, factory, etcdraftv2.CorePSSMapper{},
			etcdraftv2.DecisionProjector{}, etcdraftv2.WorkloadRouter{},
		)
	}
	report, err := controlexperiment.ExecuteQualified(
		ctx, config, seedBundle.Qualification.Qualification, factory, etcdraftv2.CorePSSMapper{},
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
