package conformance_test

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
)

func TestControlPathGradeIsDerivedFromWitnessFacts(t *testing.T) {
	base := conformance.ControlPathWitness{
		ID: "path", TargetID: "target", Surface: conformance.SurfaceMessage,
		Granularity: "connection", EvidenceDigest: "0000000000000000000000000000000000000000000000000000000000000000",
	}
	tests := []struct {
		name   string
		mutate func(*conformance.ControlPathWitness)
		want   conformance.ControlGrade
	}{
		{"opaque", func(*conformance.ControlPathWitness) {}, conformance.ControlOpaque},
		{"observable", func(w *conformance.ControlPathWitness) { w.Observed = true }, conformance.ControlObservable},
		{"interceptable", func(w *conformance.ControlPathWitness) { w.Observed, w.Intercepted = true, true }, conformance.ControlInterceptable},
		{"actuated", func(w *conformance.ControlPathWitness) {
			w.Observed, w.Intercepted, w.RuntimeAction, w.SelectionChecked = true, true, true, true
		}, conformance.ControlSchedulerActuated},
		{"owned", func(w *conformance.ControlPathWitness) {
			w.Observed, w.Intercepted, w.RuntimeAction, w.SelectionChecked = true, true, true, true
			w.StableItemID, w.RuntimeOwnsTerminal = true, true
		}, conformance.ControlSchedulerOwned},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			witness := base
			test.mutate(&witness)
			assessment, err := conformance.AssessControlPath(witness)
			if err != nil {
				t.Fatal(err)
			}
			if assessment.Grade != test.want {
				t.Fatalf("grade = %s, want %s", assessment.Grade, test.want)
			}
			if err := assessment.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestControlPathRejectsSelfInconsistentFactsAndTampering(t *testing.T) {
	base := conformance.ControlPathWitness{
		ID: "path", TargetID: "target", Surface: conformance.SurfaceMessage,
		Granularity: "connection", EvidenceDigest: "0000000000000000000000000000000000000000000000000000000000000000", Observed: true, Intercepted: true,
	}
	invalid := []conformance.ControlPathWitness{base, base, base}
	invalid[0].SelectionChecked = true
	invalid[1].RuntimeOwnsTerminal = true
	invalid[2].AtomicExternalEffect = true
	for _, witness := range invalid {
		if _, err := conformance.AssessControlPath(witness); err == nil || err.Error() != "CONTROL_PATH_WITNESS_FACTS_INCONSISTENT" {
			t.Fatalf("inconsistent witness error = %v", err)
		}
	}
	base.RuntimeAction, base.SelectionChecked = true, true
	assessment, err := conformance.AssessControlPath(base)
	if err != nil {
		t.Fatal(err)
	}
	assessment.Grade = conformance.ControlSchedulerOwned
	if err := assessment.Validate(); err == nil || err.Error() != "CONTROL_PATH_ASSESSMENT_DIGEST_MISMATCH" {
		t.Fatalf("tampered grade error = %v", err)
	}
}
