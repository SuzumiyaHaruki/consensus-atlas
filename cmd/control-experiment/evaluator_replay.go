package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const evaluatorReplayStrategy = "evaluator-replay-v1"

// runEvaluatorReplayCLI is the evaluator-owned entry into the existing
// qualified executor. It performs no search and accepts no Agent input: the
// exact policy, seed, workload and limits come from the sealed Bundle recipe.
func runEvaluatorReplayCLI(ctx context.Context, options controlExperimentOptions, stdout io.Writer) error {
	if options.BundleIn == "" || options.BundleOut == "" || options.Out == "" || options.Target == "" ||
		options.CampaignDirectory != "" || options.CampaignResume || options.AgentKeyFile != "" ||
		options.AgentModel != "" || options.SemanticInput != "" || options.RiskInput != "" ||
		len(options.KnowledgeSourceMounts) != 0 || options.InvestigationEpisodes != 1 {
		return errors.New("EVALUATOR_REPLAY_INPUT_INVALID")
	}
	var source controlexperiment.ExecutionBundle
	if err := readStrictJSONFile(options.BundleIn, 64<<20, &source); err != nil ||
		source.Validate() != nil || source.Recipe == nil || source.Recipe.TargetID != options.Target ||
		source.Identity.MethodSpecDigest == "" {
		return errors.New("EVALUATOR_REPLAY_BUNDLE_INVALID")
	}
	var report controlexperiment.Report
	var fresh controlexperiment.ExecutionBundle
	var err error
	switch options.Target {
	case "etcdraft-v2":
		var adapterConfig etcdraftv2.Config
		if decodeStrictReplayTargetConfig(source.Recipe.TargetConfig, &adapterConfig) != nil {
			return errors.New("EVALUATOR_REPLAY_TARGET_CONFIG_INVALID")
		}
		factory := func() (control.Adapter, error) { return etcdraftv2.NewWithConfig(adapterConfig) }
		report, fresh, err = controlexperiment.ExecuteQualifiedBundleV3(
			ctx, source.Recipe.Config, source.Qualification, factory, etcdraftv2.CorePSSMapper{},
			etcdraftv2.DecisionProjector{}, etcdraftv2.WorkloadRouter{}, source.Identity.MethodSpecDigest,
		)
	case "omnipaxos-v2":
		var adapterConfig omnipaxosv2.Config
		if options.WorkerPath == "" ||
			decodeStrictReplayTargetConfig(source.Recipe.TargetConfig, &adapterConfig) != nil ||
			adapterConfig.ValidateNodeConfiguration() != nil {
			return errors.New("EVALUATOR_REPLAY_WORKER_REQUIRED")
		}
		adapterConfig.WorkerPath = options.WorkerPath
		factory := func() (control.Adapter, error) {
			return omnipaxosv2.New(adapterConfig)
		}
		report, fresh, err = controlexperiment.ExecuteQualifiedBundleV3(
			ctx, source.Recipe.Config, source.Qualification, factory, omnipaxosv2.CorePSSMapper{},
			omnipaxosv2.DecisionProjector{}, omnipaxosv2.WorkloadRouter{}, source.Identity.MethodSpecDigest,
		)
	default:
		return errors.New("EVALUATOR_REPLAY_TARGET_UNSUPPORTED")
	}
	if err != nil {
		return err
	}
	fresh, err = fresh.WithExecutionRecipe(*source.Recipe)
	if err != nil || fresh.Trace.Digest != source.Trace.Digest ||
		len(fresh.Trace.Records) != len(source.Trace.Records) {
		return errors.New("EVALUATOR_REPLAY_TRACE_MISMATCH")
	}
	reportBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	bundleBytes, err := json.MarshalIndent(fresh, "", "  ")
	if err != nil {
		return err
	}
	if err := writeReport(options.Out, reportBytes); err != nil {
		return err
	}
	if err := writeReport(options.BundleOut, bundleBytes); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "replayed target=%s decisions=%d trace=%s\n", options.Target, len(fresh.Trace.Records), fresh.Trace.Digest)
	return err
}

func decodeStrictReplayTargetConfig(encoded []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) == nil {
		return errors.New("EVALUATOR_REPLAY_TARGET_CONFIG_TRAILING_JSON")
	}
	return nil
}
