package conformance

import "github.com/SuzumiyaHaruki/consensus-atlas/internal/control"

// RequiredCapabilityIDs returns the full required denominator of a validated
// Profile. Callers that claim strict portable execution should use this set;
// narrower exploratory experiments may declare an explicit trusted subset.
func (profile QualificationProfile) RequiredCapabilityIDs() []string {
	result := make([]string, 0, len(profile.Capabilities))
	for _, capability := range profile.Capabilities {
		if capability.Required {
			result = append(result, capability.ID)
		}
	}
	return result
}

// PortableCFTProfile separates basic control ownership from strict replay,
// so an Adapter cannot gain or lose message/lifecycle credit merely because
// its clock or package randomness is not replayable.
func PortableCFTProfile() (QualificationProfile, error) {
	return (QualificationProfile{
		ID: "portable-cft-control-v2",
		Capabilities: []CapabilityRequirement{
			{
				ID: "strict-yield-evidence", Required: true,
				Manifest:         ManifestRequirements{StrictYield: true, EvidenceSchema: true},
				ConformanceCases: []string{"collect-idempotent"},
			},
			{
				ID: "pure-enabled-check", Required: true,
				Manifest:         ManifestRequirements{StrictYield: true},
				ConformanceCases: []string{"enabled-check-pure"},
			},
			{
				ID: "natural-temporal-progress", Required: true,
				Manifest: ManifestRequirements{
					Actions: []control.ActionKind{control.ActionFireTemporal},
					Items:   []control.ItemKind{control.ItemTemporal},
					AnyTemporalKinds: []control.TemporalKind{
						control.TemporalOneShotTimer, control.TemporalPeriodicPulse, control.TemporalSleepWakeup,
					},
				},
				ConformanceCases: []string{"natural-released-message-recovery"},
			},
			{
				ID: "runtime-owned-message", Required: true,
				Manifest: ManifestRequirements{
					MinNodes: 2,
					Actions:  []control.ActionKind{control.ActionDeliverMessage, control.ActionDropMessage},
					Items:    []control.ItemKind{control.ItemMessage},
				},
				ConformanceCases: []string{"released-message-lifecycle"},
			},
			{
				ID: "crash-restart-incarnation", Required: true,
				Manifest: ManifestRequirements{
					Actions: []control.ActionKind{control.ActionCrash, control.ActionRestart}, CrashModes: []string{"power-loss"},
				},
				ConformanceCases: []string{"released-message-lifecycle"},
			},
			{
				ID: "strict-decision-replay", Required: true,
				Manifest:         ManifestRequirements{StrictYield: true, StrictReplay: true},
				ConformanceCases: []string{"natural-crash-cancels-captured", "natural-released-message-recovery", "opaque-invoke-boundary"},
			},
			{
				ID: "audited-entropy-replay", Required: true,
				Manifest:         ManifestRequirements{EntropyStrictReplay: true},
				ConformanceCases: []string{"entropy-audit-stable"},
			},
			{
				ID: "opaque-invoke-boundary", Required: true,
				Manifest:         ManifestRequirements{Actions: []control.ActionKind{control.ActionInvoke}},
				ConformanceCases: []string{"opaque-invoke-accepted"},
			},
			{
				ID: "formal-process-isolation", Required: false,
				Manifest:         ManifestRequirements{EntropyStrictReplay: true},
				ConformanceCases: []string{"process-isolation-boundary"},
			},
		},
	}).Seal()
}
