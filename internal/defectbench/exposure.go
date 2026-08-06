package defectbench

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

const ExposureAuditVersion = 1

const (
	ExposureBlindManifestMismatch = "blind-manifest-mismatch"
	ExposureInvalidPublicJSON     = "invalid-public-json"
	ExposurePrivateString         = "private-string-exposed"
)

// PublicArtifact is a byte snapshot of one object that crossed into the
// runner/Planner environment. Its label is intentionally not persisted in an
// ExposureAudit: local paths can themselves be sensitive curator metadata.
type PublicArtifact struct {
	Bytes []byte
}

type ExposureArtifact struct {
	Index  int    `json:"index"`
	Digest string `json:"digest"`
}

// ExposureFinding never contains the private string that matched. This lets a
// curator persist or hand off the report without turning the audit into a new
// disclosure channel.
type ExposureFinding struct {
	ArtifactIndex int    `json:"artifact_index,omitempty"`
	Code          string `json:"code"`
}

type ExposureAudit struct {
	Version         int                `json:"version"`
	BenchmarkID     string             `json:"benchmark_id"`
	BenchmarkDigest string             `json:"benchmark_digest"`
	PublicArtifacts []ExposureArtifact `json:"public_artifacts"`
	Passed          bool               `json:"passed"`
	Findings        []ExposureFinding  `json:"findings"`
}

// AuditExposure verifies that the released BlindManifest is exactly the
// trusted projection of private, frozen Manifest, then checks every JSON
// string atom in public artifacts against private-only manifest values. It is
// a deterministic boundary audit, not a claim that public protocol knowledge
// cannot allow a model to infer a semantic hypothesis.
func AuditExposure(manifest Manifest, blind BlindManifest, artifacts []PublicArtifact) (ExposureAudit, error) {
	expected, err := manifest.Blind()
	if err != nil {
		return ExposureAudit{}, fmt.Errorf("validate private manifest: %w", err)
	}
	report := ExposureAudit{
		Version: ExposureAuditVersion, BenchmarkID: expected.BenchmarkID,
		BenchmarkDigest: expected.BenchmarkDigest, Passed: true,
		PublicArtifacts: make([]ExposureArtifact, 0, len(artifacts)),
		Findings:        make([]ExposureFinding, 0),
	}
	if !sameBlindManifest(expected, blind) {
		report.Passed = false
		report.Findings = append(report.Findings, ExposureFinding{Code: ExposureBlindManifestMismatch})
	}
	secrets := privateStrings(manifest)
	for index, artifact := range artifacts {
		report.PublicArtifacts = append(report.PublicArtifacts, ExposureArtifact{Index: index + 1, Digest: bytesDigest(artifact.Bytes)})
		strings, err := jsonStrings(artifact.Bytes)
		if err != nil {
			report.Passed = false
			report.Findings = append(report.Findings, ExposureFinding{ArtifactIndex: index + 1, Code: ExposureInvalidPublicJSON})
			continue
		}
		if containsPrivateString(strings, secrets) {
			report.Passed = false
			report.Findings = append(report.Findings, ExposureFinding{ArtifactIndex: index + 1, Code: ExposurePrivateString})
		}
	}
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].ArtifactIndex != report.Findings[j].ArtifactIndex {
			return report.Findings[i].ArtifactIndex < report.Findings[j].ArtifactIndex
		}
		return report.Findings[i].Code < report.Findings[j].Code
	})
	return report, nil
}

func sameBlindManifest(left, right BlindManifest) bool {
	left.Trials = append([]BlindTrial(nil), left.Trials...)
	right.Trials = append([]BlindTrial(nil), right.Trials...)
	sort.Slice(left.Trials, func(i, j int) bool { return left.Trials[i].TrialID < left.Trials[j].TrialID })
	sort.Slice(right.Trials, func(i, j int) bool { return right.Trials[i].TrialID < right.Trials[j].TrialID })
	leftBytes, leftErr := json.Marshal(left)
	rightBytes, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftBytes, rightBytes)
}

func privateStrings(manifest Manifest) map[string]bool {
	strings := make(map[string]bool)
	for _, monitor := range manifest.KillPolicy.AllowedMonitors {
		strings[monitor] = true
	}
	for _, variant := range manifest.Variants {
		for _, value := range []string{
			variant.ID, variant.RootCauseID, variant.Category,
			variant.SourceDigest, variant.SUTManifestDigest, variant.BuildAuditDigest, variant.BinaryDigest,
		} {
			if value != "" {
				strings[value] = true
			}
		}
	}
	return strings
}

func jsonStrings(data []byte) (map[string]bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, err
	}
	result := make(map[string]bool)
	collectJSONStrings(value, result)
	return result, nil
}

func collectJSONStrings(value any, result map[string]bool) {
	switch typed := value.(type) {
	case string:
		result[typed] = true
	case []any:
		for _, item := range typed {
			collectJSONStrings(item, result)
		}
	case map[string]any:
		for key, item := range typed {
			result[key] = true
			collectJSONStrings(item, result)
		}
	}
}

func bytesDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func containsPrivateString(public, private map[string]bool) bool {
	for value := range public {
		if private[value] {
			return true
		}
	}
	return false
}
