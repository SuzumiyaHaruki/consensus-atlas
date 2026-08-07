package conformance

import "github.com/SuzumiyaHaruki/consensus-atlas/internal/control"

// PortableCFTProfile is the first protocol-neutral admission profile. It
// covers only the control-plane subset required before a second CFT Adapter is
// used for portability claims. Protocol safety semantics remain Evidence and
// Oracle responsibilities and are intentionally absent here.
func PortableCFTProfile() (QualificationProfile, error) {
	return (QualificationProfile{
		ID: "portable-cft-control-v1",
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
						control.TemporalOneShotTimer,
						control.TemporalPeriodicPulse,
						control.TemporalSleepWakeup,
					},
				},
				ConformanceCases: []string{"natural-released-message-recovery"},
			},
			{
				ID: "runtime-owned-message", Required: true,
				Manifest: ManifestRequirements{
					MinNodes: 2,
					Actions: []control.ActionKind{
						control.ActionDeliverMessage, control.ActionDropMessage,
					},
					Items: []control.ItemKind{control.ItemMessage},
				},
				ConformanceCases: []string{"natural-released-message-recovery"},
			},
			{
				ID: "crash-incarnation", Required: true,
				Manifest: ManifestRequirements{
					Actions:    []control.ActionKind{control.ActionCrash, control.ActionRestart},
					CrashModes: []string{"power-loss"},
				},
				ConformanceCases: []string{"natural-crash-cancels-captured"},
			},
			{
				ID: "strict-decision-replay", Required: true,
				Manifest: ManifestRequirements{StrictYield: true, StrictReplay: true},
				ConformanceCases: []string{
					"natural-crash-cancels-captured",
					"natural-released-message-recovery",
					"opaque-invoke-boundary",
				},
			},
			{
				ID: "audited-entropy-replay", Required: true,
				Manifest:         ManifestRequirements{EntropyStrictReplay: true},
				ConformanceCases: []string{"entropy-audit-stable"},
			},
			{
				ID: "opaque-invoke-boundary", Required: true,
				Manifest:         ManifestRequirements{Actions: []control.ActionKind{control.ActionInvoke}},
				ConformanceCases: []string{"opaque-invoke-boundary"},
			},
			{
				ID: "formal-process-isolation", Required: false,
				Manifest:         ManifestRequirements{EntropyStrictReplay: true},
				ConformanceCases: []string{"process-isolation-boundary"},
			},
		},
	}).Seal()
}

// PortableCFTProfileV2 separates basic control ownership from strict replay,
// so an Adapter cannot gain or lose message/lifecycle credit merely because
// its clock or package randomness is not replayable.
func PortableCFTProfileV2() (QualificationProfile, error) {
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
