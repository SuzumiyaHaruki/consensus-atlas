package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

type etcdraftAgentFeedbackBatch struct {
	IntentInputs controlexperiment.AgentSemanticView
	ActionClass  etcdraftMethodExecution
	Uniform      etcdraftMethodExecution
	Sources      []controlexperiment.AgentBatchFeedbackInput
	Feedback     controlexperiment.AgentBatchFeedbackView
}

func persistEtcdraftAgentFeedbackBatch(
	out string,
	artifactDir string,
	batch etcdraftAgentFeedbackBatch,
	stdout io.Writer,
) error {
	if err := batch.Feedback.ValidateInputs(batch.IntentInputs, batch.Sources); err != nil {
		return err
	}
	if err := persistEtcdraftMethodExecution(
		filepath.Join(artifactDir, "action-class-method.json"),
		filepath.Join(artifactDir, "action-class-evidence"), batch.ActionClass, io.Discard,
	); err != nil {
		return err
	}
	if err := persistEtcdraftMethodExecution(
		filepath.Join(artifactDir, "uniform-method.json"),
		filepath.Join(artifactDir, "uniform-evidence"), batch.Uniform, io.Discard,
	); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(batch.Feedback, "", "  ")
	if err != nil {
		return err
	}
	var persisted controlexperiment.AgentBatchFeedbackView
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		return err
	}
	if err := persisted.ValidateInputs(batch.IntentInputs, batch.Sources); err != nil {
		return err
	}
	if err := writeReport(out, encoded); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s\nbackends=%d budget=%d/%d/%d digest=%s\n",
		out, len(persisted.Backends), persisted.Budget.MaxExecutionAttempts,
		persisted.Budget.MaxPrimaryWorkUnits, persisted.Budget.MaxReplayWorkUnits,
		persisted.Digest)
	return nil
}

func newEtcdraftAgentFeedbackBatch(
	ctx context.Context,
	decisions int,
	baseSeed uint64,
) (etcdraftAgentFeedbackBatch, error) {
	actionClass, err := etcdraftActionClassMethod(ctx, decisions, baseSeed)
	if err != nil {
		return etcdraftAgentFeedbackBatch{}, err
	}
	uniform, err := etcdraftAdmissibleUniformMethod(ctx, decisions, baseSeed)
	if err != nil {
		return etcdraftAgentFeedbackBatch{}, err
	}
	return newEtcdraftAgentFeedbackBatchFromMethods(ctx, actionClass, uniform)
}

func newEtcdraftAgentFeedbackBatchFromMethods(
	ctx context.Context,
	actionClass etcdraftMethodExecution,
	uniform etcdraftMethodExecution,
) (etcdraftAgentFeedbackBatch, error) {
	intentInputs, err := newEtcdraftIntentInputs(ctx)
	if err != nil {
		return etcdraftAgentFeedbackBatch{}, err
	}
	return newEtcdraftAgentFeedbackBatchFromInputs(
		intentInputs, actionClass, uniform, false,
	)
}

func newEtcdraftComparableAgentFeedbackBatch(
	ctx context.Context,
	decisions int,
	baseSeed uint64,
) (etcdraftAgentFeedbackBatch, error) {
	actionClass, err := etcdraftActionClassMethodV2(ctx, decisions, baseSeed)
	if err != nil {
		return etcdraftAgentFeedbackBatch{}, err
	}
	uniform, err := etcdraftAdmissibleUniformMethod(ctx, decisions, baseSeed)
	if err != nil {
		return etcdraftAgentFeedbackBatch{}, err
	}
	return newEtcdraftComparableAgentFeedbackBatchFromMethods(ctx, actionClass, uniform)
}

func newEtcdraftComparableAgentFeedbackBatchFromMethods(
	ctx context.Context,
	actionClass etcdraftMethodExecution,
	uniform etcdraftMethodExecution,
) (etcdraftAgentFeedbackBatch, error) {
	intentInputs, err := newEtcdraftFeedbackIntentInputs(ctx)
	if err != nil {
		return etcdraftAgentFeedbackBatch{}, err
	}
	return newEtcdraftAgentFeedbackBatchFromInputs(
		intentInputs, actionClass, uniform, true,
	)
}

func newEtcdraftAgentFeedbackBatchFromInputs(
	intentInputs etcdraftIntentInputs,
	actionClass etcdraftMethodExecution,
	uniform etcdraftMethodExecution,
	workloadOutcomes bool,
) (etcdraftAgentFeedbackBatch, error) {
	sources := []controlexperiment.AgentBatchFeedbackInput{
		{
			BackendID: etcdraftBackendActionClass, Observation: actionClass.Observation,
			Bundles: actionClass.Bundles, Mapper: etcdraftv2.CorePSSMapper{},
		},
		{
			BackendID: etcdraftBackendUniform, Observation: uniform.Observation,
			Bundles: uniform.Bundles, Mapper: etcdraftv2.CorePSSMapper{},
		},
	}
	var feedback controlexperiment.AgentBatchFeedbackView
	var err error
	if workloadOutcomes {
		feedback, err = controlexperiment.NewAgentBatchFeedbackViewV2(
			"etcdraft-comparable-agent-feedback-m5-18b3", intentInputs.View, sources,
		)
	} else {
		feedback, err = controlexperiment.NewAgentBatchFeedbackView(
			"etcdraft-agent-batch-feedback-m5-18b2", intentInputs.View, sources,
		)
	}
	if err != nil {
		return etcdraftAgentFeedbackBatch{}, err
	}
	return etcdraftAgentFeedbackBatch{
		IntentInputs: intentInputs.View, ActionClass: actionClass, Uniform: uniform,
		Sources: sources, Feedback: feedback,
	}, nil
}
