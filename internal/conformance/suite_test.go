package conformance_test

import (
	"context"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestFixturePassesCoreConformance(t *testing.T) {
	report, err := conformance.EvaluateCore(context.Background(), func() control.Adapter {
		return fixture.New()
	}, corePlan())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Cases) != 3 || len(report.ValidatedCapabilities) != 3 {
		t.Fatalf("core conformance failed: %+v", report)
	}
}

func TestCoreRejectsNonIdempotentCollect(t *testing.T) {
	report, err := conformance.EvaluateCore(context.Background(), func() control.Adapter {
		return &unstableCollectAdapter{Adapter: fixture.New()}
	}, corePlan())
	if err != nil {
		t.Fatal(err)
	}
	assertFailedCase(t, report, "collect-idempotent", "COLLECT_NOT_IDEMPOTENT")
}

func TestCoreRejectsCheckThatMutatesObservableState(t *testing.T) {
	report, err := conformance.EvaluateCore(context.Background(), func() control.Adapter {
		return &mutatingCheckAdapter{Adapter: fixture.New()}
	}, corePlan())
	if err != nil {
		t.Fatal(err)
	}
	assertFailedCase(t, report, "enabled-check-pure", "ADAPTER_CHECK_MUTATED_EVIDENCE")
}

func corePlan() conformance.CorePlan {
	return conformance.CorePlan{
		Seed: []byte("conformance-seed"), ExpectedEntropyNodes: []control.NodeID{"n1", "n2"},
	}
}

func assertFailedCase(t *testing.T, report conformance.Report, id, reason string) {
	t.Helper()
	for _, result := range report.Cases {
		if result.ID == id {
			if result.Passed || result.ReasonCode != reason {
				t.Fatalf("case %s = %+v", id, result)
			}
			return
		}
	}
	t.Fatalf("case %s missing", id)
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
