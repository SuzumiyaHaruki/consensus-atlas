package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	etcdraftCampaignSpecVersion     = "consensus-atlas/etcdraft-campaign-spec/v1"
	etcdraftCampaignArtifactVersion = "consensus-atlas/etcdraft-campaign-artifact/v1"
	etcdraftCampaignTargetID        = "etcdraft-v2"
	etcdraftCampaignStrategy        = "workload-admissible-uniform"
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
	adapter, err := etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	if err != nil {
		return etcdraftCampaignProvider{}, err
	}
	manifest, err := adapter.Manifest(ctx)
	if err != nil {
		return etcdraftCampaignProvider{}, err
	}
	digest, err := manifest.Digest()
	if err != nil {
		return etcdraftCampaignProvider{}, err
	}
	return etcdraftCampaignProvider{
		spec: spec, targetIdentityDigest: digest, execute: etcdraftExecution,
	}, nil
}

func (provider etcdraftCampaignProvider) campaignConfig(
	id string,
	attempts int,
	wallClockCeilingMillis int64,
) (controlexperiment.CampaignConfig, error) {
	if err := provider.spec.validate(); err != nil || attempts <= 0 ||
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
	return controlexperiment.NewCampaignConfig(
		id, etcdraftCampaignTargetID, provider.targetIdentityDigest, provider.spec.Digest,
		controlexperiment.CampaignLogicalBudget{
			MaxAttempts: attempts, MaxPrimarySchedulerDecisions: decisions,
			MaxPrimaryWorkUnits: work, MaxReplayWorkUnits: work,
		},
		wallClockCeilingMillis,
	)
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
	if err := provider.spec.validate(); err != nil {
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
		request.ExperimentSpecDigest != provider.spec.Digest {
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

	report, bundle, executionErr := provider.execute(
		ctx, provider.spec.Strategy, provider.spec.DecisionsPerAttempt, seed, true,
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
			request, provider.spec, seed, controlexperiment.CampaignAttemptFailed,
			typed, failure.Work, nil, nil,
		)
		if err != nil {
			return controlexperiment.CampaignAttemptResult{}, err
		}
		if err := artifact.validate(request, provider.spec, provider.targetIdentityDigest); err != nil {
			return controlexperiment.CampaignAttemptResult{}, err
		}
		return controlexperiment.CampaignAttemptResult{
			Outcome: artifact.Outcome, Failure: artifact.Failure,
			Work: artifact.Work, Artifact: encoded,
		}, nil
	}
	artifact, encoded, err := newEtcdraftCampaignArtifact(
		request, provider.spec, seed, controlexperiment.CampaignAttemptCompleted,
		nil, report.Work, &report, &bundle,
	)
	if err != nil {
		return controlexperiment.CampaignAttemptResult{}, err
	}
	if err := artifact.validate(request, provider.spec, provider.targetIdentityDigest); err != nil {
		return controlexperiment.CampaignAttemptResult{}, err
	}
	return controlexperiment.CampaignAttemptResult{
		Outcome: artifact.Outcome, Work: artifact.Work, Artifact: encoded,
	}, nil
}

type etcdraftCampaignArtifact struct {
	SchemaVersion        string                             `json:"schema_version"`
	RequestDigest        string                             `json:"request_digest"`
	ExperimentSpecDigest string                             `json:"experiment_spec_digest"`
	TargetIdentityDigest string                             `json:"target_identity_digest"`
	Ordinal              int                                `json:"ordinal"`
	PolicySeed           uint64                             `json:"policy_seed"`
	Outcome              string                             `json:"outcome"`
	Failure              *controlexperiment.MethodFailure   `json:"failure,omitempty"`
	Work                 controlexperiment.WorkLedger       `json:"work"`
	Report               *controlexperiment.Report          `json:"report,omitempty"`
	Bundle               *controlexperiment.ExecutionBundle `json:"bundle,omitempty"`
}

func newEtcdraftCampaignArtifact(
	request controlexperiment.CampaignAttemptRequest,
	spec etcdraftCampaignSpec,
	seed uint64,
	outcome string,
	failure *controlexperiment.MethodFailure,
	work controlexperiment.WorkLedger,
	report *controlexperiment.Report,
	bundle *controlexperiment.ExecutionBundle,
) (etcdraftCampaignArtifact, []byte, error) {
	artifact := etcdraftCampaignArtifact{
		SchemaVersion: etcdraftCampaignArtifactVersion,
		RequestDigest: request.Digest, ExperimentSpecDigest: spec.Digest,
		TargetIdentityDigest: request.TargetIdentityDigest,
		Ordinal:              request.Ordinal, PolicySeed: seed, Outcome: outcome,
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
	spec etcdraftCampaignSpec,
	targetIdentityDigest string,
) error {
	if err := request.Validate(); err != nil {
		return err
	}
	seed, err := spec.seed(request.Ordinal)
	if err != nil {
		return err
	}
	if artifact.SchemaVersion != etcdraftCampaignArtifactVersion ||
		artifact.RequestDigest != request.Digest || artifact.ExperimentSpecDigest != spec.Digest ||
		artifact.TargetIdentityDigest != targetIdentityDigest ||
		artifact.TargetIdentityDigest != request.TargetIdentityDigest ||
		artifact.Ordinal != request.Ordinal || artifact.PolicySeed != seed {
		return errors.New("ETCDRAFT_CAMPAIGN_ARTIFACT_IDENTITY_MISMATCH")
	}
	if _, err := controlexperiment.NewCampaignAttemptRecord(controlexperiment.CampaignAttemptRecord{
		Ordinal: artifact.Ordinal, ID: fmt.Sprintf("artifact-validation-%d", artifact.Ordinal),
		InputDigest:    artifact.RequestDigest,
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
		if artifact.Report.ManifestDigest != targetIdentityDigest ||
			artifact.Bundle.Identity.ManifestDigest != targetIdentityDigest ||
			artifact.Bundle.Identity.ReportDigest != artifact.Report.Digest ||
			artifact.Bundle.Work != artifact.Report.Work || artifact.Work != artifact.Report.Work ||
			artifact.Report.Config.DecisionsPerRun != spec.DecisionsPerAttempt ||
			len(artifact.Report.Config.Runs) != 1 ||
			artifact.Report.Config.Runs[0].Policy.Version != controlexperiment.AdmissibleUniformPolicyVersion ||
			artifact.Report.Config.Runs[0].Policy.SeedHex != randomPolicySeed(seed, 1) {
			return errors.New("ETCDRAFT_CAMPAIGN_COMPLETED_ARTIFACT_MISMATCH")
		}
	case controlexperiment.CampaignAttemptFailed:
		if artifact.Failure == nil || artifact.Report != nil || artifact.Bundle != nil {
			return errors.New("ETCDRAFT_CAMPAIGN_FAILED_ARTIFACT_INVALID")
		}
	default:
		return errors.New("ETCDRAFT_CAMPAIGN_ARTIFACT_OUTCOME_INVALID")
	}
	return nil
}
