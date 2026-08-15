package main

import (
	"context"
	"errors"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
	raftfamily "github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic/raft"
)

const (
	etcdraftSemanticCalibrationSpecVersion = "consensus-atlas/etcdraft-semantic-calibration-spec/v1"
	etcdraftSemanticCalibrationID          = "etcdraft-public-semantic-explorer-a2b3"
	etcdraftSemanticCalibrationStrategy    = "etcdraft-public-semantic-explorer-a2b3"
	etcdraftSemanticCalibrationRootID      = "invoked"
	etcdraftSemanticCalibrationExposure    = "public-calibration-official-source"
	etcdraftSemanticCalibrationClass       = "public-calibration-not-agent-effectiveness-holdout-or-correctness"
	etcdraftSemanticPrefixProjectorID      = "official-etcdraft-v2-leader-change-prefix-v1"
)

type etcdraftSemanticCalibrationInputs struct {
	campaign   etcdraftStatelessCampaignInputs
	root       controlruntime.Trace
	frontier   controlexperiment.ActionFrontierView
	riskSpec   semantic.RiskWitnessSpec
	knowledge  controlexperiment.ProtocolKnowledgePack
	hypothesis controlexperiment.TestHypothesis
	experiment etcdraftAgentExperimentConfig
	client     openRouterIntentClient
	searchSpec controlexperiment.StatelessDFSSpec
	spec       etcdraftSemanticCalibrationSpec
}

// etcdraftSemanticPrefixProjector is target-owned composition. Generic
// semantic search remains unaware of terms, roles and etcd/raft evidence.
type etcdraftSemanticPrefixProjector struct{}

func (etcdraftSemanticPrefixProjector) ID() string {
	return etcdraftSemanticPrefixProjectorID
}

func (projector etcdraftSemanticPrefixProjector) Project(
	id string,
	spec semantic.RiskWitnessSpec,
	trace controlruntime.Trace,
) (semantic.RiskWitnessResult, error) {
	return projectEtcdraftSemanticRisk(id, spec, trace, nil, nil)
}

func projectEtcdraftSemanticRisk(
	id string,
	spec semantic.RiskWitnessSpec,
	trace controlruntime.Trace,
	clients []controlexperiment.ClientHistoryEntry,
	operations *controlexperiment.OperationHistory,
) (semantic.RiskWitnessResult, error) {
	want, err := raftfamily.LeaderChangeWithInflightProposalWitness()
	if err != nil || spec.Validate() != nil || !reflect.DeepEqual(spec, want) || trace.Validate() != nil {
		return semantic.RiskWitnessResult{}, errors.New("ETCDRAFT_SEMANTIC_PREFIX_PROJECTOR_INPUT_INVALID")
	}
	milestones, err := projectEtcdraftLeaderChangeRiskMilestones(trace, clients, operations, 1)
	if err != nil {
		return semantic.RiskWitnessResult{}, err
	}
	return semantic.NewRiskWitnessResult(
		id, spec, trace.ManifestDigest, trace.Digest, etcdraftSemanticPrefixProjectorID, milestones,
	)
}

// etcdraftSemanticCalibrationSpec freezes the public target composition. It
// is deliberately target-local and does not expand the protocol-neutral core.
type etcdraftSemanticCalibrationSpec struct {
	SchemaVersion            string                                         `json:"schema_version"`
	ID                       string                                         `json:"id"`
	TargetID                 string                                         `json:"target_id"`
	SourceExposure           string                                         `json:"source_exposure"`
	Classification           string                                         `json:"classification"`
	SourceBundleDigest       string                                         `json:"source_bundle_digest"`
	CorpusDigest             string                                         `json:"corpus_digest"`
	RootID                   string                                         `json:"root_id"`
	RootPrefixDigest         string                                         `json:"root_prefix_digest"`
	RootDecisions            int                                            `json:"root_decisions"`
	RootFrontierDigest       string                                         `json:"root_frontier_digest"`
	RootFrontierActions      int                                            `json:"root_frontier_actions"`
	RiskSpecDigest           string                                         `json:"risk_spec_digest"`
	KnowledgeDigest          string                                         `json:"knowledge_digest"`
	HypothesisDigest         string                                         `json:"hypothesis_digest"`
	SearchSpecDigest         string                                         `json:"search_spec_digest"`
	BaselineGuidanceID       string                                         `json:"baseline_guidance_id"`
	ExplorerGuidanceID       string                                         `json:"explorer_guidance_id"`
	ProjectorID              string                                         `json:"projector_id"`
	PromptVersion            string                                         `json:"prompt_version"`
	ScenarioPromptVersion    string                                         `json:"scenario_prompt_version"`
	ScenarioMaxCalls         int                                            `json:"scenario_max_calls"`
	ScenarioMaxPlanSteps     int                                            `json:"scenario_max_plan_steps"`
	ScenarioMaxDecisions     int                                            `json:"scenario_max_decisions"`
	ScenarioSemanticExposure controlexperiment.ScenarioSemanticExposureMode `json:"scenario_semantic_exposure"`
	ExplorerBudget           controlexperiment.SemanticExplorerBudget       `json:"explorer_budget"`
	Transport                controlexperiment.AgentTransportFreeze         `json:"transport"`
	Digest                   string                                         `json:"digest"`
}

func newEtcdraftSemanticCalibrationSpec(
	inputs etcdraftStatelessCampaignInputs,
	root controlruntime.Trace,
	frontier controlexperiment.ActionFrontierView,
	riskSpec semantic.RiskWitnessSpec,
	knowledge controlexperiment.ProtocolKnowledgePack,
	hypothesis controlexperiment.TestHypothesis,
	experiment etcdraftAgentExperimentConfig,
	searchSpec controlexperiment.StatelessDFSSpec,
	transport controlexperiment.AgentTransportFreeze,
) (etcdraftSemanticCalibrationSpec, error) {
	spec := etcdraftSemanticCalibrationSpec{
		SchemaVersion: etcdraftSemanticCalibrationSpecVersion,
		ID:            etcdraftSemanticCalibrationID, TargetID: etcdraftCampaignTargetID,
		SourceExposure:     etcdraftSemanticCalibrationExposure,
		Classification:     etcdraftSemanticCalibrationClass,
		SourceBundleDigest: inputs.source.Digest, CorpusDigest: inputs.corpus.Digest,
		RootID: etcdraftSemanticCalibrationRootID, RootPrefixDigest: root.Digest,
		RootDecisions: len(root.Records), RootFrontierDigest: frontier.Digest,
		RootFrontierActions: len(frontier.Actions), RiskSpecDigest: riskSpec.Digest,
		KnowledgeDigest: knowledge.Digest, HypothesisDigest: hypothesis.Digest,
		SearchSpecDigest:   searchSpec.Digest,
		BaselineGuidanceID: controlexperiment.SemanticBestFirstGuidanceID,
		ExplorerGuidanceID: "etcdraft-public-semantic-explorer-v1",
		ProjectorID:        etcdraftSemanticPrefixProjector{}.ID(), PromptVersion: semanticExplorerPromptVersion,
		ScenarioPromptVersion:    scenarioAgentPromptVersion,
		ScenarioMaxCalls:         experiment.ScenarioMaxCalls,
		ScenarioMaxPlanSteps:     experiment.ScenarioMaxSteps,
		ScenarioMaxDecisions:     experiment.ScenarioMaxDecisions,
		ScenarioSemanticExposure: experiment.ScenarioSemanticExposure,
		ExplorerBudget:           experiment.ExplorerBudget,
		Transport:                transport,
	}
	sealed, err := spec.seal()
	if err != nil || sealed.ValidateInputs(
		inputs, root, frontier, riskSpec, knowledge, hypothesis, experiment, searchSpec,
	) != nil {
		return etcdraftSemanticCalibrationSpec{}, errors.New("ETCDRAFT_SEMANTIC_CALIBRATION_SPEC_INVALID")
	}
	return sealed, nil
}

func prepareEtcdraftSemanticCalibration(
	ctx context.Context,
	corpusPath string,
	semanticInputPath string,
	client openRouterIntentClient,
) (etcdraftSemanticCalibrationInputs, error) {
	riskSpec, err := raftfamily.LeaderChangeWithInflightProposalWitness()
	if err != nil {
		return etcdraftSemanticCalibrationInputs{}, err
	}
	knowledge, hypothesis, experiment, workload, err := loadEtcdraftSemanticAuthoringSource(
		semanticInputPath, riskSpec,
	)
	if err != nil {
		return etcdraftSemanticCalibrationInputs{}, err
	}
	var inputs etcdraftStatelessCampaignInputs
	if corpusPath == "" {
		inputs, err = prepareEtcdraftFreshCampaignInputs(ctx, workload)
	} else {
		inputs, err = loadEtcdraftStatelessCampaignInputsWithWorkload(ctx, corpusPath, workload)
	}
	if err != nil {
		return etcdraftSemanticCalibrationInputs{}, err
	}
	root, err := inputs.corpus.Prefix(inputs.source, etcdraftSemanticCalibrationRootID)
	if err != nil {
		return etcdraftSemanticCalibrationInputs{}, err
	}
	client.ReasoningEffort = experiment.ModelReasoningEffort
	client.ExcludeReasoning = experiment.ModelExcludeReasoning
	client.MaxOutputTokens = experiment.ModelMaxOutputTokens
	client.MaxRetries = experiment.ModelMaxRetries
	if openRouterTransportFreeze(client).Validate() != nil {
		return etcdraftSemanticCalibrationInputs{}, errors.New("ETCDRAFT_SEMANTIC_TRANSPORT_CONFIG_INVALID")
	}
	envelope := experiment.faultEnvelope()
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(experiment.AdapterConfig)
	}
	frontier, _, err := controlexperiment.ReconstructActionFrontierView(
		ctx, "etcdraft-public-semantic-root-frontier", root, len(root.Records),
		experiment.Runtime, envelope, factory,
	)
	if err != nil || len(frontier.Actions) < 2 {
		return etcdraftSemanticCalibrationInputs{}, errors.New("ETCDRAFT_SEMANTIC_CALIBRATION_FRONTIER_INVALID")
	}
	searchSpec, err := controlexperiment.NewStatelessDFSSpec(
		"etcdraft-public-semantic-search-a2b3", root, experiment.Runtime, envelope,
		experiment.SearchMaxDepth, experiment.SearchMaxWorkItems, experiment.SearchMaxWorkUnits,
	)
	if err != nil {
		return etcdraftSemanticCalibrationInputs{}, err
	}
	spec, err := newEtcdraftSemanticCalibrationSpec(
		inputs, root, frontier, riskSpec, knowledge, hypothesis, experiment, searchSpec,
		openRouterTransportFreeze(client),
	)
	if err != nil {
		return etcdraftSemanticCalibrationInputs{}, err
	}
	return etcdraftSemanticCalibrationInputs{
		campaign: inputs, root: root, frontier: frontier, riskSpec: riskSpec,
		knowledge: knowledge, hypothesis: hypothesis, experiment: experiment,
		client: client, searchSpec: searchSpec, spec: spec,
	}, nil
}

func (spec etcdraftSemanticCalibrationSpec) ValidateInputs(
	inputs etcdraftStatelessCampaignInputs,
	root controlruntime.Trace,
	frontier controlexperiment.ActionFrontierView,
	riskSpec semantic.RiskWitnessSpec,
	knowledge controlexperiment.ProtocolKnowledgePack,
	hypothesis controlexperiment.TestHypothesis,
	experiment etcdraftAgentExperimentConfig,
	searchSpec controlexperiment.StatelessDFSSpec,
) error {
	if spec.SchemaVersion != etcdraftSemanticCalibrationSpecVersion || spec.ID != etcdraftSemanticCalibrationID ||
		spec.TargetID != etcdraftCampaignTargetID || spec.SourceExposure != etcdraftSemanticCalibrationExposure ||
		spec.Classification != etcdraftSemanticCalibrationClass || inputs.source.Validate() != nil ||
		inputs.corpus.Validate(inputs.source) != nil || root.Validate() != nil || frontier.Validate() != nil ||
		riskSpec.Validate() != nil || knowledge.Validate() != nil || experiment.validate() != nil ||
		hypothesis.Validate(knowledge, riskSpec, controlexperiment.SemanticBestFirstAlgorithmID) != nil ||
		searchSpec.Validate(root) != nil || spec.SourceBundleDigest != inputs.source.Digest ||
		spec.CorpusDigest != inputs.corpus.Digest || spec.RootID != etcdraftSemanticCalibrationRootID ||
		spec.RootPrefixDigest != root.Digest || spec.RootDecisions != len(root.Records) ||
		spec.RootFrontierDigest != frontier.Digest || spec.RootFrontierActions != len(frontier.Actions) ||
		len(frontier.Actions) < 2 || spec.RiskSpecDigest != riskSpec.Digest ||
		spec.KnowledgeDigest != knowledge.Digest || spec.HypothesisDigest != hypothesis.Digest ||
		spec.SearchSpecDigest != searchSpec.Digest ||
		spec.BaselineGuidanceID != controlexperiment.SemanticBestFirstGuidanceID ||
		spec.ExplorerGuidanceID != "etcdraft-public-semantic-explorer-v1" ||
		spec.ProjectorID != (etcdraftSemanticPrefixProjector{}).ID() ||
		spec.PromptVersion != semanticExplorerPromptVersion ||
		spec.ScenarioPromptVersion != scenarioAgentPromptVersion ||
		spec.ScenarioMaxCalls != experiment.ScenarioMaxCalls ||
		spec.ScenarioMaxPlanSteps != experiment.ScenarioMaxSteps ||
		spec.ScenarioMaxDecisions != experiment.ScenarioMaxDecisions ||
		spec.ScenarioSemanticExposure != experiment.ScenarioSemanticExposure ||
		spec.ExplorerBudget != experiment.ExplorerBudget || spec.Transport.Validate() != nil ||
		spec.Transport.Provider != openRouterProvider ||
		spec.Transport.Endpoint != openRouterChatEndpoint || !validOpenRouterModelID(spec.Transport.Model) ||
		spec.Transport.Thinking != experiment.ModelReasoningEffort ||
		spec.Transport.ExcludeReasoning != experiment.ModelExcludeReasoning ||
		spec.Transport.MaxOutputTokens != experiment.ModelMaxOutputTokens ||
		spec.Transport.MaxRetries != experiment.ModelMaxRetries ||
		searchSpec.Runtime != experiment.Runtime || searchSpec.FaultEnvelope == nil ||
		*searchSpec.FaultEnvelope != experiment.FaultEnvelope ||
		searchSpec.MaxDepth != experiment.SearchMaxDepth ||
		searchSpec.MaxWorkItems != experiment.SearchMaxWorkItems ||
		searchSpec.MaxWorkUnits != experiment.SearchMaxWorkUnits {
		return errors.New("ETCDRAFT_SEMANTIC_CALIBRATION_SPEC_INPUT_MISMATCH")
	}
	want, err := spec.seal()
	if err != nil || len(spec.Digest) != 64 || want.Digest != spec.Digest {
		return errors.New("ETCDRAFT_SEMANTIC_CALIBRATION_SPEC_DIGEST_MISMATCH")
	}
	return nil
}

func (spec etcdraftSemanticCalibrationSpec) seal() (etcdraftSemanticCalibrationSpec, error) {
	spec.Digest = ""
	digest, err := control.CanonicalDigest(spec)
	spec.Digest = digest
	return spec, err
}
