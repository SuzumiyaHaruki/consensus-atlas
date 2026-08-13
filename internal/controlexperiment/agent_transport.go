package controlexperiment

import "errors"

// AgentTransportFreeze is the trusted, protocol-neutral description of one
// bounded provider call. It contains no credential and grants no execution or
// scheduling authority.
type AgentTransportFreeze struct {
	Provider        string `json:"provider"`
	Endpoint        string `json:"endpoint"`
	Model           string `json:"model"`
	Thinking        string `json:"thinking"`
	Temperature     int    `json:"temperature"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	MaxCallsPerArm  int    `json:"max_calls_per_arm"`
	MaxRetries      int    `json:"max_retries"`
}

func (transport AgentTransportFreeze) Validate() error {
	if !transport.valid() {
		return errors.New("EXPERIMENT_AGENT_TRANSPORT_FREEZE_INVALID")
	}
	return nil
}

func (transport AgentTransportFreeze) valid() bool {
	return validMethodToken(transport.Provider) && transport.Endpoint != "" &&
		validMethodToken(transport.Model) && transport.Thinking == "disabled" &&
		transport.Temperature == 0 && transport.MaxOutputTokens > 0 &&
		transport.MaxOutputTokens <= 4096 && transport.MaxCallsPerArm == 1 &&
		transport.MaxRetries == 0
}
