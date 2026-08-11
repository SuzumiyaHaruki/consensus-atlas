package conformance_test

import (
	"context"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestFixturePassesExternalConformance(t *testing.T) {
	report, err := conformance.Evaluate(context.Background(), func() control.Adapter {
		return fixture.New()
	}, witnessPlan(t))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("conformance failed: %+v", report.Cases)
	}
	if len(report.Cases) != 13 || len(report.ValidatedCapabilities) != 13 {
		t.Fatalf("cases/capabilities = %d/%d, want 13/13", len(report.Cases), len(report.ValidatedCapabilities))
	}
}

func TestSuiteRejectsNonIdempotentCollect(t *testing.T) {
	report, err := conformance.Evaluate(context.Background(), func() control.Adapter {
		return &unstableCollectAdapter{Adapter: fixture.New()}
	}, witnessPlan(t))
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed {
		t.Fatal("non-idempotent Adapter passed conformance")
	}
	found := false
	for _, result := range report.Cases {
		if result.ID == "collect-idempotent" {
			found = true
			if result.Passed || result.ReasonCode != "COLLECT_NOT_IDEMPOTENT" {
				t.Fatalf("collect result = %+v", result)
			}
		}
	}
	if !found {
		t.Fatal("collect-idempotent case missing")
	}
}

func TestSuiteRejectsCheckThatMutatesObservableState(t *testing.T) {
	report, err := conformance.Evaluate(context.Background(), func() control.Adapter {
		return &mutatingCheckAdapter{Adapter: fixture.New()}
	}, witnessPlan(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range report.Cases {
		if result.ID == "enabled-check-pure" {
			if result.Passed || result.ReasonCode != "ADAPTER_CHECK_MUTATED_EVIDENCE" {
				t.Fatalf("enabled-check result = %+v", result)
			}
			return
		}
	}
	t.Fatal("enabled-check-pure case missing")
}

type unstableCollectAdapter struct {
	control.Adapter
	collects int
}

type mutatingCheckAdapter struct {
	control.Adapter
	checked bool
}

func (adapter *mutatingCheckAdapter) Check(ctx context.Context, command control.AdapterCommand) (control.CommandEligibility, error) {
	eligibility, err := adapter.Adapter.Check(ctx, command)
	adapter.checked = true
	return eligibility, err
}

func (adapter *mutatingCheckAdapter) SnapshotEvidence(ctx context.Context) (control.EvidenceEnvelope, error) {
	evidence, err := adapter.Adapter.SnapshotEvidence(ctx)
	if err != nil || !adapter.checked {
		return evidence, err
	}
	bytes := append([]byte(nil), evidence.Payload.Bytes...)
	bytes = append(bytes, ' ')
	evidence.Payload, err = control.NewPayload(evidence.Payload.SchemaVersion, evidence.Payload.Encoding, bytes)
	return evidence, err
}

func (adapter *unstableCollectAdapter) Collect(ctx context.Context, yield control.YieldID) (control.Emission, error) {
	emission, err := adapter.Adapter.Collect(ctx, yield)
	if err != nil {
		return control.Emission{}, err
	}
	adapter.collects++
	if adapter.collects == 2 && len(emission.Items) > 0 && emission.Items[0].Temporal != nil {
		emission.Items[0].Temporal.Deadline++
	}
	return emission, nil
}

func witnessPlan(t *testing.T) conformance.WitnessPlan {
	t.Helper()
	message, err := fixture.InputPayload(fixture.Input{
		Operation: fixture.OpEmitMessage, Target: "n2", Value: "message",
	})
	if err != nil {
		t.Fatal(err)
	}
	durable, err := fixture.InputPayload(fixture.Input{
		Operation: fixture.OpEmitDurableMessage, Target: "n2", Value: "durable",
	})
	if err != nil {
		t.Fatal(err)
	}
	sleep, err := fixture.InputPayload(fixture.Input{Operation: fixture.OpSleep, Delay: 5})
	if err != nil {
		t.Fatal(err)
	}
	oneShot, err := fixture.InputPayload(fixture.Input{Operation: fixture.OpOneShot, Delay: 3})
	if err != nil {
		t.Fatal(err)
	}
	callback, err := fixture.InputPayload(fixture.Input{Operation: fixture.OpCallback, Value: "callback"})
	if err != nil {
		t.Fatal(err)
	}
	return conformance.WitnessPlan{
		Seed: []byte("conformance-seed"), Source: "n1", Target: "n2",
		MessageInput: message, DurableMessageInput: durable, SleepInput: sleep,
		OneShotInput: oneShot, CallbackInput: callback,
		ExpectedInitialDeadlinePeers: []control.NodeID{"n1", "n2"},
	}
}
