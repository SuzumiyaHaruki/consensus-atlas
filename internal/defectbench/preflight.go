package defectbench

import (
	"errors"
	"sort"
)

const ReadinessReportVersion = 1

const (
	ReadinessManifestNotV2       = "manifest-not-v2"
	ReadinessInsufficientRoots   = "insufficient-distinct-root-causes"
	ReadinessInsufficientControl = "insufficient-controls"
	ReadinessNonHistorical       = "non-historical-variant"
	ReadinessMissingArtifacts    = "missing-artifact-binding"
)

// ReadinessPolicy is a curator-side admission policy for a formal benchmark.
// It deliberately checks only mechanically available facts. In particular,
// distinct root-cause labels do not prove causal independence.
type ReadinessPolicy struct {
	MinDistinctRootCauses int  `json:"min_distinct_root_causes"`
	MinControls           int  `json:"min_controls"`
	RequireHistorical     bool `json:"require_historical"`
	RequireArtifactBinds  bool `json:"require_artifact_bindings"`
}

func (policy ReadinessPolicy) Validate() error {
	if policy.MinDistinctRootCauses < 1 || policy.MinControls < 1 {
		return errors.New("readiness policy minimum root causes and controls must be positive")
	}
	return nil
}

// ReadinessReport is private curator output. It contains counts and stable
// finding codes, never variant IDs, root-cause labels, source digests, or
// monitor names that could be copied into a Planner-visible artifact.
type ReadinessReport struct {
	Version             int             `json:"version"`
	BenchmarkID         string          `json:"benchmark_id"`
	BenchmarkDigest     string          `json:"benchmark_digest"`
	Policy              ReadinessPolicy `json:"policy"`
	Passed              bool            `json:"passed"`
	DefectVariants      int             `json:"defect_variants"`
	Controls            int             `json:"controls"`
	DistinctRootCauses  int             `json:"distinct_root_causes"`
	HistoricalVariants  int             `json:"historical_variants"`
	ArtifactBoundTrials int             `json:"artifact_bound_trials"`
	Findings            []string        `json:"findings"`
}

// PreflightReadiness admits only the mechanical prerequisites for a formal
// evaluation. It is intentionally stricter than Manifest.Validate: a valid
// development/calibration manifest can still be unsuitable for a holdout.
func PreflightReadiness(manifest Manifest, policy ReadinessPolicy) (ReadinessReport, error) {
	if err := policy.Validate(); err != nil {
		return ReadinessReport{}, err
	}
	digest, err := Digest(manifest)
	if err != nil {
		return ReadinessReport{}, err
	}
	report := ReadinessReport{
		Version: ReadinessReportVersion, BenchmarkID: manifest.ID, BenchmarkDigest: digest,
		Policy: policy, Passed: true,
	}
	roots := make(map[string]bool)
	missingArtifacts, nonHistorical := false, false
	for _, variant := range manifest.Variants {
		if variant.Kind == KindControl {
			report.Controls++
		} else {
			report.DefectVariants++
			roots[variant.RootCauseID] = true
		}
		if variant.Provenance == ProvenanceHistorical {
			report.HistoricalVariants++
		} else {
			nonHistorical = true
		}
		if variant.BuildAuditDigest != "" && variant.BinaryDigest != "" {
			report.ArtifactBoundTrials++
		} else {
			missingArtifacts = true
		}
	}
	report.DistinctRootCauses = len(roots)
	if manifest.Version != ManifestVersion2 {
		report.Findings = append(report.Findings, ReadinessManifestNotV2)
	}
	if report.DistinctRootCauses < policy.MinDistinctRootCauses {
		report.Findings = append(report.Findings, ReadinessInsufficientRoots)
	}
	if report.Controls < policy.MinControls {
		report.Findings = append(report.Findings, ReadinessInsufficientControl)
	}
	if policy.RequireHistorical && nonHistorical {
		report.Findings = append(report.Findings, ReadinessNonHistorical)
	}
	if policy.RequireArtifactBinds && missingArtifacts {
		report.Findings = append(report.Findings, ReadinessMissingArtifacts)
	}
	sort.Strings(report.Findings)
	report.Passed = len(report.Findings) == 0
	return report, nil
}
