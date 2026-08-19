package main

import (
	"errors"
	"os"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func loadFixedRiskInput(
	path string,
	target agenticEpisodeTarget,
) (*controlexperiment.RiskCandidateAssessment, string, error) {
	if path == "" {
		return nil, "", nil
	}
	if target.validate() != nil {
		return nil, "", errors.New("AGENTIC_FIXED_RISK_TARGET_INVALID")
	}
	encoded, err := os.ReadFile(path)
	if err != nil || len(encoded) == 0 || len(encoded) > controlexperiment.RiskCandidateMaxBytes {
		return nil, "", errors.New("AGENTIC_FIXED_RISK_INPUT_INVALID")
	}
	candidate, err := controlexperiment.ParseRiskCandidate(encoded)
	if err != nil {
		return nil, "", errors.New("AGENTIC_FIXED_RISK_INPUT_INVALID")
	}
	assessment, err := controlexperiment.AssessRiskCandidateForTarget(
		target.Knowledge, candidate, target.ObservationProjector.Capabilities(),
		target.Surface.Capabilities.ComposableActions, &target.Surface,
	)
	if err != nil || !assessment.Qualification.Qualified || len(assessment.CapabilityGaps) != 0 {
		return nil, "", errors.New("AGENTIC_FIXED_RISK_NOT_ACCEPTED")
	}
	digest, err := control.CanonicalDigest(candidate)
	if err != nil {
		return nil, "", errors.New("AGENTIC_FIXED_RISK_INPUT_INVALID")
	}
	return &assessment, digest, nil
}

func agenticRiskInputIdentity(
	fixed *controlexperiment.RiskCandidateAssessment,
	digest string,
) (string, string, bool) {
	if fixed == nil {
		return controlexperiment.AgenticRiskInputAgentDiscovery, "", digest == ""
	}
	want, err := control.CanonicalDigest(fixed.Candidate)
	return controlexperiment.AgenticRiskInputFixedAccepted, digest,
		err == nil && validAgenticSHA256(digest) && digest == want
}
