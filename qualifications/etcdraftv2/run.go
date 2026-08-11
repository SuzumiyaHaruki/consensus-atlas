// Package etcdraftv2 is the protocol-specific composition root for the current
// portable CFT qualification. Generic qualification logic remains in
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
		Seed:                 []byte("portable-v2-etcd-core"),
		ExpectedEntropyNodes: []control.NodeID{"n1", "n2", "n3"},
	})
	if err != nil {
		return Bundle{}, err
	}
	lifecyclePlan := conformance.NaturalLifecyclePlan{
		Seed: []byte("portable-v2-etcd-lifecycle"), DecisionBound: 192,
	}
	natural, err := conformance.EvaluateNaturalLifecycle(ctx, factory, lifecyclePlan)
	if err != nil {
		return Bundle{}, err
	}
	released, err := conformance.EvaluateReleasedMessageLifecycle(ctx, factory, lifecyclePlan)
	if err != nil {
		return Bundle{}, err
	}
	input, err := adapterv2.InputPayload(adapterv2.Input{
		Operation: adapterv2.OperationPropose,
		RequestID: "portable-v2",
		Value:     []byte("value"),
	})
	if err != nil {
		return Bundle{}, err
	}
	invokePlan := conformance.OpaqueInvokePlan{
		Seed: []byte("portable-v2-etcd-invoke"), Node: "n1", Input: input, DecisionBound: 256,
	}
	invokeReplay, err := conformance.EvaluateOpaqueInvoke(ctx, factory, invokePlan)
	if err != nil {
		return Bundle{}, err
	}
	invokeAccepted, err := conformance.EvaluateOpaqueInvokeAccepted(ctx, factory, invokePlan)
	if err != nil {
		return Bundle{}, err
	}
	unsupported := []conformance.UnsupportedDeclaration{{
		CapabilityID: "formal-process-isolation",
		ReasonCode:   conformance.UnsupportedProcessIsolation,
	}}
	reports := []conformance.Report{core, natural, released, invokeReplay, invokeAccepted}
	qualification, err := conformance.Qualify(manifest, profile, unsupported, reports)
	if err != nil {
		return Bundle{}, err
	}
	return (Bundle{
		SchemaVersion: conformance.QualificationBundleSchemaVersion, Profile: profile, Manifest: manifest,
		ConformanceReports: reports, Unsupported: unsupported, Qualification: qualification,
	}).Seal()
}
