// Package omnipaxosv2 is the target composition root for the M5.21z
// Portable CFT qualification audit. Generic qualification remains in
// internal/conformance.
package omnipaxosv2

import (
	"context"
	"errors"

	adapterv2 "github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

type Bundle = conformance.QualificationBundle

func Run(ctx context.Context, workerPath string) (Bundle, error) {
	return RunWithConfig(ctx, adapterv2.Config{WorkerPath: workerPath})
}

func RunWithConfig(ctx context.Context, adapterConfig adapterv2.Config) (Bundle, error) {
	if adapterConfig.WorkerPath == "" || adapterConfig.ValidateNodeConfiguration() != nil {
		return Bundle{}, errors.New("OMNIPAXOS_QUALIFICATION_WORKER_PATH_REQUIRED")
	}
	baseFactory := func() control.Adapter {
		adapter, err := adapterv2.New(adapterConfig)
		if err != nil {
			panic(err)
		}
		return adapter
	}
	factory, workMeter := conformance.MeterFactory(baseFactory)
	manifestAdapter, err := adapterv2.New(adapterConfig)
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
	profile, err := conformance.PortableCFTProfileV3()
	if err != nil {
		return Bundle{}, err
	}
	independent, err := conformance.EvaluateIndependentControl(ctx, factory, conformance.NaturalLifecyclePlan{
		Seed: []byte("portable-v3-omnipaxos-control"), DecisionBound: 256,
	})
	if err != nil {
		return Bundle{}, err
	}
	core, err := conformance.EvaluateCore(ctx, factory, conformance.CorePlan{
		Seed:                 []byte("portable-v2-omnipaxos-core"),
		ExpectedEntropyNodes: adapterConfig.NodeIDs(),
	})
	if err != nil {
		return Bundle{}, err
	}
	input, err := adapterv2.InputPayload(adapterv2.Input{
		RequestID: "portable-v2-omnipaxos", Value: []byte("value"),
	})
	if err != nil {
		return Bundle{}, err
	}
	invokePlan := conformance.OpaqueInvokePlan{
		Seed: []byte("portable-v2-omnipaxos-invoke"), Node: "n2", Input: input, DecisionBound: 256,
	}
	invokeReplay, err := conformance.EvaluateOpaqueInvoke(ctx, factory, invokePlan)
	if err != nil {
		return Bundle{}, err
	}
	unsupported := []conformance.UnsupportedDeclaration{
		{CapabilityID: conformance.CapabilityCrashRestart, ReasonCode: conformance.UnsupportedControlSurface},
		{CapabilityID: conformance.CapabilityAuditedEntropyReplay, ReasonCode: conformance.UnsupportedEntropyControl},
		{CapabilityID: conformance.CapabilityFormalProcessIsolation, ReasonCode: conformance.UnsupportedProcessIsolation},
	}
	reports := []conformance.Report{core, independent, invokeReplay}
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
