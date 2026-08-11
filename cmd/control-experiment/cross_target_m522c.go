package main

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

type portableM522cCampaign struct {
	Directory string
	Config    controlexperiment.CampaignConfig
	Recovery  controlexperiment.CampaignRecovery
	Planned   controlexperiment.CampaignPlannedAttempt
}

type portableM522cResult struct {
	Inputs    portableM522bInputs
	Planning  portableM522bPlanning
	Etcd      portableM522bTargetExecution
	Omni      portableM522bTargetExecution
	Campaigns map[string]portableM522cCampaign
	Ledger    controlexperiment.CrossTargetCampaignLedger
}

func runPortableM522cDurableCrossTarget(
	ctx context.Context,
	omniWorkerPath string,
	directory string,
) (portableM522cResult, error) {
	if directory == "" {
		return portableM522cResult{}, errors.New("CROSS_TARGET_M522C_DIRECTORY_REQUIRED")
	}
	inputs, err := newPortableM522bInputs(ctx, omniWorkerPath)
	if err != nil {
		return portableM522cResult{}, err
	}
	planning, err := planPortableM522bTargets(inputs)
	if err != nil {
		return portableM522cResult{}, err
	}
	etcdIdentity, err := inputs.EtcdQualification.Manifest.Digest()
	if err != nil {
		return portableM522cResult{}, err
	}
	omniIdentity, err := inputs.OmniQualification.Manifest.Digest()
	if err != nil {
		return portableM522cResult{}, err
	}
	etcdCampaign, err := preparePortableM522cCampaign(
		filepath.Join(directory, "target-a"), "portable-target-a-campaign-m5-22c", "target-a",
		etcdIdentity, inputs.CommonView, inputs.EtcdView,
		planning.EtcdIntent, planning.EtcdPlan, planning.EtcdInstance,
	)
	if err != nil {
		return portableM522cResult{}, err
	}
	omniCampaign, err := preparePortableM522cCampaign(
		filepath.Join(directory, "target-b"), "portable-target-b-campaign-m5-22c", "target-b",
		omniIdentity, inputs.CommonView, inputs.OmniView,
		planning.OmniIntent, planning.OmniPlan, planning.OmniInstance,
	)
	if err != nil {
		return portableM522cResult{}, err
	}

	etcd, err := executePortableM522bEtcd(
		ctx, inputs, planning.EtcdIntent, planning.EtcdPlan, planning.EtcdInstance,
		[]control.ActionKind{control.ActionCompleteEffect},
	)
	if err != nil {
		return portableM522cResult{}, err
	}
	if err := commitPortableM522cCampaign(
		&etcdCampaign, inputs.CommonView, planning.Intent, etcd,
	); err != nil {
		return portableM522cResult{}, err
	}
	omni, err := executePortableM522bOmni(
		ctx, inputs, planning.OmniIntent, planning.OmniPlan, planning.OmniInstance,
	)
	if err != nil {
		return portableM522cResult{}, err
	}
	if err := commitPortableM522cCampaign(
		&omniCampaign, inputs.CommonView, planning.Intent, omni,
	); err != nil {
		return portableM522cResult{}, err
	}

	etcdCampaign.Recovery, err = controlexperiment.RecoverCampaignDirectory(
		etcdCampaign.Directory, etcdCampaign.Config,
	)
	if err != nil {
		return portableM522cResult{}, err
	}
	omniCampaign.Recovery, err = controlexperiment.RecoverCampaignDirectory(
		omniCampaign.Directory, omniCampaign.Config,
	)
	if err != nil {
		return portableM522cResult{}, err
	}
	sources := portableM522cSources(&etcdCampaign, &omniCampaign)
	ledger, err := controlexperiment.NewCrossTargetCampaignLedger(
		"portable-cross-target-ledger-m5-22c", inputs.CommonView, planning.Intent, sources,
	)
	if err != nil {
		return portableM522cResult{}, err
	}
	if err := ledger.ValidateRecoveries(inputs.CommonView, sources); err != nil {
		return portableM522cResult{}, err
	}
	return portableM522cResult{
		Inputs: inputs, Planning: planning, Etcd: etcd, Omni: omni,
		Campaigns: map[string]portableM522cCampaign{"target-a": etcdCampaign, "target-b": omniCampaign},
		Ledger:    ledger,
	}, nil
}

func preparePortableM522cCampaign(
	directory string,
	campaignID string,
	targetID string,
	targetIdentityDigest string,
	common controlexperiment.CrossTargetPlannerView,
	semantic controlexperiment.AgentSemanticView,
	intent controlexperiment.GuardedTestIntent,
	plan controlexperiment.CompiledIntentPlanV2,
	instance controlexperiment.IntentExecutionInstance,
) (portableM522cCampaign, error) {
	campaign, err := newPortableM522cCampaign(
		directory, campaignID, targetID, targetIdentityDigest, common, plan, instance, 0,
	)
	if err != nil {
		return portableM522cCampaign{}, err
	}
	if err := preparePortableM522cPlannedAttempt(
		&campaign, "m5-22c", semantic, intent, plan, instance, controlexperiment.ModelWork{},
	); err != nil {
		return portableM522cCampaign{}, err
	}
	return campaign, nil
}

func newPortableM522cCampaign(
	directory string,
	campaignID string,
	targetID string,
	targetIdentityDigest string,
	common controlexperiment.CrossTargetPlannerView,
	plan controlexperiment.CompiledIntentPlanV2,
	instance controlexperiment.IntentExecutionInstance,
	modelTokenBudget int,
) (portableM522cCampaign, error) {
	modelCalls := 0
	if modelTokenBudget > 0 {
		modelCalls = 1
	}
	config, err := controlexperiment.NewCampaignConfig(
		campaignID, targetID, targetIdentityDigest, common.Digest,
		controlexperiment.CampaignLogicalBudget{
			MaxAttempts: 1, MaxPrimarySchedulerDecisions: plan.Decisions,
			MaxPrimaryWorkUnits: instance.Budget.MaxPrimaryWorkUnits,
			MaxReplayWorkUnits:  instance.Budget.MaxReplayWorkUnits,
			MaxModelCalls:       modelCalls, MaxModelTokens: modelTokenBudget,
		},
		600_000,
	)
	if err != nil {
		return portableM522cCampaign{}, err
	}
	if modelTokenBudget > 0 {
		config, err = controlexperiment.RequireDurableCampaignPlanner(config)
	} else {
		config, err = controlexperiment.RequirePlannedCampaignAttempts(config)
	}
	if err != nil {
		return portableM522cCampaign{}, err
	}
	recovered, err := controlexperiment.CreateCampaignDirectory(directory, config)
	if err != nil {
		return portableM522cCampaign{}, err
	}
	return portableM522cCampaign{Directory: directory, Config: config, Recovery: recovered}, nil
}

func preparePortableM522cPlannedAttempt(
	campaign *portableM522cCampaign,
	identitySuffix string,
	semantic controlexperiment.AgentSemanticView,
	intent controlexperiment.GuardedTestIntent,
	plan controlexperiment.CompiledIntentPlanV2,
	instance controlexperiment.IntentExecutionInstance,
	planningWork controlexperiment.ModelWork,
) error {
	if campaign == nil || identitySuffix == "" {
		return errors.New("CROSS_TARGET_M522C_PLANNED_INPUT_INVALID")
	}
	recovered := &campaign.Recovery
	config := campaign.Config
	summary, err := controlexperiment.NewCampaignSummary(recovered)
	if err != nil {
		return err
	}
	observation, err := controlexperiment.NewCampaignObservation(summary, nil)
	if err != nil {
		return err
	}
	request, err := controlexperiment.NewCampaignAttemptRequest(config, recovered.Head)
	if err != nil {
		return err
	}
	view, err := controlexperiment.NewCampaignPlannerView(
		config.TargetID+"-planner-view-"+identitySuffix, semantic, observation, request, intent,
	)
	if err != nil {
		return err
	}
	choice, err := controlexperiment.NewCampaignExecutionChoice(intent, plan, instance)
	if err != nil {
		return err
	}
	planned, err := controlexperiment.NewCampaignPlannedAttempt(
		config.TargetID+"-planned-attempt-"+identitySuffix, view, intent, plan, instance, choice,
		planningWork,
	)
	if err != nil {
		return err
	}
	if _, err := recovered.PreparePlannedAttempt(planned); err != nil {
		return err
	}
	campaign.Planned = planned
	return nil
}

func commitPortableM522cCampaign(
	campaign *portableM522cCampaign,
	common controlexperiment.CrossTargetPlannerView,
	parent controlexperiment.GuardedTestIntent,
	execution portableM522bTargetExecution,
) error {
	if campaign == nil {
		return errors.New("CROSS_TARGET_M522C_CAMPAIGN_REQUIRED")
	}
	artifact, err := controlexperiment.NewCrossTargetAttemptArtifact(
		common, parent, campaign.Planned, execution.Report, execution.Bundle, execution.Outcome,
	)
	if err != nil {
		return err
	}
	encoded, err := controlexperiment.EncodeCrossTargetAttemptArtifact(artifact)
	if err != nil {
		return err
	}
	work, err := campaign.Planned.AttachPlanningWork(execution.Report.Work)
	if err != nil {
		return err
	}
	record, err := controlexperiment.NewCampaignAttemptRecord(controlexperiment.CampaignAttemptRecord{
		Ordinal: 1, ID: campaign.Config.ID + "-attempt-1", InputDigest: campaign.Planned.Digest,
		ArtifactDigest: controlexperiment.CampaignArtifactDigest(encoded),
		Outcome:        controlexperiment.CampaignAttemptCompleted, Work: work,
	})
	if err != nil {
		return err
	}
	_, err = campaign.Recovery.CommitAttempt(record, encoded, 1)
	return err
}

func portableM522cSources(
	etcd *portableM522cCampaign,
	omni *portableM522cCampaign,
) []controlexperiment.CrossTargetCampaignSource {
	return []controlexperiment.CrossTargetCampaignSource{
		{Slot: "target-a", Campaign: &etcd.Recovery},
		{Slot: "target-b", Campaign: &omni.Recovery},
	}
}
