package contractonly_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/autoonboard"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/contractonly"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolcontract"
)

func TestValidateProposalRejectsUnsafeGeneratedCode(t *testing.T) {
	proposal := fixtureProposal(t)
	proposal.Files[0].Content = "package integration\nimport \"os\"\nvar _ = os.Args\n"
	findings, _ := contractonly.ValidateProposal(proposal)
	if !hasFinding(findings, "go.import") {
		t.Fatalf("unsafe import was not rejected: %+v", findings)
	}
	proposal = fixtureProposal(t)
	proposal.Files[0].Path = "../driver.go"
	findings, _ = contractonly.ValidateProposal(proposal)
	if !hasFinding(findings, "file.path") {
		t.Fatalf("escaping path was not rejected: %+v", findings)
	}
}

func TestComponentDigestsDistinguishCodeFromIdentityOnlyRepair(t *testing.T) {
	first := fixtureProposal(t)
	filesBefore, bindingBefore := contractonly.ComponentDigests(first)
	first.Binding.ID = "renamed-only"
	filesAfter, bindingAfter := contractonly.ComponentDigests(first)
	if filesBefore != filesAfter || bindingBefore == bindingAfter {
		t.Fatalf("component digests did not isolate identity-only change")
	}
}

func TestSourceContextExcludesReferenceIntegrationArtifacts(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	moduleCacheOutput, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	api, target, err := contractonly.LoadContexts(repoRoot, strings.TrimSpace(string(moduleCacheOutput)))
	if err != nil {
		t.Fatal(err)
	}
	if len(api) == 0 || len(target.Sources) == 0 {
		t.Fatal("source context is empty")
	}
	for _, source := range append(api, target.Sources...) {
		for _, forbidden := range []string{"drivers/etcdraft", "onboarding/etcdraft", "scenarios/etcdraft", "bindings/bindings.go"} {
			if strings.Contains(source.Path, forbidden) {
				t.Fatalf("reference artifact leaked through source path %q", source.Path)
			}
		}
	}
}

func TestSandboxBuildsGeneratedModuleAndReturnsMechanicalReport(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bubblewrap is unavailable")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	moduleCacheOutput, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	moduleCache := strings.TrimSpace(string(moduleCacheOutput))
	attemptDir := filepath.Join(t.TempDir(), "attempt-01")
	proposal := fixtureProposal(t)
	if findings, _ := contractonly.ValidateProposal(proposal); len(findings) != 0 {
		t.Fatalf("fixture proposal is invalid: %+v", findings)
	}
	if err := contractonly.MaterializeWorkspace(
		attemptDir, repoRoot, filepath.Join(repoRoot, "contracts", "etcdraft-v1.json"), proposal,
	); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	build, report, err := contractonly.RunSandbox(ctx, attemptDir, moduleCache)
	if err != nil {
		t.Fatal(err)
	}
	if !build.Succeeded {
		t.Fatalf("sandbox build failed: %s", build.Diagnostics)
	}
	if report == nil || report.Status != "invalid" {
		t.Fatalf("expected an invalid mechanical report, got %+v", report)
	}
	foundFactory := false
	for _, finding := range report.Findings {
		foundFactory = foundFactory || finding.Code == "runtime.factory"
	}
	if !foundFactory {
		t.Fatalf("runtime factory failure was not reported: %+v", report.Findings)
	}
}

func fixtureProposal(t *testing.T) *contractonly.Proposal {
	t.Helper()
	root := filepath.Join("..", "..")
	contract, err := protocolcontract.Load(filepath.Join(root, "contracts", "etcdraft-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	digest, err := protocolcontract.Digest(contract)
	if err != nil {
		t.Fatal(err)
	}
	return &contractonly.Proposal{
		Version: 1,
		ID:      "sandbox-fixture",
		Files: []contractonly.GeneratedFile{
			{
				Path: "integration/driver.go",
				Content: `package integration

import (
	"errors"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/adapter"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
)

func New(coverage.Profile) (adapter.Adapter, driver.Manifest, error) {
	return nil, driver.Manifest{}, errors.New("fixture driver is intentionally unavailable")
}
`,
			},
			{
				Path:    "scenarios/smoke.json",
				Content: `{"version":1,"name":"smoke","steps":[{"op":"inject","kind":"start","target":"n1"}]}`,
			},
		},
		Binding: autoonboard.Binding{
			Version: 1, ID: "sandbox-fixture", ContractID: contract.ID,
			ContractDigest: digest, Protocol: contract.Protocol, Driver: "fixture",
			Capabilities: []autoonboard.CapabilityBinding{},
			Operations:   []autoonboard.OperationBinding{},
			Witnesses: []autoonboard.Witness{{
				ID: "smoke", Scenario: "scenarios/smoke.json", Covers: []string{"transition.campaign"},
			}},
		},
	}
}

func hasFinding(findings []contractonly.Finding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
