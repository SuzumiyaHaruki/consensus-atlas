package conformance

import "github.com/SuzumiyaHaruki/consensus-atlas/internal/control"

const (
	CapabilityStrictYieldEvidence    = "strict-yield-evidence"
	CapabilityPureEnabledCheck       = "pure-enabled-check"
	CapabilityNaturalTemporal        = "natural-temporal-progress"
	CapabilityRuntimeOwnedMessage    = "runtime-owned-message"
	CapabilityCrashRestart           = "crash-restart-incarnation"
	CapabilityStrictDecisionReplay   = "strict-decision-replay"
	CapabilityAuditedEntropyReplay   = "audited-entropy-replay"
	CapabilityOpaqueInvokeBoundary   = "opaque-invoke-boundary"
	CapabilityFormalProcessIsolation = "formal-process-isolation"
)

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
	return portableCFTProfile(portableWitnesses{
		ID: "portable-cft-control-v2", Temporal: "natural-released-message-recovery",
		Message: "released-message-lifecycle",
		Replay:  []string{"natural-crash-cancels-captured", "natural-released-message-recovery", "opaque-invoke-boundary"},
		Invoke:  "opaque-invoke-accepted",
	})
}

// PortableCFTProfileV3 preserves the strict v2 denominator while separating
// temporal, message, and replay witnesses from crash/restart lifecycle.
func PortableCFTProfileV3() (QualificationProfile, error) {
	return portableCFTProfile(portableWitnesses{
		ID: "portable-cft-control-v3", Temporal: "natural-temporal-progress-independent",
		Message: "released-message-control", Replay: []string{"action-trace-replay"},
		Invoke: "opaque-invoke-boundary",
	})
}

type portableWitnesses struct {
	ID       string
	Temporal string
	Message  string
	Replay   []string
	Invoke   string
}

func portableCFTProfile(witnesses portableWitnesses) (QualificationProfile, error) {
	return (QualificationProfile{
		ID: witnesses.ID,
		Capabilities: []CapabilityRequirement{
			{
				ID: CapabilityStrictYieldEvidence, Required: true,
				Manifest:         ManifestRequirements{StrictYield: true, EvidenceSchema: true},
				ConformanceCases: []string{"collect-idempotent"},
			},
			{
				ID: CapabilityPureEnabledCheck, Required: true,
				Manifest:         ManifestRequirements{StrictYield: true},
				ConformanceCases: []string{"enabled-check-pure"},
			},
			{
				ID: CapabilityNaturalTemporal, Required: true,
				Manifest: ManifestRequirements{
					Actions: []control.ActionKind{control.ActionFireTemporal},
					Items:   []control.ItemKind{control.ItemTemporal},
					AnyTemporalKinds: []control.TemporalKind{
						control.TemporalOneShotTimer, control.TemporalPeriodicPulse, control.TemporalSleepWakeup,
					},
				},
				ConformanceCases: []string{witnesses.Temporal},
			},
			{
				ID: CapabilityRuntimeOwnedMessage, Required: true,
				Manifest: ManifestRequirements{
					MinNodes: 2,
					Actions:  []control.ActionKind{control.ActionDeliverMessage, control.ActionDropMessage},
					Items:    []control.ItemKind{control.ItemMessage},
				},
				ConformanceCases: []string{witnesses.Message},
			},
			{
				ID: CapabilityCrashRestart, Required: true,
				Manifest: ManifestRequirements{
					Actions: []control.ActionKind{control.ActionCrash, control.ActionRestart}, CrashModes: []string{"power-loss"},
				},
				ConformanceCases: []string{"released-message-lifecycle"},
			},
			{
				ID: CapabilityStrictDecisionReplay, Required: true,
				Manifest:         ManifestRequirements{StrictYield: true, StrictReplay: true},
				ConformanceCases: append([]string(nil), witnesses.Replay...),
			},
			{
				ID: CapabilityAuditedEntropyReplay, Required: true,
				Manifest:         ManifestRequirements{EntropyStrictReplay: true},
				ConformanceCases: []string{"entropy-audit-stable"},
			},
			{
				ID: CapabilityOpaqueInvokeBoundary, Required: true,
				Manifest:         ManifestRequirements{Actions: []control.ActionKind{control.ActionInvoke}},
				ConformanceCases: []string{witnesses.Invoke},
			},
			{
				ID: CapabilityFormalProcessIsolation, Required: false,
				Manifest:         ManifestRequirements{EntropyStrictReplay: true},
				ConformanceCases: []string{"process-isolation-boundary"},
			},
		},
	}).Seal()
}
