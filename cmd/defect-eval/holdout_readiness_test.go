package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
)

const holdoutReadinessSchema = "consensus-atlas/holdout-readiness-gap/v1"

type holdoutReadinessPolicy struct {
	MinDistinctRootCauses  int  `json:"min_distinct_root_causes"`
	MinMatchingControls    int  `json:"min_matching_controls"`
	RequirePrivateDataset  bool `json:"require_private_dataset"`
	RequireBlindSurface    bool `json:"require_blind_surface"`
	RequireSecondStrictCFT bool `json:"require_second_strict_cft"`
}

type holdoutReadinessSource struct {
	Path               string `json:"path"`
	Status             string `json:"status"`
	FileSHA256         string `json:"file_sha256"`
	ManifestDigest     string `json:"manifest_digest,omitempty"`
	Pairs              int    `json:"pairs"`
	Controls           int    `json:"controls"`
	DistinctRootCauses int    `json:"distinct_root_causes"`
}

type holdoutCurrentSurface struct {
	Scope                             string   `json:"scope"`
	SupportedBenchmarkClassifications []string `json:"supported_benchmark_classifications"`
	LiveManifestCount                 int      `json:"live_manifest_count"`
	ArchivedManifestCount             int      `json:"archived_manifest_count"`
	FreshEvaluatorPairLimit           int      `json:"fresh_evaluator_pair_limit"`
	PrivateBlindSurface               bool     `json:"private_blind_surface"`
}

type holdoutSecondTarget struct {
	Target                        string   `json:"target"`
	QualificationBundleDigest     string   `json:"qualification_bundle_digest"`
	QualificationReportDigest     string   `json:"qualification_report_digest"`
	Qualified                     bool     `json:"qualified"`
	RequiredCapabilities          int      `json:"required_capabilities"`
	ValidatedRequiredCapabilities int      `json:"validated_required_capabilities"`
	MissingRequiredCapabilities   []string `json:"missing_required_capabilities"`
}

type holdoutReadinessSummary struct {
	RepositoryPublicPairs              int  `json:"repository_public_pairs"`
	RepositoryPublicDistinctRootCauses int  `json:"repository_public_distinct_root_causes"`
	LivePublicPairs                    int  `json:"live_public_pairs"`
	LivePublicDistinctRootCauses       int  `json:"live_public_distinct_root_causes"`
	ArchivedPublicPairs                int  `json:"archived_public_pairs"`
	ArchivedPublicDistinctRootCauses   int  `json:"archived_public_distinct_root_causes"`
	FormalPrivatePairs                 int  `json:"formal_private_pairs"`
	FormalEligibleRootCauses           int  `json:"formal_eligible_root_causes"`
	FormalEligibleControls             int  `json:"formal_eligible_controls"`
	Ready                              bool `json:"ready"`
}

type holdoutReadinessReport struct {
	SchemaVersion    string                   `json:"schema_version"`
	ID               string                   `json:"id"`
	Policy           holdoutReadinessPolicy   `json:"policy"`
	Sources          []holdoutReadinessSource `json:"sources"`
	CurrentSurface   holdoutCurrentSurface    `json:"current_surface"`
	SecondStrictCFT  holdoutSecondTarget      `json:"second_strict_cft"`
	Summary          holdoutReadinessSummary  `json:"summary"`
	Findings         []string                 `json:"findings"`
	NewModelCalls    int                      `json:"new_model_calls"`
	NewSUTExecutions int                      `json:"new_sut_executions"`
	Digest           string                   `json:"digest"`
}

type archivedManifest struct {
	Version       int                `json:"version"`
	ID            string             `json:"id"`
	BlindingNonce string             `json:"blinding_nonce"`
	Protocol      string             `json:"protocol"`
	ProfileID     string             `json:"profile_id"`
	ProfileDigest string             `json:"profile_digest"`
	Budget        archivedBudget     `json:"budget"`
	KillPolicy    archivedKillPolicy `json:"kill_policy"`
	Variants      []archivedVariant  `json:"variants"`
}

type archivedBudget struct {
	MaxRuns             int `json:"max_runs"`
	MaxDecisions        int `json:"max_decisions"`
	MaxPrimaryWorkUnits int `json:"max_primary_work_units"`
}

type archivedKillPolicy struct {
	AllowedMonitors     []string `json:"allowed_monitors"`
	RequireReplayStable bool     `json:"require_replay_stable"`
	RequireConformant   bool     `json:"require_conformant"`
}

type archivedVariant struct {
	ID                string `json:"id"`
	TrialID           string `json:"trial_id"`
	Kind              string `json:"kind"`
	RootCauseID       string `json:"root_cause_id,omitempty"`
	Category          string `json:"category"`
	Provenance        string `json:"provenance"`
	SourceDigest      string `json:"source_digest"`
	SUTManifestDigest string `json:"sut_manifest_digest"`
	BuildAuditDigest  string `json:"build_audit_digest,omitempty"`
	BinaryDigest      string `json:"binary_digest,omitempty"`
}

func TestArchivedHoldoutReadinessGapIsRecomputed(t *testing.T) {
	report := recomputeHoldoutReadiness(t)
	wantPath := filepath.Join("..", "..", "benchmarks", "experiments", "formal-holdout-readiness-m5.21k", "report.json")
	var archived holdoutReadinessReport
	_, err := readStrictJSONBytes(wantPath, &archived)
	if err != nil {
		encoded, encodeErr := json.MarshalIndent(report, "", "  ")
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		t.Fatalf("read archived readiness report: %v\n%s", err, append(encoded, '\n'))
	}
	if !reflect.DeepEqual(archived, report) {
		encoded, _ := json.MarshalIndent(report, "", "  ")
		t.Fatalf("archived readiness report is stale; recomputed:\n%s", append(encoded, '\n'))
	}
}

func recomputeHoldoutReadiness(t *testing.T) holdoutReadinessReport {
	t.Helper()
	root := filepath.Join("..", "..")
	var paths []string
	err := filepath.WalkDir(filepath.Join(root, "benchmarks"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == "manifest.json" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil || len(paths) == 0 {
		t.Fatalf("discover benchmark manifests: %v (%d paths)", err, len(paths))
	}
	sort.Strings(paths)
	var sources []holdoutReadinessSource
	liveRoots, archivedRoots, allRoots := map[string]bool{}, map[string]bool{}, map[string]bool{}
	livePairs, archivedPairs := 0, 0
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var envelope struct {
			SchemaVersion string `json:"schema_version"`
			Version       int    `json:"version"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatal(err)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		source := holdoutReadinessSource{Path: filepath.ToSlash(relative), FileSHA256: sha256Bytes(data)}
		switch {
		case envelope.SchemaVersion == defectbench.BundleBenchmarkSchemaVersion ||
			envelope.SchemaVersion == defectbench.BundleBenchmarkSchemaVersionV2:
			var manifest defectbench.BundleBenchmark
			if _, err := readStrictJSONBytes(path, &manifest); err != nil {
				t.Fatal(err)
			}
			if err := manifest.Validate(); err != nil {
				t.Fatal(err)
			}
			if _, _, err := pairTrials(manifest); err != nil {
				t.Fatal(err)
			}
			source.Status = "live-public-calibration"
			source.ManifestDigest = manifest.Digest
			source.Pairs, source.Controls = 1, 1
			roots := rootsFromCurrent(manifest)
			source.DistinctRootCauses = len(roots)
			mergeRoots(liveRoots, roots)
			mergeRoots(allRoots, roots)
			livePairs++
		case envelope.Version == 1:
			var manifest archivedManifest
			if _, err := readStrictJSONBytes(path, &manifest); err != nil {
				t.Fatal(err)
			}
			roots := validateArchivedPair(t, manifest)
			source.Status = "archived-public-sample"
			source.Pairs, source.Controls = 1, 1
			source.DistinctRootCauses = len(roots)
			mergeRoots(archivedRoots, roots)
			mergeRoots(allRoots, roots)
			archivedPairs++
		default:
			t.Fatalf("unclassified evaluator manifest: %s", path)
		}
		sources = append(sources, source)
	}

	assertFormalClassificationRejected(t, sources, paths)
	assertFreshEvaluatorIsSinglePair(t)
	privateBlindSurface := hasGoSources(t, filepath.Join(root, "cmd", "benchmark-preflight")) ||
		hasGoSources(t, filepath.Join(root, "cmd", "blind-audit")) ||
		hasGoSources(t, filepath.Join(root, "cmd", "blind-replay"))

	var qualification conformance.QualificationBundle
	qualificationPath := filepath.Join(root, "benchmarks", "qualifications", "hashicorp-raft-v2-m5.4c", "report.json")
	if _, err := readStrictJSONBytes(qualificationPath, &qualification); err != nil {
		t.Fatal(err)
	}
	if err := qualification.Validate(); err != nil {
		t.Fatal(err)
	}
	missing, required, validated := missingRequiredCapabilities(qualification.Qualification)

	report := holdoutReadinessReport{
		SchemaVersion: holdoutReadinessSchema,
		ID:            "formal-holdout-readiness-m5-21k",
		Policy: holdoutReadinessPolicy{
			MinDistinctRootCauses: 3, MinMatchingControls: 3,
			RequirePrivateDataset: true, RequireBlindSurface: true, RequireSecondStrictCFT: true,
		},
		Sources: sources,
		CurrentSurface: holdoutCurrentSurface{
			Scope:                             "current-repository",
			SupportedBenchmarkClassifications: []string{"public-calibration-only"},
			LiveManifestCount:                 livePairs, ArchivedManifestCount: archivedPairs,
			FreshEvaluatorPairLimit: 1, PrivateBlindSurface: privateBlindSurface,
		},
		SecondStrictCFT: holdoutSecondTarget{
			Target: "hashicorp-raft-v2", QualificationBundleDigest: qualification.Digest,
			QualificationReportDigest: qualification.Qualification.Digest,
			Qualified:                 qualification.Qualification.Qualified,
			RequiredCapabilities:      required, ValidatedRequiredCapabilities: validated,
			MissingRequiredCapabilities: missing,
		},
		Summary: holdoutReadinessSummary{
			RepositoryPublicPairs:              livePairs + archivedPairs,
			RepositoryPublicDistinctRootCauses: len(allRoots),
			LivePublicPairs:                    livePairs, LivePublicDistinctRootCauses: len(liveRoots),
			ArchivedPublicPairs: archivedPairs, ArchivedPublicDistinctRootCauses: len(archivedRoots),
			FormalPrivatePairs: 0, FormalEligibleRootCauses: 0, FormalEligibleControls: 0,
			Ready: false,
		},
		Findings: []string{
			"archived-public-samples-not-formal-holdout",
			"formal-classification-unsupported",
			"formal-private-dataset-absent",
			"fresh-evaluator-single-pair-only",
			"insufficient-formal-controls",
			"insufficient-formal-root-causes",
			"private-blind-surface-retired",
			"second-strict-cft-target-unqualified",
		},
		NewModelCalls: 0, NewSUTExecutions: 0,
	}
	report.Digest = ""
	digest, err := control.CanonicalDigest(report)
	if err != nil {
		t.Fatal(err)
	}
	report.Digest = digest
	return report
}

func rootsFromCurrent(manifest defectbench.BundleBenchmark) map[string]bool {
	roots := map[string]bool{}
	for _, variant := range manifest.Variants {
		if variant.Kind == defectbench.BundleKindCalibration {
			roots[variant.RootCauseID] = true
		}
	}
	return roots
}

func validateArchivedPair(t *testing.T, manifest archivedManifest) map[string]bool {
	t.Helper()
	if manifest.Version != 1 || manifest.ID == "" || manifest.BlindingNonce == "" ||
		manifest.Protocol == "" || manifest.ProfileID == "" || manifest.ProfileDigest == "" ||
		manifest.Budget.MaxRuns <= 0 || manifest.Budget.MaxDecisions <= 0 ||
		manifest.Budget.MaxPrimaryWorkUnits <= 0 || len(manifest.KillPolicy.AllowedMonitors) == 0 {
		t.Fatal("archived manifest identity is incomplete")
	}
	controls, defects := 0, 0
	roots := map[string]bool{}
	for _, variant := range manifest.Variants {
		if variant.ID == "" || variant.TrialID == "" || variant.Category == "" ||
			variant.Provenance == "" || variant.SourceDigest == "" || variant.SUTManifestDigest == "" {
			t.Fatal("archived variant identity is incomplete")
		}
		switch variant.Kind {
		case "control":
			controls++
			if variant.RootCauseID != "" {
				t.Fatal("archived control declares a root cause")
			}
		case "defect":
			defects++
			if variant.RootCauseID == "" {
				t.Fatal("archived defect lacks a root cause")
			}
			roots[variant.RootCauseID] = true
		default:
			t.Fatalf("unsupported archived variant kind %q", variant.Kind)
		}
	}
	if controls != 1 || defects != 1 {
		t.Fatalf("archived manifest is not one pair: controls=%d defects=%d", controls, defects)
	}
	return roots
}

func assertFormalClassificationRejected(t *testing.T, sources []holdoutReadinessSource, paths []string) {
	t.Helper()
	for index, source := range sources {
		if source.Status != "live-public-calibration" {
			continue
		}
		var manifest defectbench.BundleBenchmark
		if _, err := readStrictJSONBytes(paths[index], &manifest); err != nil {
			t.Fatal(err)
		}
		manifest.Classification = "formal-holdout"
		manifest.Digest = ""
		if _, err := manifest.Seal(); err == nil {
			t.Fatal("current benchmark contract unexpectedly accepted formal holdout classification")
		}
		return
	}
	t.Fatal("no live manifest available for classification check")
}

func assertFreshEvaluatorIsSinglePair(t *testing.T) {
	t.Helper()
	manifest, err := (defectbench.BundleBenchmark{
		ID: "multi-pair", Classification: "public-calibration-only", ProjectorID: "projector",
		Budget: defectbench.BundleBudget{MaxDecisions: 1, MaxPrimaryWorkUnits: 1},
		Variants: []defectbench.BundleVariant{
			{TrialID: "control-a", VariantID: "control-variant-a", Kind: defectbench.BundleKindControl, ExpectedBuildID: "control-a"},
			{TrialID: "candidate-a", VariantID: "candidate-variant-a", Kind: defectbench.BundleKindCalibration, RootCauseID: "root-a", ExpectedBuildID: "candidate-a"},
			{TrialID: "control-b", VariantID: "control-variant-b", Kind: defectbench.BundleKindControl, ExpectedBuildID: "control-b"},
			{TrialID: "candidate-b", VariantID: "candidate-variant-b", Kind: defectbench.BundleKindCalibration, RootCauseID: "root-b", ExpectedBuildID: "candidate-b"},
		},
	}).Seal()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := pairTrials(manifest); err == nil {
		t.Fatal("fresh evaluator unexpectedly accepted more than one pair")
	}
}

func hasGoSources(t *testing.T, path string) bool {
	t.Helper()
	found := false
	err := filepath.WalkDir(path, func(current string, entry os.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return filepath.SkipDir
		}
		if err != nil {
			return err
		}
		if !entry.IsDir() && filepath.Ext(current) == ".go" {
			found = true
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return found
}

func missingRequiredCapabilities(report conformance.QualificationReport) ([]string, int, int) {
	var missing []string
	required, validated := 0, 0
	for _, capability := range report.Capabilities {
		if !capability.Required {
			continue
		}
		required++
		if capability.Status == conformance.CapabilityValidated {
			validated++
		} else {
			missing = append(missing, capability.ID)
		}
	}
	sort.Strings(missing)
	return missing, required, validated
}

func mergeRoots(destination, source map[string]bool) {
	for root := range source {
		destination[root] = true
	}
}

func sha256Bytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
