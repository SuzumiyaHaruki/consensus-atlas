package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	etcdraftCampaignModelPlannerVersion = "consensus-atlas/etcdraft-campaign-model-planner/v2"
	etcdraftCampaignModelPromptVersion  = "campaign-preference-only-v2"
	etcdraftCampaignModelFailed         = "ETCDRAFT_CAMPAIGN_MODEL_CALL_FAILED"
	etcdraftCampaignModelAmbiguous      = "ETCDRAFT_CAMPAIGN_MODEL_CALL_AMBIGUOUS"
)

type etcdraftCampaignModelTransport func(
	context.Context, deepSeekPreparedRequest,
) (deepSeekCall, error)

type etcdraftDurableModelCampaignProvider struct {
	planned   etcdraftPlannedCampaignProvider
	client    deepSeekIntentClient
	freeze    controlexperiment.AgentTransportFreeze
	transport etcdraftCampaignModelTransport
}

func newEtcdraftDurableModelCampaignProvider(
	base etcdraftCampaignProvider,
	recovered *controlexperiment.CampaignRecovery,
	client deepSeekIntentClient,
	transport etcdraftCampaignModelTransport,
) (etcdraftDurableModelCampaignProvider, error) {
	freeze := controlexperiment.AgentTransportFreeze{
		Provider: deepSeekProvider, Endpoint: client.Endpoint, Model: client.Model,
		Thinking: "disabled", Temperature: 0, MaxOutputTokens: client.MaxOutputTokens,
		MaxCallsPerArm: 1, MaxRetries: 0,
	}
	if transport == nil || client.Endpoint != deepSeekChatEndpoint || client.Model != deepSeekV4Flash ||
		client.MaxOutputTokens <= 0 || client.MaxOutputTokens > 4096 {
		return etcdraftDurableModelCampaignProvider{}, errors.New("ETCDRAFT_CAMPAIGN_MODEL_PROVIDER_INVALID")
	}
	identity, err := control.CanonicalDigest(struct {
		SchemaVersion string                                 `json:"schema_version"`
		PromptVersion string                                 `json:"prompt_version"`
		Transport     controlexperiment.AgentTransportFreeze `json:"transport"`
	}{etcdraftCampaignModelPlannerVersion, etcdraftCampaignModelPromptVersion, freeze})
	if err != nil {
		return etcdraftDurableModelCampaignProvider{}, err
	}
	base.plannerIdentity = identity
	base.experimentSpecDigest, err = base.compositionDigest()
	if err != nil || base.validate() != nil {
		return etcdraftDurableModelCampaignProvider{}, errors.New("ETCDRAFT_CAMPAIGN_MODEL_COMPOSITION_INVALID")
	}
	return etcdraftDurableModelCampaignProvider{
		planned: newEtcdraftPlannedCampaignProvider(base, recovered),
		client:  client, freeze: freeze, transport: transport,
	}, nil
}

func (provider etcdraftDurableModelCampaignProvider) campaignConfig(
	id string, attempts int, modelTokensPerAttempt int, wallClockCeilingMillis int64,
) (controlexperiment.CampaignConfig, error) {
	if attempts <= 0 || modelTokensPerAttempt <= 0 {
		return controlexperiment.CampaignConfig{}, errors.New("ETCDRAFT_CAMPAIGN_MODEL_BUDGET_INVALID")
	}
	decisions, ok := checkedCampaignMultiply(attempts, provider.planned.base.spec.DecisionsPerAttempt)
	if !ok {
		return controlexperiment.CampaignConfig{}, errors.New("ETCDRAFT_CAMPAIGN_BUDGET_OVERFLOW")
	}
	work, ok := checkedCampaignMultiply(attempts, provider.planned.base.spec.DecisionsPerAttempt+2)
	if !ok {
		return controlexperiment.CampaignConfig{}, errors.New("ETCDRAFT_CAMPAIGN_BUDGET_OVERFLOW")
	}
	modelTokens, ok := checkedCampaignMultiply(attempts, modelTokensPerAttempt)
	if !ok {
		return controlexperiment.CampaignConfig{}, errors.New("ETCDRAFT_CAMPAIGN_BUDGET_OVERFLOW")
	}
	config, err := controlexperiment.NewCampaignConfig(
		id, etcdraftCampaignTargetID, provider.planned.base.targetIdentityDigest,
		provider.planned.base.experimentSpecDigest,
		controlexperiment.CampaignLogicalBudget{
			MaxAttempts: attempts, MaxPrimarySchedulerDecisions: decisions,
			MaxPrimaryWorkUnits: work, MaxReplayWorkUnits: work,
			MaxModelCalls: attempts, MaxModelTokens: modelTokens,
		}, wallClockCeilingMillis,
	)
	if err != nil {
		return controlexperiment.CampaignConfig{}, err
	}
	return controlexperiment.RequireDurableCampaignPlanner(config)
}

func (provider etcdraftDurableModelCampaignProvider) Attempt(
	ctx context.Context, request controlexperiment.CampaignAttemptRequest,
) (controlexperiment.CampaignAttemptResult, error) {
	planned, err := provider.prepare(ctx, request)
	if err != nil {
		return controlexperiment.CampaignAttemptResult{}, err
	}
	bound := provider.planned.base
	bound.intent, bound.plan, bound.planningWork = planned.Proposal, planned.Plan, planned.PlanningWork
	bound.plannedAttemptDigest = planned.Digest
	return bound.Attempt(ctx, request)
}

func (provider etcdraftDurableModelCampaignProvider) prepare(
	ctx context.Context, request controlexperiment.CampaignAttemptRequest,
) (controlexperiment.CampaignPlannedAttempt, error) {
	recovered := provider.planned.recovered
	if recovered == nil {
		return controlexperiment.CampaignPlannedAttempt{}, errors.New("ETCDRAFT_CAMPAIGN_MODEL_RECOVERY_REQUIRED")
	}
	if count := len(recovered.PlannedAttempts); count == recovered.Head.Sequence+1 {
		planned := recovered.PlannedAttempts[count-1]
		return planned, planned.ValidateRequest(request)
	}
	view, err := provider.planned.plannerView(request)
	if err != nil {
		return controlexperiment.CampaignPlannedAttempt{}, err
	}
	prepared, err := provider.prepareRequest(view)
	if err != nil {
		return controlexperiment.CampaignPlannedAttempt{}, err
	}
	result, err := provider.resolveCall(ctx, request, view, prepared)
	if err != nil {
		if result.Digest != "" {
			err = controlexperiment.NewCampaignModelCallProviderFailure(err, result)
		}
		return controlexperiment.CampaignPlannedAttempt{}, err
	}
	proposal, err := controlexperiment.ParseGuardedTestIntentProposal(result.Content)
	if err != nil {
		return controlexperiment.CampaignPlannedAttempt{},
			controlexperiment.NewCampaignModelCallProviderFailure(err, result)
	}
	if err := controlexperiment.ValidateCampaignPlannerProposal(view, proposal); err != nil {
		return controlexperiment.CampaignPlannedAttempt{},
			controlexperiment.NewCampaignModelCallProviderFailure(err, result)
	}
	planned, err := provider.planned.buildPlannedAttempt(view, proposal, result.Work)
	if err != nil {
		return controlexperiment.CampaignPlannedAttempt{},
			controlexperiment.NewCampaignModelCallProviderFailure(err, result)
	}
	planned, err = recovered.PreparePlannedAttempt(planned)
	if err != nil {
		err = controlexperiment.NewCampaignModelCallProviderFailure(err, result)
	}
	return planned, err
}

// freezeNextCall persists the exact next intent without granting transport authority.
func (provider etcdraftDurableModelCampaignProvider) freezeNextCall(
	request controlexperiment.CampaignAttemptRequest,
) (bool, error) {
	recovered := provider.planned.recovered
	if recovered == nil {
		return false, errors.New("ETCDRAFT_CAMPAIGN_MODEL_RECOVERY_REQUIRED")
	}
	if count := len(recovered.PlannedAttempts); count == recovered.Head.Sequence+1 {
		return false, recovered.PlannedAttempts[count-1].ValidateRequest(request)
	}
	view, err := provider.planned.plannerView(request)
	if err != nil {
		return false, err
	}
	prepared, err := provider.prepareRequest(view)
	if err != nil {
		return false, err
	}
	call, err := provider.prepareCall(request, view, prepared)
	if err != nil {
		return false, err
	}
	return call.Status == controlexperiment.CampaignModelCallPrepared, nil
}

func (provider etcdraftDurableModelCampaignProvider) prepareRequest(
	view controlexperiment.CampaignPlannerView,
) (deepSeekPreparedRequest, error) {
	if err := view.Validate(); err != nil {
		return deepSeekPreparedRequest{}, err
	}
	contract, err := controlexperiment.NewCampaignPlannerProposalContract(view)
	if err != nil {
		return deepSeekPreparedRequest{}, err
	}
	contractJSON, err := contract.MarshalIndent()
	if err != nil {
		return deepSeekPreparedRequest{}, err
	}
	encoded, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return deepSeekPreparedRequest{}, err
	}
	system := "Return exactly one preference-only GuardedTestIntent JSON object and no prose. " +
		"Copy proposal_template exactly, changing only prefer.backend_ids and prefer.actions. " +
		"Both Prefer fields must remain JSON arrays, use only allowed values, and leave digest empty."
	user := "Planner schema " + etcdraftCampaignModelPromptVersion + ".\n" +
		"Trusted output contract JSON:\n" + string(contractJSON) +
		"\nCampaignPlannerView JSON:\n" + string(encoded)
	return provider.client.prepare(system, user)
}

func (provider etcdraftDurableModelCampaignProvider) resolveCall(
	ctx context.Context,
	request controlexperiment.CampaignAttemptRequest,
	view controlexperiment.CampaignPlannerView,
	prepared deepSeekPreparedRequest,
) (controlexperiment.CampaignModelCallResult, error) {
	call, err := provider.prepareCall(request, view, prepared)
	if err != nil {
		return controlexperiment.CampaignModelCallResult{}, err
	}
	if call.Result != nil {
		if call.Result.Status == controlexperiment.CampaignModelCallFailed {
			return *call.Result, errors.New(etcdraftCampaignModelFailed)
		}
		return *call.Result, nil
	}
	if call.Dispatch != nil {
		return controlexperiment.CampaignModelCallResult{}, errors.New(etcdraftCampaignModelAmbiguous)
	}
	dispatch, err := provider.planned.recovered.DispatchModelCall()
	if err != nil {
		return controlexperiment.CampaignModelCallResult{}, err
	}
	raw, transportErr := provider.transport(ctx, prepared)
	result, err := newEtcdraftCampaignModelResult(call.Intent, dispatch, prepared, raw, transportErr)
	if err != nil {
		return controlexperiment.CampaignModelCallResult{}, err
	}
	result, err = provider.planned.recovered.CommitModelCallResult(result)
	if err != nil {
		return controlexperiment.CampaignModelCallResult{}, err
	}
	if result.Status == controlexperiment.CampaignModelCallFailed {
		return result, errors.New(etcdraftCampaignModelFailed)
	}
	return result, nil
}

func (provider etcdraftDurableModelCampaignProvider) prepareCall(
	request controlexperiment.CampaignAttemptRequest,
	view controlexperiment.CampaignPlannerView,
	prepared deepSeekPreparedRequest,
) (controlexperiment.CampaignModelCallRecovery, error) {
	intent, err := controlexperiment.NewCampaignModelCallIntent(
		fmt.Sprintf("etcdraft-model-call-%d", request.Ordinal), request, view.Digest,
		provider.freeze, prepared.PromptBytes, prepared.RequestBytes,
	)
	if err != nil {
		return controlexperiment.CampaignModelCallRecovery{}, err
	}
	if _, err := provider.planned.recovered.PrepareModelCall(intent); err != nil {
		return controlexperiment.CampaignModelCallRecovery{}, err
	}
	return provider.planned.recovered.ModelCalls[len(provider.planned.recovered.ModelCalls)-1], nil
}

func newEtcdraftCampaignModelResult(
	intent controlexperiment.CampaignModelCallIntent,
	dispatch controlexperiment.CampaignModelCallDispatch,
	prepared deepSeekPreparedRequest,
	call deepSeekCall,
	transportErr error,
) (controlexperiment.CampaignModelCallResult, error) {
	result := controlexperiment.CampaignModelCallResult{
		Status: controlexperiment.CampaignModelCallCompleted, Content: call.Content,
		ResponseDigest: call.ResponseDigest, Response: call.Response,
		DurationMillis: call.DurationMillis, Work: call.Work,
	}
	if transportErr != nil {
		result.Status, result.FailureCode = controlexperiment.CampaignModelCallFailed, "agent-transport-failed"
		result.Content, result.Response, result.ResponseDigest = nil, nil, ""
		result.Work = controlexperiment.ModelWork{Calls: 1}
	} else if call.PromptDigest != prepared.PromptDigest || call.RequestDigest != prepared.RequestDigest {
		return controlexperiment.CampaignModelCallResult{}, errors.New("ETCDRAFT_CAMPAIGN_MODEL_RESULT_BINDING_INVALID")
	} else if call.FailureCode != "" {
		result.Status, result.FailureCode = controlexperiment.CampaignModelCallFailed, "agent-response-rejected"
		if call.FailureCode == deepSeekFailureTransport {
			result.FailureCode = "agent-transport-failed"
		}
		result.Content, result.Response = nil, nil
	}
	return controlexperiment.NewCampaignModelCallResult(intent, dispatch, result)
}
