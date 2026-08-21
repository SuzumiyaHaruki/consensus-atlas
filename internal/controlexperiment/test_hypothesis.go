package controlexperiment

import (
	"errors"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const TestHypothesisSchemaVersion = "consensus-atlas/test-hypothesis/v1"

// TestHypothesis is a non-authoritative semantic claim. It identifies one
// trusted risk, while the composition root explicitly supplies the backend,
// root, faults, budget and stop rule.
type TestHypothesis struct {
	SchemaVersion   string `json:"schema_version"`
	ID              string `json:"id"`
	KnowledgeDigest string `json:"knowledge_digest"`
	RiskID          string `json:"risk_id"`
	RiskSpecDigest  string `json:"risk_spec_digest"`
	Rationale       string `json:"rationale"`
	Digest          string `json:"digest"`
}

func NewTestHypothesis(
	id string,
	knowledge ProtocolKnowledgePack,
	riskSpec semantic.RiskWitnessSpec,
	rationale string,
	backendID string,
) (TestHypothesis, error) {
	hypothesis := TestHypothesis{
		SchemaVersion: TestHypothesisSchemaVersion, ID: id,
		KnowledgeDigest: knowledge.Digest, RiskID: riskSpec.RiskID,
		RiskSpecDigest: riskSpec.Digest, Rationale: rationale,
	}
	sealed, err := hypothesis.seal()
	if err != nil {
		return TestHypothesis{}, err
	}
	if err := sealed.Validate(knowledge, riskSpec, backendID); err != nil {
		return TestHypothesis{}, err
	}
	return sealed, nil
}

// Validate requires every consumer to name its actual backend. TestHypothesis
// itself stays protocol-semantic and does not silently default to DFS.
func (hypothesis TestHypothesis) Validate(
	knowledge ProtocolKnowledgePack,
	riskSpec semantic.RiskWitnessSpec,
	backendID string,
) error {
	if hypothesis.validateIdentity() != nil || knowledge.Validate() != nil || riskSpec.Validate() != nil ||
		hypothesis.KnowledgeDigest != knowledge.Digest || hypothesis.RiskID != riskSpec.RiskID ||
		hypothesis.RiskSpecDigest != riskSpec.Digest || riskSpec.FamilyID != knowledge.Family ||
		!validMethodToken(backendID) {
		return errors.New("EXPERIMENT_TEST_HYPOTHESIS_SOURCE_INVALID")
	}
	for _, risk := range knowledge.Risks {
		if risk.ID == hypothesis.RiskID {
			for _, allowed := range risk.AllowedBackendIDs {
				if allowed == backendID {
					return nil
				}
			}
			return errors.New("EXPERIMENT_TEST_HYPOTHESIS_BACKEND_UNSUPPORTED")
		}
	}
	return errors.New("EXPERIMENT_TEST_HYPOTHESIS_RISK_UNKNOWN")
}

func (hypothesis TestHypothesis) validateIdentity() error {
	if hypothesis.SchemaVersion != TestHypothesisSchemaVersion || !validMethodToken(hypothesis.ID) ||
		!validSHA256(hypothesis.KnowledgeDigest) || !validMethodToken(hypothesis.RiskID) ||
		!validSHA256(hypothesis.RiskSpecDigest) || hypothesis.Rationale == "" ||
		len(hypothesis.Rationale) > 2048 || strings.TrimSpace(hypothesis.Rationale) != hypothesis.Rationale {
		return errors.New("EXPERIMENT_TEST_HYPOTHESIS_INVALID")
	}
	sealed, err := hypothesis.seal()
	if err != nil || !validSHA256(hypothesis.Digest) || sealed.Digest != hypothesis.Digest {
		return errors.New("EXPERIMENT_TEST_HYPOTHESIS_DIGEST_MISMATCH")
	}
	return nil
}

func (hypothesis TestHypothesis) seal() (TestHypothesis, error) {
	hypothesis.Digest = ""
	digest, err := control.CanonicalDigest(hypothesis)
	hypothesis.Digest = digest
	return hypothesis, err
}
