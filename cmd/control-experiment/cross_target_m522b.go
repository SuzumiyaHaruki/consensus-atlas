package main

import (
	"context"
	"encoding/hex"
	"errors"
	"slices"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	omniadapter "github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	etcdqualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/etcdraftv2"
	omniqualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/omnipaxosv2"
)

const (
	portableM522bRiskID         = "opaque-input-under-natural-progress"
	portableM522bBackend        = "bounded-action-class"
	portableM522eBackendUniform = "bounded-uniform"
)

type portableM522bInputs struct {
	Knowledge         controlexperiment.ProtocolKnowledgePack
	Catalog           controlexperiment.IntentCompilerCatalog
	CommonView        controlexperiment.CrossTargetPlannerView
	EtcdView          controlexperiment.AgentSemanticView
	OmniView          controlexperiment.AgentSemanticView
	EtcdQualification etcdqualification.Bundle
	OmniQualification omniqualification.Bundle
	OmniWorkerPath    string
}

type portableM522bTargetExecution struct {
	Intent   controlexperiment.GuardedTestIntent
	Plan     controlexperiment.CompiledIntentPlanV2
	Instance controlexperiment.IntentExecutionInstance
	Report   controlexperiment.Report
	Bundle   controlexperiment.ExecutionBundle
	Outcome  controlexperiment.IntentOutcome
}

type portableM522bPlanning struct {
	Intent       controlexperiment.GuardedTestIntent
	EtcdIntent   controlexperiment.GuardedTestIntent
	EtcdPlan     controlexperiment.CompiledIntentPlanV2
	EtcdInstance controlexperiment.IntentExecutionInstance
	OmniIntent   controlexperiment.GuardedTestIntent
	OmniPlan     controlexperiment.CompiledIntentPlanV2
	OmniInstance controlexperiment.IntentExecutionInstance
}

func planPortableM522bTargets(inputs portableM522bInputs) (portableM522bPlanning, error) {
	intent, err := controlexperiment.NewGuardedTestIntent(controlexperiment.GuardedTestIntent{
		ID:         "portable-natural-progress-m5-22b",
		ViewDigest: inputs.CommonView.Digest,
		RiskID:     portableM522bRiskID,
		Must:       controlexperiment.IntentMust{Decisions: 96},
		Prefer:     controlexperiment.IntentPrefer{BackendIDs: []string{portableM522bBackend}},
	})
	if err != nil {
		return portableM522bPlanning{}, err
	}
	return planPortableM522bTargetsWithIntent(inputs, intent)
}

func planPortableM522bTargetsWithIntent(
	inputs portableM522bInputs,
	intent controlexperiment.GuardedTestIntent,
) (portableM522bPlanning, error) {
	return planPortableTargetsWithIntent(inputs, intent, "m5-22b", 1)
}

func planPortableTargetsWithIntent(
	inputs portableM522bInputs,
	intent controlexperiment.GuardedTestIntent,
	stageID string,
	policySeed uint64,
) (portableM522bPlanning, error) {
	if stageID == "" {
		return portableM522bPlanning{}, errors.New("CROSS_TARGET_PORTABLE_STAGE_REQUIRED")
	}
	if err := intent.Validate(); err != nil {
		return portableM522bPlanning{}, err
	}
	if intent.ViewDigest != inputs.CommonView.Digest {
		return portableM522bPlanning{}, errors.New("CROSS_TARGET_M522B_PARENT_VIEW_MISMATCH")
	}
	sources := []controlexperiment.AgentSemanticView{inputs.EtcdView, inputs.OmniView}
	etcdIntent, etcdPlan, err := compilePortableM522bTarget(
		inputs, sources, intent, inputs.EtcdView, inputs.EtcdQualification.Manifest,
		inputs.EtcdQualification.Qualification,
	)
	if err != nil {
		return portableM522bPlanning{}, err
	}
	omniIntent, omniPlan, err := compilePortableM522bTarget(
		inputs, sources, intent, inputs.OmniView, inputs.OmniQualification.Manifest,
		inputs.OmniQualification.Qualification,
	)
	if err != nil {
		return portableM522bPlanning{}, err
	}
	if !portableM522bPlansEquivalent(etcdPlan, omniPlan) {
		return portableM522bPlanning{}, errors.New("CROSS_TARGET_M522B_PLAN_SEMANTICS_MISMATCH")
	}
	budget := controlexperiment.MethodBudget{
		MaxExecutionAttempts: 1,
		MaxPrimaryWorkUnits:  intent.Must.Decisions + 2,
		MaxReplayWorkUnits:   intent.Must.Decisions + 2,
	}
	etcdInstance, err := controlexperiment.NewIntentExecutionInstance(
		"portable-etcd-target-"+stageID, etcdPlan, policySeed, budget,
	)
	if err != nil {
		return portableM522bPlanning{}, err
	}
	omniInstance, err := controlexperiment.NewIntentExecutionInstance(
		"portable-omni-target-"+stageID, omniPlan, policySeed, budget,
	)
	if err != nil {
		return portableM522bPlanning{}, err
	}
	return portableM522bPlanning{
		Intent: intent, EtcdIntent: etcdIntent, EtcdPlan: etcdPlan, EtcdInstance: etcdInstance,
		OmniIntent: omniIntent, OmniPlan: omniPlan, OmniInstance: omniInstance,
	}, nil
}

func newPortableM522bInputs(
	ctx context.Context,
	omniWorkerPath string,
) (portableM522bInputs, error) {
	if omniWorkerPath == "" {
		return portableM522bInputs{}, errors.New("CROSS_TARGET_M522B_OMNI_WORKER_REQUIRED")
	}
	etcdQualified, err := etcdqualification.Run(ctx)
	if err != nil {
		return portableM522bInputs{}, err
	}
	omniQualified, err := omniqualification.Run(ctx, omniWorkerPath)
	if err != nil {
		return portableM522bInputs{}, err
	}
	knowledge, err := portableM522bKnowledge()
	if err != nil {
		return portableM522bInputs{}, err
	}
	catalog, err := portableM522bCatalog()
	if err != nil {
		return portableM522bInputs{}, err
	}
	etcdView, err := controlexperiment.NewAgentSemanticView(
		"portable-source-a-m5-22b", knowledge, catalog,
		etcdQualified.Manifest, etcdQualified.Qualification,
	)
	if err != nil {
		return portableM522bInputs{}, err
	}
	omniView, err := controlexperiment.NewAgentSemanticView(
		"portable-source-b-m5-22b", knowledge, catalog,
		omniQualified.Manifest, omniQualified.Qualification,
	)
	if err != nil {
		return portableM522bInputs{}, err
	}
	common, err := controlexperiment.NewCrossTargetPlannerView(
		"portable-leader-based-cft-view-m5-22b",
		[]controlexperiment.AgentSemanticView{etcdView, omniView},
	)
	if err != nil {
		return portableM522bInputs{}, err
	}
	return portableM522bInputs{
		Knowledge: knowledge, Catalog: catalog, CommonView: common,
		EtcdView: etcdView, OmniView: omniView,
		EtcdQualification: etcdQualified, OmniQualification: omniQualified,
		OmniWorkerPath: omniWorkerPath,
	}, nil
}

func portableM522bKnowledge() (controlexperiment.ProtocolKnowledgePack, error) {
	return controlexperiment.NewProtocolKnowledgePack(controlexperiment.ProtocolKnowledgePack{
		ID:     "portable-natural-progress-knowledge-m5-22b",
		Family: "leader-based-cft", Protocol: "portable-control",
		Knowledge: []controlexperiment.KnowledgeStatement{
			{ID: "natural-progress", Text: "Coordination changes arise from natural temporal progress and controlled peer-message delivery."},
			{ID: "opaque-input", Text: "A target-owned workload router submits one opaque input without exposing protocol-specific fields."},
			{ID: "strict-replay", Text: "Every selected common Action must be reproduced by a fresh deterministic replay."},
		},
		Risks: []controlexperiment.ProtocolRisk{{
			ID:                   portableM522bRiskID,
			Summary:              "Exercise one opaque input while natural time and controlled peer delivery advance a leader-based CFT target.",
			RequiredCapabilities: portableM522bCapabilities(),
			RequiredActions:      portableM522bActions(),
			AllowedBackendIDs:    []string{portableM522bBackend},
		}},
	})
}

func portableM522bCatalog() (controlexperiment.IntentCompilerCatalog, error) {
	return controlexperiment.NewIntentCompilerCatalog(controlexperiment.IntentCompilerCatalog{
		ID: "portable-bounded-backends-m5-22b",
		Templates: []controlexperiment.IntentBackendTemplate{{
			ID: portableM522bBackend, Strategy: portableM522bBackend,
			RequiredCapabilities: portableM522bCapabilities(),
			SupportedActions:     portableM522bActions(),
			MinDecisions:         1, MaxDecisions: 96, FallbackRank: 1,
		}},
	})
}

func portableM522bCapabilities() []string {
	return []string{
		conformance.CapabilityNaturalTemporal,
		conformance.CapabilityOpaqueInvokeBoundary,
		conformance.CapabilityPureEnabledCheck,
		conformance.CapabilityRuntimeOwnedMessage,
		conformance.CapabilityStrictDecisionReplay,
		conformance.CapabilityStrictYieldEvidence,
	}
}

func portableM522bActions() []control.ActionKind {
	return []control.ActionKind{
		control.ActionDeliverMessage,
		control.ActionFireTemporal,
		control.ActionInvoke,
	}
}

func compilePortableM522bTarget(
	inputs portableM522bInputs,
	sources []controlexperiment.AgentSemanticView,
	intent controlexperiment.GuardedTestIntent,
	target controlexperiment.AgentSemanticView,
	manifest control.AdapterManifest,
	qualification conformance.QualificationReport,
) (controlexperiment.GuardedTestIntent, controlexperiment.CompiledIntentPlanV2, error) {
	projected, err := controlexperiment.ProjectCrossTargetIntent(
		inputs.CommonView, sources, intent, target,
	)
	if err != nil {
		return controlexperiment.GuardedTestIntent{}, controlexperiment.CompiledIntentPlanV2{}, err
	}
	plan, err := controlexperiment.CompileGuardedTestIntentV2(
		target, inputs.Knowledge, inputs.Catalog, manifest, qualification, projected,
	)
	return projected, plan, err
}

func portableM522bPlansEquivalent(
	left, right controlexperiment.CompiledIntentPlanV2,
) bool {
	return left.CatalogDigest == right.CatalogDigest && left.RiskID == right.RiskID &&
		left.BackendID == right.BackendID && left.Strategy == right.Strategy &&
		left.Decisions == right.Decisions && left.FaultEnvelope == right.FaultEnvelope &&
		left.FallbackUsed == right.FallbackUsed && left.CompilerWork == right.CompilerWork &&
		slices.Equal(left.RequiredCapabilities, right.RequiredCapabilities) &&
		slices.Equal(left.RequiredActions, right.RequiredActions) &&
		slices.Equal(left.PreferenceMisses, right.PreferenceMisses)
}

func portableM522bPolicy(
	targetID string,
	plan controlexperiment.CompiledIntentPlanV2,
	instance controlexperiment.IntentExecutionInstance,
	plumbing []control.ActionKind,
) (controlexperiment.Policy, error) {
	selectable := append([]control.ActionKind(nil), plan.RequiredActions...)
	selectable = append(selectable, plumbing...)
	slices.Sort(selectable)
	selectable = slices.Compact(selectable)
	priority := []control.ActionKind{control.ActionInvoke}
	priority = append(priority, plumbing...)
	version := ""
	switch plan.Strategy {
	case portableM522bBackend:
		version = controlexperiment.BoundedActionClassPolicyVersion
		// Preserve the M5.22b-d progress-oriented backend exactly. It is a
		// distinct executable backend, not a pure action-class methodology arm.
		priority = append(priority, control.ActionDeliverMessage)
	case portableM522eBackendUniform:
		version = controlexperiment.BoundedUniformPolicyVersion
	default:
		return controlexperiment.Policy{}, errors.New("CROSS_TARGET_PORTABLE_STRATEGY_UNSUPPORTED")
	}
	policy := controlexperiment.Policy{
		Version: version,
		ID:      targetID + "-" + plan.Strategy, SeedHex: randomPolicySeed(instance.PolicySeed, 1),
		Priority: priority, SelectableActions: selectable,
	}
	if err := policy.Validate(plan.Decisions); err != nil {
		return controlexperiment.Policy{}, err
	}
	return policy, nil
}

func executePortableM522bEtcd(
	ctx context.Context,
	inputs portableM522bInputs,
	intent controlexperiment.GuardedTestIntent,
	plan controlexperiment.CompiledIntentPlanV2,
	instance controlexperiment.IntentExecutionInstance,
	plumbing []control.ActionKind,
) (portableM522bTargetExecution, error) {
	return executePortableEtcd(
		ctx, inputs, intent, plan, instance, plumbing, "m5-22b",
	)
}

func executePortableEtcd(
	ctx context.Context,
	inputs portableM522bInputs,
	intent controlexperiment.GuardedTestIntent,
	plan controlexperiment.CompiledIntentPlanV2,
	instance controlexperiment.IntentExecutionInstance,
	plumbing []control.ActionKind,
	stageID string,
) (portableM522bTargetExecution, error) {
	if stageID == "" {
		return portableM522bTargetExecution{}, errors.New("CROSS_TARGET_PORTABLE_STAGE_REQUIRED")
	}
	if err := plan.ValidateInputs(
		inputs.EtcdView, inputs.Knowledge, inputs.Catalog,
		inputs.EtcdQualification.Manifest, inputs.EtcdQualification.Qualification, intent,
	); err != nil {
		return portableM522bTargetExecution{}, err
	}
	admission, err := controlexperiment.BindExecutionAdmission(
		inputs.EtcdQualification.Qualification,
		controlexperiment.ExecutionRequirements{
			Capabilities: inputs.EtcdQualification.Profile.RequiredCapabilityIDs(),
		},
	)
	if err != nil {
		return portableM522bTargetExecution{}, err
	}
	policy, err := portableM522bPolicy("portable-etcd", plan, instance, plumbing)
	if err != nil {
		return portableM522bTargetExecution{}, err
	}
	requestID := "portable-request-" + stageID
	payload, err := etcdraftv2.InputPayload(etcdraftv2.Input{
		Operation: etcdraftv2.OperationPropose, RequestID: requestID, Value: []byte("portable-value"),
	})
	if err != nil {
		return portableM522bTargetExecution{}, err
	}
	workload := controlexperiment.WorkloadPlan{
		SchemaVersion: controlexperiment.WorkloadPlanVersion, ID: "portable-single-opaque-input",
		TargetSelector: controlexperiment.TargetSingleCoordinatingMember,
		Invocations: []controlexperiment.WorkloadInvocation{{
			ID: requestID, Input: payload, ExpectedStatus: "committed",
		}},
	}
	envelope := plan.FaultEnvelope
	config := controlexperiment.Config{
		SchemaVersion: controlexperiment.SchemaVersionV2,
		ID:            "portable-etcd-target-" + stageID, PSSID: etcdraftv2.CorePSSMappingID,
		WorkloadRouterID: etcdraftv2.WorkloadRouterID,
		Runtime:          etcdraftCampaignRuntimeConfig(), Admission: &admission, FaultEnvelope: &envelope,
		DecisionsPerRun: plan.Decisions, RequireReplay: true,
		Runs: []controlexperiment.RunPlan{{
			Run: 1, StopAfterWorkload: true,
			Policy:   policy,
			Workload: &workload,
		}},
	}
	report, bundle, err := controlexperiment.ExecuteQualifiedBundle(
		ctx, config, inputs.EtcdQualification,
		func() (control.Adapter, error) { return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig()) },
		etcdraftv2.CorePSSMapper{}, etcdraftv2.DecisionProjector{}, etcdraftv2.WorkloadRouter{},
	)
	if err != nil {
		return portableM522bTargetExecution{}, err
	}
	if err := bundle.ValidateProjection(etcdraftv2.DecisionProjector{}); err != nil {
		return portableM522bTargetExecution{}, err
	}
	outcome, err := controlexperiment.NewIntentOutcome(
		"portable-etcd-outcome-"+stageID, plan, instance, report, bundle,
	)
	if err != nil {
		return portableM522bTargetExecution{}, err
	}
	return portableM522bTargetExecution{
		Intent: intent, Plan: plan, Instance: instance,
		Report: report, Bundle: bundle, Outcome: outcome,
	}, nil
}

func executePortableM522bOmni(
	ctx context.Context,
	inputs portableM522bInputs,
	intent controlexperiment.GuardedTestIntent,
	plan controlexperiment.CompiledIntentPlanV2,
	instance controlexperiment.IntentExecutionInstance,
) (portableM522bTargetExecution, error) {
	return executePortableOmni(ctx, inputs, intent, plan, instance, "m5-22b")
}

func executePortableOmni(
	ctx context.Context,
	inputs portableM522bInputs,
	intent controlexperiment.GuardedTestIntent,
	plan controlexperiment.CompiledIntentPlanV2,
	instance controlexperiment.IntentExecutionInstance,
	stageID string,
) (portableM522bTargetExecution, error) {
	if stageID == "" {
		return portableM522bTargetExecution{}, errors.New("CROSS_TARGET_PORTABLE_STAGE_REQUIRED")
	}
	if err := plan.ValidateInputs(
		inputs.OmniView, inputs.Knowledge, inputs.Catalog,
		inputs.OmniQualification.Manifest, inputs.OmniQualification.Qualification, intent,
	); err != nil {
		return portableM522bTargetExecution{}, err
	}
	admission, err := controlexperiment.BindExecutionAdmission(
		inputs.OmniQualification.Qualification,
		controlexperiment.ExecutionRequirements{Capabilities: plan.RequiredCapabilities},
	)
	if err != nil {
		return portableM522bTargetExecution{}, err
	}
	policy, err := portableM522bPolicy("portable-omni", plan, instance, nil)
	if err != nil {
		return portableM522bTargetExecution{}, err
	}
	requestID := "portable-request-" + stageID
	payload, err := omniadapter.InputPayload(omniadapter.Input{
		RequestID: requestID, Value: []byte("portable-value"),
	})
	if err != nil {
		return portableM522bTargetExecution{}, err
	}
	workload := controlexperiment.WorkloadPlan{
		SchemaVersion: controlexperiment.WorkloadPlanVersion, ID: "portable-single-opaque-input",
		TargetSelector: controlexperiment.TargetSingleCoordinatingMember,
		Invocations: []controlexperiment.WorkloadInvocation{{
			ID: requestID, Input: payload, ExpectedStatus: "decided",
		}},
	}
	envelope := plan.FaultEnvelope
	// The stage-bound seed keeps replays deterministic and preserves the
	// frozen M5.22b-d identity (hex("portable-omni-m5-22b")).
	omniRuntimeSeed := hex.EncodeToString([]byte("portable-omni-" + stageID))
	config := controlexperiment.Config{
		SchemaVersion: controlexperiment.SchemaVersionV2,
		ID:            "portable-omni-target-" + stageID, PSSID: omniadapter.CorePSSMappingID,
		WorkloadRouterID: omniadapter.WorkloadRouterID,
		Runtime: controlexperiment.RuntimeConfig{
			SeedHex: omniRuntimeSeed, MaxClones: 1,
		},
		Admission: &admission, FaultEnvelope: &envelope,
		DecisionsPerRun: plan.Decisions, RequireReplay: true,
		Runs: []controlexperiment.RunPlan{{
			Run: 1, StopAfterWorkload: true,
			Policy: policy, Workload: &workload,
		}},
	}
	var opened []*omniadapter.Adapter
	factory := func() (control.Adapter, error) {
		adapter, factoryErr := omniadapter.New(omniadapter.Config{WorkerPath: inputs.OmniWorkerPath})
		if factoryErr == nil {
			opened = append(opened, adapter)
		}
		return adapter, factoryErr
	}
	defer func() {
		for _, adapter := range opened {
			_ = adapter.Close()
		}
	}()
	report, bundle, err := controlexperiment.ExecuteQualifiedBundle(
		ctx, config, inputs.OmniQualification, factory,
		omniadapter.CorePSSMapper{}, omniadapter.DecisionProjector{}, omniadapter.WorkloadRouter{},
	)
	if err != nil {
		return portableM522bTargetExecution{}, err
	}
	if err := bundle.ValidateProjection(omniadapter.DecisionProjector{}); err != nil {
		return portableM522bTargetExecution{}, err
	}
	outcome, err := controlexperiment.NewIntentOutcome(
		"portable-omni-outcome-"+stageID, plan, instance, report, bundle,
	)
	if err != nil {
		return portableM522bTargetExecution{}, err
	}
	return portableM522bTargetExecution{
		Intent: intent, Plan: plan, Instance: instance,
		Report: report, Bundle: bundle, Outcome: outcome,
	}, nil
}
