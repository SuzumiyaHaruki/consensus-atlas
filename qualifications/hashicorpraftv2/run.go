// Package hashicorpraftv2 is the implementation-specific composition root for
// the M5.4c partial qualification. Generic evaluation remains in conformance.
package hashicorpraftv2

import (
	"context"

	adapterv2 "github.com/SuzumiyaHaruki/consensus-atlas/adapters/hashicorpraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

type Bundle = conformance.QualificationBundle

func Run(ctx context.Context) (Bundle, error) {
	var opened []*adapterv2.Adapter
	factory := func() control.Adapter {
		adapter := adapterv2.NewAdapter()
		opened = append(opened, adapter)
		return adapter
	}
	defer func() {
		for _, adapter := range opened {
			_ = adapter.Close()
		}
	}()

	manifest, err := factory().Manifest(ctx)
	if err != nil {
		return Bundle{}, err
	}
	profile, err := conformance.PortableCFTProfile()
	if err != nil {
		return Bundle{}, err
	}
	lifecycle, err := conformance.EvaluateReleasedMessageLifecycle(ctx, factory, conformance.NaturalLifecyclePlan{
		Seed: []byte("m5.4c-hashicorp-lifecycle"), DecisionBound: 64,
	})
	if err != nil {
		return Bundle{}, err
	}
	input, err := adapterv2.InputPayload([]byte("m5.4c-opaque-value"))
	if err != nil {
		return Bundle{}, err
	}
	invoke, err := conformance.EvaluateOpaqueInvokeAccepted(ctx, factory, conformance.OpaqueInvokePlan{
		Seed: []byte("m5.4c-hashicorp-invoke"), Node: "n1", Input: input, DecisionBound: 32,
	})
	if err != nil {
		return Bundle{}, err
	}
	unsupported := []conformance.UnsupportedDeclaration{
		{CapabilityID: "strict-yield-evidence", ReasonCode: conformance.UnsupportedConformanceWitness},
		{CapabilityID: "pure-enabled-check", ReasonCode: conformance.UnsupportedConformanceWitness},
		{CapabilityID: "natural-temporal-progress", ReasonCode: conformance.UnsupportedClockControl},
		{CapabilityID: "strict-decision-replay", ReasonCode: conformance.UnsupportedClockControl},
		{CapabilityID: "audited-entropy-replay", ReasonCode: conformance.UnsupportedEntropyControl},
		{CapabilityID: "formal-process-isolation", ReasonCode: conformance.UnsupportedProcessIsolation},
	}
	reports := []conformance.Report{lifecycle, invoke}
	qualification, err := conformance.Qualify(manifest, profile, unsupported, reports)
	if err != nil {
		return Bundle{}, err
	}
	return (Bundle{
		SchemaVersion: conformance.QualificationBundleSchemaVersion,
		Profile:       profile, Manifest: manifest, ConformanceReports: reports,
		Unsupported: unsupported, Qualification: qualification,
	}).Seal()
}
