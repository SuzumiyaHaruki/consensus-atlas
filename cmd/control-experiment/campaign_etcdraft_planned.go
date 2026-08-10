package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

type etcdraftPlannedCampaignProvider struct {
	base      etcdraftCampaignProvider
	recovered *controlexperiment.CampaignRecovery
	planner   func(controlexperiment.CampaignPlannerView) (controlexperiment.GuardedTestIntent, error)
}

func newEtcdraftPlannedCampaignProvider(
	base etcdraftCampaignProvider,
	recovered *controlexperiment.CampaignRecovery,
) etcdraftPlannedCampaignProvider {
	return etcdraftPlannedCampaignProvider{
		base: base, recovered: recovered, planner: controlexperiment.PlanDeterministicCampaignFixture,
	}
}

func (provider etcdraftPlannedCampaignProvider) Attempt(
	ctx context.Context,
	request controlexperiment.CampaignAttemptRequest,
) (controlexperiment.CampaignAttemptResult, error) {
	planned, err := provider.prepare(request)
	if err != nil {
		return controlexperiment.CampaignAttemptResult{}, err
	}
	bound := provider.base
	bound.intent, bound.plan, bound.plannedAttemptDigest = planned.Proposal, planned.Plan, planned.Digest
	return bound.Attempt(ctx, request)
}

func (provider etcdraftPlannedCampaignProvider) prepare(
	request controlexperiment.CampaignAttemptRequest,
) (controlexperiment.CampaignPlannedAttempt, error) {
	if provider.recovered == nil || provider.planner == nil {
		return controlexperiment.CampaignPlannedAttempt{}, errors.New("ETCDRAFT_CAMPAIGN_PLANNER_REQUIRED")
	}
	if count := len(provider.recovered.PlannedAttempts); count == provider.recovered.Head.Sequence+1 {
		planned := provider.recovered.PlannedAttempts[count-1]
		return planned, planned.ValidateRequest(request)
	}
	observation, err := newEtcdraftCampaignObservation(provider.recovered, provider.base)
	if err != nil {
		return controlexperiment.CampaignPlannedAttempt{}, err
	}
	baseline, err := provider.base.baseline()
	if err != nil {
		return controlexperiment.CampaignPlannedAttempt{}, err
	}
	view, err := controlexperiment.NewCampaignPlannerView(
		fmt.Sprintf("etcdraft-planner-view-%d", request.Ordinal), provider.base.inputs.View,
		observation, request, baseline,
	)
	if err != nil {
		return controlexperiment.CampaignPlannedAttempt{}, err
	}
	proposal, err := provider.planner(view)
	if err != nil {
		return controlexperiment.CampaignPlannedAttempt{}, err
	}
	plan, err := controlexperiment.CompileGuardedTestIntentV2(
		provider.base.inputs.View, provider.base.inputs.Knowledge, provider.base.inputs.Catalog,
		provider.base.inputs.Qualification.Manifest, provider.base.inputs.Qualification.Qualification, proposal,
	)
	if err != nil {
		return controlexperiment.CampaignPlannedAttempt{}, err
	}
	seed, err := provider.base.spec.seed(request.Ordinal)
	if err != nil {
		return controlexperiment.CampaignPlannedAttempt{}, err
	}
	bound := provider.base
	bound.intent, bound.plan = proposal, plan
	instance, err := bound.executionInstance(request.Ordinal, seed)
	if err != nil {
		return controlexperiment.CampaignPlannedAttempt{}, err
	}
	choice, err := controlexperiment.NewCampaignExecutionChoice(proposal, plan, instance)
	if err != nil {
		return controlexperiment.CampaignPlannedAttempt{}, err
	}
	planned, err := controlexperiment.NewCampaignPlannedAttempt(
		fmt.Sprintf("etcdraft-planned-attempt-%d", request.Ordinal),
		view, proposal, plan, instance, choice, controlexperiment.ModelWork{},
	)
	if err != nil {
		return controlexperiment.CampaignPlannedAttempt{}, err
	}
	return provider.recovered.PreparePlannedAttempt(planned)
}
