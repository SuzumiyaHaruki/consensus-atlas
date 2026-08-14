package main

import (
	"context"
	"errors"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	etcdraftSemanticCalibrationArtifactVersion = "consensus-atlas/etcdraft-semantic-calibration-artifact/v2"
	etcdraftSemanticCalibrationComparisonV1    = "consensus-atlas/etcdraft-semantic-comparison/v1"
	etcdraftSemanticCalibrationSucceeded       = "completed"
	etcdraftSemanticCalibrationFailed          = "failed"
)

type etcdraftSemanticCalibrationComparison struct {
	SchemaVersion             string `json:"schema_version"`
	BaselineSearchDigest      string `json:"baseline_search_digest"`
	ExplorerSearchDigest      string `json:"explorer_search_digest"`
	BaselineFirstCandidateID  string `json:"baseline_first_candidate_id"`
	ExplorerFirstCandidateID  string `json:"explorer_first_candidate_id"`
	BaselineFirstPrefixDigest string `json:"baseline_first_prefix_digest"`
	ExplorerFirstPrefixDigest string `json:"explorer_first_prefix_digest"`
	DistinctFirstExpansion    bool   `json:"distinct_first_expansion"`
	BaselineReachedCandidates int    `json:"baseline_reached_candidates"`
	ExplorerReachedCandidates int    `json:"explorer_reached_candidates"`
	ExplorerAcceptedCalls     int    `json:"explorer_accepted_calls"`
	ExplorerRejectedCalls     int    `json:"explorer_rejected_calls"`
	Classification            string `json:"classification"`
	Digest                    string `json:"digest"`
}

type etcdraftSemanticCalibrationArtifact struct {
	SchemaVersion string                                      `json:"schema_version"`
	Spec          etcdraftSemanticCalibrationSpec             `json:"spec"`
	Status        string                                      `json:"status"`
	Baseline      controlexperiment.SemanticBestFirstResult   `json:"baseline"`
	Explorer      *controlexperiment.SemanticExplorerResult   `json:"explorer,omitempty"`
	Failure       *controlexperiment.SemanticExplorerFailure  `json:"failure,omitempty"`
	ProviderCalls []controlexperiment.StatelessAgentCallAudit `json:"provider_calls"`
	ModelWork     controlexperiment.ModelWork                 `json:"model_work"`
	Comparison    *etcdraftSemanticCalibrationComparison      `json:"comparison,omitempty"`
	Testing       *etcdraftSemanticTestingResult              `json:"testing_result,omitempty"`
	Digest        string                                      `json:"digest"`
}

func newEtcdraftSemanticCalibrationArtifact(
	inputs etcdraftSemanticCalibrationInputs,
	baseline controlexperiment.SemanticBestFirstResult,
	explorer *controlexperiment.SemanticExplorerResult,
	failure *controlexperiment.SemanticExplorerFailure,
	audits []controlexperiment.StatelessAgentCallAudit,
	testing *etcdraftSemanticTestingResult,
) (etcdraftSemanticCalibrationArtifact, error) {
	artifact := etcdraftSemanticCalibrationArtifact{
		SchemaVersion: etcdraftSemanticCalibrationArtifactVersion,
		Spec:          inputs.spec, Baseline: baseline,
		ProviderCalls: append([]controlexperiment.StatelessAgentCallAudit(nil), audits...),
	}
	if explorer != nil && failure == nil && testing != nil {
		comparison, err := newEtcdraftSemanticCalibrationComparison(baseline, *explorer)
		if err != nil {
			return etcdraftSemanticCalibrationArtifact{}, err
		}
		value := *explorer
		artifact.Status, artifact.Explorer = etcdraftSemanticCalibrationSucceeded, &value
		artifact.ModelWork = explorer.ModelWork
		artifact.Comparison = &comparison
		testingValue := *testing
		artifact.Testing = &testingValue
	} else if failure != nil && explorer == nil && testing == nil {
		value := *failure
		artifact.Status, artifact.Failure = etcdraftSemanticCalibrationFailed, &value
		artifact.ModelWork = failure.ModelWork
	} else {
		return etcdraftSemanticCalibrationArtifact{}, errors.New("ETCDRAFT_SEMANTIC_ARTIFACT_OUTCOME_INVALID")
	}
	sealed, err := artifact.seal()
	if err != nil || sealed.ValidateInputs(inputs) != nil {
		return etcdraftSemanticCalibrationArtifact{}, errors.New("ETCDRAFT_SEMANTIC_ARTIFACT_INVALID")
	}
	return sealed, nil
}

func (artifact etcdraftSemanticCalibrationArtifact) ValidateInputs(
	inputs etcdraftSemanticCalibrationInputs,
) error {
	if artifact.SchemaVersion != etcdraftSemanticCalibrationArtifactVersion ||
		artifact.Spec.ValidateInputs(
			inputs.campaign, inputs.root, inputs.frontier, inputs.riskSpec,
			inputs.knowledge, inputs.hypothesis, inputs.experiment, inputs.searchSpec,
		) != nil || artifact.Spec.Digest != inputs.spec.Digest ||
		artifact.Baseline.Validate(inputs.root, inputs.riskSpec) != nil ||
		artifact.Baseline.GuidanceID != artifact.Spec.BaselineGuidanceID ||
		artifact.Baseline.ProjectorID != artifact.Spec.ProjectorID ||
		len(artifact.ProviderCalls) > artifact.Spec.ExplorerBudget.MaxCalls {
		return errors.New("ETCDRAFT_SEMANTIC_ARTIFACT_INPUT_MISMATCH")
	}
	var auditWork controlexperiment.ModelWork
	for index, audit := range artifact.ProviderCalls {
		if audit.Validate() != nil || audit.Ordinal != index+1 || audit.RootID != artifact.Spec.RootID {
			return errors.New("ETCDRAFT_SEMANTIC_ARTIFACT_AUDIT_INVALID")
		}
		addAgentModelWork(&auditWork, audit.Work)
	}
	if auditWork != artifact.ModelWork {
		return errors.New("ETCDRAFT_SEMANTIC_ARTIFACT_MODEL_WORK_MISMATCH")
	}
	switch artifact.Status {
	case etcdraftSemanticCalibrationSucceeded:
		if artifact.Explorer == nil || artifact.Failure != nil || artifact.Comparison == nil ||
			artifact.Testing == nil || artifact.Testing.Validate(inputs, *artifact.Explorer) != nil ||
			artifact.Explorer.Validate(inputs.root, inputs.riskSpec) != nil ||
			artifact.Explorer.GuidanceID != artifact.Spec.ExplorerGuidanceID ||
			artifact.Explorer.ModelWork != artifact.ModelWork ||
			artifact.Comparison.Validate(artifact.Baseline, *artifact.Explorer) != nil ||
			!etcdraftSemanticCallsMatchAudits(artifact.Explorer.Calls, artifact.ProviderCalls, false) {
			return errors.New("ETCDRAFT_SEMANTIC_ARTIFACT_SUCCESS_INVALID")
		}
	case etcdraftSemanticCalibrationFailed:
		if artifact.Explorer != nil || artifact.Failure == nil || artifact.Comparison != nil || artifact.Testing != nil ||
			artifact.Failure.Validate() != nil || artifact.Failure.GuidanceID != artifact.Spec.ExplorerGuidanceID ||
			artifact.Failure.ModelWork != artifact.ModelWork ||
			!etcdraftSemanticCallsMatchAudits(
				artifact.Failure.Calls, artifact.ProviderCalls,
				artifact.Failure.ReasonCode == controlexperiment.SemanticExplorerFailurePlanner,
			) {
			return errors.New("ETCDRAFT_SEMANTIC_ARTIFACT_FAILURE_INVALID")
		}
	default:
		return errors.New("ETCDRAFT_SEMANTIC_ARTIFACT_STATUS_INVALID")
	}
	want, err := artifact.seal()
	if err != nil || len(artifact.Digest) != 64 || want.Digest != artifact.Digest {
		return errors.New("ETCDRAFT_SEMANTIC_ARTIFACT_DIGEST_MISMATCH")
	}
	return nil
}

func (artifact etcdraftSemanticCalibrationArtifact) ValidateSources(
	ctx context.Context,
	inputs etcdraftSemanticCalibrationInputs,
) error {
	if artifact.ValidateInputs(inputs) != nil {
		return errors.New("ETCDRAFT_SEMANTIC_ARTIFACT_SOURCE_INVALID")
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(inputs.experiment.AdapterConfig)
	}
	if artifact.Baseline.ValidateSources(
		ctx, inputs.root, inputs.riskSpec, factory, etcdraftSemanticPrefixProjector{},
		controlexperiment.NewDeterministicSemanticBestFirstGuidance(),
	) != nil {
		return errors.New("ETCDRAFT_SEMANTIC_ARTIFACT_BASELINE_SOURCE_MISMATCH")
	}
	if artifact.Explorer != nil && artifact.Explorer.ValidateSources(
		ctx, inputs.root, inputs.riskSpec, inputs.knowledge, inputs.hypothesis,
		factory, etcdraftSemanticPrefixProjector{},
	) != nil {
		return errors.New("ETCDRAFT_SEMANTIC_ARTIFACT_EXPLORER_SOURCE_MISMATCH")
	}
	if artifact.Testing != nil && artifact.Testing.Validate(inputs, *artifact.Explorer) != nil {
		return errors.New("ETCDRAFT_SEMANTIC_ARTIFACT_TESTING_SOURCE_MISMATCH")
	}
	return nil
}

func newEtcdraftSemanticCalibrationComparison(
	baseline controlexperiment.SemanticBestFirstResult,
	explorer controlexperiment.SemanticExplorerResult,
) (etcdraftSemanticCalibrationComparison, error) {
	baselineID, baselinePrefix, err := etcdraftSemanticFirstExpansion(baseline)
	if err != nil {
		return etcdraftSemanticCalibrationComparison{}, err
	}
	explorerID, explorerPrefix, err := etcdraftSemanticFirstExpansion(explorer.Search)
	if err != nil {
		return etcdraftSemanticCalibrationComparison{}, err
	}
	comparison := etcdraftSemanticCalibrationComparison{
		SchemaVersion:        etcdraftSemanticCalibrationComparisonV1,
		BaselineSearchDigest: baseline.Digest, ExplorerSearchDigest: explorer.Search.Digest,
		BaselineFirstCandidateID: baselineID, ExplorerFirstCandidateID: explorerID,
		BaselineFirstPrefixDigest: baselinePrefix, ExplorerFirstPrefixDigest: explorerPrefix,
		DistinctFirstExpansion:    baselinePrefix != explorerPrefix,
		BaselineReachedCandidates: etcdraftSemanticReachedCandidates(baseline.Candidates),
		ExplorerReachedCandidates: etcdraftSemanticReachedCandidates(explorer.Search.Candidates),
		Classification:            etcdraftSemanticCalibrationClass,
	}
	for _, call := range explorer.Calls {
		if call.Status == controlexperiment.SemanticExplorerCallAccepted {
			comparison.ExplorerAcceptedCalls++
		} else {
			comparison.ExplorerRejectedCalls++
		}
	}
	sealed, err := comparison.seal()
	if err != nil || sealed.Validate(baseline, explorer) != nil {
		return etcdraftSemanticCalibrationComparison{}, errors.New("ETCDRAFT_SEMANTIC_COMPARISON_INVALID")
	}
	return sealed, nil
}

func (comparison etcdraftSemanticCalibrationComparison) Validate(
	baseline controlexperiment.SemanticBestFirstResult,
	explorer controlexperiment.SemanticExplorerResult,
) error {
	if comparison.SchemaVersion != etcdraftSemanticCalibrationComparisonV1 ||
		comparison.Classification != etcdraftSemanticCalibrationClass {
		return errors.New("ETCDRAFT_SEMANTIC_COMPARISON_IDENTITY_INVALID")
	}
	want, err := newEtcdraftSemanticCalibrationComparisonUnchecked(baseline, explorer)
	if err != nil || !reflect.DeepEqual(want, comparison) {
		return errors.New("ETCDRAFT_SEMANTIC_COMPARISON_SOURCE_MISMATCH")
	}
	return nil
}

func newEtcdraftSemanticCalibrationComparisonUnchecked(
	baseline controlexperiment.SemanticBestFirstResult,
	explorer controlexperiment.SemanticExplorerResult,
) (etcdraftSemanticCalibrationComparison, error) {
	baselineID, baselinePrefix, err := etcdraftSemanticFirstExpansion(baseline)
	if err != nil {
		return etcdraftSemanticCalibrationComparison{}, err
	}
	explorerID, explorerPrefix, err := etcdraftSemanticFirstExpansion(explorer.Search)
	if err != nil {
		return etcdraftSemanticCalibrationComparison{}, err
	}
	value := etcdraftSemanticCalibrationComparison{
		SchemaVersion:        etcdraftSemanticCalibrationComparisonV1,
		BaselineSearchDigest: baseline.Digest, ExplorerSearchDigest: explorer.Search.Digest,
		BaselineFirstCandidateID: baselineID, ExplorerFirstCandidateID: explorerID,
		BaselineFirstPrefixDigest: baselinePrefix, ExplorerFirstPrefixDigest: explorerPrefix,
		DistinctFirstExpansion:    baselinePrefix != explorerPrefix,
		BaselineReachedCandidates: etcdraftSemanticReachedCandidates(baseline.Candidates),
		ExplorerReachedCandidates: etcdraftSemanticReachedCandidates(explorer.Search.Candidates),
		Classification:            etcdraftSemanticCalibrationClass,
	}
	for _, call := range explorer.Calls {
		if call.Status == controlexperiment.SemanticExplorerCallAccepted {
			value.ExplorerAcceptedCalls++
		} else {
			value.ExplorerRejectedCalls++
		}
	}
	return value.seal()
}

func etcdraftSemanticFirstExpansion(
	result controlexperiment.SemanticBestFirstResult,
) (string, string, error) {
	if len(result.ExpansionOrder) == 0 {
		return "", "", errors.New("ETCDRAFT_SEMANTIC_EXPANSION_MISSING")
	}
	want := result.ExpansionOrder[0]
	for index, candidate := range result.Candidates {
		if candidate.CandidateID == want {
			return want, result.Search.Items[index].ChildPrefixDigest, nil
		}
	}
	return "", "", errors.New("ETCDRAFT_SEMANTIC_EXPANSION_UNKNOWN")
}

func etcdraftSemanticReachedCandidates(candidates []controlexperiment.SemanticCandidateRecord) int {
	count := 0
	for _, candidate := range candidates {
		if candidate.RiskResult.Status == semantic.RiskWitnessReached {
			count++
		}
	}
	return count
}

func etcdraftSemanticCallsMatchAudits(
	calls []controlexperiment.SemanticExplorerCall,
	audits []controlexperiment.StatelessAgentCallAudit,
	allowTerminalFailure bool,
) bool {
	if len(audits) < len(calls) || len(audits) > len(calls)+1 ||
		(len(audits) == len(calls)+1 && !allowTerminalFailure) {
		return false
	}
	for index, call := range calls {
		if audits[index].Status != controlexperiment.StatelessAgentCallContentReady ||
			audits[index].SearchRequestDigest != call.Request.Digest || audits[index].Work != call.ModelWork {
			return false
		}
	}
	if len(audits) == len(calls)+1 && audits[len(audits)-1].Status != controlexperiment.StatelessAgentCallFailed {
		return false
	}
	return true
}

func addAgentModelWork(total *controlexperiment.ModelWork, value controlexperiment.ModelWork) {
	total.Calls += value.Calls
	total.InputTokens += value.InputTokens
	total.OutputTokens += value.OutputTokens
	total.TotalTokens += value.TotalTokens
}

func (comparison etcdraftSemanticCalibrationComparison) seal() (etcdraftSemanticCalibrationComparison, error) {
	comparison.Digest = ""
	digest, err := control.CanonicalDigest(comparison)
	comparison.Digest = digest
	return comparison, err
}

func (artifact etcdraftSemanticCalibrationArtifact) seal() (etcdraftSemanticCalibrationArtifact, error) {
	artifact.ProviderCalls = append([]controlexperiment.StatelessAgentCallAudit(nil), artifact.ProviderCalls...)
	artifact.Digest = ""
	digest, err := control.CanonicalDigest(artifact)
	artifact.Digest = digest
	return artifact, err
}
