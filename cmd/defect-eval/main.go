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
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/sutbuild"
)

const formalFreshInputsSchemaVersion = "consensus-atlas/formal-fresh-inputs/v1"

type formalFreshTrialInput struct {
	TrialID        string `json:"trial_id"`
	BuildAuditPath string `json:"build_audit_path"`
	BinaryPath     string `json:"binary_path"`
}

// formalFreshInputs is private curator-side I/O. Paths never enter the
// evaluation ledger or any Agent-facing artifact.
type formalFreshInputs struct {
	SchemaVersion string                  `json:"schema_version"`
	Trials        []formalFreshTrialInput `json:"trials"`
}

type freshTrialSource struct {
	trialID    string
	auditPath  string
	binaryPath string
}

type loadedFreshTrial struct {
	trialID      string
	audit        sutbuild.Audit
	auditDigest  string
	binary       []byte
	binaryDigest string
}

type freshBundleRunner func(
	trialID string,
	binary []byte,
	spec controlexperiment.MethodSpec,
) (controlexperiment.Report, controlexperiment.ExecutionBundle, error)

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
	formalContractPath := flags.String("formal-contract", "", "private FormalBenchmarkContract")
	formalExposurePath := flags.String("formal-exposure-audit", "", "passed private FormalExposureAudit")
	formalInputsPath := flags.String("formal-inputs", "", "private multi-trial audit/binary path manifest")
	if err := flags.Parse(args); err != nil {
		return err
	}
	formalMode := *formalContractPath != "" || *formalExposurePath != "" || *formalInputsPath != ""
	if formalMode {
		if *formalContractPath == "" || *formalExposurePath == "" || *formalInputsPath == "" ||
			*methodSpecPath == "" || *freshArtifacts == "" || *outPath == "" ||
			*manifestPath != "" || *controlPath != "" || *candidatePath != "" ||
			*controlAuditPath != "" || *controlBinaryPath != "" ||
			*candidateAuditPath != "" || *candidateBinaryPath != "" {
			return errors.New("formal evaluation requires contract, exposure audit, inputs, method spec, fresh artifacts, out, and no public-pair flags")
		}
		return runFormalFreshEvaluation(
			*formalContractPath, *formalExposurePath, *formalInputsPath,
			*methodSpecPath, *freshArtifacts, *outPath, runFreshBundle,
		)
	}
	if *manifestPath == "" || *outPath == "" {
		return errors.New("public evaluation requires -manifest and -out")
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
	sources := []freshTrialSource{
		{trialID: controlTrial, auditPath: controlAuditPath, binaryPath: controlBinaryPath},
		{trialID: candidateTrial, auditPath: candidateAuditPath, binaryPath: candidateBinaryPath},
	}
	loaded, err := loadFreshTrials(sources)
	if err != nil {
		return err
	}
	evidence, err := executeFreshTrials(loaded, spec, artifactDir, runFreshBundle)
	if err != nil {
		return err
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

func runFormalFreshEvaluation(
	contractPath string,
	exposurePath string,
	inputsPath string,
	methodSpecPath string,
	artifactDir string,
	outPath string,
	runner freshBundleRunner,
) error {
	if runner == nil {
		return errors.New("FORMAL_CLI_RUNNER_REQUIRED")
	}
	var contract defectbench.FormalBenchmarkContract
	if err := readStrictJSON(contractPath, &contract); err != nil {
		return err
	}
	var exposure defectbench.FormalExposureAudit
	if err := readStrictJSON(exposurePath, &exposure); err != nil {
		return err
	}
	var spec controlexperiment.MethodSpec
	if err := readStrictJSON(methodSpecPath, &spec); err != nil {
		return err
	}
	if contract.Composition.ProjectorID != etcdraftv2.DecisionProjectionID {
		return errors.New("FORMAL_CLI_PROJECTOR_UNSUPPORTED")
	}
	registeredMonitors := []oracle.BundleMonitor{oracle.BundleAgreement{}}
	if err := defectbench.ValidateFormalFreshEvaluationAdmission(
		contract, exposure, spec, etcdraftv2.DecisionProjector{}, registeredMonitors...,
	); err != nil {
		return err
	}

	var inputs formalFreshInputs
	if err := readStrictJSON(inputsPath, &inputs); err != nil {
		return err
	}
	sources, variants, err := validateFormalFreshInputs(inputsPath, inputs, contract)
	if err != nil {
		return err
	}
	loaded, err := loadFreshTrials(sources)
	if err != nil {
		return err
	}
	for _, current := range loaded {
		variant := variants[current.trialID]
		if current.audit.TrialID != current.trialID || current.audit.SUTBuildIdentity != variant.ExpectedBuildID ||
			current.auditDigest != variant.ExpectedBuildAuditDigest ||
			current.binaryDigest != variant.ExpectedBinaryDigest {
			return fmt.Errorf("FORMAL_CLI_BUILD_EVIDENCE_MISMATCH: %s", current.trialID)
		}
	}
	if err := requireNewFormalOutputs(artifactDir, outPath); err != nil {
		return err
	}
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return err
	}
	evidence, err := executeFreshTrials(loaded, spec, artifactDir, runner)
	if err != nil {
		return err
	}
	report, err := defectbench.EvaluateFormalFreshBundles(
		contract, exposure, spec, evidence, etcdraftv2.DecisionProjector{}, registeredMonitors...,
	)
	if err != nil {
		return err
	}
	if err := writeJSONExclusive(outPath, report); err != nil {
		return err
	}
	fmt.Printf("wrote %s\nmethod=%s pairs=%d controls=%d false-positive=%d candidates=%d killed=%d invalid=%d digest=%s\n",
		outPath, spec.Digest, len(report.Pairs), report.Summary.Controls, report.Summary.FalsePositives,
		report.Summary.Candidates, report.Summary.KilledCandidates,
		report.Summary.InvalidTrials, report.Digest)
	return nil
}

func validateFormalFreshInputs(
	inputsPath string,
	inputs formalFreshInputs,
	contract defectbench.FormalBenchmarkContract,
) ([]freshTrialSource, map[string]defectbench.FormalVariant, error) {
	if inputs.SchemaVersion != formalFreshInputsSchemaVersion || len(inputs.Trials) != len(contract.Pairs)*2 {
		return nil, nil, errors.New("FORMAL_CLI_INPUT_SET_INVALID")
	}
	variants := make(map[string]defectbench.FormalVariant, len(inputs.Trials))
	for _, pair := range contract.Pairs {
		variants[pair.Control.TrialID] = pair.Control
		variants[pair.Candidate.TrialID] = pair.Candidate
	}
	base, err := filepath.Abs(filepath.Dir(inputsPath))
	if err != nil {
		return nil, nil, err
	}
	seen := make(map[string]bool, len(inputs.Trials))
	sources := make([]freshTrialSource, 0, len(inputs.Trials))
	for _, input := range inputs.Trials {
		if _, ok := variants[input.TrialID]; !ok || seen[input.TrialID] ||
			strings.TrimSpace(input.BuildAuditPath) == "" || strings.TrimSpace(input.BinaryPath) == "" {
			return nil, nil, errors.New("FORMAL_CLI_INPUT_SET_INVALID")
		}
		seen[input.TrialID] = true
		sources = append(sources, freshTrialSource{
			trialID: input.TrialID, auditPath: resolveFormalInputPath(base, input.BuildAuditPath),
			binaryPath: resolveFormalInputPath(base, input.BinaryPath),
		})
	}
	return sources, variants, nil
}

func resolveFormalInputPath(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, filepath.Clean(path))
}

func loadFreshTrials(sources []freshTrialSource) ([]loadedFreshTrial, error) {
	loaded := make([]loadedFreshTrial, 0, len(sources))
	for _, source := range sources {
		var audit sutbuild.Audit
		auditBytes, err := readStrictJSONBytes(source.auditPath, &audit)
		if err != nil {
			return nil, err
		}
		if err := audit.Validate(); err != nil {
			return nil, fmt.Errorf("validate %s build audit: %w", source.trialID, err)
		}
		binary, err := os.ReadFile(source.binaryPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", source.binaryPath, err)
		}
		binaryDigest := digestBytes(binary)
		if binaryDigest != audit.BinaryDigest {
			return nil, fmt.Errorf("binary %s does not match its build audit", source.binaryPath)
		}
		loaded = append(loaded, loadedFreshTrial{
			trialID: source.trialID, audit: audit, auditDigest: digestBytes(auditBytes),
			binary: binary, binaryDigest: binaryDigest,
		})
	}
	return loaded, nil
}

func executeFreshTrials(
	inputs []loadedFreshTrial,
	spec controlexperiment.MethodSpec,
	artifactDir string,
	runner freshBundleRunner,
) (map[string]defectbench.FreshBundleEvidence, error) {
	evidence := make(map[string]defectbench.FreshBundleEvidence, len(inputs))
	for _, current := range inputs {
		report, bundle, err := runner(current.trialID, current.binary, spec)
		if err != nil {
			return nil, err
		}
		if err := writeJSON(filepath.Join(artifactDir, current.trialID, "report.json"), report); err != nil {
			return nil, err
		}
		if err := writeJSON(filepath.Join(artifactDir, current.trialID, "bundle.json"), bundle); err != nil {
			return nil, err
		}
		evidence[current.trialID] = defectbench.FreshBundleEvidence{
			Report: report, Bundle: bundle, BuildAudit: current.audit,
			BuildAuditDigest: current.auditDigest, BinaryDigest: current.binaryDigest,
		}
	}
	return evidence, nil
}

func requireNewFormalOutputs(artifactDir, outPath string) error {
	artifactAbsolute, err := filepath.Abs(artifactDir)
	if err != nil {
		return err
	}
	outputAbsolute, err := filepath.Abs(outPath)
	if err != nil {
		return err
	}
	separator := string(os.PathSeparator)
	if outputAbsolute == artifactAbsolute ||
		strings.HasPrefix(outputAbsolute, artifactAbsolute+separator) ||
		strings.HasPrefix(artifactAbsolute, outputAbsolute+separator) {
		return errors.New("FORMAL_CLI_OUTPUT_PATHS_OVERLAP")
	}
	for _, path := range []string{artifactDir, outPath} {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("FORMAL_CLI_OUTPUT_EXISTS: %s", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
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

func writeJSONExclusive(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
