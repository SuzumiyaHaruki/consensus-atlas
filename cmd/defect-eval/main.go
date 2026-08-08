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

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/sutbuild"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "defect-eval:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("defect-eval", flag.ContinueOnError)
	manifestPath := flags.String("manifest", "", "bundle benchmark manifest")
	controlPath := flags.String("control-bundle", "", "correct control ExecutionBundle")
	candidatePath := flags.String("candidate-bundle", "", "public calibration candidate ExecutionBundle")
	outPath := flags.String("out", "", "trusted bundle evaluation output")
	methodSpecPath := flags.String("method-spec", "", "frozen MethodSpec for evaluator-owned fresh execution")
	controlAuditPath := flags.String("control-build-audit", "", "control build audit for fresh execution")
	controlBinaryPath := flags.String("control-binary", "", "control binary for fresh execution")
	candidateAuditPath := flags.String("candidate-build-audit", "", "candidate build audit for fresh execution")
	candidateBinaryPath := flags.String("candidate-binary", "", "candidate binary for fresh execution")
	freshArtifacts := flags.String("fresh-artifacts", "", "directory for evaluator-owned reports and bundles")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *manifestPath == "" || *outPath == "" {
		return errors.New("-manifest and -out are required")
	}
	var manifest defectbench.BundleBenchmark
	if err := readStrictJSON(*manifestPath, &manifest); err != nil {
		return err
	}
	freshMode := *methodSpecPath != "" || *controlAuditPath != "" || *controlBinaryPath != "" ||
		*candidateAuditPath != "" || *candidateBinaryPath != "" || *freshArtifacts != ""
	if freshMode {
		if *methodSpecPath == "" || *controlAuditPath == "" || *controlBinaryPath == "" ||
			*candidateAuditPath == "" || *candidateBinaryPath == "" || *freshArtifacts == "" ||
			*controlPath != "" || *candidatePath != "" {
			return errors.New("fresh evaluation requires method spec, both build audits/binaries, fresh artifacts, and no submitted bundles")
		}
		return runFreshEvaluation(
			manifest, *methodSpecPath, *controlAuditPath, *controlBinaryPath,
			*candidateAuditPath, *candidateBinaryPath, *freshArtifacts, *outPath,
		)
	}
	if *controlPath == "" || *candidatePath == "" {
		return errors.New("legacy bundle evaluation requires -control-bundle and -candidate-bundle")
	}
	controlTrial, candidateTrial, err := pairTrials(manifest)
	if err != nil {
		return err
	}
	var controlBundle, candidateBundle controlexperiment.ExecutionBundle
	if err := readStrictJSON(*controlPath, &controlBundle); err != nil {
		return err
	}
	if err := readStrictJSON(*candidatePath, &candidateBundle); err != nil {
		return err
	}
	report, err := defectbench.EvaluateBundles(
		manifest,
		map[string]controlexperiment.ExecutionBundle{
			controlTrial: controlBundle, candidateTrial: candidateBundle,
		},
		etcdraftv2.DecisionProjector{},
	)
	if err != nil {
		return err
	}
	if err := writeJSON(*outPath, report); err != nil {
		return err
	}
	fmt.Printf("wrote %s\ncontrols=%d false-positive=%d candidates=%d killed=%d invalid=%d digest=%s\n",
		*outPath, report.Summary.Controls, report.Summary.FalsePositives,
		report.Summary.Candidates, report.Summary.KilledCandidates,
		report.Summary.InvalidTrials, report.Digest)
	return nil
}

func runFreshEvaluation(
	manifest defectbench.BundleBenchmark,
	methodSpecPath string,
	controlAuditPath string,
	controlBinaryPath string,
	candidateAuditPath string,
	candidateBinaryPath string,
	artifactDir string,
	outPath string,
) error {
	var spec controlexperiment.MethodSpec
	if err := readStrictJSON(methodSpecPath, &spec); err != nil {
		return err
	}
	if err := spec.Validate(); err != nil {
		return err
	}
	controlTrial, candidateTrial, err := pairTrials(manifest)
	if err != nil {
		return err
	}
	type input struct {
		trialID    string
		auditPath  string
		binaryPath string
	}
	inputs := []input{
		{trialID: controlTrial, auditPath: controlAuditPath, binaryPath: controlBinaryPath},
		{trialID: candidateTrial, auditPath: candidateAuditPath, binaryPath: candidateBinaryPath},
	}
	evidence := make(map[string]defectbench.FreshBundleEvidence, len(inputs))
	for _, current := range inputs {
		var audit sutbuild.Audit
		auditBytes, err := readStrictJSONBytes(current.auditPath, &audit)
		if err != nil {
			return err
		}
		if err := audit.Validate(); err != nil {
			return fmt.Errorf("validate %s build audit: %w", current.trialID, err)
		}
		binary, err := os.ReadFile(current.binaryPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", current.binaryPath, err)
		}
		binaryDigest := digestBytes(binary)
		if binaryDigest != audit.BinaryDigest {
			return fmt.Errorf("binary %s does not match its build audit", current.binaryPath)
		}
		report, bundle, err := runFreshBundle(current.trialID, binary, spec)
		if err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(artifactDir, current.trialID, "report.json"), report); err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(artifactDir, current.trialID, "bundle.json"), bundle); err != nil {
			return err
		}
		evidence[current.trialID] = defectbench.FreshBundleEvidence{
			Report: report, Bundle: bundle, BuildAudit: audit,
			BuildAuditDigest: digestBytes(auditBytes), BinaryDigest: binaryDigest,
		}
	}
	report, err := defectbench.EvaluateFreshBundles(
		manifest, spec, evidence, etcdraftv2.DecisionProjector{},
	)
	if err != nil {
		return err
	}
	if err := writeJSON(outPath, report); err != nil {
		return err
	}
	fmt.Printf("wrote %s\nmethod=%s controls=%d false-positive=%d candidates=%d killed=%d invalid=%d digest=%s\n",
		outPath, spec.Digest, report.Summary.Controls, report.Summary.FalsePositives,
		report.Summary.Candidates, report.Summary.KilledCandidates,
		report.Summary.InvalidTrials, report.Digest)
	return nil
}

func runFreshBundle(
	trialID string,
	binary []byte,
	spec controlexperiment.MethodSpec,
) (controlexperiment.Report, controlexperiment.ExecutionBundle, error) {
	temporary, err := os.MkdirTemp("", "consensus-atlas-fresh-eval-*")
	if err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	defer os.RemoveAll(temporary)
	binaryPath := filepath.Join(temporary, "sut")
	if err := os.WriteFile(binaryPath, binary, 0o700); err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	reportPath := filepath.Join(temporary, "report.json")
	bundlePath := filepath.Join(temporary, "bundle.json")
	ctx, cancel := context.WithTimeout(
		context.Background(), time.Duration(spec.TimeoutMillis)*time.Millisecond,
	)
	defer cancel()
	command := exec.CommandContext(ctx, binaryPath,
		"-strategy", spec.Strategy,
		"-decisions", fmt.Sprintf("%d", spec.Decisions),
		"-policy-seed", fmt.Sprintf("%d", spec.PolicySeed),
		"-out", reportPath,
		"-bundle-out", bundlePath,
		"-bundle-evidence-version", "3",
		"-method-spec-digest", spec.Digest,
	)
	command.Env = []string{"TZ=UTC"}
	combined, err := command.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return controlexperiment.Report{}, controlexperiment.ExecutionBundle{},
				fmt.Errorf("fresh execution for %s timed out: %w", trialID, ctx.Err())
		}
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{},
			fmt.Errorf("fresh execution for %s failed: %w: %s", trialID, err, strings.TrimSpace(string(combined)))
	}
	var report controlexperiment.Report
	if err := readStrictJSON(reportPath, &report); err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	var bundle controlexperiment.ExecutionBundle
	if err := readStrictJSON(bundlePath, &bundle); err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	if err := spec.ValidateExecution(report, bundle); err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{},
			fmt.Errorf("fresh execution for %s violated MethodSpec: %w", trialID, err)
	}
	return report, bundle, nil
}

func pairTrials(manifest defectbench.BundleBenchmark) (string, string, error) {
	if err := manifest.Validate(); err != nil {
		return "", "", err
	}
	var control, candidate string
	for _, variant := range manifest.Variants {
		switch variant.Kind {
		case defectbench.BundleKindControl:
			if control != "" {
				return "", "", errors.New("bundle evaluator requires exactly one control")
			}
			control = variant.TrialID
		case defectbench.BundleKindCalibration:
			if candidate != "" {
				return "", "", errors.New("bundle evaluator requires exactly one calibration candidate")
			}
			candidate = variant.TrialID
		}
	}
	if control == "" || candidate == "" {
		return "", "", errors.New("bundle evaluator requires a control/candidate pair")
	}
	return control, candidate, nil
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
		return nil, fmt.Errorf("decode %s: trailing JSON data", path)
	}
	return data, nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
