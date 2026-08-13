package main

import (
	"context"
	"errors"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	etcdraftSemanticTestingPassed    = "passed"
	etcdraftSemanticTestingViolation = "violation"
)

// etcdraftSemanticTestingResult is a target-local composition view over
// existing trusted artifacts. It is not a second execution ledger: exact
// decisions, PSS samples, Replay evidence and qualifications stay in Bundle.
type etcdraftSemanticTestingResult struct {
	SelectedCandidateID string                            `json:"selected_candidate_id"`
	SelectedWorkItem    string                            `json:"selected_work_item_digest"`
	SelectedPrefix      string                            `json:"selected_prefix_digest"`
	Bundle              controlexperiment.ExecutionBundle `json:"execution_bundle"`
	Risk                semantic.RiskWitnessResult        `json:"risk"`
	CorePSSSamples      int                               `json:"core_pss_samples"`
	UniqueCorePSSStates int                               `json:"unique_core_pss_states"`
	Replay              controlexperiment.ReplayResult    `json:"replay"`
	Oracle              oracle.Result                     `json:"oracle"`
	Outcome             string                            `json:"outcome"`
}

func executeEtcdraftSemanticTesting(
	ctx context.Context,
	inputs etcdraftSemanticCalibrationInputs,
	explorer controlexperiment.SemanticExplorerResult,
) (etcdraftSemanticTestingResult, error) {
	selectedID, candidate, item, err := etcdraftSemanticSelected(explorer.Search)
	if err != nil {
		return etcdraftSemanticTestingResult{}, err
	}
	_, bundle, err := executeEtcdraftStatelessPrefix(
		ctx, "etcdraft-semantic-a3-selected-prefix", explorer.Search.Search,
		inputs.root, item, inputs.experiment.Runtime, inputs.experiment.AdapterConfig,
		inputs.experiment.faultEnvelope(),
		inputs.campaign.qualification, inputs.campaign.admission, inputs.campaign.workload,
	)
	if err != nil {
		return etcdraftSemanticTestingResult{}, err
	}
	risk, err := (etcdraftSemanticPrefixProjector{}).Project(
		candidate.RiskResult.ID, inputs.riskSpec, bundle.Trace,
	)
	if err != nil {
		return etcdraftSemanticTestingResult{}, err
	}
	verdict := oracle.CheckBundle(bundle, oracle.BundleTraceIntegrity{}, oracle.BundleAgreement{})
	outcome := etcdraftSemanticTestingPassed
	if len(verdict.Violations) > 0 {
		outcome = etcdraftSemanticTestingViolation
	}
	result := etcdraftSemanticTestingResult{
		SelectedCandidateID: selectedID, SelectedWorkItem: item.Digest,
		SelectedPrefix: item.ChildPrefixDigest, Bundle: bundle, Risk: risk,
		CorePSSSamples: bundle.Run.CorePSSSamples, UniqueCorePSSStates: bundle.Run.UniqueCoreStates,
		Replay: bundle.Run.Replay, Oracle: verdict, Outcome: outcome,
	}
	if err := result.Validate(inputs, explorer); err != nil {
		return etcdraftSemanticTestingResult{}, err
	}
	return result, nil
}

func (result etcdraftSemanticTestingResult) Validate(
	inputs etcdraftSemanticCalibrationInputs,
	explorer controlexperiment.SemanticExplorerResult,
) error {
	if explorer.Validate(inputs.root, inputs.riskSpec) != nil {
		return errors.New("ETCDRAFT_SEMANTIC_TESTING_EXPLORER_INVALID")
	}
	selectedID, candidate, item, err := etcdraftSemanticSelected(explorer.Search)
	if err != nil || result.SelectedCandidateID != selectedID ||
		result.SelectedWorkItem != item.Digest || result.SelectedPrefix != item.ChildPrefixDigest ||
		result.Bundle.Validate() != nil ||
		result.Bundle.ValidateProjection(etcdraftv2.DecisionProjector{}) != nil ||
		result.Bundle.Trace.Digest != item.ChildPrefixDigest ||
		!reflect.DeepEqual(result.Bundle.Qualification, inputs.campaign.qualification) ||
		result.CorePSSSamples != result.Bundle.Run.CorePSSSamples ||
		result.UniqueCorePSSStates != result.Bundle.Run.UniqueCoreStates ||
		result.Replay != result.Bundle.Run.Replay || !result.Replay.Required || !result.Replay.Stable {
		return errors.New("ETCDRAFT_SEMANTIC_TESTING_EXECUTION_MISMATCH")
	}
	wantRisk, err := (etcdraftSemanticPrefixProjector{}).Project(
		candidate.RiskResult.ID, inputs.riskSpec, result.Bundle.Trace,
	)
	if err != nil || !reflect.DeepEqual(wantRisk, candidate.RiskResult) ||
		!reflect.DeepEqual(result.Risk, wantRisk) {
		return errors.New("ETCDRAFT_SEMANTIC_TESTING_RISK_MISMATCH")
	}
	wantOracle := oracle.CheckBundle(
		result.Bundle, oracle.BundleTraceIntegrity{}, oracle.BundleAgreement{},
	)
	wantOutcome := etcdraftSemanticTestingPassed
	if len(wantOracle.Violations) > 0 {
		wantOutcome = etcdraftSemanticTestingViolation
	}
	if !reflect.DeepEqual(result.Oracle, wantOracle) || result.Outcome != wantOutcome {
		return errors.New("ETCDRAFT_SEMANTIC_TESTING_ORACLE_MISMATCH")
	}
	return nil
}

func etcdraftSemanticSelected(
	search controlexperiment.SemanticBestFirstResult,
) (string, controlexperiment.SemanticCandidateRecord, controlexperiment.StatelessDFSWorkItem, error) {
	if len(search.ExpansionOrder) == 0 {
		return "", controlexperiment.SemanticCandidateRecord{},
			controlexperiment.StatelessDFSWorkItem{}, errors.New("ETCDRAFT_SEMANTIC_TESTING_SELECTION_MISSING")
	}
	selectedID := search.ExpansionOrder[0]
	for index, candidate := range search.Candidates {
		if candidate.CandidateID == selectedID {
			return selectedID, candidate, search.Search.Items[index], nil
		}
	}
	return "", controlexperiment.SemanticCandidateRecord{},
		controlexperiment.StatelessDFSWorkItem{}, errors.New("ETCDRAFT_SEMANTIC_TESTING_SELECTION_UNKNOWN")
}
