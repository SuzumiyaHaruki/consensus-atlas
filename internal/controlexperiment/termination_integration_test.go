package controlexperiment

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
)

// quiescentAdapter keeps the fixture's deterministic initial yield and
// evidence, but declares no schedulable Action. It is a test witness for an
// honestly empty Runtime frontier rather than a policy-selection failure.
type quiescentAdapter struct {
	*fixture.Adapter
}

func (adapter *quiescentAdapter) Manifest(ctx context.Context) (control.AdapterManifest, error) {
	manifest, err := adapter.Adapter.Manifest(ctx)
	if err != nil {
		return control.AdapterManifest{}, err
	}
	manifest.AdapterID = "control-quiescent-fixture-v1"
	manifest.ImplementationID = "protocol-free-quiescent-fixture/v1"
	manifest.Capabilities.Actions = nil
	return manifest, nil
}

type quiescentMapper struct{}

type terminalActionAdapter struct {
	*fixture.Adapter
	initialized bool
}

func (adapter *terminalActionAdapter) RunUntilYield(ctx context.Context) (control.Yield, error) {
	if adapter.initialized {
		return control.Yield{}, errors.New("TEST_TARGET_ACTION_FAILED")
	}
	adapter.initialized = true
	return adapter.Adapter.RunUntilYield(ctx)
}

func (quiescentMapper) ID() string { return "fixture/quiescent-core-pss-v1" }

func (quiescentMapper) Map(evidence control.EvidenceEnvelope) (psscore.SemanticObservation, error) {
	var snapshot struct {
		LogicalTime uint64 `json:"logical_time"`
	}
	if err := json.Unmarshal(evidence.Payload.Bytes, &snapshot); err != nil {
		return psscore.SemanticObservation{}, err
	}
	return psscore.SemanticObservation{
		LogicalTime: snapshot.LogicalTime,
		Graph: psscore.SemanticGraph{Entities: []psscore.Entity{
			{ID: "n1", Kind: psscore.EntityParticipant, Mode: psscore.ModePassive},
			{ID: "n2", Kind: psscore.EntityParticipant, Mode: psscore.ModePassive},
		}},
	}, nil
}

func TestExperimentV2RecordsAndReplaysQuiescentTermination(t *testing.T) {
	config := Config{
		SchemaVersion:   SchemaVersionV2,
		ID:              "quiescent-termination",
		PSSID:           quiescentMapper{}.ID(),
		Runtime:         RuntimeConfig{SeedHex: "01"},
		DecisionsPerRun: 4,
		RequireReplay:   true,
		Runs: []RunPlan{{Run: 1, Policy: Policy{
			Version: PolicyVersion,
			ID:      "unused-on-empty-frontier",
			Priority: []control.ActionKind{
				control.ActionCrash,
			},
		}}},
	}
	report, err := execute(t.Context(), config, func() (control.Adapter, error) {
		return &quiescentAdapter{Adapter: fixture.New()}, nil
	}, quiescentMapper{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	run := report.Runs[0]
	if run.Termination != RunTerminationQuiescent || run.ChargedDecisions != 0 ||
		run.BudgetReached || run.Replay.Decisions != 0 || len(run.Selections) != 0 {
		t.Fatalf("unexpected quiescent run: %#v", run)
	}
	if run.CorePSSSamples != 1 || report.StateDiscovery.TotalDecisions != 0 ||
		report.StateDiscovery.UniqueStates != 1 {
		t.Fatalf("unexpected quiescent discovery: %#v", report.StateDiscovery)
	}
}

func TestExperimentV2DistinguishesPolicySurfaceExhaustionFromQuiescence(t *testing.T) {
	config := Config{
		SchemaVersion: SchemaVersionV2, ID: "policy-surface-termination",
		PSSID: quiescentMapper{}.ID(), Runtime: RuntimeConfig{SeedHex: "01"},
		DecisionsPerRun: 4, RequireReplay: true,
		Runs: []RunPlan{{Run: 1, Policy: Policy{
			Version: BoundedUniformPolicyVersion, ID: "invoke-only", SeedHex: "01",
			SelectableActions: []control.ActionKind{control.ActionInvoke},
		}}},
	}
	report, err := execute(t.Context(), config, func() (control.Adapter, error) {
		return fixture.New(), nil
	}, quiescentMapper{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	run := report.Runs[0]
	if run.Termination != RunTerminationPolicySurface || run.ChargedDecisions != 0 ||
		run.BudgetReached || run.Replay.Decisions != 0 || len(run.Selections) != 0 {
		t.Fatalf("unexpected policy-surface run: %#v", run)
	}
}

func TestExperimentChargesFailedActionAndPreservesTerminalOutcome(t *testing.T) {
	config := Config{
		SchemaVersion: SchemaVersionV2, ID: "terminal-action-accounting",
		PSSID: quiescentMapper{}.ID(), Runtime: RuntimeConfig{SeedHex: "01"},
		DecisionsPerRun: 1, RequireReplay: true,
		Runs: []RunPlan{{Run: 1, Policy: Policy{
			Version: PolicyVersion, ID: "select-terminal-temporal",
			Priority: []control.ActionKind{control.ActionFireTemporal},
		}}},
	}
	_, err := execute(t.Context(), config, func() (control.Adapter, error) {
		return &terminalActionAdapter{Adapter: fixture.New()}, nil
	}, quiescentMapper{}, nil, nil)
	var failure *ExecutionFailure
	if !errors.As(err, &failure) || failure.Phase != "primary-select" ||
		failure.Decision != 1 || failure.Work.Primary.SchedulerDecisions != 1 ||
		failure.Work.Primary.WorkUnits != 2 || failure.Terminal == nil ||
		failure.Terminal.Validate() != nil || failure.Terminal.Decision != 1 ||
		failure.Terminal.Class != control.ExecutionFailureAdapter ||
		failure.Terminal.Code != "ADAPTER_ACTION_FAILED" {
		t.Fatalf("terminal execution failure = %#v / %v", failure, err)
	}
	methodFailure := failure.MethodFailure()
	encoded, err := json.Marshal(methodFailure)
	if err != nil {
		t.Fatal(err)
	}
	var restored MethodFailure
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Terminal == nil || restored.Terminal.Validate() != nil ||
		restored.Terminal.Digest != failure.Terminal.Digest ||
		restored.Phase != failure.Phase || restored.Decision != failure.Decision {
		t.Fatalf("persisted terminal failure drifted: %#v", restored)
	}
}
