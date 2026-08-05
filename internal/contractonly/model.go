// Package contractonly runs isolated, model-generated protocol integrations.
// The model receives protocol knowledge and public implementation sources, but
// never a checked-in Driver, Binding, witness, Profile, or validation answer.
package contractonly

import (
	"context"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/autoonboard"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolcontract"
)

const Version = 1

type SourceFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type Target struct {
	Module  string       `json:"module"`
	Version string       `json:"version"`
	Sources []SourceFile `json:"sources"`
}

type GeneratedFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Proposal is a full generated snapshot. Patches are deliberately avoided so
// every attempt is independently auditable and can be rebuilt from its JSON.
type Proposal struct {
	Version int                 `json:"version"`
	ID      string              `json:"id"`
	Files   []GeneratedFile     `json:"files"`
	Binding autoonboard.Binding `json:"binding"`
}

type GenerationRequest struct {
	Contract         *protocolcontract.Contract `json:"contract"`
	ContractDigest   string                     `json:"contract_digest"`
	DriverAPI        []SourceFile               `json:"driver_api"`
	Target           Target                     `json:"target"`
	Attempt          int                        `json:"attempt"`
	PreviousProposal *Proposal                  `json:"previous_proposal,omitempty"`
	PreviousResult   *AttemptResult             `json:"previous_result,omitempty"`
}

type Generator interface {
	Generate(context.Context, GenerationRequest) (*Proposal, error)
}

type Finding struct {
	Stage      string `json:"stage"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Actionable bool   `json:"actionable"`
}

type BuildResult struct {
	Succeeded      bool   `json:"succeeded"`
	DurationMillis int64  `json:"duration_millis"`
	Diagnostics    string `json:"diagnostics,omitempty"`
}

type AttemptResult struct {
	Number               int                         `json:"number"`
	Status               string                      `json:"status"`
	ProposalID           string                      `json:"proposal_id,omitempty"`
	ProposalDigest       string                      `json:"proposal_digest,omitempty"`
	GeneratedFilesDigest string                      `json:"generated_files_digest,omitempty"`
	BindingDigest        string                      `json:"binding_digest,omitempty"`
	Generation           autoonboard.GenerationAudit `json:"generation"`
	Findings             []Finding                   `json:"findings,omitempty"`
	Build                BuildResult                 `json:"build"`
	Validation           *autoonboard.Report         `json:"validation,omitempty"`
	AttemptDir           string                      `json:"attempt_dir,omitempty"`
}

type ExperimentReport struct {
	Version            int                 `json:"version"`
	ID                 string              `json:"id"`
	Status             string              `json:"status"`
	ContractID         string              `json:"contract_id"`
	ContractDigest     string              `json:"contract_digest"`
	DriverAPIDigest    string              `json:"driver_api_digest"`
	TargetModule       string              `json:"target_module"`
	TargetVersion      string              `json:"target_version"`
	TargetSourceDigest string              `json:"target_source_digest"`
	Attempts           []AttemptResult     `json:"attempts"`
	FinalProposal      *Proposal           `json:"final_proposal,omitempty"`
	Final              *autoonboard.Report `json:"final_validation,omitempty"`
}

type AuditProvider interface {
	LastGenerationAudit() autoonboard.GenerationAudit
}

func cloneProposal(proposal *Proposal) *Proposal {
	if proposal == nil {
		return nil
	}
	copy := *proposal
	copy.Files = append([]GeneratedFile(nil), proposal.Files...)
	copy.Binding = *autoonboardCloneBinding(&proposal.Binding)
	return &copy
}

// Keep Binding cloning local so the contract-only package cannot accidentally
// share mutable proposal state with a model-backed generator.
func autoonboardCloneBinding(binding *autoonboard.Binding) *autoonboard.Binding {
	if binding == nil {
		return nil
	}
	copy := *binding
	copy.Capabilities = append([]autoonboard.CapabilityBinding(nil), binding.Capabilities...)
	copy.Operations = append([]autoonboard.OperationBinding(nil), binding.Operations...)
	copy.Witnesses = make([]autoonboard.Witness, len(binding.Witnesses))
	for index, witness := range binding.Witnesses {
		copy.Witnesses[index] = witness
		copy.Witnesses[index].Covers = append([]string(nil), witness.Covers...)
		copy.Witnesses[index].ValidatesCapabilities = append([]string(nil), witness.ValidatesCapabilities...)
	}
	return &copy
}
