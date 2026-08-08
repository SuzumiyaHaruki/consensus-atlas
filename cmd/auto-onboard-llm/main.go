package main

import (
	"bytes"
	"context"
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

	"github.com/SuzumiyaHaruki/consensus-atlas/bindings"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/autoonboard"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/modelcommand"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolcontract"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/scenario"
)

func main() {
	repoRoot := flag.String("repo", ".", "repository root")
	contractPath := flag.String("contract", "contracts/etcdraft-v1.json", "protocol knowledge contract")
	keyPath := flag.String("key-file", "key.txt", "permission-restricted DeepSeek API key file")
	model := flag.String("model", "deepseek-v4-flash", "DeepSeek model ID")
	endpoint := flag.String("endpoint", "https://api.deepseek.com/chat/completions", "DeepSeek Chat Completions endpoint")
	scenarioPrefix := flag.String("scenario-prefix", "etcdraft-", "only expose scenario filenames with this prefix")
	maxAttempts := flag.Int("max-attempts", 5, "maximum model/validator feedback attempts")
	timeout := flag.Duration("timeout", 10*time.Minute, "overall onboarding deadline")
	reportPath := flag.String("report-out", "artifacts/onboarding/deepseek-etcdraft-v1.json", "loop report output")
	bindingPath := flag.String("binding-out", "artifacts/onboarding/deepseek-etcdraft-binding-v1.json", "last generated binding output")
	profilePath := flag.String("profile-out", "artifacts/onboarding/deepseek-etcdraft-profile-v1.json", "validated profile output")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err := run(ctx, config{
		repoRoot: *repoRoot, contractPath: *contractPath, keyPath: *keyPath,
		model: *model, endpoint: *endpoint, scenarioPrefix: *scenarioPrefix,
		maxAttempts: *maxAttempts, reportPath: *reportPath, bindingPath: *bindingPath, profilePath: *profilePath,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "auto-onboard-llm:", err)
		os.Exit(1)
	}
}

type config struct {
	repoRoot, contractPath, keyPath      string
	model, endpoint, scenarioPrefix      string
	reportPath, bindingPath, profilePath string
	maxAttempts                          int
}

func run(ctx context.Context, cfg config) error {
	if cfg.maxAttempts < 1 || cfg.maxAttempts > 10 {
		return errors.New("max-attempts must be between 1 and 10")
	}
	root, err := filepath.Abs(cfg.repoRoot)
	if err != nil {
		return err
	}
	keyPath, err := filepath.Abs(cfg.keyPath)
	if err != nil {
		return err
	}
	if err := validateSecretFile(keyPath); err != nil {
		return err
	}
	contractPath := resolve(root, cfg.contractPath)
	contract, err := protocolcontract.Load(contractPath)
	if err != nil {
		return err
	}
	if err := contract.Validate(); err != nil {
		return err
	}
	declaredCapabilities := make(map[string]bool, len(contract.Capabilities))
	for _, capability := range contract.Capabilities {
		declaredCapabilities[capability.ID] = true
	}
	declaredProfile, err := protocolcontract.Compile(contract, declaredCapabilities)
	if err != nil {
		return err
	}
	_, manifest, err := bindings.New(declaredProfile)
	if err != nil {
		return err
	}
	scenarios, err := loadScenarios(root, cfg.scenarioPrefix)
	if err != nil {
		return err
	}
	if len(scenarios) == 0 {
		return errors.New("no scenarios matched the configured prefix")
	}

	python, err := exec.LookPath("python3")
	if err != nil {
		return err
	}
	generator := &autoonboard.CommandGenerator{
		Path: python,
		Args: []string{filepath.Join(root, "agents", "deepseek_onboarding.py")},
		Dir:  root,
		Env: []string{
			"CONSENSUS_ATLAS_DEEPSEEK_KEY_FILE=" + keyPath,
			"CONSENSUS_ATLAS_DEEPSEEK_MODEL=" + cfg.model,
			"CONSENSUS_ATLAS_DEEPSEEK_ENDPOINT=" + cfg.endpoint,
		},
	}
	validator := func(ctx context.Context, proposal *autoonboard.Binding) (autoonboard.Report, error) {
		return autoonboard.Validate(ctx, root, contract, proposal, bindings.New)
	}
	loop, err := autoonboard.Coordinate(ctx, contract, autoonboard.GenerationContext{
		Driver: manifest, Scenarios: scenarios,
	}, cfg.maxAttempts, generator, validator)
	if err != nil {
		return err
	}
	if err := writeJSON(resolve(root, cfg.reportPath), loop); err != nil {
		return err
	}
	if loop.FinalBinding != nil {
		if err := writeJSON(resolve(root, cfg.bindingPath), loop.FinalBinding); err != nil {
			return err
		}
	}
	if loop.Status != "validated" {
		return errors.New("model attempts exhausted without a mechanically validated binding")
	}
	if err := writeJSON(resolve(root, cfg.profilePath), loop.Final.Profile); err != nil {
		return err
	}
	fmt.Printf("validated model binding after %d attempt(s): capabilities %.2f, obligations %.2f\n",
		len(loop.Attempts), loop.Final.CapabilitySupport, loop.Final.ObligationSupport)
	return nil
}

func loadScenarios(root, prefix string) ([]autoonboard.ScenarioCandidate, error) {
	directory := filepath.Join(root, "scenarios")
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	var result []autoonboard.ScenarioCandidate
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || filepath.Ext(entry.Name()) != ".json" || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		var spec scenario.Spec
		if err := readStrictJSON(path, &spec); err != nil {
			return nil, fmt.Errorf("load scenario %s: %w", entry.Name(), err)
		}
		if err := spec.Validate(); err != nil {
			return nil, fmt.Errorf("validate scenario %s: %w", entry.Name(), err)
		}
		result = append(result, autoonboard.ScenarioCandidate{
			Path: filepath.ToSlash(filepath.Join("scenarios", entry.Name())), Spec: spec,
		})
	}
	return result, nil
}

func validateSecretFile(path string) error {
	return modelcommand.ValidateSecretFile(path)
}

func resolve(root, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, filepath.Clean(path))
}

func readStrictJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func writeJSON(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}
