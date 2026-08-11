package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	portableM522dPromptVersion = "cross-target-preference-only-v1"
	portableM522dModelTokens   = 100
)

type portableM522dCampaignPair struct {
	Etcd portableM522cCampaign
	Omni portableM522cCampaign
}

type portableM522dArm struct {
	Planning    portableM522bPlanning
	Etcd        portableM522bTargetExecution
	Omni        portableM522bTargetExecution
	Campaigns   portableM522dCampaignPair
	Ledger      controlexperiment.CrossTargetCampaignLedger
	ModelCall   *controlexperiment.CampaignModelCallIntent
	ModelResult *controlexperiment.CampaignModelCallResult
	Request     *deepSeekPreparedRequest
}

type portableM522dResult struct {
	Inputs   portableM522bInputs
	Baseline portableM522dArm
	Agent    portableM522dArm
}

func runPortableM522dPreferenceComparison(
	ctx context.Context,
	omniWorkerPath string,
	directory string,
) (portableM522dResult, error) {
	if directory == "" {
		return portableM522dResult{}, errors.New("CROSS_TARGET_M522D_DIRECTORY_REQUIRED")
	}
	inputs, err := newPortableM522bInputs(ctx, omniWorkerPath)
	if err != nil {
		return portableM522dResult{}, err
	}
	baselinePlanning, err := planPortableM522bTargets(inputs)
	if err != nil {
		return portableM522dResult{}, err
	}
	baselineCampaigns, err := newPortableM522dCampaignPair(
		filepath.Join(directory, "baseline"), "baseline", inputs, baselinePlanning, 0,
	)
	if err != nil {
		return portableM522dResult{}, err
	}
	if err := preparePortableM522dPair(
		&baselineCampaigns, "m5-22d-baseline", inputs, baselinePlanning,
		controlexperiment.ModelWork{},
	); err != nil {
		return portableM522dResult{}, err
	}
	baseline, err := executePortableM522dArm(
		ctx, "baseline", inputs, baselinePlanning, baselineCampaigns,
	)
	if err != nil {
		return portableM522dResult{}, err
	}

	agentCampaigns, err := newPortableM522dCampaignPair(
		filepath.Join(directory, "agent"), "agent", inputs, baselinePlanning, portableM522dModelTokens,
	)
	if err != nil {
		return portableM522dResult{}, err
	}
	proposal, prepared, callIntent, callResult, err := resolvePortableM522dMockProposal(
		inputs, baselinePlanning.Intent, &agentCampaigns.Etcd,
	)
	if err != nil {
		return portableM522dResult{}, err
	}
	agentPlanning, err := planPortableM522bTargetsWithIntent(inputs, proposal)
	if err != nil {
		return portableM522dResult{}, err
	}
	if err := preparePortableM522dPair(
		&agentCampaigns, "m5-22d-agent", inputs, agentPlanning, callResult.Work,
	); err != nil {
		return portableM522dResult{}, err
	}
	agent, err := executePortableM522dArm(ctx, "agent", inputs, agentPlanning, agentCampaigns)
	if err != nil {
		return portableM522dResult{}, err
	}
	agent.ModelCall, agent.ModelResult, agent.Request = &callIntent, &callResult, &prepared
	return portableM522dResult{Inputs: inputs, Baseline: baseline, Agent: agent}, nil
}

func newPortableM522dCampaignPair(
	directory string,
	armID string,
	inputs portableM522bInputs,
	planning portableM522bPlanning,
	modelTokens int,
) (portableM522dCampaignPair, error) {
	etcdIdentity, err := inputs.EtcdQualification.Manifest.Digest()
	if err != nil {
		return portableM522dCampaignPair{}, err
	}
	omniIdentity, err := inputs.OmniQualification.Manifest.Digest()
	if err != nil {
		return portableM522dCampaignPair{}, err
	}
	etcd, err := newPortableM522cCampaign(
		filepath.Join(directory, "target-a"), "portable-"+armID+"-target-a-m5-22d", "target-a",
		etcdIdentity, inputs.CommonView, planning.EtcdPlan, planning.EtcdInstance, modelTokens,
	)
	if err != nil {
		return portableM522dCampaignPair{}, err
	}
	omni, err := newPortableM522cCampaign(
		filepath.Join(directory, "target-b"), "portable-"+armID+"-target-b-m5-22d", "target-b",
		omniIdentity, inputs.CommonView, planning.OmniPlan, planning.OmniInstance, 0,
	)
	if err != nil {
		return portableM522dCampaignPair{}, err
	}
	return portableM522dCampaignPair{Etcd: etcd, Omni: omni}, nil
}

func preparePortableM522dPair(
	pair *portableM522dCampaignPair,
	identitySuffix string,
	inputs portableM522bInputs,
	planning portableM522bPlanning,
	ownerWork controlexperiment.ModelWork,
) error {
	if pair == nil {
		return errors.New("CROSS_TARGET_M522D_CAMPAIGN_PAIR_REQUIRED")
	}
	if err := preparePortableM522cPlannedAttempt(
		&pair.Etcd, identitySuffix, inputs.EtcdView, planning.EtcdIntent,
		planning.EtcdPlan, planning.EtcdInstance, ownerWork,
	); err != nil {
		return err
	}
	return preparePortableM522cPlannedAttempt(
		&pair.Omni, identitySuffix, inputs.OmniView, planning.OmniIntent,
		planning.OmniPlan, planning.OmniInstance, controlexperiment.ModelWork{},
	)
}

func executePortableM522dArm(
	ctx context.Context,
	armID string,
	inputs portableM522bInputs,
	planning portableM522bPlanning,
	campaigns portableM522dCampaignPair,
) (portableM522dArm, error) {
	etcd, err := executePortableM522bEtcd(
		ctx, inputs, planning.EtcdIntent, planning.EtcdPlan, planning.EtcdInstance,
		[]control.ActionKind{control.ActionCompleteEffect},
	)
	if err != nil {
		return portableM522dArm{}, err
	}
	if err := commitPortableM522cCampaign(
		&campaigns.Etcd, inputs.CommonView, planning.Intent, etcd,
	); err != nil {
		return portableM522dArm{}, err
	}
	omni, err := executePortableM522bOmni(
		ctx, inputs, planning.OmniIntent, planning.OmniPlan, planning.OmniInstance,
	)
	if err != nil {
		return portableM522dArm{}, err
	}
	if err := commitPortableM522cCampaign(
		&campaigns.Omni, inputs.CommonView, planning.Intent, omni,
	); err != nil {
		return portableM522dArm{}, err
	}
	campaigns.Etcd.Recovery, err = controlexperiment.RecoverCampaignDirectory(
		campaigns.Etcd.Directory, campaigns.Etcd.Config,
	)
	if err != nil {
		return portableM522dArm{}, err
	}
	campaigns.Omni.Recovery, err = controlexperiment.RecoverCampaignDirectory(
		campaigns.Omni.Directory, campaigns.Omni.Config,
	)
	if err != nil {
		return portableM522dArm{}, err
	}
	sources := portableM522cSources(&campaigns.Etcd, &campaigns.Omni)
	ledger, err := controlexperiment.NewCrossTargetCampaignLedger(
		"portable-"+armID+"-ledger-m5-22d", inputs.CommonView, planning.Intent, sources,
	)
	if err != nil {
		return portableM522dArm{}, err
	}
	return portableM522dArm{
		Planning: planning, Etcd: etcd, Omni: omni, Campaigns: campaigns, Ledger: ledger,
	}, nil
}

func resolvePortableM522dMockProposal(
	inputs portableM522bInputs,
	baseline controlexperiment.GuardedTestIntent,
	owner *portableM522cCampaign,
) (
	controlexperiment.GuardedTestIntent,
	deepSeekPreparedRequest,
	controlexperiment.CampaignModelCallIntent,
	controlexperiment.CampaignModelCallResult,
	error,
) {
	emptyIntent, emptyRequest := controlexperiment.GuardedTestIntent{}, deepSeekPreparedRequest{}
	emptyCall, emptyResult := controlexperiment.CampaignModelCallIntent{}, controlexperiment.CampaignModelCallResult{}
	if owner == nil {
		return emptyIntent, emptyRequest, emptyCall, emptyResult,
			errors.New("CROSS_TARGET_M522D_MODEL_OWNER_REQUIRED")
	}
	contract, err := controlexperiment.NewCrossTargetPlannerProposalContract(inputs.CommonView, baseline)
	if err != nil {
		return emptyIntent, emptyRequest, emptyCall, emptyResult, err
	}
	contractJSON, err := contract.MarshalIndent()
	if err != nil {
		return emptyIntent, emptyRequest, emptyCall, emptyResult, err
	}
	viewJSON, err := json.MarshalIndent(inputs.CommonView, "", "  ")
	if err != nil {
		return emptyIntent, emptyRequest, emptyCall, emptyResult, err
	}
	client := defaultDeepSeekIntentClient()
	prepared, err := client.prepare(
		"Return exactly one preference-only GuardedTestIntent JSON object and no prose. "+
			"Copy proposal_template exactly, changing only prefer.backend_ids and prefer.actions.",
		"Planner schema "+portableM522dPromptVersion+".\nTrusted output contract JSON:\n"+
			string(contractJSON)+"\nCrossTargetPlannerView JSON:\n"+string(viewJSON),
	)
	if err != nil {
		return emptyIntent, emptyRequest, emptyCall, emptyResult, err
	}
	request, err := controlexperiment.NewCampaignAttemptRequest(owner.Config, owner.Recovery.Head)
	if err != nil {
		return emptyIntent, emptyRequest, emptyCall, emptyResult, err
	}
	freeze := controlexperiment.AgentTransportFreeze{
		Provider: deepSeekProvider, Endpoint: client.Endpoint, Model: client.Model,
		Thinking: "disabled", Temperature: 0, MaxOutputTokens: client.MaxOutputTokens,
		MaxCallsPerArm: 1, MaxRetries: 0,
	}
	callIntent, err := controlexperiment.NewCampaignModelCallIntent(
		"cross-target-model-call-1", request, inputs.CommonView.Digest, freeze,
		prepared.PromptBytes, prepared.RequestBytes,
	)
	if err != nil {
		return emptyIntent, emptyRequest, emptyCall, emptyResult, err
	}
	if _, err := owner.Recovery.PrepareModelCall(callIntent); err != nil {
		return emptyIntent, emptyRequest, emptyCall, emptyResult, err
	}
	dispatch, err := owner.Recovery.DispatchModelCall()
	if err != nil {
		return emptyIntent, emptyRequest, emptyCall, emptyResult, err
	}
	mock := baseline
	mock.Prefer = controlexperiment.IntentPrefer{
		BackendIDs: []string{portableM522bBackend},
		Actions: []control.ActionKind{
			control.ActionFireTemporal, control.ActionDeliverMessage, control.ActionInvoke,
		},
	}
	mock.Digest = ""
	content, err := json.Marshal(mock)
	if err != nil {
		return emptyIntent, emptyRequest, emptyCall, emptyResult, err
	}
	callResult, err := controlexperiment.NewCampaignModelCallResult(
		callIntent, dispatch, controlexperiment.CampaignModelCallResult{
			Status: controlexperiment.CampaignModelCallCompleted, Content: content,
			ResponseDigest: controlexperiment.AgentInvocationDigest([]byte("offline-cross-target-m5-22d")),
			Response: &controlexperiment.AgentResponseIdentity{
				ID: "offline-cross-target-m5-22d", Model: deepSeekV4Flash, FinishReason: "stop",
			},
			DurationMillis: 1,
			Work: controlexperiment.ModelWork{
				Calls: 1, InputTokens: 4, OutputTokens: 3, TotalTokens: 7,
			},
		},
	)
	if err != nil {
		return emptyIntent, emptyRequest, emptyCall, emptyResult, err
	}
	callResult, err = owner.Recovery.CommitModelCallResult(callResult)
	if err != nil {
		return emptyIntent, emptyRequest, emptyCall, emptyResult, err
	}
	proposal, err := controlexperiment.ParseGuardedTestIntentProposal(callResult.Content)
	if err != nil {
		return emptyIntent, emptyRequest, emptyCall, emptyResult, err
	}
	if err := controlexperiment.ValidateCrossTargetPlannerProposal(
		inputs.CommonView, baseline, proposal,
	); err != nil {
		return emptyIntent, emptyRequest, emptyCall, emptyResult, err
	}
	return proposal, prepared, callIntent, callResult, nil
}
