package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

// newEtcdraftCampaignObservation is target-owned composition. The Campaign
// package only receives protocol-neutral projections after every committed
// artifact and target-specific semantic projection has been revalidated.
func newEtcdraftCampaignObservation(
	recovered *controlexperiment.CampaignRecovery,
	provider etcdraftCampaignProvider,
) (controlexperiment.CampaignObservation, error) {
	if recovered == nil || provider.targetIdentityDigest == "" ||
		provider.targetIdentityDigest != recovered.Config.TargetIdentityDigest ||
		provider.experimentSpecDigest != recovered.Config.ExperimentSpecDigest {
		return controlexperiment.CampaignObservation{}, errors.New("ETCDRAFT_CAMPAIGN_OBSERVATION_COMPOSITION_INVALID")
	}
	if err := provider.spec.validate(); err != nil {
		return controlexperiment.CampaignObservation{}, err
	}
	summary, err := controlexperiment.NewCampaignSummary(recovered)
	if err != nil {
		return controlexperiment.CampaignObservation{}, err
	}
	projections := make([]controlexperiment.CampaignAttemptProjection, 0, len(summary.Attempts))
	for index, attempt := range summary.Attempts {
		request, err := controlexperiment.NewCampaignAttemptRequest(
			recovered.Config, recovered.Checkpoints[index],
		)
		if err != nil {
			return controlexperiment.CampaignObservation{}, err
		}
		encoded, err := recovered.ReadAttemptArtifact(attempt.Ordinal)
		if err != nil {
			return controlexperiment.CampaignObservation{}, err
		}
		artifact, err := decodeEtcdraftCampaignArtifact(encoded)
		if err != nil {
			return controlexperiment.CampaignObservation{}, err
		}
		if index >= len(recovered.PlannedAttempts) {
			return controlexperiment.CampaignObservation{}, errors.New("ETCDRAFT_CAMPAIGN_OBSERVATION_PLAN_MISSING")
		}
		planned := recovered.PlannedAttempts[index]
		if err := planned.ValidateRequest(request); err != nil {
			return controlexperiment.CampaignObservation{}, err
		}
		bound := provider
		bound.intent, bound.plan, bound.planningWork = planned.Proposal, planned.Plan, planned.PlanningWork
		bound.plannedAttemptDigest = planned.Digest
		if err := artifact.validate(request, bound); err != nil {
			return controlexperiment.CampaignObservation{}, err
		}
		if artifact.Outcome != attempt.Record.Outcome || artifact.Work != attempt.Record.Work ||
			attempt.Record.InputDigest != planned.Digest ||
			!sameCampaignMethodFailure(artifact.Failure, attempt.Record.Failure) {
			return controlexperiment.CampaignObservation{}, errors.New("ETCDRAFT_CAMPAIGN_OBSERVATION_RECORD_MISMATCH")
		}
		projection := controlexperiment.CampaignAttemptProjection{
			Ordinal: attempt.Ordinal, ArtifactDigest: attempt.Record.ArtifactDigest,
			Outcome: artifact.Outcome, Work: artifact.Work, Choice: &artifact.Choice,
		}
		if artifact.Bundle != nil {
			if err := artifact.Bundle.ValidateProjection(etcdraftv2.DecisionProjector{}); err != nil {
				return controlexperiment.CampaignObservation{}, err
			}
			if _, err := controlexperiment.NewPSSFeedback(
				fmt.Sprintf("campaign-attempt-%d-reprojection", attempt.Ordinal),
				*artifact.Bundle, etcdraftv2.CorePSSMapper{},
			); err != nil {
				return controlexperiment.CampaignObservation{}, err
			}
			checked := oracle.CheckBundle(
				*artifact.Bundle, oracle.BundleTraceIntegrity{}, oracle.BundleAgreement{},
			)
			projection.ExecutionEvidence = true
			projection.BundleDigest = artifact.Bundle.Digest
			projection.PSSID = artifact.Bundle.Identity.PSSID
			projection.CorePSS = append(
				[]controlexperiment.CorePSSSample(nil), artifact.Bundle.CorePSS...,
			)
			projection.Faults = artifact.Bundle.Run.Faults
			projection.Workload = artifact.Bundle.Run.Workload
			projection.CheckedMonitors = append([]string(nil), checked.Checked...)
			for _, violation := range checked.Violations {
				class := controlexperiment.CampaignMonitorRequirement
				if violation.Monitor == (oracle.BundleTraceIntegrity{}).Name() {
					class = controlexperiment.CampaignMonitorEvidenceInvalid
				}
				projection.MonitorFindings = append(
					projection.MonitorFindings, controlexperiment.CampaignMonitorFinding{
						Monitor: violation.Monitor, Step: violation.Step,
						Class: class, Message: violation.Message,
					},
				)
			}
		}
		projections = append(projections, projection)
	}
	return controlexperiment.NewCampaignObservation(summary, projections)
}

func decodeEtcdraftCampaignArtifact(encoded []byte) (etcdraftCampaignArtifact, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var artifact etcdraftCampaignArtifact
	if err := decoder.Decode(&artifact); err != nil {
		return etcdraftCampaignArtifact{}, fmt.Errorf("ETCDRAFT_CAMPAIGN_OBSERVATION_ARTIFACT_JSON_INVALID: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return etcdraftCampaignArtifact{}, errors.New("ETCDRAFT_CAMPAIGN_OBSERVATION_ARTIFACT_JSON_TRAILING")
	}
	return artifact, nil
}

func sameCampaignMethodFailure(
	left *controlexperiment.MethodFailure,
	right *controlexperiment.MethodFailure,
) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
