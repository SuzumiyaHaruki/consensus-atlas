package controlexperiment

import (
	"errors"
	"strings"
)

// AgentTransportFreeze is the trusted, protocol-neutral description of one
// bounded provider call. It contains no credential and grants no execution or
// scheduling authority.
type AgentTransportFreeze struct {
	Provider         string `json:"provider"`
	Endpoint         string `json:"endpoint"`
	Model            string `json:"model"`
	Thinking         string `json:"thinking"`
	ExcludeReasoning bool   `json:"exclude_reasoning,omitempty"`
	Temperature      int    `json:"temperature"`
	MaxOutputTokens  int    `json:"max_output_tokens"`
	MaxCallsPerArm   int    `json:"max_calls_per_arm"`
	MaxRetries       int    `json:"max_retries"`
}

func (transport AgentTransportFreeze) Validate() error {
	if !transport.valid() {
		return errors.New("EXPERIMENT_AGENT_TRANSPORT_FREEZE_INVALID")
	}
	return nil
}

func (transport AgentTransportFreeze) valid() bool {
	return validMethodToken(transport.Provider) && transport.Endpoint != "" &&
		validAgentModelID(transport.Model) && validAgentReasoningEffort(transport.Thinking) &&
		transport.Temperature == 0 && transport.MaxOutputTokens > 0 &&
		transport.MaxOutputTokens <= 4096 && transport.MaxCallsPerArm == 1 &&
		transport.MaxRetries >= 0 && transport.MaxRetries <= 2
}

func validAgentReasoningEffort(value string) bool {
	switch value {
	case "disabled", "none", "minimal", "low", "medium", "high", "xhigh", "max":
		return true
	default:
		return false
	}
}

func validAgentModelID(model string) bool {
	if model == "" || model != strings.TrimSpace(model) || strings.ToLower(model) != model || len(model) > 200 {
		return false
	}
	for _, character := range model {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') ||
			character == '-' || character == '_' || character == '.' || character == '/' ||
			character == ':' || character == '~' {
			continue
		}
		return false
	}
	return true
}
