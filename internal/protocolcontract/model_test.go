package protocolcontract_test

import (
	"path/filepath"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolcontract"
)

func TestEtcdraftContractCompilesWithFixedDenominator(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	contract, err := protocolcontract.Load(filepath.Join(root, "contracts/etcdraft-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := contract.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(contract.InitialMembership.Voters) != 3 || len(contract.InitialMembership.Learners) != 0 {
		t.Fatalf("unexpected initial membership: %+v", contract.InitialMembership)
	}
	partial, err := protocolcontract.Compile(contract, map[string]bool{"explicit-campaign": true})
	if err != nil {
		t.Fatal(err)
	}
	full := make(map[string]bool)
	for _, capability := range contract.Capabilities {
		full[capability.ID] = true
	}
	complete, err := protocolcontract.Compile(contract, full)
	if err != nil {
		t.Fatal(err)
	}
	if partial.Version != 2 || complete.Version != 2 {
		t.Fatalf("compiled profile versions = %d/%d, want v2", partial.Version, complete.Version)
	}
	if len(partial.Coverage.Obligations) != len(complete.Coverage.Obligations) {
		t.Fatal("capability validation changed the coverage denominator")
	}
	supported := 0
	for _, obligation := range partial.Coverage.Obligations {
		if obligation.Status == "supported" {
			supported++
		}
		if len(obligation.Evidence.Reach) == 0 || len(obligation.Evidence.Observe) == 0 || len(obligation.Monitors) == 0 {
			t.Fatalf("compiled obligation lacks structured evidence: %+v", obligation)
		}
	}
	if supported == len(partial.Coverage.Obligations) {
		t.Fatal("unvalidated capabilities were silently treated as supported")
	}
	byID := make(map[string]int)
	for index, obligation := range complete.Coverage.Obligations {
		byID[obligation.ID] = index
	}
	campaign := complete.Coverage.Obligations[byID["transition.campaign"]]
	if campaign.Evidence.Observe[0].EventKind != core.EventCampaign {
		t.Fatalf("campaign transition lacks real event evidence: %+v", campaign.Evidence)
	}
	persist := complete.Coverage.Obligations[byID["ordering.persist"]]
	if len(persist.Evidence.Orderings) != 1 ||
		persist.Evidence.Orderings[0].Before.EventKind != core.EventPersist ||
		persist.Evidence.Orderings[0].After.EventKind != core.EventAcknowledge {
		t.Fatalf("persist obligation lacks strict event ordering: %+v", persist.Evidence)
	}
}
