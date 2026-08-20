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
	return RunWithConfig(ctx, adapterv2.ThreeNodeConfig())
}

// RunWithConfig qualifies the same concrete membership that will be used by
// the experiment. This prevents an N-node execution from borrowing the
// Manifest and entropy evidence of the historical three-node target.
func RunWithConfig(ctx context.Context, config adapterv2.Config) (Bundle, error) {
	prototype, err := adapterv2.NewWithConfig(config)
	if err != nil {
		return Bundle{}, err
	}
	baseFactory := func() control.Adapter {
		adapter, err := adapterv2.NewWithConfig(config)
		if err != nil {
			panic(err)
		}
		return adapter
	}
	factory, workMeter := conformance.MeterFactory(baseFactory)
	manifest, err := prototype.Manifest(ctx)
	if err != nil {
		return Bundle{}, err
	}
	profile, err := conformance.PortableCFTProfile()
	if err != nil {
		return Bundle{}, err
	}
	core, err := conformance.EvaluateCore(ctx, factory, conformance.CorePlan{
		Seed:                 []byte("portable-v2-etcd-core"),
		ExpectedEntropyNodes: append([]control.NodeID(nil), manifest.Nodes...),
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
		Seed: []byte("portable-v2-etcd-invoke"), Node: manifest.Nodes[0], Input: input, DecisionBound: 256,
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
	work := workMeter.Snapshot()
	if err := work.Validate(); err != nil {
		return Bundle{}, err
	}
	return (Bundle{
		SchemaVersion: conformance.QualificationBundleSchemaVersion, Profile: profile, Manifest: manifest,
		ConformanceReports: reports, Unsupported: unsupported, Qualification: qualification,
		Work: &work,
	}).Seal()
}
