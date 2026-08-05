package autoonboard

import (
	"context"
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolcontract"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/scenario"
)

type ScenarioCandidate struct {
	Path string        `json:"path"`
	Spec scenario.Spec `json:"spec"`
}

type GenerationContext struct {
	Driver    driver.Manifest     `json:"driver"`
	Scenarios []ScenarioCandidate `json:"scenarios"`
}

// GenerationRequest is the only feedback an onboarding agent needs: immutable
// protocol knowledge plus deterministic findings from its previous proposal.
type GenerationRequest struct {
	Contract         *protocolcontract.Contract `json:"contract"`
	ContractDigest   string                     `json:"contract_digest"`
	Context          GenerationContext          `json:"context"`
	Attempt          int                        `json:"attempt"`
	PreviousBinding  *Binding                   `json:"previous_binding,omitempty"`
	PreviousFindings []Finding                  `json:"previous_findings,omitempty"`
	PreviousReport   *Report                    `json:"previous_report,omitempty"`
}

type Generator interface {
	Generate(context.Context, GenerationRequest) (*Binding, error)
}

type Validator func(context.Context, *Binding) (Report, error)

type Attempt struct {
	Number     int             `json:"number"`
	BindingID  string          `json:"binding_id,omitempty"`
	Generation GenerationAudit `json:"generation"`
	Report     Report          `json:"report"`
}

type LoopReport struct {
	Status       string    `json:"status"`
	Attempts     []Attempt `json:"attempts"`
	FinalBinding *Binding  `json:"final_binding,omitempty"`
	Final        Report    `json:"final"`
}

type GenerationAudit struct {
	Provider              string   `json:"provider"`
	Model                 string   `json:"model"`
	Endpoint              string   `json:"endpoint,omitempty"`
	PromptDigest          string   `json:"prompt_digest,omitempty"`
	ThinkingMode          string   `json:"thinking_mode,omitempty"`
	Temperature           *float64 `json:"temperature,omitempty"`
	MaxTokens             int      `json:"max_tokens,omitempty"`
	ResponseID            string   `json:"response_id,omitempty"`
	SystemFingerprint     string   `json:"system_fingerprint,omitempty"`
	FinishReason          string   `json:"finish_reason,omitempty"`
	PromptTokens          int      `json:"prompt_tokens,omitempty"`
	PromptCacheHitTokens  int      `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens int      `json:"prompt_cache_miss_tokens,omitempty"`
	CompletionTokens      int      `json:"completion_tokens,omitempty"`
	ReasoningTokens       int      `json:"reasoning_tokens,omitempty"`
	TotalTokens           int      `json:"total_tokens,omitempty"`
	DurationMillis        int64    `json:"duration_millis,omitempty"`
	RequestDigest         string   `json:"request_digest,omitempty"`
	ResponseDigest        string   `json:"response_digest,omitempty"`
}

type AuditProvider interface {
	LastGenerationAudit() GenerationAudit
}

// Coordinate is deterministic orchestration, not an agent. It accepts only a
// mechanically validated proposal and feeds exact findings into the next
// generation attempt.
func Coordinate(ctx context.Context, contract *protocolcontract.Contract, generationContext GenerationContext, maxAttempts int, generator Generator, validator Validator) (LoopReport, error) {
	if contract == nil || generator == nil || validator == nil || maxAttempts < 1 {
		return LoopReport{}, errors.New("contract, generator, validator, and a positive attempt limit are required")
	}
	digest, err := protocolcontract.Digest(contract)
	if err != nil {
		return LoopReport{}, err
	}
	loop := LoopReport{Status: "exhausted"}
	var previousBinding *Binding
	var previousFindings []Finding
	var previousReport *Report
	for number := 1; number <= maxAttempts; number++ {
		proposal, err := generator.Generate(ctx, GenerationRequest{
			Contract: contract, ContractDigest: digest, Context: generationContext, Attempt: number,
			PreviousBinding:  cloneBinding(previousBinding),
			PreviousFindings: append([]Finding(nil), previousFindings...),
			PreviousReport:   previousReport,
		})
		if err != nil {
			return loop, err
		}
		report, err := validator(ctx, proposal)
		if err != nil {
			return loop, err
		}
		attempt := Attempt{Number: number, Report: report}
		if provider, ok := generator.(AuditProvider); ok {
			attempt.Generation = provider.LastGenerationAudit()
		}
		if proposal != nil {
			attempt.BindingID = proposal.ID
		}
		loop.Attempts = append(loop.Attempts, attempt)
		loop.Final = report
		loop.FinalBinding = cloneBinding(proposal)
		if report.Status == "validated" {
			loop.Status = "validated"
			return loop, nil
		}
		previousBinding = cloneBinding(proposal)
		previousFindings = append([]Finding(nil), report.Findings...)
		reportCopy := report
		previousReport = &reportCopy
	}
	return loop, nil
}

// StaticGenerator makes a checked-in proposal usable by the same coordinator
// path as a future repository-aware model-backed generator.
type StaticGenerator struct {
	Binding *Binding
}

func (g StaticGenerator) Generate(context.Context, GenerationRequest) (*Binding, error) {
	if g.Binding == nil {
		return nil, errors.New("static binding is nil")
	}
	return cloneBinding(g.Binding), nil
}

func (StaticGenerator) LastGenerationAudit() GenerationAudit {
	return GenerationAudit{Provider: "static", Model: "checked-in-binding"}
}

func cloneBinding(binding *Binding) *Binding {
	if binding == nil {
		return nil
	}
	copy := *binding
	copy.Capabilities = append([]CapabilityBinding(nil), binding.Capabilities...)
	copy.Operations = append([]OperationBinding(nil), binding.Operations...)
	copy.Witnesses = make([]Witness, len(binding.Witnesses))
	for index, witness := range binding.Witnesses {
		copy.Witnesses[index] = witness
		copy.Witnesses[index].Covers = append([]string(nil), witness.Covers...)
		copy.Witnesses[index].ValidatesCapabilities = append([]string(nil), witness.ValidatesCapabilities...)
	}
	return &copy
}
