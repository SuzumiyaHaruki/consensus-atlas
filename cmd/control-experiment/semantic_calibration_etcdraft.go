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
	etcdraftSemanticCalibrationMaxCalls    = 2
	etcdraftSemanticCalibrationMaxTokens   = 16000
	etcdraftSemanticCalibrationSearchWork  = 12000
	etcdraftSemanticPrefixProjectorID      = "official-etcdraft-v2-leader-change-prefix-v1"
)

type etcdraftSemanticCalibrationInputs struct {
	campaign   etcdraftStatelessCampaignInputs
	root       controlruntime.Trace
	frontier   controlexperiment.ActionFrontierView
	riskSpec   semantic.RiskWitnessSpec
	knowledge  controlexperiment.ProtocolKnowledgePack
	hypothesis controlexperiment.TestHypothesis
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
	want, err := raftfamily.LeaderChangeWithInflightProposalWitness()
	if err != nil || spec.Validate() != nil || !reflect.DeepEqual(spec, want) || trace.Validate() != nil {
		return semantic.RiskWitnessResult{}, errors.New("ETCDRAFT_SEMANTIC_PREFIX_PROJECTOR_INPUT_INVALID")
	}
	milestones, err := projectEtcdraftLeaderChangeRiskMilestones(trace, nil, nil, 1)
	if err != nil {
		return semantic.RiskWitnessResult{}, err
	}
	return semantic.NewRiskWitnessResult(
		id, spec, trace.ManifestDigest, trace.Digest, projector.ID(), milestones,
	)
}

// etcdraftSemanticCalibrationSpec freezes the public target composition. It
// is deliberately target-local and does not expand the protocol-neutral core.
type etcdraftSemanticCalibrationSpec struct {
	SchemaVersion       string                                   `json:"schema_version"`
	ID                  string                                   `json:"id"`
	TargetID            string                                   `json:"target_id"`
	SourceExposure      string                                   `json:"source_exposure"`
	Classification      string                                   `json:"classification"`
	SourceBundleDigest  string                                   `json:"source_bundle_digest"`
	CorpusDigest        string                                   `json:"corpus_digest"`
	RootID              string                                   `json:"root_id"`
	RootPrefixDigest    string                                   `json:"root_prefix_digest"`
	RootDecisions       int                                      `json:"root_decisions"`
	RootFrontierDigest  string                                   `json:"root_frontier_digest"`
	RootFrontierActions int                                      `json:"root_frontier_actions"`
	RiskSpecDigest      string                                   `json:"risk_spec_digest"`
	KnowledgeDigest     string                                   `json:"knowledge_digest"`
	HypothesisDigest    string                                   `json:"hypothesis_digest"`
	SearchSpecDigest    string                                   `json:"search_spec_digest"`
	BaselineGuidanceID  string                                   `json:"baseline_guidance_id"`
	ExplorerGuidanceID  string                                   `json:"explorer_guidance_id"`
	ProjectorID         string                                   `json:"projector_id"`
	PromptVersion       string                                   `json:"prompt_version"`
	ExplorerBudget      controlexperiment.SemanticExplorerBudget `json:"explorer_budget"`
	Transport           controlexperiment.AgentTransportFreeze   `json:"transport"`
	Digest              string                                   `json:"digest"`
}

func newEtcdraftSemanticCalibrationSpec(
	inputs etcdraftStatelessCampaignInputs,
	root controlruntime.Trace,
	frontier controlexperiment.ActionFrontierView,
	riskSpec semantic.RiskWitnessSpec,
	knowledge controlexperiment.ProtocolKnowledgePack,
	hypothesis controlexperiment.TestHypothesis,
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
		ExplorerBudget: controlexperiment.SemanticExplorerBudget{
			MaxCalls: etcdraftSemanticCalibrationMaxCalls, MaxTokens: etcdraftSemanticCalibrationMaxTokens,
		},
		Transport: transport,
	}
	sealed, err := spec.seal()
	if err != nil || sealed.ValidateInputs(
		inputs, root, frontier, riskSpec, knowledge, hypothesis, searchSpec,
	) != nil {
		return etcdraftSemanticCalibrationSpec{}, errors.New("ETCDRAFT_SEMANTIC_CALIBRATION_SPEC_INVALID")
	}
	return sealed, nil
}

func prepareEtcdraftSemanticCalibration(
	ctx context.Context,
	corpusPath string,
	client deepSeekIntentClient,
) (etcdraftSemanticCalibrationInputs, error) {
	inputs, err := loadEtcdraftStatelessCampaignInputs(ctx, corpusPath)
	if err != nil {
		return etcdraftSemanticCalibrationInputs{}, err
	}
	root, err := inputs.corpus.Prefix(inputs.source, etcdraftSemanticCalibrationRootID)
	if err != nil {
		return etcdraftSemanticCalibrationInputs{}, err
	}
	envelope := etcdraftSemanticCalibrationFaultEnvelope()
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	frontier, _, err := controlexperiment.ReconstructActionFrontierView(
		ctx, "etcdraft-public-semantic-root-frontier", root, len(root.Records),
		etcdraftCampaignRuntimeConfig(), envelope, factory,
	)
	if err != nil || len(frontier.Actions) < 2 {
		return etcdraftSemanticCalibrationInputs{}, errors.New("ETCDRAFT_SEMANTIC_CALIBRATION_FRONTIER_INVALID")
	}
	riskSpec, err := raftfamily.LeaderChangeWithInflightProposalWitness()
	if err != nil {
		return etcdraftSemanticCalibrationInputs{}, err
	}
	knowledge, err := etcdraftSemanticCalibrationKnowledge(riskSpec)
	if err != nil {
		return etcdraftSemanticCalibrationInputs{}, err
	}
	hypothesis, err := controlexperiment.NewTestHypothesisForBackend(
		"etcdraft-public-semantic-hypothesis-a2b3", knowledge, riskSpec,
		"Prefer exact prefixes that advance the frozen leader-change-with-inflight-proposal milestones.",
		controlexperiment.SemanticBestFirstAlgorithmID,
	)
	if err != nil {
		return etcdraftSemanticCalibrationInputs{}, err
	}
	searchSpec, err := controlexperiment.NewStatelessDFSSpec(
		"etcdraft-public-semantic-search-a2b3", root, etcdraftCampaignRuntimeConfig(), envelope,
		2, len(frontier.Actions)+1, etcdraftSemanticCalibrationSearchWork,
	)
	if err != nil {
		return etcdraftSemanticCalibrationInputs{}, err
	}
	spec, err := newEtcdraftSemanticCalibrationSpec(
		inputs, root, frontier, riskSpec, knowledge, hypothesis, searchSpec,
		deepSeekTransportFreeze(client),
	)
	if err != nil {
		return etcdraftSemanticCalibrationInputs{}, err
	}
	return etcdraftSemanticCalibrationInputs{
		campaign: inputs, root: root, frontier: frontier, riskSpec: riskSpec,
		knowledge: knowledge, hypothesis: hypothesis, searchSpec: searchSpec, spec: spec,
	}, nil
}

func etcdraftSemanticCalibrationFaultEnvelope() *controlexperiment.FaultEnvelope {
	return &controlexperiment.FaultEnvelope{
		MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
		MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
	}
}

func (spec etcdraftSemanticCalibrationSpec) ValidateInputs(
	inputs etcdraftStatelessCampaignInputs,
	root controlruntime.Trace,
	frontier controlexperiment.ActionFrontierView,
	riskSpec semantic.RiskWitnessSpec,
	knowledge controlexperiment.ProtocolKnowledgePack,
	hypothesis controlexperiment.TestHypothesis,
	searchSpec controlexperiment.StatelessDFSSpec,
) error {
	if spec.SchemaVersion != etcdraftSemanticCalibrationSpecVersion || spec.ID != etcdraftSemanticCalibrationID ||
		spec.TargetID != etcdraftCampaignTargetID || spec.SourceExposure != etcdraftSemanticCalibrationExposure ||
		spec.Classification != etcdraftSemanticCalibrationClass || inputs.source.Validate() != nil ||
		inputs.corpus.Validate(inputs.source) != nil || root.Validate() != nil || frontier.Validate() != nil ||
		riskSpec.Validate() != nil || knowledge.Validate() != nil ||
		hypothesis.ValidateForBackend(knowledge, riskSpec, controlexperiment.SemanticBestFirstAlgorithmID) != nil ||
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
		spec.ExplorerBudget != (controlexperiment.SemanticExplorerBudget{
			MaxCalls: etcdraftSemanticCalibrationMaxCalls, MaxTokens: etcdraftSemanticCalibrationMaxTokens,
		}) || spec.Transport.Validate() != nil || spec.Transport.Provider != deepSeekProvider ||
		spec.Transport.Model != deepSeekV4Flash || spec.Transport.MaxRetries != 0 ||
		searchSpec.MaxDepth != 2 || searchSpec.MaxWorkItems != len(frontier.Actions)+1 {
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

func etcdraftSemanticCalibrationKnowledge(
	riskSpec semantic.RiskWitnessSpec,
) (controlexperiment.ProtocolKnowledgePack, error) {
	if riskSpec.Validate() != nil || riskSpec.FamilyID != "raft" ||
		riskSpec.RiskID != raftfamily.LeaderChangeWithInflightProposalRiskID {
		return controlexperiment.ProtocolKnowledgePack{}, errors.New("ETCDRAFT_SEMANTIC_KNOWLEDGE_RISK_INVALID")
	}
	return controlexperiment.NewProtocolKnowledgePack(controlexperiment.ProtocolKnowledgePack{
		ID: "etcdraft-public-semantic-knowledge-a2b3", Family: "raft", Protocol: "etcdraft",
		Knowledge: []controlexperiment.KnowledgeStatement{
			{ID: "inflight-change", Text: "A proposal may remain in flight while leadership changes; explore prefixes that preserve this ordering."},
			{ID: "natural-progress", Text: "Leadership change and recovery arise through controlled message delivery, lifecycle events, and natural temporal progress."},
			{ID: "semantic-queue-only", Text: "Choose only the complete Candidate ID set supplied by the current trusted semantic queue."},
		},
		Risks: []controlexperiment.ProtocolRisk{{
			ID:                   riskSpec.RiskID,
			Summary:              "Exercise a leader change while a client proposal is in flight, followed by old-leader recovery.",
			RequiredCapabilities: []string{"runtime-owned-message-control", "strict-replay"},
			RequiredActions: []control.ActionKind{
				control.ActionCrash, control.ActionDeliverMessage, control.ActionFireTemporal,
				control.ActionInvoke, control.ActionRestart,
			},
			AllowedBackendIDs: []string{controlexperiment.SemanticBestFirstAlgorithmID},
		}},
	})
}
