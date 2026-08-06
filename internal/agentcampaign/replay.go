package agentcampaign

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/campaign"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/testplan"
)

const TrustedReplayBundleVersion = 1

// TrustedReplayBundle is never returned to a Planner. It records the trusted
// resolution of opaque target refs and enough Coordinator identity to replay
// an accepted Blind campaign without a model call.
type TrustedReplayBundle struct {
	Version                  int             `json:"version"`
	Config                   Config          `json:"config"`
	Scope                    BlindScope      `json:"blind_scope"`
	ProfileID                string          `json:"profile_id"`
	ProfileDigest            string          `json:"profile_digest"`
	CapabilitySnapshotDigest string          `json:"capability_snapshot_digest"`
	Plans                    []testplan.Plan `json:"plans"`
}

func (report BlindReport) TrustedReplayBundle() (TrustedReplayBundle, error) {
	if err := report.Config.Validate(); err != nil {
		return TrustedReplayBundle{}, err
	}
	if err := report.Scope.Validate(); err != nil {
		return TrustedReplayBundle{}, err
	}
	if report.Profile.ID == "" || len(report.Profile.Digest) != 64 || len(report.Capabilities.Digest) != 64 || len(report.TrustedPlans) == 0 {
		return TrustedReplayBundle{}, errors.New("blind report has no complete trusted replay material")
	}
	bundle := TrustedReplayBundle{Version: TrustedReplayBundleVersion, Config: report.Config, Scope: report.Scope, ProfileID: report.Profile.ID, ProfileDigest: report.Profile.Digest, CapabilitySnapshotDigest: report.Capabilities.Digest, Plans: make([]testplan.Plan, len(report.TrustedPlans))}
	for index, plan := range report.TrustedPlans {
		bundle.Plans[index] = cloneTrustedPlan(plan)
	}
	return bundle, nil
}

func (bundle TrustedReplayBundle) Validate(profile coverage.Profile, manifest driver.Manifest) error {
	if bundle.Version != TrustedReplayBundleVersion {
		return errors.New("unsupported trusted replay bundle version")
	}
	if err := bundle.Config.Validate(); err != nil {
		return err
	}
	if err := bundle.Scope.Validate(); err != nil {
		return err
	}
	digest, err := coverage.Digest(profile)
	if err != nil {
		return err
	}
	if bundle.ProfileID != profile.ID || bundle.ProfileDigest != digest {
		return errors.New("trusted replay bundle profile does not match")
	}
	capabilities, err := newBlindCapabilities(manifest)
	if err != nil {
		return err
	}
	if bundle.CapabilitySnapshotDigest != capabilities.Digest {
		return errors.New("trusted replay bundle capability snapshot does not match")
	}
	suite := testplan.Suite{Version: testplan.Version, ID: bundle.Config.ID, ProfileID: bundle.ProfileID, ProfileDigest: bundle.ProfileDigest, MaxTotalRuns: bundle.Config.MaxTotalRuns, MaxTotalDecisions: bundle.Config.MaxTotalDecisions, Plans: bundle.Plans}
	if err := suite.Validate(profile); err != nil {
		return fmt.Errorf("trusted replay plans: %w", err)
	}
	return nil
}

func ReplayTrusted(ctx context.Context, profile coverage.Profile, manifest driver.Manifest, matcher coverage.SemanticMatcher, bundle TrustedReplayBundle, newAdapter campaign.AdapterFactory, options campaign.Options) (campaign.Report, error) {
	if err := bundle.Validate(profile, manifest); err != nil {
		return campaign.Report{}, err
	}
	capabilities, err := newBlindCapabilities(manifest)
	if err != nil {
		return campaign.Report{}, err
	}
	digest, err := blindCoordinatorDigest(bundle.Config, bundle.ProfileDigest, bundle.Scope, capabilities.Digest)
	if err != nil {
		return campaign.Report{}, err
	}
	session, err := campaign.NewSession(profile, manifest, matcher, bundle.Config.ID, digest, newAdapter, options)
	if err != nil {
		return campaign.Report{}, err
	}
	for _, plan := range bundle.Plans {
		if _, err := session.ExecutePlan(ctx, cloneTrustedPlan(plan)); err != nil {
			return campaign.Report{}, err
		}
	}
	return session.Report(), nil
}

func cloneTrustedPlan(plan testplan.Plan) testplan.Plan {
	encoded, err := json.Marshal(plan)
	if err != nil {
		return testplan.Plan{}
	}
	var cloned testplan.Plan
	if json.Unmarshal(encoded, &cloned) != nil {
		return testplan.Plan{}
	}
	return cloned
}
