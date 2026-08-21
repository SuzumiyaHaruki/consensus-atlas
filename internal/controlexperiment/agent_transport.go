package controlexperiment

import (
	"errors"
	"strings"
)

// AgentTransportFreeze is the trusted, protocol-neutral description of one
// bounded provider call. It contains no credential and grants no execution or
// scheduling authority.
type AgentTransportFreeze struct {
	Provider              string `json:"provider"`
	Endpoint              string `json:"endpoint"`
	Model                 string `json:"model"`
	Thinking              string `json:"thinking"`
	ExcludeReasoning      bool   `json:"exclude_reasoning,omitempty"`
	StructuredOutputMode  string `json:"structured_output_mode"`
	Stream                bool   `json:"stream"`
	RequestTimeoutMS      int64  `json:"request_timeout_ms"`
	RoutingPolicy         string `json:"routing_policy"`
	AllowProviderFallback bool   `json:"allow_provider_fallback"`
	Temperature           int    `json:"temperature"`
	MaxOutputTokens       int    `json:"max_output_tokens"`
	MaxCallsPerArm        int    `json:"max_calls_per_arm"`
	MaxRetries            int    `json:"max_retries"`
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
		validAgentStructuredOutputMode(transport.StructuredOutputMode) && !transport.Stream &&
		transport.RequestTimeoutMS > 0 && transport.RequestTimeoutMS <= 30*60*1000 &&
		validMethodToken(transport.RoutingPolicy) &&
		transport.Temperature == 0 && transport.MaxOutputTokens > 0 &&
		transport.MaxOutputTokens <= 32000 && transport.MaxCallsPerArm == 1 &&
		transport.MaxRetries >= 0 && transport.MaxRetries <= 2
}

func validAgentStructuredOutputMode(value string) bool {
	return value == "json-schema" || value == "json-object"
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
