package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/bindings"
	"github.com/SuzumiyaHaruki/consensus-atlas/families"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/engine"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/scenario"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

type runOutput struct {
	ProfileID         string                          `json:"profile_id"`
	PSSID             string                          `json:"pss_id,omitempty"`
	Scenario          string                          `json:"scenario"`
	Protocol          string                          `json:"protocol"`
	ReplayStable      bool                            `json:"replay_stable"`
	ReplayFingerprint string                          `json:"replay_fingerprint"`
	CanonicalKey      string                          `json:"canonical_key"`
	CanonicalTrace    any                             `json:"canonical_trace"`
	Oracle            oracle.Result                   `json:"oracle"`
	Coverage          coverage.Summary                `json:"coverage"`
	CoverageLedger    *coverage.Ledger                `json:"coverage_ledger"`
	CoverageDebt      []coverage.Debt                 `json:"coverage_debt"`
	Trace             []core.TraceRecord              `json:"trace"`
	PendingAtEnd      []core.Event                    `json:"pending_at_end,omitempty"`
	Capabilities      driver.Manifest                 `json:"capabilities"`
	ProtocolStates    *protocolstate.DiscoverySummary `json:"protocol_state_discovery,omitempty"`
}

func main() {
	profilePath := flag.String("profile", "profiles/toy-v1.json", "coverage profile JSON")
	scenarioPath := flag.String("scenario", "scenarios/toy-election.json", "scenario JSON")
	outPath := flag.String("out", "", "write the full result to this path; stdout when empty")
	flag.Parse()

	if err := run(*profilePath, *scenarioPath, *outPath); err != nil {
		fmt.Fprintln(os.Stderr, "runner:", err)
		os.Exit(1)
	}
}

func run(profilePath, scenarioPath, outPath string) error {
	var profile coverage.Profile
	// 读取 profile JSON 文件并解码到 profile 结构体中
	if err := readJSON(profilePath, &profile); err != nil {
		return err
	}
	if err := profile.Validate(); err != nil {
		return fmt.Errorf("validate profile: %w", err)
	}
	var spec scenario.Spec
	// 读取 scenario JSON 文件并解码到 spec 结构体中
	if err := readJSON(scenarioPath, &spec); err != nil {
		return err
	}
	if err := spec.Validate(); err != nil {
		return fmt.Errorf("validate scenario: %w", err)
	}

	// 执行 scenario 并获取执行结果和是否符合规范的标志
	first, firstConform, err := execute(profile, spec)
	if err != nil {
		return err
	}
	second, secondConform, err := execute(profile, spec)
	if err != nil {
		return fmt.Errorf("deterministic replay: %w", err)
	}
	firstHash, err := semantic.ExecutionFingerprint(first.trace)
	if err != nil {
		return err
	}
	secondHash, err := semantic.ExecutionFingerprint(second.trace)
	if err != nil {
		return err
	}
	replayStable := firstHash == secondHash
	canonicalKey, err := semantic.CanonicalFingerprint(first.trace)
	if err != nil {
		return err
	}
	protocolStates, err := discoverProtocolStates(profile, first.trace)
	if err != nil {
		return err
	}

	// 检查执行结果的完整性和一致性，并生成覆盖率评估
	oracleResult := oracle.Check(first.trace, oracle.TraceIntegrity{}, oracle.Agreement{})
	var ledgerOptions []coverage.LedgerOption
	if profile.PSSID != "" {
		matcher, matcherErr := families.CoverageMatcher(profile.PSSID)
		if matcherErr != nil {
			return matcherErr
		}
		ledgerOptions = append(ledgerOptions, coverage.WithSemanticMatcher(matcher))
	}
	ledger, err := coverage.NewLedger(profile, first.capabilities, ledgerOptions...)
	if err != nil {
		return fmt.Errorf("create coverage ledger: %w", err)
	}
	if err := ledger.AddRun(profile, coverage.RunEvidence{
		ID: "scenario:" + spec.Name, Scenario: spec.Name, Trace: first.trace,
		Oracle: oracleResult, ReplayStable: replayStable, Conformant: firstConform && secondConform,
		Manifest: first.capabilities,
	}); err != nil {
		return fmt.Errorf("record coverage evidence: %w", err)
	}
	summary := ledger.Summary()
	result := runOutput{
		ProfileID:         profile.ID,
		PSSID:             profile.PSSID,
		Scenario:          spec.Name,
		Protocol:          profile.Protocol,
		ReplayStable:      replayStable,
		ReplayFingerprint: firstHash,
		CanonicalKey:      canonicalKey,
		CanonicalTrace:    semantic.StructuralCanonicalize(first.trace),
		Oracle:            oracleResult,
		Coverage:          summary,
		CoverageLedger:    ledger,
		CoverageDebt:      ledger.Debts(),
		Trace:             first.trace,
		PendingAtEnd:      first.pending,
		Capabilities:      first.capabilities,
		ProtocolStates:    protocolStates,
	}

	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if outPath == "" {
		_, err = os.Stdout.Write(encoded)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(outPath, encoded, 0o644); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	fmt.Printf("wrote %s\nscore: %.2f, capability support: %.2f, high: %v, replay stable: %v, violations: %d\n",
		outPath, summary.Score, summary.CapabilitySupport, summary.High, replayStable, len(oracleResult.Violations))
	return nil
}

func discoverProtocolStates(profile coverage.Profile, trace []core.TraceRecord) (*protocolstate.DiscoverySummary, error) {
	if profile.PSSID == "" {
		return nil, nil
	}
	projector, err := families.Projector(profile.PSSID)
	if err != nil {
		return nil, err
	}
	summary, err := protocolstate.Discover(trace, projector)
	if err != nil {
		return nil, err
	}
	return &summary, nil
}

type execution struct {
	trace        []core.TraceRecord
	pending      []core.Event
	capabilities driver.Manifest
}

func execute(profile coverage.Profile, spec scenario.Spec) (execution, bool, error) {
	protocolAdapter, capabilities, err := bindings.New(profile)
	if err != nil {
		return execution{}, false, err
	}
	conform := protocolAdapter.CheckConformance() == nil
	e := engine.New(protocolAdapter)
	if err := scenario.Run(context.Background(), e, spec); err != nil {
		return execution{}, conform, err
	}
	if err := protocolAdapter.CheckConformance(); err != nil {
		conform = false
	}
	return execution{trace: e.Trace(), pending: e.Pending(), capabilities: capabilities}, conform, nil
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
