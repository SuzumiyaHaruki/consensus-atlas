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

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/contractonly"
)

func main() {
	repo := flag.String("repo", ".", "ConsensusAtlas repository root")
	contract := flag.String("contract", "contracts/etcdraft-v1.json", "protocol contract")
	proposalPath := flag.String("proposal", "", "saved Contract-only proposal JSON")
	output := flag.String("output", "", "new replay workspace directory")
	timeout := flag.Duration("timeout", 2*time.Minute, "sandbox deadline")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err := run(ctx, *repo, *contract, *proposalPath, *output); err != nil {
		fmt.Fprintln(os.Stderr, "contract-only-replay:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, repo, contract, proposalPath, output string) error {
	if proposalPath == "" || output == "" {
		return errors.New("proposal and output are required")
	}
	repoRoot, err := filepath.Abs(repo)
	if err != nil {
		return err
	}
	proposal, err := loadProposal(proposalPath)
	if err != nil {
		return err
	}
	findings, digest := contractonly.ValidateProposal(proposal)
	if len(findings) != 0 {
		return fmt.Errorf("proposal %s has %d preflight finding(s): %+v", digest, len(findings), findings)
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return err
	}
	contractPath := contract
	if !filepath.IsAbs(contractPath) {
		contractPath = filepath.Join(repoRoot, filepath.Clean(contractPath))
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		return err
	}
	moduleOutput, err := exec.CommandContext(ctx, goBinary, "env", "GOMODCACHE").Output()
	if err != nil {
		return err
	}
	if err := contractonly.MaterializeWorkspace(output, repoRoot, contractPath, proposal); err != nil {
		return err
	}
	build, report, err := contractonly.RunSandbox(ctx, output, strings.TrimSpace(string(moduleOutput)))
	if err != nil {
		return err
	}
	result := struct {
		Version        int    `json:"version"`
		ProposalDigest string `json:"proposal_digest"`
		Build          any    `json:"build"`
		Validation     any    `json:"validation,omitempty"`
	}{Version: 1, ProposalDigest: digest, Build: build, Validation: report}
	if err := writeJSON(filepath.Join(output, "replay-result.json"), result); err != nil {
		return err
	}
	if !build.Succeeded {
		return fmt.Errorf("sandbox build failed: %s", build.Diagnostics)
	}
	if report == nil {
		return errors.New("sandbox produced no validation report")
	}
	fmt.Printf("replayed proposal %s: %s\n", digest, report.Status)
	return nil
}

func loadProposal(path string) (*contractonly.Proposal, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var proposal contractonly.Proposal
	if err := decoder.Decode(&proposal); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("proposal contains multiple JSON values")
		}
		return nil, err
	}
	return &proposal, nil
}

func writeJSON(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}
