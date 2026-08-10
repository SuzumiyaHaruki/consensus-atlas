package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	etcdraftCampaignSpecVersion     = "consensus-atlas/etcdraft-campaign-spec/v1"
	etcdraftCampaignArtifactVersion = "consensus-atlas/etcdraft-campaign-artifact/v3"
	etcdraftCampaignComposition     = "consensus-atlas/etcdraft-campaign-composition/v2"
	etcdraftCampaignPlannerRule     = "deterministic-pss-novelty-rotation-v1"
	etcdraftCampaignTargetID        = "etcdraft-v2"
	etcdraftCampaignStrategy        = "workload-admissible-uniform-b4"
	etcdraftCampaignSeedRule        = "first-seed-plus-ordinal-minus-one"
)

// etcdraftCampaignSpec freezes only target-specific composition choices. The
// generic Campaign package neither imports etcd/raft nor interprets them.
type etcdraftCampaignSpec struct {
	SchemaVersion       string `json:"schema_version"`
	ID                  string `json:"id"`
	Strategy            string `json:"strategy"`
	DecisionsPerAttempt int    `json:"decisions_per_attempt"`
	FirstPolicySeed     uint64 `json:"first_policy_seed"`
	SeedRule            string `json:"seed_rule"`
	ArtifactSchema      string `json:"artifact_schema"`
	Digest              string `json:"digest"`
}

func newEtcdraftCampaignSpec(
	id string,
	decisionsPerAttempt int,
	firstPolicySeed uint64,
) (etcdraftCampaignSpec, error) {
	spec := etcdraftCampaignSpec{
		SchemaVersion: etcdraftCampaignSpecVersion,
		ID:            id, Strategy: etcdraftCampaignStrategy,
		DecisionsPerAttempt: decisionsPerAttempt,
		FirstPolicySeed:     firstPolicySeed, SeedRule: etcdraftCampaignSeedRule,
		ArtifactSchema: etcdraftCampaignArtifactVersion,
	}
	sealed, err := spec.seal()
	if err != nil {
		return etcdraftCampaignSpec{}, err
	}
	if err := sealed.validate(); err != nil {
		return etcdraftCampaignSpec{}, err
	}
	return sealed, nil
}

func (spec etcdraftCampaignSpec) validate() error {
	if spec.SchemaVersion != etcdraftCampaignSpecVersion || spec.ID == "" ||
		spec.Strategy != etcdraftCampaignStrategy || spec.DecisionsPerAttempt <= 0 ||
		spec.FirstPolicySeed == 0 || spec.SeedRule != etcdraftCampaignSeedRule ||
		spec.ArtifactSchema != etcdraftCampaignArtifactVersion {
		return errors.New("ETCDRAFT_CAMPAIGN_SPEC_INVALID")
	}
	sealed, err := spec.seal()
	if err != nil || sealed.Digest != spec.Digest {
		return errors.New("ETCDRAFT_CAMPAIGN_SPEC_DIGEST_MISMATCH")
	}
	return nil
}

func (spec etcdraftCampaignSpec) seed(ordinal int) (uint64, error) {
	if err := spec.validate(); err != nil {
		return 0, err
	}
	if ordinal <= 0 || uint64(ordinal-1) > math.MaxUint64-spec.FirstPolicySeed {
		return 0, errors.New("ETCDRAFT_CAMPAIGN_SEED_OVERFLOW")
	}
	return spec.FirstPolicySeed + uint64(ordinal-1), nil
}

func (spec etcdraftCampaignSpec) seal() (etcdraftCampaignSpec, error) {
	spec.Digest = ""
	digest, err := control.CanonicalDigest(spec)
	if err != nil {
		return etcdraftCampaignSpec{}, err
	}
	spec.Digest = digest
	return spec, nil
}

type etcdraftCampaignProvider struct {
	spec                 etcdraftCampaignSpec
	targetIdentityDigest string
	experimentSpecDigest string
	inputs               etcdraftIntentInputs
	intent               controlexperiment.GuardedTestIntent
	plan                 controlexperiment.CompiledIntentPlanV2
	plannedAttemptDigest string
	execute              func(
		context.Context, string, int, uint64, bool,
	) (controlexperiment.Report, controlexperiment.ExecutionBundle, error)
}

func newEtcdraftCampaignProvider(
	ctx context.Context,
	spec etcdraftCampaignSpec,
) (etcdraftCampaignProvider, error) {
	if err := spec.validate(); err != nil {
		return etcdraftCampaignProvider{}, err
	}
	inputs, err := newEtcdraftB4IntentInputs(ctx)
	if err != nil {
		return etcdraftCampaignProvider{}, err
	}
	intent, err := controlexperiment.NewGuardedTestIntent(controlexperiment.GuardedTestIntent{
		ID: "etcdraft-campaign-intent-m5-21b", ViewDigest: inputs.View.Digest,
		RiskID: etcdraftIntentRiskLeaderChange,
		Must: controlexperiment.IntentMust{
			Decisions: spec.DecisionsPerAttempt,
			FaultEnvelope: controlexperiment.FaultEnvelope{
				MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
				MaxMessageDuplicates: 1,
			},
		},
		Prefer: controlexperiment.IntentPrefer{BackendIDs: []string{etcdraftBackendUniform}},
	})
	if err != nil {
		return etcdraftCampaignProvider{}, err
	}
	plan, err := controlexperiment.CompileGuardedTestIntentV2(
		inputs.View, inputs.Knowledge, inputs.Catalog, inputs.Qualification.Manifest,
		inputs.Qualification.Qualification, intent,
	)
	if err != nil {
		return etcdraftCampaignProvider{}, err
	}
	provider := etcdraftCampaignProvider{
		spec: spec, targetIdentityDigest: inputs.Qualification.Qualification.ManifestDigest,
		inputs: inputs, intent: intent, plan: plan, execute: etcdraftExecution,
	}
	provider.experimentSpecDigest, err = provider.compositionDigest()
	if err != nil {
		return etcdraftCampaignProvider{}, err
	}
	if err := provider.validate(); err != nil {
		return etcdraftCampaignProvider{}, err
	}
	return provider, nil
}

func (provider etcdraftCampaignProvider) compositionDigest() (string, error) {
	baseline, err := provider.baseline()
	if err != nil {
		return "", err
	}
	return control.CanonicalDigest(struct {
		SchemaVersion      string `json:"schema_version"`
		SpecDigest         string `json:"spec_digest"`
		SemanticViewDigest string `json:"semantic_view_digest"`
		BaselineDigest     string `json:"baseline_digest"`
		PlannerRule        string `json:"planner_rule"`
	}{
		SchemaVersion: etcdraftCampaignComposition, SpecDigest: provider.spec.Digest,
		SemanticViewDigest: provider.inputs.View.Digest,
		BaselineDigest:     baseline.Digest, PlannerRule: etcdraftCampaignPlannerRule,
	})
}

func (provider etcdraftCampaignProvider) baseline() (controlexperiment.GuardedTestIntent, error) {
	baseline := provider.intent
	baseline.Prefer = controlexperiment.IntentPrefer{}
	baseline.Digest = ""
	return controlexperiment.NewGuardedTestIntent(baseline)
}

func (provider etcdraftCampaignProvider) validate() error {
	if err := provider.spec.validate(); err != nil {
		return err
	}
	if provider.targetIdentityDigest != provider.inputs.Qualification.Qualification.ManifestDigest ||
		provider.plan.Decisions != provider.spec.DecisionsPerAttempt {
		return errors.New("ETCDRAFT_CAMPAIGN_PLAN_BINDING_INVALID")
	}
	if err := provider.plan.ValidateInputs(
		provider.inputs.View, provider.inputs.Knowledge, provider.inputs.Catalog,
		provider.inputs.Qualification.Manifest, provider.inputs.Qualification.Qualification, provider.intent,
	); err != nil {
		return err
	}
	digest, err := provider.compositionDigest()
	if err != nil || digest != provider.experimentSpecDigest {
		return errors.New("ETCDRAFT_CAMPAIGN_COMPOSITION_DIGEST_MISMATCH")
	}
	return nil
}

func (provider etcdraftCampaignProvider) campaignConfig(
	id string,
	attempts int,
	wallClockCeilingMillis int64,
) (controlexperiment.CampaignConfig, error) {
	if err := provider.validate(); err != nil || attempts <= 0 ||
		provider.targetIdentityDigest == "" {
		return controlexperiment.CampaignConfig{}, errors.New("ETCDRAFT_CAMPAIGN_COMPOSITION_INVALID")
	}
	decisions, ok := checkedCampaignMultiply(attempts, provider.spec.DecisionsPerAttempt)
	if !ok {
		return controlexperiment.CampaignConfig{}, errors.New("ETCDRAFT_CAMPAIGN_BUDGET_OVERFLOW")
	}
	work, ok := checkedCampaignMultiply(attempts, provider.spec.DecisionsPerAttempt+2)
	if !ok {
		return controlexperiment.CampaignConfig{}, errors.New("ETCDRAFT_CAMPAIGN_BUDGET_OVERFLOW")
	}
	config, err := controlexperiment.NewCampaignConfig(
		id, etcdraftCampaignTargetID, provider.targetIdentityDigest, provider.experimentSpecDigest,
		controlexperiment.CampaignLogicalBudget{
			MaxAttempts: attempts, MaxPrimarySchedulerDecisions: decisions,
			MaxPrimaryWorkUnits: work, MaxReplayWorkUnits: work,
		},
		wallClockCeilingMillis,
	)
	if err != nil {
		return controlexperiment.CampaignConfig{}, err
	}
	return controlexperiment.RequirePlannedCampaignAttempts(config)
}

func checkedCampaignMultiply(left int, right int) (int, bool) {
	if left <= 0 || right <= 0 || left > int(^uint(0)>>1)/right {
		return 0, false
	}
	return left * right, true
}

func (provider etcdraftCampaignProvider) Attempt(
	ctx context.Context,
	request controlexperiment.CampaignAttemptRequest,
) (controlexperiment.CampaignAttemptResult, error) {
	if err := provider.validate(); err != nil {
		return controlexperiment.CampaignAttemptResult{}, err
	}
	if provider.execute == nil {
		return controlexperiment.CampaignAttemptResult{}, errors.New("ETCDRAFT_CAMPAIGN_EXECUTOR_REQUIRED")
	}
	if err := request.Validate(); err != nil {
		return controlexperiment.CampaignAttemptResult{}, err
	}
	if request.TargetID != etcdraftCampaignTargetID ||
		request.TargetIdentityDigest != provider.targetIdentityDigest ||
		request.ExperimentSpecDigest != provider.experimentSpecDigest {
		return controlexperiment.CampaignAttemptResult{}, errors.New("ETCDRAFT_CAMPAIGN_REQUEST_IDENTITY_MISMATCH")
	}
	seed, err := provider.spec.seed(request.Ordinal)
	if err != nil {
		return controlexperiment.CampaignAttemptResult{}, err
	}
	requiredWork := provider.spec.DecisionsPerAttempt + 2
	if request.Allowance.PrimarySchedulerDecisions < provider.spec.DecisionsPerAttempt ||
		request.Allowance.PrimaryWorkUnits < requiredWork ||
		request.Allowance.ReplayWorkUnits < requiredWork {
		return controlexperiment.CampaignAttemptResult{}, errors.New("ETCDRAFT_CAMPAIGN_ALLOWANCE_TOO_SMALL")
	}
	instance, err := provider.executionInstance(request.Ordinal, seed)
	if err != nil {
		return controlexperiment.CampaignAttemptResult{}, err
	}
	choice, err := controlexperiment.NewCampaignExecutionChoice(provider.intent, provider.plan, instance)
	if err != nil {
		return controlexperiment.CampaignAttemptResult{}, err
	}

	report, bundle, executionErr := provider.execute(
		ctx, provider.plan.Strategy, provider.plan.Decisions, seed, true,
	)
	if executionErr != nil {
		var failure *controlexperiment.ExecutionFailure
		if !errors.As(executionErr, &failure) {
			return controlexperiment.CampaignAttemptResult{}, executionErr
		}
		typed := &controlexperiment.MethodFailure{
			Phase: failure.Phase, Code: failure.Code, Decision: failure.Decision,
		}
		artifact, encoded, err := newEtcdraftCampaignArtifact(
			request, provider, choice, controlexperiment.CampaignAttemptFailed,
			typed, failure.Work, nil, nil,
		)
		if err != nil {
			return controlexperiment.CampaignAttemptResult{}, err
		}
		if err := artifact.validate(request, provider); err != nil {
			return controlexperiment.CampaignAttemptResult{}, err
		}
		return controlexperiment.CampaignAttemptResult{
			InputDigest: provider.plannedAttemptDigest, Outcome: artifact.Outcome, Failure: artifact.Failure,
			Work: artifact.Work, Artifact: encoded,
		}, nil
	}
	artifact, encoded, err := newEtcdraftCampaignArtifact(
		request, provider, choice, controlexperiment.CampaignAttemptCompleted,
		nil, report.Work, &report, &bundle,
	)
	if err != nil {
		return controlexperiment.CampaignAttemptResult{}, err
	}
	if err := artifact.validate(request, provider); err != nil {
		return controlexperiment.CampaignAttemptResult{}, err
	}
	return controlexperiment.CampaignAttemptResult{
		InputDigest: provider.plannedAttemptDigest,
		Outcome:     artifact.Outcome, Work: artifact.Work, Artifact: encoded,
	}, nil
}

func (provider etcdraftCampaignProvider) executionInstance(
	ordinal int,
	seed uint64,
) (controlexperiment.IntentExecutionInstance, error) {
	return controlexperiment.NewIntentExecutionInstance(
		fmt.Sprintf("etcdraft-campaign-attempt-%d", ordinal), provider.plan, seed,
		controlexperiment.MethodBudget{
			MaxExecutionAttempts: 1,
			MaxPrimaryWorkUnits:  provider.spec.DecisionsPerAttempt + 2,
			MaxReplayWorkUnits:   provider.spec.DecisionsPerAttempt + 2,
		},
	)
}

type etcdraftCampaignArtifact struct {
	SchemaVersion        string                                    `json:"schema_version"`
	RequestDigest        string                                    `json:"request_digest"`
	ExperimentSpecDigest string                                    `json:"experiment_spec_digest"`
	TargetIdentityDigest string                                    `json:"target_identity_digest"`
	Ordinal              int                                       `json:"ordinal"`
	PlannedAttemptDigest string                                    `json:"planned_attempt_digest"`
	Choice               controlexperiment.CampaignExecutionChoice `json:"choice"`
	Outcome              string                                    `json:"outcome"`
	Failure              *controlexperiment.MethodFailure          `json:"failure,omitempty"`
	Work                 controlexperiment.WorkLedger              `json:"work"`
	Report               *controlexperiment.Report                 `json:"report,omitempty"`
	Bundle               *controlexperiment.ExecutionBundle        `json:"bundle,omitempty"`
}

func newEtcdraftCampaignArtifact(
	request controlexperiment.CampaignAttemptRequest,
	provider etcdraftCampaignProvider,
	choice controlexperiment.CampaignExecutionChoice,
	outcome string,
	failure *controlexperiment.MethodFailure,
	work controlexperiment.WorkLedger,
	report *controlexperiment.Report,
	bundle *controlexperiment.ExecutionBundle,
) (etcdraftCampaignArtifact, []byte, error) {
	artifact := etcdraftCampaignArtifact{
		SchemaVersion: etcdraftCampaignArtifactVersion,
		RequestDigest: request.Digest, ExperimentSpecDigest: provider.experimentSpecDigest,
		TargetIdentityDigest: request.TargetIdentityDigest,
		Ordinal:              request.Ordinal, PlannedAttemptDigest: provider.plannedAttemptDigest,
		Choice: choice, Outcome: outcome,
		Failure: failure, Work: work, Report: report, Bundle: bundle,
	}
	encoded, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return etcdraftCampaignArtifact{}, nil, err
	}
	return artifact, append(encoded, '\n'), nil
}

func (artifact etcdraftCampaignArtifact) validate(
	request controlexperiment.CampaignAttemptRequest,
	provider etcdraftCampaignProvider,
) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if err := provider.validate(); err != nil {
		return err
	}
	seed, err := provider.spec.seed(request.Ordinal)
	if err != nil {
		return err
	}
	instance, err := provider.executionInstance(request.Ordinal, seed)
	if err != nil {
		return err
	}
	if err := artifact.Choice.ValidateInputs(provider.intent, provider.plan, instance); err != nil {
		return err
	}
	if artifact.SchemaVersion != etcdraftCampaignArtifactVersion ||
		artifact.RequestDigest != request.Digest || artifact.ExperimentSpecDigest != provider.experimentSpecDigest ||
		artifact.TargetIdentityDigest != provider.targetIdentityDigest ||
		artifact.TargetIdentityDigest != request.TargetIdentityDigest ||
		artifact.Ordinal != request.Ordinal || artifact.PlannedAttemptDigest != provider.plannedAttemptDigest {
		return errors.New("ETCDRAFT_CAMPAIGN_ARTIFACT_IDENTITY_MISMATCH")
	}
	if _, err := controlexperiment.NewCampaignAttemptRecord(controlexperiment.CampaignAttemptRecord{
		Ordinal: artifact.Ordinal, ID: fmt.Sprintf("artifact-validation-%d", artifact.Ordinal),
		InputDigest:    artifact.PlannedAttemptDigest,
		ArtifactDigest: artifact.ExperimentSpecDigest,
		Outcome:        artifact.Outcome, Failure: artifact.Failure, Work: artifact.Work,
	}); err != nil {
		return err
	}
	switch artifact.Outcome {
	case controlexperiment.CampaignAttemptCompleted:
		if artifact.Failure != nil || artifact.Report == nil || artifact.Bundle == nil {
			return errors.New("ETCDRAFT_CAMPAIGN_COMPLETED_ARTIFACT_INVALID")
		}
		if err := artifact.Report.Validate(); err != nil {
			return err
		}
		if err := artifact.Bundle.Validate(); err != nil {
			return err
		}
		if _, err := controlexperiment.NewIntentOutcome(
			fmt.Sprintf("etcdraft-campaign-outcome-%d", artifact.Ordinal),
			provider.plan, instance, *artifact.Report, *artifact.Bundle,
		); err != nil {
			return err
		}
		expectedPolicy := map[string]string{
			etcdraftBackendActionClass: controlexperiment.ActionClassPolicyVersion,
			etcdraftBackendUniform:     controlexperiment.AdmissibleUniformPolicyVersion,
		}[provider.plan.BackendID]
		if artifact.Report.ManifestDigest != provider.targetIdentityDigest ||
			artifact.Bundle.Identity.ManifestDigest != provider.targetIdentityDigest ||
			artifact.Bundle.Identity.ReportDigest != artifact.Report.Digest ||
			artifact.Bundle.Work != artifact.Report.Work || artifact.Work != artifact.Report.Work ||
			artifact.Report.Config.DecisionsPerRun != provider.plan.Decisions ||
			len(artifact.Report.Config.Runs) != 1 ||
			expectedPolicy == "" || artifact.Report.Config.Runs[0].Policy.Version != expectedPolicy ||
			artifact.Report.Config.Runs[0].Policy.SeedHex != randomPolicySeed(instance.PolicySeed, 1) {
			return errors.New("ETCDRAFT_CAMPAIGN_COMPLETED_ARTIFACT_MISMATCH")
		}
	case controlexperiment.CampaignAttemptFailed:
		if artifact.Failure == nil || artifact.Report != nil || artifact.Bundle != nil {
			return errors.New("ETCDRAFT_CAMPAIGN_FAILED_ARTIFACT_INVALID")
		}
	default:
		return errors.New("ETCDRAFT_CAMPAIGN_ARTIFACT_OUTCOME_INVALID")
	}
	if artifact.Work.Primary.WorkUnits > instance.Budget.MaxPrimaryWorkUnits ||
		artifact.Work.Replay.WorkUnits > instance.Budget.MaxReplayWorkUnits {
		return errors.New("ETCDRAFT_CAMPAIGN_ARTIFACT_INSTANCE_BUDGET_EXCEEDED")
	}
	return nil
}
