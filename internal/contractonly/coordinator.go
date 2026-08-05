package contractonly

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolcontract"
)

type Config struct {
	ID            string
	RepoRoot      string
	ContractPath  string
	ExperimentDir string
	ModuleCache   string
	MaxAttempts   int
	DriverAPI     []SourceFile
	Target        Target
}

func Coordinate(ctx context.Context, config Config, generator Generator) (ExperimentReport, error) {
	if generator == nil || config.ID == "" || config.RepoRoot == "" || config.ContractPath == "" ||
		config.ExperimentDir == "" || config.ModuleCache == "" || config.MaxAttempts < 1 {
		return ExperimentReport{}, errors.New("complete contract-only config, generator, and a positive attempt limit are required")
	}
	contract, err := protocolcontract.Load(config.ContractPath)
	if err != nil {
		return ExperimentReport{}, err
	}
	if err := contract.Validate(); err != nil {
		return ExperimentReport{}, err
	}
	contractDigest, err := protocolcontract.Digest(contract)
	if err != nil {
		return ExperimentReport{}, err
	}
	if err := os.Mkdir(config.ExperimentDir, 0o755); err != nil {
		return ExperimentReport{}, fmt.Errorf("create new experiment directory: %w", err)
	}
	report := ExperimentReport{
		Version: Version, ID: config.ID, Status: "exhausted",
		ContractID: contract.ID, ContractDigest: contractDigest,
		DriverAPIDigest: sourceDigest(config.DriverAPI),
		TargetModule:    config.Target.Module, TargetVersion: config.Target.Version,
		TargetSourceDigest: sourceDigest(config.Target.Sources),
	}
	var previousProposal *Proposal
	var previousResult *AttemptResult
	for number := 1; number <= config.MaxAttempts; number++ {
		proposal, generateErr := generator.Generate(ctx, GenerationRequest{
			Contract: contract, ContractDigest: contractDigest,
			DriverAPI: config.DriverAPI, Target: config.Target, Attempt: number,
			PreviousProposal: cloneProposal(previousProposal), PreviousResult: previousResult,
		})
		if generateErr != nil {
			return report, generateErr
		}
		attempt := AttemptResult{Number: number, Status: "proposal_invalid"}
		if provider, ok := generator.(AuditProvider); ok {
			attempt.Generation = provider.LastGenerationAudit()
		}
		if proposal != nil {
			attempt.ProposalID = proposal.ID
		}
		attempt.Findings, attempt.ProposalDigest = ValidateProposal(proposal)
		attempt.GeneratedFilesDigest, attempt.BindingDigest = ComponentDigests(proposal)
		attemptName := fmt.Sprintf("attempt-%02d", number)
		attemptDir := filepath.Join(config.ExperimentDir, attemptName)
		attempt.AttemptDir = attemptName
		if len(attempt.Findings) > 0 {
			if err := os.Mkdir(attemptDir, 0o755); err != nil {
				return report, err
			}
			if err := writeJSON(filepath.Join(attemptDir, "proposal.json"), proposal); err != nil {
				return report, err
			}
		} else {
			if err := MaterializeWorkspace(attemptDir, config.RepoRoot, config.ContractPath, proposal); err != nil {
				return report, err
			}
			attempt.Build, attempt.Validation, err = RunSandbox(ctx, attemptDir, config.ModuleCache)
			if err != nil {
				return report, err
			}
			switch {
			case !attempt.Build.Succeeded:
				attempt.Status = "build_failed"
				attempt.Findings = append(attempt.Findings, Finding{
					Stage: "build", Code: "build.failed", Message: attempt.Build.Diagnostics, Actionable: true,
				})
			case attempt.Validation == nil:
				attempt.Status = "validation_failed"
				attempt.Findings = append(attempt.Findings, Finding{
					Stage: "validation", Code: "validation.missing", Message: "sandbox produced no validation report", Actionable: true,
				})
			case attempt.Validation.Status != "validated":
				attempt.Status = "validation_invalid"
				for _, finding := range attempt.Validation.Findings {
					attempt.Findings = append(attempt.Findings, Finding{
						Stage: "validation", Code: finding.Code, Message: finding.Message, Actionable: finding.Actionable,
					})
				}
			default:
				attempt.Status = "validated"
			}
		}
		report.Attempts = append(report.Attempts, attempt)
		report.FinalProposal = cloneProposal(proposal)
		report.Final = attempt.Validation
		if err := writeJSON(filepath.Join(attemptDir, "attempt-result.json"), attempt); err != nil {
			return report, err
		}
		if attempt.Status == "validated" {
			report.Status = "validated"
			if err := writeJSON(filepath.Join(config.ExperimentDir, "report.json"), report); err != nil {
				return report, err
			}
			return report, nil
		}
		if err := writeJSON(filepath.Join(config.ExperimentDir, "report.json"), report); err != nil {
			return report, err
		}
		previousProposal = cloneProposal(proposal)
		resultCopy := attempt
		resultCopy.AttemptDir = ""
		previousResult = &resultCopy
	}
	return report, nil
}
