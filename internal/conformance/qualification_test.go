package conformance_test

import (
	"context"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestQualificationIsMechanicalAndPartial(t *testing.T) {
	ctx := context.Background()
	adapter := fixture.New()
	manifest, err := adapter.Manifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	witness, err := conformance.Evaluate(ctx, func() control.Adapter {
		return fixture.New()
	}, witnessPlan(t))
	if err != nil {
		t.Fatal(err)
	}
	profile := qualificationProfile(t)
	report, err := conformance.Qualify(
		manifest,
		profile,
		[]conformance.UnsupportedDeclaration{{
			CapabilityID: "natural-time", ReasonCode: conformance.UnsupportedClockControl,
		}},
		[]conformance.Report{witness},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Qualified {
		t.Fatal("partial qualification unexpectedly passed required profile")
	}
	if report.Summary != (conformance.QualificationSummary{
		Total: 4, Required: 4, Validated: 1, Unsupported: 1, Undeclared: 1, Unvalidated: 1,
	}) {
		t.Fatalf("summary = %+v", report.Summary)
	}
	want := map[string]conformance.CapabilityStatus{
		"message-retention": conformance.CapabilityValidated,
		"natural-time":      conformance.CapabilityUnsupported,
		"trial-cancel":      conformance.CapabilityUndeclared,
		"opaque-invoke":     conformance.CapabilityUnvalidated,
	}
	for _, result := range report.Capabilities {
		if result.Status != want[result.ID] {
			t.Fatalf("capability %s status = %s, want %s", result.ID, result.Status, want[result.ID])
		}
	}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestQualificationRejectsRewrittenConformance(t *testing.T) {
	ctx := context.Background()
	manifest, err := fixture.New().Manifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	witness, err := conformance.Evaluate(ctx, func() control.Adapter {
		return fixture.New()
	}, witnessPlan(t))
	if err != nil {
		t.Fatal(err)
	}
	witness.Cases[0].Passed = false
	if _, err := conformance.Qualify(manifest, qualificationProfile(t), nil, []conformance.Report{witness}); err == nil || err.Error() != "CONFORMANCE_REPORT_FAILED_REASON_REQUIRED" {
		t.Fatalf("tampered report error = %v", err)
	}
}

func TestQualificationProfileDigestUsesSetOrdering(t *testing.T) {
	left := qualificationProfile(t)
	right := left
	right.Capabilities = append([]conformance.CapabilityRequirement(nil), left.Capabilities...)
	for i, j := 0, len(right.Capabilities)-1; i < j; i, j = i+1, j-1 {
		right.Capabilities[i], right.Capabilities[j] = right.Capabilities[j], right.Capabilities[i]
	}
	right.Digest = ""
	right, err := right.Seal()
	if err != nil {
		t.Fatal(err)
	}
	if left.Digest != right.Digest {
		t.Fatalf("profile digest depends on declaration order: %s != %s", left.Digest, right.Digest)
	}
}

func TestQualificationRejectsUnknownUnsupportedReason(t *testing.T) {
	manifest, err := fixture.New().Manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = conformance.Qualify(manifest, qualificationProfile(t), []conformance.UnsupportedDeclaration{{
		CapabilityID: "natural-time", ReasonCode: "FREE_FORM_REASON",
	}}, nil)
	if err == nil || err.Error() != "QUALIFICATION_UNSUPPORTED_REASON_UNKNOWN" {
		t.Fatalf("unknown unsupported reason error = %v", err)
	}
}

func qualificationProfile(t *testing.T) conformance.QualificationProfile {
	t.Helper()
	profile, err := (conformance.QualificationProfile{
		ID: "test-adapter-profile",
		Capabilities: []conformance.CapabilityRequirement{
			{
				ID: "message-retention", Required: true,
				Manifest: conformance.ManifestRequirements{
					MinNodes: 2,
					Actions:  []control.ActionKind{control.ActionDeliverMessage, control.ActionDropMessage},
					Items:    []control.ItemKind{control.ItemMessage},
				},
				ConformanceCases: []string{"message-crash-retention"},
			},
			{
				ID: "natural-time", Required: true,
				Manifest: conformance.ManifestRequirements{
					Actions:       []control.ActionKind{control.ActionFireTemporal},
					Items:         []control.ItemKind{control.ItemTemporal},
					TemporalKinds: []control.TemporalKind{control.TemporalPeriodicPulse},
				},
				ConformanceCases: []string{"earliest-temporal-only"},
			},
			{
				ID: "trial-cancel", Required: true,
				Manifest: conformance.ManifestRequirements{
					Actions: []control.ActionKind{control.ActionCancelTrial},
				},
				ConformanceCases: []string{"trial-cancel-boundary"},
			},
			{
				ID: "opaque-invoke", Required: true,
				Manifest:         conformance.ManifestRequirements{Actions: []control.ActionKind{control.ActionInvoke}},
				ConformanceCases: []string{"opaque-invoke-boundary"},
			},
		},
	}).Seal()
	if err != nil {
		t.Fatal(err)
	}
	return profile
}
