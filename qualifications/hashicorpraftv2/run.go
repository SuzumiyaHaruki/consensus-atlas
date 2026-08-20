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
	return RunWithConfig(ctx, adapterv2.Config{})
}

func RunWithConfig(ctx context.Context, adapterConfig adapterv2.Config) (Bundle, error) {
	baseFactory := func() control.Adapter {
		adapter, err := adapterv2.NewWithConfig(adapterConfig)
		if err != nil {
			panic(err)
		}
		return adapter
	}
	factory, workMeter := conformance.MeterFactory(baseFactory)
	manifestAdapter, err := adapterv2.NewWithConfig(adapterConfig)
	if err != nil {
		return Bundle{}, err
	}
	manifest, err := manifestAdapter.Manifest(ctx)
	closeErr := manifestAdapter.Close()
	if err != nil {
		return Bundle{}, err
	}
	if closeErr != nil {
		return Bundle{}, closeErr
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
	work := workMeter.Snapshot()
	if err := work.Validate(); err != nil {
		return Bundle{}, err
	}
	return (Bundle{
		SchemaVersion: conformance.QualificationBundleSchemaVersion,
		Profile:       profile, Manifest: manifest, ConformanceReports: reports,
		Unsupported: unsupported, Qualification: qualification,
		Work: &work,
	}).Seal()
}
