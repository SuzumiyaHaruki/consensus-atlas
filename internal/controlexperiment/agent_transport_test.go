package controlexperiment

import "testing"

func TestAgentTransportFreezeKeepsProviderAuthorityBounded(t *testing.T) {
	valid := AgentTransportFreeze{
		Provider: "deepseek", Endpoint: "https://api.deepseek.com/chat/completions",
		Model: "deepseek-v4-flash", Thinking: "disabled", Temperature: 0,
		MaxOutputTokens: 1200, MaxCallsPerArm: 1, MaxRetries: 0,
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []AgentTransportFreeze{
		func() AgentTransportFreeze { changed := valid; changed.Thinking = "enabled"; return changed }(),
		func() AgentTransportFreeze { changed := valid; changed.Temperature = 1; return changed }(),
		func() AgentTransportFreeze { changed := valid; changed.MaxCallsPerArm = 2; return changed }(),
		func() AgentTransportFreeze { changed := valid; changed.MaxRetries = 1; return changed }(),
		func() AgentTransportFreeze { changed := valid; changed.MaxOutputTokens = 4097; return changed }(),
	}
	for index, candidate := range cases {
		if err := candidate.Validate(); err == nil {
			t.Fatalf("authority expansion case %d accepted", index)
		}
	}
}
