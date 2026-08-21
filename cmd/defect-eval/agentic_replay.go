package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/sutbuild"
)

type agenticReplaySource struct {
	Audit           sutbuild.Audit
	AuditDigest     string
	SUTBinary       []byte
	SUTBinaryDigest string
	Executor        []byte
}

func loadAgenticReplaySource(base string, input agenticHoldoutTrialInput) (agenticReplaySource, error) {
	var source agenticReplaySource
	auditBytes, err := readStrictJSONBytes(resolveFormalInputPath(base, input.BuildAuditPath), &source.Audit)
	if err != nil || source.Audit.Validate() != nil {
		return source, errors.New("AGENTIC_HOLDOUT_REPLAY_BUILD_AUDIT_INVALID")
	}
	source.AuditDigest = digestBytes(auditBytes)
	source.SUTBinary, err = os.ReadFile(resolveFormalInputPath(base, input.SUTBinaryPath))
	if err != nil {
		return source, err
	}
	source.SUTBinaryDigest = digestBytes(source.SUTBinary)
	if source.SUTBinaryDigest != source.Audit.BinaryDigest {
		return source, errors.New("AGENTIC_HOLDOUT_REPLAY_SUT_BINARY_MISMATCH")
	}
	source.Executor, err = os.ReadFile(resolveFormalInputPath(base, input.ExecutorPath))
	if err != nil || len(source.Executor) == 0 {
		return source, errors.New("AGENTIC_HOLDOUT_REPLAY_EXECUTOR_INVALID")
	}
	return source, nil
}

func loadAgenticReplaySources(
	inputsPath string,
	inputs agenticHoldoutInputs,
	contract defectbench.FormalBenchmarkContract,
) (map[string]agenticReplaySource, error) {
	if inputs.SchemaVersion != agenticHoldoutInputsSchemaVersion || len(inputs.Trials) != len(contract.Pairs)*2 {
		return nil, errors.New("AGENTIC_HOLDOUT_CLI_REPLAY_INPUT_SET_INVALID")
	}
	base, err := filepath.Abs(filepath.Dir(inputsPath))
	if err != nil {
		return nil, err
	}
	result := make(map[string]agenticReplaySource, len(inputs.Trials))
	for _, input := range inputs.Trials {
		if input.BuildAuditPath == "" || input.SUTBinaryPath == "" || input.ExecutorPath == "" ||
			result[input.TrialID].AuditDigest != "" {
			return nil, errors.New("AGENTIC_HOLDOUT_CLI_REPLAY_INPUT_SET_INVALID")
		}
		source, err := loadAgenticReplaySource(base, input)
		variant := formalVariantForTrial(contract, input.TrialID)
		if err != nil || variant == nil || source.Audit.TrialID != input.TrialID ||
			source.Audit.SUTBuildIdentity != variant.ExpectedBuildID ||
			source.AuditDigest != variant.ExpectedBuildAuditDigest ||
			source.SUTBinaryDigest != variant.ExpectedBinaryDigest {
			return nil, fmt.Errorf("AGENTIC_HOLDOUT_CLI_BUILD_EVIDENCE_MISMATCH: %s", input.TrialID)
		}
		result[input.TrialID] = source
	}
	return result, nil
}

func formalVariantForTrial(
	contract defectbench.FormalBenchmarkContract,
	trialID string,
) *defectbench.FormalVariant {
	for _, pair := range contract.Pairs {
		for _, variant := range []defectbench.FormalVariant{pair.Control, pair.Candidate} {
			if variant.TrialID == trialID {
				copyVariant := variant
				return &copyVariant
			}
		}
	}
	return nil
}

func newAgenticEvaluatorReplayRunner(
	sources map[string]agenticReplaySource,
	evidence map[string]defectbench.AgenticTrialEvidence,
	artifactRoot string,
) defectbench.AgenticReplayRunner {
	var mutex sync.Mutex
	ordinals := make(map[string]int)
	return func(trialID string, sourceBundle controlexperiment.ExecutionBundle) (
		controlexperiment.ExecutionBundle, error,
	) {
		source, ok := sources[trialID]
		trialEvidence, evidenceOK := evidence[trialID]
		if !ok || !evidenceOK || sourceBundle.Recipe == nil {
			return controlexperiment.ExecutionBundle{}, errors.New("AGENTIC_HOLDOUT_REPLAY_SOURCE_MISSING")
		}
		if sourceBundle.Recipe.TargetID == "etcdraft-v2" &&
			digestBytes(source.Executor) != source.SUTBinaryDigest {
			return controlexperiment.ExecutionBundle{}, errors.New("AGENTIC_HOLDOUT_REPLAY_EMBEDDED_SUT_EXECUTOR_MISMATCH")
		}
		mutex.Lock()
		ordinals[trialID]++
		ordinal := ordinals[trialID]
		mutex.Unlock()
		directory := filepath.Join(artifactRoot, trialID, fmt.Sprintf("replay-%03d", ordinal))
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return controlexperiment.ExecutionBundle{}, err
		}
		executorPath := filepath.Join(directory, "executor")
		if err := os.WriteFile(executorPath, source.Executor, 0o700); err != nil {
			return controlexperiment.ExecutionBundle{}, err
		}
		sutPath := executorPath
		if sourceBundle.Recipe.TargetID == "omnipaxos-v2" {
			sutPath = filepath.Join(directory, "sut-worker")
			if err := os.WriteFile(sutPath, source.SUTBinary, 0o700); err != nil {
				return controlexperiment.ExecutionBundle{}, err
			}
		}
		inputPath := filepath.Join(directory, "source-bundle.json")
		reportPath := filepath.Join(directory, "report.json")
		bundlePath := filepath.Join(directory, "bundle.json")
		if err := writeJSON(inputPath, sourceBundle); err != nil {
			return controlexperiment.ExecutionBundle{}, err
		}
		timeoutMS := trialEvidence.MethodSpec.EpisodeLimits.PreparationWallClockMS
		if timeoutMS <= 0 {
			return controlexperiment.ExecutionBundle{}, errors.New("AGENTIC_HOLDOUT_REPLAY_TIMEOUT_INVALID")
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMS)*time.Millisecond)
		defer cancel()
		args := []string{
			"-strategy", "evaluator-replay-v1", "-target", sourceBundle.Recipe.TargetID,
			"-bundle-in", inputPath, "-out", reportPath, "-bundle-out", bundlePath,
		}
		if sourceBundle.Recipe.TargetID == "omnipaxos-v2" {
			args = append(args, "-worker", sutPath)
		}
		command := exec.CommandContext(ctx, executorPath, args...)
		command.Env = []string{"TZ=UTC"}
		combined, err := command.CombinedOutput()
		if err != nil {
			if ctx.Err() != nil {
				return controlexperiment.ExecutionBundle{}, fmt.Errorf("Agentic replay %s timed out: %w", trialID, ctx.Err())
			}
			return controlexperiment.ExecutionBundle{}, fmt.Errorf(
				"Agentic replay %s failed: %w: %s", trialID, err, strings.TrimSpace(string(combined)),
			)
		}
		var fresh controlexperiment.ExecutionBundle
		if err := readStrictJSON(bundlePath, &fresh); err != nil {
			return controlexperiment.ExecutionBundle{}, err
		}
		return fresh, nil
	}
}
