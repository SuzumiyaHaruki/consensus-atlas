package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/families"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/campaign"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/sutbuild"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "defect-eval:", err)
		os.Exit(1)
	}
}

func run() error {
	manifestPath := flag.String("manifest", "", "private defect benchmark manifest")
	submissionPath := flag.String("submission", "", "blind trial-to-campaign-report submission")
	outPath := flag.String("out", "", "trusted evaluation report output")
	blindOutPath := flag.String("blind-out", "", "optional Agent-facing blind manifest output")
	legacyPSSID := flag.String("legacy-pss-id", "", "PSS identity only for archived v1 manifests without pss_id")
	flag.Parse()
	if *manifestPath == "" {
		return errors.New("-manifest is required")
	}
	if (*submissionPath == "") != (*outPath == "") {
		return errors.New("-submission and -out must be provided together")
	}
	if *submissionPath == "" && *blindOutPath == "" {
		return errors.New("provide -blind-out, or both -submission and -out")
	}

	var manifest defectbench.Manifest
	if err := readStrictJSON(*manifestPath, &manifest); err != nil {
		return err
	}
	blind, err := manifest.Blind()
	if err != nil {
		return fmt.Errorf("validate private manifest: %w", err)
	}
	if *blindOutPath != "" {
		if err := writeJSON(*blindOutPath, blind); err != nil {
			return err
		}
	}
	if *submissionPath == "" {
		fmt.Printf("wrote blind benchmark manifest %s (%d opaque trials)\n", *blindOutPath, len(blind.Trials))
		return nil
	}
	pssID, err := frozenPSSID(manifest, *legacyPSSID)
	if err != nil {
		return err
	}

	var submission defectbench.Submission
	if err := readStrictJSON(*submissionPath, &submission); err != nil {
		return err
	}
	if err := submission.Validate(blind); err != nil {
		return fmt.Errorf("validate submission: %w", err)
	}
	monitors, err := families.RegisteredMonitors(pssID)
	if err != nil {
		return err
	}
	ledger, err := defectbench.NewLedger(manifest, monitors...)
	if err != nil {
		return err
	}
	base := filepath.Dir(*submissionPath)
	for _, trial := range submission.Trials {
		reportPath := resolveArtifact(base, trial.CampaignReport)
		auditPath := resolveArtifact(base, trial.BuildAudit)
		binaryPath := resolveArtifact(base, trial.Binary)
		profilePath := resolveArtifact(base, trial.Profile)
		plansPath := resolveArtifact(base, trial.Plans)
		var submitted campaign.Report
		if err := readStrictJSON(reportPath, &submitted); err != nil {
			return err
		}
		var audit sutbuild.Audit
		auditBytes, err := readStrictJSONBytes(auditPath, &audit)
		if err != nil {
			return err
		}
		if err := audit.Validate(); err != nil {
			return fmt.Errorf("validate build audit %s: %w", auditPath, err)
		}
		if audit.TrialID != trial.TrialID {
			return fmt.Errorf("build audit %s belongs to trial %s, want %s", auditPath, audit.TrialID, trial.TrialID)
		}
		binaryBytes, err := os.ReadFile(binaryPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", binaryPath, err)
		}
		binaryDigest := digestBytes(binaryBytes)
		if binaryDigest != audit.BinaryDigest {
			return fmt.Errorf("binary %s does not match its build audit", binaryPath)
		}
		report, err := runTrustedCampaign(trial.TrialID, binaryPath, profilePath, plansPath)
		if err != nil {
			return err
		}
		submittedDigest, err := defectbench.CampaignReportDigest(submitted)
		if err != nil {
			return err
		}
		reportDigest, err := defectbench.CampaignReportDigest(report)
		if err != nil {
			return err
		}
		if reportDigest != submittedDigest {
			return fmt.Errorf("trusted rerun for trial %s does not match the submitted Campaign report", trial.TrialID)
		}
		evidence := defectbench.TrialEvidence{
			BuildAuditDigest: digestBytes(auditBytes), BinaryDigest: binaryDigest,
			BuildSpecDigest: audit.BuildSpecDigest, SourceDigest: audit.OutputSourceDigest,
			SUTBuildIdentity: audit.SUTBuildIdentity, CampaignReportDigest: reportDigest,
		}
		if _, err := ledger.AddTrial(trial.TrialID, report, evidence); err != nil {
			return err
		}
	}
	evaluation := ledger.Report()
	if err := writeJSON(*outPath, evaluation); err != nil {
		return err
	}
	fmt.Printf("wrote %s\nroot causes: %d, killed: %d (%.2f%%), controls: %d, false positives: %d (%.2f%%), invalid: %d, primary-work: %d\n",
		*outPath, evaluation.Summary.RootCauses, evaluation.Summary.KilledRootCauses,
		evaluation.Summary.RootCauseKillRate, evaluation.Summary.Controls,
		evaluation.Summary.FalsePositives, evaluation.Summary.FalsePositiveRate,
		evaluation.Summary.InvalidTrials, evaluation.Summary.PrimaryWorkUnits)
	return nil
}

func frozenPSSID(manifest defectbench.Manifest, legacy string) (string, error) {
	if manifest.PSSID != "" {
		return manifest.PSSID, nil
	}
	if legacy != "" {
		return legacy, nil
	}
	return "", errors.New("benchmark manifest has no pss_id; archived v1 manifests require -legacy-pss-id")
}

func runTrustedCampaign(trialID, binaryPath, profilePath, plansPath string) (campaign.Report, error) {
	output, err := os.CreateTemp("", "consensus-atlas-evaluator-*.json")
	if err != nil {
		return campaign.Report{}, err
	}
	outputPath := output.Name()
	if err := output.Close(); err != nil {
		return campaign.Report{}, err
	}
	defer os.Remove(outputPath)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, binaryPath,
		"-profile", profilePath, "-plans", plansPath, "-out", outputPath,
		"-artifact", "trial://"+trialID,
	)
	// Evaluated SUTs do not inherit credentials or user configuration.
	command.Env = []string{"TZ=UTC"}
	combined, err := command.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return campaign.Report{}, fmt.Errorf("trusted Campaign rerun for trial %s timed out: %w", trialID, ctx.Err())
		}
		return campaign.Report{}, fmt.Errorf("trusted Campaign rerun for trial %s failed: %w: %s",
			trialID, err, strings.TrimSpace(string(combined)))
	}
	var report campaign.Report
	if err := readStrictJSON(outputPath, &report); err != nil {
		return campaign.Report{}, err
	}
	return report, nil
}

func readStrictJSON(path string, target any) error {
	_, err := readStrictJSONBytes(path, target)
	return err
}

func readStrictJSONBytes(path string, target any) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("decode %s: multiple JSON values", path)
		}
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return data, nil
}

func resolveArtifact(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(base, path))
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
