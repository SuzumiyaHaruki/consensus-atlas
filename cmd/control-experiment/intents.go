package main

import (
	"context"
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	qualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/etcdraftv2"
)

const (
	etcdraftIntentRiskLeaderChange = "leader-change-with-inflight-proposal"
	etcdraftBackendFixed           = "fixed-progress"
	etcdraftBackendActionClass     = "action-class-random"
	etcdraftBackendUniform         = "admissible-uniform"
)

type etcdraftIntentInputs struct {
	Knowledge     controlexperiment.ProtocolKnowledgePack
	Catalog       controlexperiment.IntentCompilerCatalog
	View          controlexperiment.AgentSemanticView
	Qualification qualification.Bundle
}

func newEtcdraftIntentInputs(ctx context.Context) (etcdraftIntentInputs, error) {
	return newEtcdraftIntentInputsWithCatalog(ctx, false)
}

func newEtcdraftFeedbackIntentInputs(ctx context.Context) (etcdraftIntentInputs, error) {
	return newEtcdraftIntentInputsWithCatalog(ctx, true)
}

func newEtcdraftIntentInputsWithCatalog(
	ctx context.Context,
	feedbackV2 bool,
) (etcdraftIntentInputs, error) {
	qualified, err := qualification.Run(ctx)
	if err != nil {
		return etcdraftIntentInputs{}, err
	}
	knowledge, err := etcdraftProtocolKnowledge()
	if err != nil {
		return etcdraftIntentInputs{}, err
	}
	var catalog controlexperiment.IntentCompilerCatalog
	if feedbackV2 {
		catalog, err = etcdraftFeedbackIntentCatalog(qualified.Profile)
	} else {
		catalog, err = etcdraftIntentCatalog(qualified.Profile)
	}
	if err != nil {
		return etcdraftIntentInputs{}, err
	}
	viewID := "etcdraft-one-shot-view-m5-18b0"
	if feedbackV2 {
		viewID = "etcdraft-feedback-view-m5-18b3"
	}
	view, err := controlexperiment.NewAgentSemanticView(
		viewID, knowledge, catalog,
		qualified.Manifest, qualified.Qualification,
	)
	if err != nil {
		return etcdraftIntentInputs{}, err
	}
	return etcdraftIntentInputs{
		Knowledge: knowledge, Catalog: catalog, View: view, Qualification: qualified,
	}, nil
}

func etcdraftProtocolKnowledge() (controlexperiment.ProtocolKnowledgePack, error) {
	return controlexperiment.NewProtocolKnowledgePack(controlexperiment.ProtocolKnowledgePack{
		ID: "etcdraft-public-knowledge-m5-18b0", Family: "raft", Protocol: "etcdraft",
		Knowledge: []controlexperiment.KnowledgeStatement{
			{ID: "natural-election", Text: "Leadership changes arise from released periodic pulses and message delivery, not a forced leader-change command."},
			{ID: "runtime-owned-messages", Text: "Released peer messages remain Runtime-owned across controlled node lifecycle transitions until delivery or drop."},
			{ID: "quorum-log-agreement", Text: "Committed commands at the same log position must agree across participants."},
		},
		Risks: []controlexperiment.ProtocolRisk{{
			ID:      etcdraftIntentRiskLeaderChange,
			Summary: "Exercise an opaque proposal while natural temporal progress, peer delivery, and crash/restart may change the coordinating participant.",
			RequiredCapabilities: []string{
				"crash-restart-incarnation", "natural-temporal-progress", "opaque-invoke-boundary",
				"runtime-owned-message", "strict-decision-replay",
			},
			RequiredActions: []control.ActionKind{
				control.ActionInvoke, control.ActionDeliverMessage, control.ActionFireTemporal,
				control.ActionCrash, control.ActionRestart,
			},
			AllowedBackendIDs: []string{etcdraftBackendActionClass, etcdraftBackendUniform},
		}},
	})
}

func etcdraftIntentCatalog(
	profile conformance.QualificationProfile,
) (controlexperiment.IntentCompilerCatalog, error) {
	return etcdraftIntentCatalogWithActionStrategy(
		profile, "etcdraft-qualified-backends-m5-18b0", "workload-action-class-random",
	)
}

func etcdraftFeedbackIntentCatalog(
	profile conformance.QualificationProfile,
) (controlexperiment.IntentCompilerCatalog, error) {
	return etcdraftIntentCatalogWithActionStrategy(
		profile, "etcdraft-qualified-backends-m5-18b3", "workload-action-class-random-v2",
	)
}

func etcdraftIntentCatalogWithActionStrategy(
	profile conformance.QualificationProfile,
	catalogID string,
	actionStrategy string,
) (controlexperiment.IntentCompilerCatalog, error) {
	fullEnvelope := controlexperiment.FaultEnvelope{
		MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
		MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
	}
	progressActions := []control.ActionKind{
		control.ActionInvoke, control.ActionCompleteEffect,
		control.ActionDeliverMessage, control.ActionFireTemporal,
	}
	searchActions := append(append([]control.ActionKind(nil), progressActions...),
		control.ActionCrash, control.ActionRestart, control.ActionDropMessage,
		control.ActionDuplicateMessage, control.ActionPartition, control.ActionHeal,
	)
	required := profile.RequiredCapabilityIDs()
	return controlexperiment.NewIntentCompilerCatalog(controlexperiment.IntentCompilerCatalog{
		ID: catalogID,
		Templates: []controlexperiment.IntentBackendTemplate{
			{
				ID: etcdraftBackendFixed, Strategy: "workload", PolicySeed: 1,
				RequiredCapabilities: required, SupportedActions: progressActions,
				MinDecisions: 1, MaxDecisions: 96, MaxFaultEnvelope: fullEnvelope, FallbackRank: 1,
			},
			{
				ID: etcdraftBackendActionClass, Strategy: actionStrategy, PolicySeed: 1,
				RequiredCapabilities: required, SupportedActions: searchActions,
				MinDecisions: 1, MaxDecisions: 96, MaxFaultEnvelope: fullEnvelope, FallbackRank: 2,
			},
			{
				ID: etcdraftBackendUniform, Strategy: "workload-admissible-uniform", PolicySeed: 1,
				RequiredCapabilities: required, SupportedActions: searchActions,
				MinDecisions: 1, MaxDecisions: 96, MaxFaultEnvelope: fullEnvelope, FallbackRank: 3,
			},
		},
	})
}

// executeEtcdraftCompiledIntent is the only b0 consumer. It re-compiles the
// Agent output from trusted inputs, then enters the existing qualified bundle
// executor; it is not a second scheduler.
func executeEtcdraftCompiledIntent(
	ctx context.Context,
	inputs etcdraftIntentInputs,
	intent controlexperiment.GuardedTestIntent,
	plan controlexperiment.CompiledIntentPlan,
) (controlexperiment.Report, controlexperiment.ExecutionBundle, error) {
	if err := plan.ValidateInputs(
		inputs.View, inputs.Knowledge, inputs.Catalog, inputs.Qualification.Manifest,
		inputs.Qualification.Qualification, intent,
	); err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	report, bundle, err := etcdraftExecution(
		ctx, plan.Strategy, plan.Decisions, plan.PolicySeed, true,
	)
	if err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	if len(report.Config.Runs) != 1 {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{},
			errors.New("ETCDRAFT_COMPILED_INTENT_RUN_COUNT_MISMATCH")
	}
	admission := report.Config.Admission
	if admission == nil || admission.ProfileID != inputs.View.ProfileID ||
		admission.ProfileDigest != inputs.View.ProfileDigest || admission.AdapterID != inputs.View.AdapterID {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{},
			errors.New("ETCDRAFT_COMPILED_INTENT_ADMISSION_MISMATCH")
	}
	expectedPolicy := map[string]string{
		"workload":                        controlexperiment.PolicyVersion,
		"workload-action-class-random":    controlexperiment.ActionClassPolicyVersion,
		"workload-action-class-random-v2": controlexperiment.ActionClassPolicyVersion,
		"workload-admissible-uniform":     controlexperiment.AdmissibleUniformPolicyVersion,
	}[plan.Strategy]
	policy := report.Config.Runs[0].Policy
	if expectedPolicy == "" || policy.Version != expectedPolicy ||
		(plan.Strategy != "workload" && policy.SeedHex != randomPolicySeed(plan.PolicySeed, 1)) {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{},
			errors.New("ETCDRAFT_COMPILED_INTENT_POLICY_MISMATCH")
	}
	if err := bundle.ValidateProjection(etcdraftv2.DecisionProjector{}); err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	if err := plan.ValidateExecution(report, bundle); err != nil {
		return controlexperiment.Report{}, controlexperiment.ExecutionBundle{}, err
	}
	return report, bundle, nil
}
