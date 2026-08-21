package defectbench

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const (
	FormalExposureAuditSchemaVersion = "consensus-atlas/formal-exposure-audit/v1"

	FormalExposureOpaqueViewMismatch = "formal-opaque-view-mismatch"
	FormalExposureInvalidPublicJSON  = "formal-public-json-invalid"
	FormalExposurePrivateAtom        = "formal-private-atom-exposed"
)

// FormalPublicArtifact is an exact byte snapshot that will cross into the
// runner or Agent environment. Labels and paths are intentionally excluded.
type FormalPublicArtifact struct {
	Bytes []byte
}

type FormalExposureArtifact struct {
	Index  int    `json:"index"`
	Digest string `json:"digest"`
}

// FormalExposureFinding never includes the matched private atom.
type FormalExposureFinding struct {
	ArtifactIndex int    `json:"artifact_index,omitempty"`
	Code          string `json:"code"`
}

type FormalExposureAudit struct {
	SchemaVersion            string                   `json:"schema_version"`
	BenchmarkID              string                   `json:"benchmark_id"`
	ContractDigest           string                   `json:"contract_digest"`
	ExpectedOpaqueViewDigest string                   `json:"expected_opaque_view_digest"`
	OpaqueArtifactDigest     string                   `json:"opaque_artifact_digest"`
	PublicArtifacts          []FormalExposureArtifact `json:"public_artifacts"`
	Passed                   bool                     `json:"passed"`
	Findings                 []FormalExposureFinding  `json:"findings"`
	Digest                   string                   `json:"digest"`
}

func AuditFormalExposure(
	contract FormalBenchmarkContract,
	view FormalOpaqueView,
	artifacts []FormalPublicArtifact,
) (FormalExposureAudit, error) {
	expected, err := contract.OpaqueView()
	if err != nil {
		return FormalExposureAudit{}, err
	}
	opaqueArtifactDigest, err := control.CanonicalDigest(view)
	if err != nil {
		return FormalExposureAudit{}, err
	}
	report := FormalExposureAudit{
		SchemaVersion: FormalExposureAuditSchemaVersion, BenchmarkID: contract.ID,
		ContractDigest: contract.Digest, ExpectedOpaqueViewDigest: expected.Digest,
		OpaqueArtifactDigest: opaqueArtifactDigest,
	}
	if err := contract.ValidateOpaqueView(view); err != nil {
		report.Findings = append(report.Findings, FormalExposureFinding{Code: FormalExposureOpaqueViewMismatch})
	}
	private := formalContractPrivateAtoms(contract)
	for index, artifact := range artifacts {
		report.PublicArtifacts = append(report.PublicArtifacts, FormalExposureArtifact{
			Index: index + 1, Digest: formalBytesDigest(artifact.Bytes),
		})
		atoms, err := formalJSONAtoms(artifact.Bytes)
		if err != nil {
			report.Findings = append(report.Findings, FormalExposureFinding{
				ArtifactIndex: index + 1, Code: FormalExposureInvalidPublicJSON,
			})
			continue
		}
		if formalAtomsIntersect(atoms, private) {
			report.Findings = append(report.Findings, FormalExposureFinding{
				ArtifactIndex: index + 1, Code: FormalExposurePrivateAtom,
			})
		}
	}
	report.Passed = len(report.Findings) == 0
	return report.Seal()
}

func (report FormalExposureAudit) Seal() (FormalExposureAudit, error) {
	report.SchemaVersion = FormalExposureAuditSchemaVersion
	report.PublicArtifacts = append([]FormalExposureArtifact(nil), report.PublicArtifacts...)
	report.Findings = append([]FormalExposureFinding(nil), report.Findings...)
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].ArtifactIndex != report.Findings[j].ArtifactIndex {
			return report.Findings[i].ArtifactIndex < report.Findings[j].ArtifactIndex
		}
		return report.Findings[i].Code < report.Findings[j].Code
	})
	report.Digest = ""
	if err := report.validateContent(); err != nil {
		return FormalExposureAudit{}, err
	}
	digest, err := control.CanonicalDigest(report)
	if err != nil {
		return FormalExposureAudit{}, err
	}
	report.Digest = digest
	return report, nil
}

func (report FormalExposureAudit) Validate() error {
	if report.SchemaVersion != FormalExposureAuditSchemaVersion {
		return errors.New("FORMAL_EXPOSURE_SCHEMA_MISMATCH")
	}
	sealed, err := report.Seal()
	if err != nil {
		return err
	}
	stored := report
	stored.Digest = ""
	digest, err := control.CanonicalDigest(stored)
	if err != nil || sealed.Digest != report.Digest || digest != report.Digest {
		return errors.New("FORMAL_EXPOSURE_DIGEST_MISMATCH")
	}
	return nil
}

func (report FormalExposureAudit) validateContent() error {
	if !validBundleID(report.BenchmarkID) || !bundleDigestValid(report.ContractDigest) ||
		!bundleDigestValid(report.ExpectedOpaqueViewDigest) || !bundleDigestValid(report.OpaqueArtifactDigest) ||
		report.Passed != (len(report.Findings) == 0) {
		return errors.New("FORMAL_EXPOSURE_IDENTITY_INVALID")
	}
	for index, artifact := range report.PublicArtifacts {
		if artifact.Index != index+1 || !bundleDigestValid(artifact.Digest) {
			return errors.New("FORMAL_EXPOSURE_ARTIFACT_INVALID")
		}
	}
	seen := map[FormalExposureFinding]bool{}
	for _, finding := range report.Findings {
		if seen[finding] {
			return errors.New("FORMAL_EXPOSURE_FINDING_DUPLICATE")
		}
		seen[finding] = true
		switch finding.Code {
		case FormalExposureOpaqueViewMismatch:
			if finding.ArtifactIndex != 0 {
				return errors.New("FORMAL_EXPOSURE_FINDING_INVALID")
			}
		case FormalExposureInvalidPublicJSON, FormalExposurePrivateAtom:
			if finding.ArtifactIndex < 1 || finding.ArtifactIndex > len(report.PublicArtifacts) {
				return errors.New("FORMAL_EXPOSURE_FINDING_INVALID")
			}
		default:
			return errors.New("FORMAL_EXPOSURE_FINDING_INVALID")
		}
	}
	return nil
}

func formalContractPrivateAtoms(contract FormalBenchmarkContract) map[string]bool {
	result := map[string]bool{
		contract.BlindingNonce: true, contract.Composition.ProjectorID: true,
	}
	for _, monitor := range contract.Composition.MonitorIDs {
		result[monitor] = true
	}
	for _, pair := range contract.Pairs {
		result[pair.PairID], result[pair.RootCauseID] = true, true
		for _, variant := range []FormalVariant{pair.Control, pair.Candidate} {
			for _, atom := range formalVariantPrivateAtoms(variant) {
				result[atom] = true
			}
		}
	}
	return result
}

func formalJSONAtoms(data []byte) (map[string]bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("FORMAL_EXPOSURE_MULTIPLE_JSON_VALUES")
		}
		return nil, err
	}
	result := map[string]bool{}
	collectFormalJSONAtoms(value, result)
	return result, nil
}

func collectFormalJSONAtoms(value any, result map[string]bool) {
	switch typed := value.(type) {
	case string:
		result[typed] = true
	case []any:
		for _, item := range typed {
			collectFormalJSONAtoms(item, result)
		}
	case map[string]any:
		for key, item := range typed {
			result[key] = true
			collectFormalJSONAtoms(item, result)
		}
	}
}

func formalAtomsIntersect(left, right map[string]bool) bool {
	for atom := range left {
		if right[atom] {
			return true
		}
	}
	return false
}

func formalBytesDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
