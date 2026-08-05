package contractonly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/autoonboard"
)

var frameworkDirectories = []string{
	"families/raft",
	"internal/adapter",
	"internal/autoonboard",
	"internal/core",
	"internal/coverage",
	"internal/driver",
	"internal/engine",
	"internal/host",
	"internal/oracle",
	"internal/protocolcontract",
	"internal/protocolstate",
	"internal/scenario",
	"internal/semantic",
}

const generatedModule = `module github.com/SuzumiyaHaruki/consensus-atlas/contractonlyrun

go 1.24

require (
	github.com/SuzumiyaHaruki/consensus-atlas v0.0.0
	go.etcd.io/raft/v3 v3.6.0
)

replace github.com/SuzumiyaHaruki/consensus-atlas => ./framework
`

const evaluatorSource = `package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/contractonlyrun/integration"
	"github.com/SuzumiyaHaruki/consensus-atlas/families/raft"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/adapter"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/autoonboard"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolcontract"
)

type pssCheckedAdapter struct { adapter.Adapter }

type evidenceSnapshot struct {
	Driver struct {
		Nodes map[string]evidenceNode ` + "`json:\"nodes\"`" + `
	} ` + "`json:\"driver\"`" + `
}

type evidenceNode struct {
	Running bool ` + "`json:\"running\"`" + `
	Role string ` + "`json:\"role\"`" + `
	Applied uint64 ` + "`json:\"applied\"`" + `
	DurableLog []evidenceEntry ` + "`json:\"durable_log\"`" + `
}

type evidenceEntry struct {
	Index uint64 ` + "`json:\"index\"`" + `
	Type string ` + "`json:\"type\"`" + `
	ValueDigest string ` + "`json:\"value_digest\"`" + `
}

var contractOwnedLabels = map[string]bool{
	"transition:follower->candidate": true,
	"transition:candidate->leader": true,
	"timeout:natural-election": true,
	"message:released": true,
	"message:delivered": true,
	"host:persist": true,
	"host:sync": true,
	"host:apply": true,
	"host:exact-ready-barrier": true,
	"fault:crash": true,
	"fault:restart": true,
	"commit": true,
}

func decodeEvidence(snapshot any) (evidenceSnapshot, error) {
	encoded, err := json.Marshal(snapshot)
	if err != nil { return evidenceSnapshot{}, err }
	var evidence evidenceSnapshot
	if err := json.Unmarshal(encoded, &evidence); err != nil { return evidenceSnapshot{}, err }
	if evidence.Driver.Nodes == nil { return evidenceSnapshot{}, fmt.Errorf("snapshot has no driver.nodes evidence") }
	return evidence, nil
}

func (a pssCheckedAdapter) Apply(ctx context.Context, event core.Event) (core.ApplyResult, error) {
	before, beforeErr := decodeEvidence(a.Adapter.Snapshot())
	if beforeErr != nil { return core.ApplyResult{}, beforeErr }
	result, err := a.Adapter.Apply(ctx, event)
	if err != nil { return result, err }
	if _, _, projectErr := raft.Project(a.Adapter.Snapshot()); projectErr != nil {
		return core.ApplyResult{}, fmt.Errorf("raft PSS evidence after %s: %w", event.Kind, projectErr)
	}
	after, afterErr := decodeEvidence(a.Adapter.Snapshot())
	if afterErr != nil { return core.ApplyResult{}, afterErr }
	filtered := result.Observations[:0]
	for _, observation := range result.Observations {
		if !contractOwnedLabels[observation.Label] { filtered = append(filtered, observation) }
	}
	result.Observations = filtered
	beforeNode, afterNode := before.Driver.Nodes[event.Target], after.Driver.Nodes[event.Target]
	add := func(kind, label, value string, evidence map[string]string) {
		result.Observations = append(result.Observations, core.Observation{
			Kind: kind, Label: label, Node: event.Target, Value: value, Evidence: evidence,
		})
	}
	switch event.Kind {
	case core.EventCampaign:
		if beforeNode.Role == "follower" && afterNode.Role == "candidate" {
			add("state", "transition:follower->candidate", "", nil)
		}
	case core.EventTimeout:
		if beforeNode.Role == "follower" && afterNode.Role == "candidate" {
			add("timeout", "timeout:natural-election", "", nil)
		}
	case core.EventMessage:
		add("transport", "message:delivered", "", nil)
		if beforeNode.Role == "candidate" && afterNode.Role == "leader" {
			add("state", "transition:candidate->leader", "", nil)
		}
	case core.EventPersist:
		add("persistence", "host:persist", "", nil)
	case core.EventSync:
		add("persistence", "host:sync", "", nil)
	case core.EventEmit:
		add("transport", "message:released", "", nil)
	case core.EventApply:
		if afterNode.Applied > beforeNode.Applied {
			add("application", "host:apply", "", nil)
			for _, entry := range afterNode.DurableLog {
				if entry.Index > beforeNode.Applied && entry.Index <= afterNode.Applied &&
					entry.Type == "EntryNormal" && entry.ValueDigest != "" {
					add("commit", "commit", entry.ValueDigest, map[string]string{"index": strconv.FormatUint(entry.Index, 10)})
				}
			}
		}
	case core.EventCrash:
		if beforeNode.Running && !afterNode.Running { add("fault", "fault:crash", "", nil) }
	case core.EventRestart:
		if !beforeNode.Running && afterNode.Running { add("fault", "fault:restart", "", nil) }
	}
	return result, nil
}

func (a pssCheckedAdapter) CheckConformance() error {
	if err := a.Adapter.CheckConformance(); err != nil { return err }
	_, _, err := raft.Project(a.Adapter.Snapshot())
	if err != nil { return fmt.Errorf("raft PSS evidence: %w", err) }
	return nil
}

func generatedFactory(profile coverage.Profile) (adapter.Adapter, driver.Manifest, error) {
	runtimeAdapter, manifest, err := integration.New(profile)
	if err != nil { return nil, driver.Manifest{}, err }
	return pssCheckedAdapter{Adapter: runtimeAdapter}, manifest, nil
}

func main() {
	contract, err := protocolcontract.Load("contract.json")
	if err != nil { fail(err) }
	binding, err := autoonboard.Load("binding.json")
	if err != nil { fail(err) }
	report, err := autoonboard.Validate(context.Background(), ".", contract, binding, generatedFactory)
	if err != nil { fail(err) }
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil { fail(err) }
	if err := os.WriteFile("validation-report.json", append(encoded, '\n'), 0o644); err != nil { fail(err) }
	fmt.Printf("contract-only validation status: %s\n", report.Status)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
`

func MaterializeWorkspace(attemptDir, repoRoot, contractPath string, proposal *Proposal) error {
	findings, _ := ValidateProposal(proposal)
	if len(findings) != 0 {
		return errors.New("cannot materialize an invalid proposal")
	}
	if err := os.Mkdir(attemptDir, 0o755); err != nil {
		return err
	}
	framework := filepath.Join(attemptDir, "framework")
	if err := os.MkdirAll(framework, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		if err := copyRegular(filepath.Join(repoRoot, name), filepath.Join(framework, name)); err != nil {
			return err
		}
	}
	for _, directory := range frameworkDirectories {
		if err := copyGoDirectory(filepath.Join(repoRoot, directory), filepath.Join(framework, directory)); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(attemptDir, "go.mod"), []byte(generatedModule), 0o644); err != nil {
		return err
	}
	if err := copyRegular(filepath.Join(repoRoot, "go.sum"), filepath.Join(attemptDir, "go.sum")); err != nil {
		return err
	}
	if err := copyRegular(contractPath, filepath.Join(attemptDir, "contract.json")); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(attemptDir, "binding.json"), proposal.Binding); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(attemptDir, "proposal.json"), proposal); err != nil {
		return err
	}
	for _, file := range proposal.Files {
		target := filepath.Join(attemptDir, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(file.Content), 0o644); err != nil {
			return err
		}
	}
	evaluator := filepath.Join(attemptDir, "cmd", "evaluate", "main.go")
	if err := os.MkdirAll(filepath.Dir(evaluator), 0o755); err != nil {
		return err
	}
	return os.WriteFile(evaluator, []byte(evaluatorSource), 0o644)
}

func RunSandbox(ctx context.Context, attemptDir, moduleCache string) (BuildResult, *autoonboard.Report, error) {
	started := time.Now()
	goBinary, err := exec.LookPath("go")
	if err != nil {
		return BuildResult{}, nil, err
	}
	goRoot := filepath.Clean(filepath.Join(filepath.Dir(goBinary), ".."))
	attemptDir, err = filepath.Abs(attemptDir)
	if err != nil {
		return BuildResult{}, nil, err
	}
	moduleCache, err = filepath.Abs(moduleCache)
	if err != nil {
		return BuildResult{}, nil, err
	}
	baseArguments := []string{
		"--die-with-parent", "--unshare-all", "--new-session",
		"--clearenv",
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp",
		"--ro-bind", "/usr", "/usr",
		"--ro-bind", goRoot, "/goroot",
		"--ro-bind", "/lib", "/lib",
		"--ro-bind", "/lib64", "/lib64",
		"--ro-bind", moduleCache, "/gomodcache",
		"--bind", attemptDir, "/workspace",
		"--chdir", "/workspace",
		"--setenv", "HOME", "/workspace/.home",
		"--setenv", "PATH", "/goroot/bin:/usr/bin:/bin",
		"--setenv", "GOROOT", "/goroot",
		"--setenv", "GOMODCACHE", "/gomodcache",
		"--setenv", "GOCACHE", "/workspace/.cache/go-build",
		"--setenv", "GOPATH", "/workspace/.gopath",
		"--setenv", "GOPROXY", "off",
		"--setenv", "GOSUMDB", "off",
		"--setenv", "GOWORK", "off",
		"--setenv", "GOTOOLCHAIN", "local",
		"--setenv", "GOTELEMETRY", "off",
		"--setenv", "CGO_ENABLED", "0",
	}
	output := &boundedBuffer{limit: 128 << 10}
	run := func(goArguments ...string) error {
		arguments := append([]string(nil), baseArguments...)
		arguments = append(arguments, "/goroot/bin/go")
		arguments = append(arguments, goArguments...)
		command := exec.CommandContext(ctx, "bwrap", arguments...)
		command.Stdout, command.Stderr = output, output
		return command.Run()
	}
	// Generated imports determine the indirect dependency closure. Tidy runs
	// with networking disabled and a read-only module cache, then the actual
	// build is forced back to readonly mode so it cannot change that closure.
	runErr := run("mod", "tidy")
	if runErr == nil && !output.exceeded {
		runErr = run("run", "-mod=readonly", "-buildvcs=false", "./cmd/evaluate")
	}
	build := BuildResult{
		Succeeded:      runErr == nil && !output.exceeded,
		DurationMillis: time.Since(started).Milliseconds(),
		Diagnostics:    strings.TrimSpace(output.String()),
	}
	if build.Succeeded {
		// Successful evaluator stdout may contain SUT logging with wall-clock
		// timestamps. It is not feedback and must not pollute the next prompt.
		build.Diagnostics = ""
	}
	if output.exceeded {
		build.Diagnostics += "\n[sandbox output exceeded 128 KiB]"
	}
	if runErr != nil || output.exceeded {
		return build, nil, nil
	}
	data, err := os.ReadFile(filepath.Join(attemptDir, "validation-report.json"))
	if err != nil {
		return build, nil, err
	}
	var report autoonboard.Report
	if err := json.Unmarshal(data, &report); err != nil {
		return build, nil, err
	}
	return build, &report, nil
}

func copyGoDirectory(source, target string) error {
	return filepath.WalkDir(source, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("framework source contains symlink: %s", current)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		return copyRegular(current, filepath.Join(target, relative))
	})
}

func copyRegular(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source is not a regular file: %s", source)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o644)
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
