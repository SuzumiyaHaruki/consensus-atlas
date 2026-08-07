// Package etcdraftv2 is the protocol-specific composition root for the M5.3
// qualification pilot. Generic qualification logic remains in
// internal/conformance.
package etcdraftv2

import (
	"context"

	adapterv2 "github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

type Bundle = conformance.QualificationBundle

func Run(ctx context.Context) (Bundle, error) {
	factory := func() control.Adapter {
		adapter, err := adapterv2.NewWithConfig(adapterv2.ThreeNodeConfig())
		if err != nil {
			panic(err)
		}
		return adapter
	}
	manifest, err := factory().Manifest(ctx)
	if err != nil {
		return Bundle{}, err
	}
	profile, err := conformance.PortableCFTProfile()
	if err != nil {
		return Bundle{}, err
	}
	core, err := conformance.EvaluateCore(ctx, factory, conformance.CorePlan{
		Seed:                 []byte("m5.3-etcdraft-core"),
		ExpectedEntropyNodes: []control.NodeID{"n1", "n2", "n3"},
	})
	if err != nil {
		return Bundle{}, err
	}
	lifecycle, err := conformance.EvaluateNaturalLifecycle(ctx, factory, conformance.NaturalLifecyclePlan{
		Seed: []byte("m5.3-etcdraft-lifecycle"), DecisionBound: 192,
	})
	if err != nil {
		return Bundle{}, err
	}
	input, err := adapterv2.InputPayload(adapterv2.Input{
		Operation: adapterv2.OperationPropose,
		RequestID: "m5.3-qualification-request",
		Value:     []byte("m5.3-opaque-value"),
	})
	if err != nil {
		return Bundle{}, err
	}
	invoke, err := conformance.EvaluateOpaqueInvoke(ctx, factory, conformance.OpaqueInvokePlan{
		Seed: []byte("m5.3-etcdraft-invoke"), Node: "n1", Input: input, DecisionBound: 256,
	})
	if err != nil {
		return Bundle{}, err
	}
	unsupported := []conformance.UnsupportedDeclaration{{
		CapabilityID: "formal-process-isolation",
		ReasonCode:   conformance.UnsupportedProcessIsolation,
	}}
	reports := []conformance.Report{core, lifecycle, invoke}
	qualification, err := conformance.Qualify(manifest, profile, unsupported, reports)
	if err != nil {
		return Bundle{}, err
	}
	return (Bundle{
		SchemaVersion: conformance.QualificationBundleSchemaVersion, Profile: profile, Manifest: manifest,
		ConformanceReports: reports, Unsupported: unsupported, Qualification: qualification,
	}).Seal()
}
