package main

import (
	"context"
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	etcdqualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/etcdraftv2"
	omniqualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/omnipaxosv2"
)

// newPortableM522eInputs creates an experiment-only two-backend view without changing the
// frozen M5.22b-d KnowledgePack, catalog, or identities.
func newPortableM522eInputs(
	ctx context.Context,
	omniWorkerPath string,
) (portableM522bInputs, error) {
	if omniWorkerPath == "" {
		return portableM522bInputs{}, errors.New("CROSS_TARGET_M522E_OMNI_WORKER_REQUIRED")
	}
	etcdQualified, err := etcdqualification.Run(ctx)
	if err != nil {
		return portableM522bInputs{}, err
	}
	omniQualified, err := omniqualification.Run(ctx, omniWorkerPath)
	if err != nil {
		return portableM522bInputs{}, err
	}
	knowledge, err := portableM522eKnowledge()
	if err != nil {
		return portableM522bInputs{}, err
	}
	catalog, err := portableM522eCatalog()
	if err != nil {
		return portableM522bInputs{}, err
	}
	etcdView, err := controlexperiment.NewAgentSemanticView(
		"portable-source-a-m5-22e", knowledge, catalog,
		etcdQualified.Manifest, etcdQualified.Qualification,
	)
	if err != nil {
		return portableM522bInputs{}, err
	}
	omniView, err := controlexperiment.NewAgentSemanticView(
		"portable-source-b-m5-22e", knowledge, catalog,
		omniQualified.Manifest, omniQualified.Qualification,
	)
	if err != nil {
		return portableM522bInputs{}, err
	}
	common, err := controlexperiment.NewCrossTargetPlannerView(
		"portable-leader-based-cft-view-m5-22e",
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

func portableM522eKnowledge() (controlexperiment.ProtocolKnowledgePack, error) {
	return controlexperiment.NewProtocolKnowledgePack(controlexperiment.ProtocolKnowledgePack{
		ID: "portable-natural-progress-knowledge-m5-22e", Family: "leader-based-cft",
		Protocol: "portable-control",
		Knowledge: []controlexperiment.KnowledgeStatement{
			{ID: "natural-progress", Text: "Coordination changes arise from natural temporal progress and controlled peer-message delivery."},
			{ID: "opaque-input", Text: "A target-owned workload router submits one opaque input without exposing protocol-specific fields."},
			{ID: "strict-replay", Text: "Every selected common Action must be reproduced by a fresh deterministic replay."},
		},
		Risks: []controlexperiment.ProtocolRisk{{
			ID:                   portableM522bRiskID,
			Summary:              "Exercise one opaque input while natural time and controlled peer delivery advance a leader-based CFT target.",
			RequiredCapabilities: portableM522bCapabilities(), RequiredActions: portableM522bActions(),
			AllowedBackendIDs: []string{portableM522bBackend, portableM522eBackendUniform},
		}},
	})
}

func portableM522eCatalog() (controlexperiment.IntentCompilerCatalog, error) {
	return controlexperiment.NewIntentCompilerCatalog(controlexperiment.IntentCompilerCatalog{
		ID: "portable-bounded-backends-m5-22e",
		Templates: []controlexperiment.IntentBackendTemplate{
			{
				ID: portableM522bBackend, Strategy: portableM522bBackend,
				RequiredCapabilities: portableM522bCapabilities(), SupportedActions: portableM522bActions(),
				MinDecisions: 1, MaxDecisions: 96, FallbackRank: 1,
			},
			{
				ID: portableM522eBackendUniform, Strategy: portableM522eBackendUniform,
				RequiredCapabilities: portableM522bCapabilities(), SupportedActions: portableM522bActions(),
				MinDecisions: 1, MaxDecisions: 96, FallbackRank: 2,
			},
		},
	})
}

func planPortableM522eTargets(
	inputs portableM522bInputs,
	preferredBackend string,
	policySeed uint64,
) (portableM522bPlanning, error) {
	if preferredBackend != portableM522bBackend && preferredBackend != portableM522eBackendUniform {
		return portableM522bPlanning{}, errors.New("CROSS_TARGET_M522E_BACKEND_UNKNOWN")
	}
	intent, err := controlexperiment.NewGuardedTestIntent(controlexperiment.GuardedTestIntent{
		ID: "portable-natural-progress-m5-22e", ViewDigest: inputs.CommonView.Digest,
		RiskID: portableM522bRiskID, Must: controlexperiment.IntentMust{Decisions: 96},
		Prefer: controlexperiment.IntentPrefer{BackendIDs: []string{preferredBackend}},
	})
	if err != nil {
		return portableM522bPlanning{}, err
	}
	return planPortableTargetsWithIntent(inputs, intent, "m5-22e", policySeed)
}

func executePortableM522eTargets(
	ctx context.Context,
	inputs portableM522bInputs,
	planning portableM522bPlanning,
) (portableM522bTargetExecution, portableM522bTargetExecution, error) {
	etcd, err := executePortableEtcd(
		ctx, inputs, planning.EtcdIntent, planning.EtcdPlan, planning.EtcdInstance,
		[]control.ActionKind{control.ActionCompleteEffect}, "m5-22e",
	)
	if err != nil {
		return portableM522bTargetExecution{}, portableM522bTargetExecution{}, err
	}
	omni, err := executePortableOmni(
		ctx, inputs, planning.OmniIntent, planning.OmniPlan, planning.OmniInstance, "m5-22e",
	)
	if err != nil {
		return portableM522bTargetExecution{}, portableM522bTargetExecution{}, err
	}
	return etcd, omni, nil
}
