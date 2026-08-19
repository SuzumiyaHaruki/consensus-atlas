package main

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const existingRiskInputMaxBytes = 512 << 10

// loadExistingRiskInput accepts either a standalone RiskCandidate, a stored
// RiskCandidateAssessment, or an Agentic Episode summary containing
// accepted_risk. Prior qualification is never trusted: the candidate is
// normalized and assessed again against the current Target composition.
func loadExistingRiskInput(
	path string,
	target agenticEpisodeTarget,
) (*controlexperiment.RiskCandidateAssessment, string, error) {
	if path == "" {
		return nil, "", nil
	}
	if target.validate() != nil {
		return nil, "", errors.New("AGENTIC_RISK_INPUT_TARGET_INVALID")
	}
	encoded, err := os.ReadFile(path)
	if err != nil || len(encoded) == 0 || len(encoded) > existingRiskInputMaxBytes {
		return nil, "", errors.New("AGENTIC_RISK_INPUT_INVALID")
	}
	candidate, err := parseExistingRiskCandidate(encoded)
	if err != nil {
		return nil, "", err
	}
	assessment, err := controlexperiment.AssessRiskCandidateForTarget(
		target.Knowledge, candidate, target.ObservationProjector.Capabilities(),
		target.Surface.Capabilities.ComposableActions, &target.Surface,
	)
	if err != nil || !assessment.Qualification.Qualified || len(assessment.CapabilityGaps) != 0 {
		return nil, "", errors.New("AGENTIC_RISK_INPUT_NOT_ACCEPTED")
	}
	digest, err := control.CanonicalDigest(candidate)
	if err != nil {
		return nil, "", errors.New("AGENTIC_RISK_INPUT_INVALID")
	}
	return &assessment, digest, nil
}

func parseExistingRiskCandidate(encoded []byte) (controlexperiment.RiskCandidate, error) {
	if candidate, err := controlexperiment.ParseRiskCandidate(encoded); err == nil {
		return candidate, nil
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(encoded, &object) != nil {
		return controlexperiment.RiskCandidate{}, errors.New("AGENTIC_RISK_INPUT_INVALID")
	}
	assessment := encoded
	if accepted, ok := object["accepted_risk"]; ok {
		assessment = accepted
	}
	var projection map[string]json.RawMessage
	if json.Unmarshal(assessment, &projection) != nil {
		return controlexperiment.RiskCandidate{}, errors.New("AGENTIC_RISK_INPUT_INVALID")
	}
	candidateBytes, ok := projection["candidate"]
	if !ok || string(candidateBytes) == "null" {
		return controlexperiment.RiskCandidate{}, errors.New("AGENTIC_RISK_INPUT_INVALID")
	}
	candidate, err := controlexperiment.ParseRiskCandidate(candidateBytes)
	if err != nil {
		return controlexperiment.RiskCandidate{}, errors.New("AGENTIC_RISK_INPUT_INVALID")
	}
	return candidate, nil
}

func agenticRiskInputIdentity(
	existing *controlexperiment.RiskCandidateAssessment,
	digest string,
) (controlexperiment.AgenticRiskInputMode, string, bool) {
	if existing == nil {
		return controlexperiment.AgenticRiskInputAgentGenerated, "", digest == ""
	}
	want, err := control.CanonicalDigest(existing.Candidate)
	return controlexperiment.AgenticRiskInputExistingCandidate, digest,
		err == nil && validAgenticSHA256(digest) && digest == want
}
