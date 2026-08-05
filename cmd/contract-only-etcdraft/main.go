package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/contractonly"
)

func main() {
	repo := flag.String("repo", ".", "ConsensusAtlas repository root")
	contract := flag.String("contract", "contracts/etcdraft-v1.json", "only protocol-semantic input")
	keyFile := flag.String("key-file", "key.txt", "private DeepSeek API key file")
	model := flag.String("model", "deepseek-v4-flash", "DeepSeek model ID")
	endpoint := flag.String("endpoint", "https://api.deepseek.com/chat/completions", "official DeepSeek endpoint")
	maxAttempts := flag.Int("max-attempts", 5, "maximum generate/build/validate attempts")
	timeout := flag.Duration("timeout", 30*time.Minute, "complete experiment deadline")
	outputRoot := flag.String("output-root", "artifacts/contract-only", "new experiment parent directory")
	experimentID := flag.String("experiment-id", "", "unique experiment id; defaults to a UTC timestamp")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err := run(ctx, cliConfig{
		repo: *repo, contract: *contract, keyFile: *keyFile,
		model: *model, endpoint: *endpoint, maxAttempts: *maxAttempts,
		outputRoot: *outputRoot, experimentID: *experimentID,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "contract-only-etcdraft:", err)
		os.Exit(1)
	}
}

type cliConfig struct {
	repo, contract, keyFile, model, endpoint string
	outputRoot, experimentID                 string
	maxAttempts                              int
}

func run(ctx context.Context, config cliConfig) error {
	if config.maxAttempts < 1 || config.maxAttempts > 10 {
		return errors.New("max-attempts must be between 1 and 10")
	}
	repoRoot, err := filepath.Abs(config.repo)
	if err != nil {
		return err
	}
	keyPath, err := filepath.Abs(config.keyFile)
	if err != nil {
		return err
	}
	if err := validateSecretFile(keyPath); err != nil {
		return err
	}
	contractPath := resolve(repoRoot, config.contract)
	outputRoot := resolve(repoRoot, config.outputRoot)
	if err := os.MkdirAll(outputRoot, 0o755); err != nil {
		return err
	}
	id := config.experimentID
	if id == "" {
		id = "etcdraft-" + time.Now().UTC().Format("20060102T150405Z")
	}
	if !validExperimentID(id) {
		return errors.New("experiment-id may contain only letters, digits, dot, dash, and underscore")
	}
	experimentDir := filepath.Join(outputRoot, id)

	goBinary, err := exec.LookPath("go")
	if err != nil {
		return err
	}
	moduleOutput, err := exec.CommandContext(ctx, goBinary, "env", "GOMODCACHE").Output()
	if err != nil {
		return fmt.Errorf("resolve Go module cache: %w", err)
	}
	moduleCache := strings.TrimSpace(string(moduleOutput))
	driverAPI, target, err := contractonly.LoadContexts(repoRoot, moduleCache)
	if err != nil {
		return err
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		return err
	}
	generator := &contractonly.CommandGenerator{
		Path: python,
		Args: []string{filepath.Join(repoRoot, "agents", "deepseek_contractonly.py")},
		Dir:  repoRoot,
		Env: []string{
			"CONSENSUS_ATLAS_DEEPSEEK_KEY_FILE=" + keyPath,
			"CONSENSUS_ATLAS_DEEPSEEK_MODEL=" + config.model,
			"CONSENSUS_ATLAS_DEEPSEEK_ENDPOINT=" + config.endpoint,
		},
	}
	report, err := contractonly.Coordinate(ctx, contractonly.Config{
		ID: id, RepoRoot: repoRoot, ContractPath: contractPath,
		ExperimentDir: experimentDir, ModuleCache: moduleCache,
		MaxAttempts: config.maxAttempts, DriverAPI: driverAPI, Target: target,
	}, generator)
	if err != nil {
		return err
	}
	fmt.Printf("Contract-only experiment %s: %s after %d attempt(s)\n", id, report.Status, len(report.Attempts))
	if report.Status != "validated" {
		return fmt.Errorf("experiment exhausted without a validated integration; inspect %s", filepath.Join(experimentDir, "report.json"))
	}
	if report.Final != nil {
		fmt.Printf("capabilities %.2f, obligations %.2f\n", report.Final.CapabilitySupport, report.Final.ObligationSupport)
	}
	return nil
}

func resolve(root, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, filepath.Clean(path))
}

func validateSecretFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("DeepSeek key path must be a regular file, not a symlink")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New("DeepSeek key file permissions must not grant group or other access")
	}
	return nil
}

func validExperimentID(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("._-", character) {
			continue
		}
		return false
	}
	return true
}
