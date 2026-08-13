package controlexperiment

import "testing"

func TestAgentTransportFreezeKeepsProviderAuthorityBounded(t *testing.T) {
	valid := AgentTransportFreeze{
		Provider: "openrouter", Endpoint: "https://openrouter.ai/api/v1/chat/completions",
		Model: "fixture/model", Thinking: "disabled", Temperature: 0,
		MaxOutputTokens: 1200, MaxCallsPerArm: 1, MaxRetries: 0,
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	retrying := valid
	retrying.MaxRetries = 2
	if err := retrying.Validate(); err != nil {
		t.Fatalf("bounded retry transport rejected: %v", err)
	}
	cases := []AgentTransportFreeze{
		func() AgentTransportFreeze { changed := valid; changed.Thinking = "enabled"; return changed }(),
		func() AgentTransportFreeze { changed := valid; changed.Temperature = 1; return changed }(),
		func() AgentTransportFreeze { changed := valid; changed.MaxCallsPerArm = 2; return changed }(),
		func() AgentTransportFreeze { changed := valid; changed.MaxRetries = 3; return changed }(),
		func() AgentTransportFreeze { changed := valid; changed.MaxOutputTokens = 4097; return changed }(),
		func() AgentTransportFreeze { changed := valid; changed.Model = "Fixture/Model Name"; return changed }(),
	}
	for index, candidate := range cases {
		if err := candidate.Validate(); err == nil {
			t.Fatalf("authority expansion case %d accepted", index)
		}
	}
}
