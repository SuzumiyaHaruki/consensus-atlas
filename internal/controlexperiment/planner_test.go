package controlexperiment

import (
	"reflect"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func plannerTestScope() PlannerScope {
	return PlannerScope{
		ExperimentID: "frozen-experiment", PSSID: "frozen-pss",
		Runtime:         RuntimeConfig{SeedHex: "01", ClockError: 2, MaxClones: 3},
		DecisionsPerRun: 4, RequireReplay: true, Runs: []int{2, 1},
	}
}

func TestDecodePlannerProposalRejectsUnknownAndTrailingFields(t *testing.T) {
	tests := []struct {
		encoded string
		want    string
	}{
		{`{"schema_version":"consensus-atlas/planner-proposal/v1","runtime":{}}`, "unknown field"},
		{`{"schema_version":"consensus-atlas/planner-proposal/v1","policies":[]} {}`,
			"PLANNER_PROPOSAL_TRAILING_JSON"},
	}
	for _, test := range tests {
		if _, err := DecodePlannerProposal([]byte(test.encoded)); err == nil ||
			!strings.Contains(err.Error(), test.want) {
			t.Fatalf("DecodePlannerProposal() error = %v, want %q", err, test.want)
		}
	}
}

func TestPlannerModelAuditDerivesOneCallWork(t *testing.T) {
	digest := strings.Repeat("a", 64)
	audit := PlannerModelAudit{
		Provider: "deepseek", RequestedModel: "requested", ResponseModel: "response",
		Endpoint:     "https://api.deepseek.com/chat/completions",
		PromptDigest: digest, RequestDigest: digest, ResponseDigest: digest,
		FinishReason: "stop", ThinkingMode: "disabled", MaxTokens: 1800,
		PromptTokens: 10, PromptCacheHitTokens: 6, PromptCacheMissTokens: 4,
		CompletionTokens: 4, TotalTokens: 14, DurationMillis: 1,
	}
	if err := audit.Validate(); err != nil {
		t.Fatal(err)
	}
	if work := audit.modelWork(); work != (ModelWork{Calls: 1, InputTokens: 10, OutputTokens: 4, TotalTokens: 14}) {
		t.Fatalf("model work = %#v", work)
	}
	audit.ResponseDigest = "not-a-digest"
	if err := audit.Validate(); err == nil {
		t.Fatal("invalid response digest was accepted")
	}
}

func TestCompileProposalFreezesScopeAndCanonicalRunOrder(t *testing.T) {
	scope := plannerTestScope()
	proposal := PlannerProposal{
		SchemaVersion: PlannerProposalVersion,
		Policies: []ProposedPolicy{
			{Run: 1, SeedHex: "02"},
			{Run: 2, Priority: []control.ActionKind{control.ActionDeliverMessage, control.ActionFireTemporal}},
		},
	}
	config, compilation, err := CompileProposal(scope, proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !compilation.Accepted || config.ID != scope.ExperimentID || config.PSSID != scope.PSSID ||
		config.Runtime != scope.Runtime || config.DecisionsPerRun != scope.DecisionsPerRun ||
		config.RequireReplay != scope.RequireReplay {
		t.Fatalf("scope escaped compilation: %#v %#v", compilation, config)
	}
	if got := []int{config.Runs[0].Run, config.Runs[1].Run}; !reflect.DeepEqual(got, scope.Runs) {
		t.Fatalf("compiled run order = %v, want frozen %v", got, scope.Runs)
	}
	if config.Runs[0].Policy.Version != PolicyVersion ||
		config.Runs[1].Policy.Version != RandomPolicyVersion {
		t.Fatalf("unexpected compiled policies: %#v", config.Runs)
	}
}

func TestCompileProposalRejectsRunSetAndMixedRandomPolicy(t *testing.T) {
	scope := plannerTestScope()
	tests := []struct {
		name     string
		proposal PlannerProposal
		code     string
	}{
		{
			name: "run set", code: ProposalRunSetInvalid,
			proposal: PlannerProposal{SchemaVersion: PlannerProposalVersion,
				Policies: []ProposedPolicy{{Run: 1, SeedHex: "01"}}},
		},
		{
			name: "mixed random", code: ProposalPolicyInvalid,
			proposal: PlannerProposal{SchemaVersion: PlannerProposalVersion, Policies: []ProposedPolicy{
				{Run: 1, SeedHex: "01", Priority: []control.ActionKind{control.ActionCrash}},
				{Run: 2, SeedHex: "02"},
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, compilation, err := CompileProposal(scope, test.proposal)
			if err != nil {
				t.Fatal(err)
			}
			if compilation.Accepted || compilation.ReasonCode != test.code || compilation.ProposalDigest == "" {
				t.Fatalf("unexpected compilation: %#v", compilation)
			}
		})
	}
}
