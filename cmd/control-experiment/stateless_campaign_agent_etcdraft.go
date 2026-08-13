package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func executeEtcdraftStatelessAgentCampaignAttempt(
	ctx context.Context,
	request controlexperiment.CampaignAttemptRequest,
	spec controlexperiment.StatelessCampaignSpec,
	inputs etcdraftStatelessCampaignInputs,
	campaignDirectory string,
	keyFile string,
	client deepSeekIntentClient,
	readKey agentKeyReader,
) (controlexperiment.StatelessCampaignExecution, error) {
	if request.Ordinal <= 0 || request.Ordinal > len(spec.Methods) ||
		spec.Methods[request.Ordinal-1].Strategy != controlexperiment.StatelessTraversalAgentOrder ||
		spec.ValidateInputs(spec.Methods, inputs.corpus, inputs.source) != nil ||
		keyFile == "" || client.HTTP == nil || readKey == nil {
		return controlexperiment.StatelessCampaignExecution{},
			errors.New("ETCDRAFT_STATELESS_AGENT_ATTEMPT_INPUT_INVALID")
	}
	sidecar, err := controlexperiment.CampaignAttemptSidecarDirectory(
		campaignDirectory, "stateless-agent", request.Ordinal,
	)
	if err != nil {
		return controlexperiment.StatelessCampaignExecution{}, err
	}
	var journal *statelessAgentCallJournal
	if _, statErr := os.Lstat(sidecar); os.IsNotExist(statErr) {
		journal, err = newStatelessAgentCallJournal(sidecar, client, "")
	} else if statErr == nil {
		journal, err = recoverStatelessAgentCallJournal(sidecar, client)
	} else {
		err = statErr
	}
	if err != nil {
		return controlexperiment.StatelessCampaignExecution{}, err
	}
	method := spec.Methods[request.Ordinal-1]
	knowledge, err := etcdraftStatelessAgentKnowledge()
	if err != nil || method.KnowledgeDigest != knowledge.Digest {
		return controlexperiment.StatelessCampaignExecution{},
			errors.New("ETCDRAFT_STATELESS_AGENT_KNOWLEDGE_DRIFT")
	}
	planner := func(
		ctx context.Context,
		view controlexperiment.StatelessSearchAgentView,
	) ([]byte, controlexperiment.ModelWork, error) {
		content, work, callErr := journal.Planner(ctx, view)
		if !errors.Is(callErr, errStatelessAgentCallKeyRequired) {
			return content, work, callErr
		}
		if err := ctx.Err(); err != nil {
			return nil, work, controlexperiment.NewCampaignAttemptDeferredError("context-before-key", err)
		}
		key, keyErr := readKey(keyFile)
		if keyErr != nil {
			return nil, work, controlexperiment.NewCampaignAttemptDeferredError("key-unavailable", keyErr)
		}
		if keyErr = journal.ActivateKey(key); keyErr != nil {
			key = ""
			return nil, work, controlexperiment.NewCampaignAttemptDeferredError("key-invalid", keyErr)
		}
		key = ""
		return journal.Planner(ctx, view)
	}
	envelope := &controlexperiment.FaultEnvelope{
		MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
		MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	history := make([]controlexperiment.StatelessSearchHistoryEntry, 0, len(inputs.corpus.Roots))
	evidence := make([]controlexperiment.StatelessCorpusRootEvidence, 0, len(inputs.corpus.Roots))
	baseline := make(map[string]bool)
	maxRootDecisions := inputs.corpus.Roots[len(inputs.corpus.Roots)-1].Decisions
	if maxRootDecisions >= len(inputs.source.CorePSS) {
		return controlexperiment.StatelessCampaignExecution{},
			errors.New("ETCDRAFT_STATELESS_AGENT_BASELINE_INVALID")
	}
	for _, sample := range inputs.source.CorePSS[:maxRootDecisions+1] {
		baseline[sample.Key] = true
	}
	var searchWork controlexperiment.StatelessDFSWork
	var qualifiedWork controlexperiment.WorkLedger
	for rootIndex, entry := range inputs.corpus.Roots {
		root, prefixErr := inputs.corpus.Prefix(inputs.source, entry.ID)
		if prefixErr != nil || journal.SetRoot(entry.ID) != nil {
			return failedEtcdraftStatelessAgentCampaignExecution(
				"root", "ETCDRAFT_STATELESS_AGENT_ROOT_INVALID", searchWork, qualifiedWork, journal,
			)
		}
		searchSpec, specErr := controlexperiment.NewStatelessDFSSpec(
			fmt.Sprintf("etcdraft-stateless-agent-root-%d", rootIndex+1), root,
			etcdraftCampaignRuntimeConfig(), envelope,
			spec.MaxDepth, spec.MaxWorkItemsPerRoot, spec.MaxSearchWorkUnitsPerRoot,
		)
		if specErr != nil {
			return failedEtcdraftStatelessAgentCampaignExecution(
				"search-spec", "ETCDRAFT_STATELESS_AGENT_SEARCH_SPEC_INVALID",
				searchWork, qualifiedWork, journal,
			)
		}
		agent, exploreErr := controlexperiment.ExploreBoundedStatelessDFSWithAgent(
			ctx, method, knowledge, append([]controlexperiment.StatelessSearchHistoryEntry(nil), history...),
			planner, searchSpec, root, factory,
		)
		if exploreErr != nil {
			var deferred *controlexperiment.CampaignAttemptDeferredError
			if errors.As(exploreErr, &deferred) {
				return controlexperiment.StatelessCampaignExecution{}, deferred
			}
			var searchFailure *controlexperiment.StatelessDFSExecutionError
			if errors.As(exploreErr, &searchFailure) {
				addEtcdraftStatelessSearchWork(&searchWork, searchFailure.Work)
			}
			return failedEtcdraftStatelessAgentCampaignExecution(
				"agent-search", "ETCDRAFT_STATELESS_AGENT_SEARCH_FAILED",
				searchWork, qualifiedWork, journal,
			)
		}
		result := agent.Traversal
		addEtcdraftStatelessSearchWork(&searchWork, result.Search.Work)
		if len(result.Search.Items) != searchSpec.MaxWorkItems ||
			result.Search.StopReason != controlexperiment.StatelessDFSStopItems {
			return failedEtcdraftStatelessAgentCampaignExecution(
				"agent-search", "ETCDRAFT_STATELESS_AGENT_SEARCH_BOUND_NOT_REACHED",
				searchWork, qualifiedWork, journal,
			)
		}
		bundles := make([]controlexperiment.ExecutionBundle, 0, len(result.Search.Items))
		for _, item := range result.Search.Items {
			_, bundle, executionErr := executeEtcdraftStatelessPrefix(
				ctx, fmt.Sprintf("etcdraft-stateless-%s-%s-%d", method.ID, entry.ID, item.Ordinal),
				result, root, item, envelope, inputs.qualification, inputs.admission, inputs.workload,
			)
			if executionErr != nil {
				var executionFailure *controlexperiment.ExecutionFailure
				if errors.As(executionErr, &executionFailure) {
					addEtcdraftStatelessWork(&qualifiedWork, executionFailure.Work)
				}
				return failedEtcdraftStatelessAgentCampaignExecution(
					"qualified-execution", "ETCDRAFT_STATELESS_AGENT_QUALIFIED_EXECUTION_FAILED",
					searchWork, qualifiedWork, journal,
				)
			}
			bundles = append(bundles, bundle)
			addEtcdraftStatelessWork(&qualifiedWork, bundle.Work)
		}
		rootDiscovery, discoveryErr := controlexperiment.NewStatelessTraversalDiscovery(
			"etcdraft-stateless-agent-"+entry.ID,
			result, root, bundles, etcdraftv2.CorePSSMapper{},
		)
		if discoveryErr != nil {
			return failedEtcdraftStatelessAgentCampaignExecution(
				"discovery", "ETCDRAFT_STATELESS_AGENT_ROOT_DISCOVERY_INVALID",
				searchWork, qualifiedWork, journal,
			)
		}
		rootNovel, rootNovelDigest, novelErr := sortedNovelM523g(
			rootDiscovery.IncrementalPSSKeys, baseline,
		)
		if novelErr != nil {
			return controlexperiment.StatelessCampaignExecution{}, novelErr
		}
		history = append(history, controlexperiment.StatelessSearchHistoryEntry{
			Ordinal: len(history) + 1, RootID: entry.ID,
			CorpusNovelPSSStates:    len(rootNovel),
			CorpusNovelPSSSetDigest: rootNovelDigest,
			DiscoveryDigest:         rootDiscovery.Digest,
		})
		evidence = append(evidence, controlexperiment.StatelessCorpusRootEvidence{
			RootID: entry.ID, Result: result, Bundles: bundles,
		})
	}
	discovery, err := controlexperiment.NewStatelessCorpusDiscovery(
		"etcdraft-stateless-discovery-"+method.Digest[:16],
		inputs.corpus, inputs.source, evidence, etcdraftv2.CorePSSMapper{},
	)
	if err != nil || discovery.Validate(
		inputs.corpus, inputs.source, evidence, etcdraftv2.CorePSSMapper{},
	) != nil {
		return failedEtcdraftStatelessAgentCampaignExecution(
			"discovery", "ETCDRAFT_STATELESS_AGENT_DISCOVERY_INVALID",
			searchWork, qualifiedWork, journal,
		)
	}
	audits, err := journal.Audits()
	if err != nil || len(audits) != statelessAgentMaxCalls {
		return controlexperiment.StatelessCampaignExecution{},
			errors.New("ETCDRAFT_STATELESS_AGENT_CALL_AUDIT_INVALID")
	}
	modelWork := statelessAgentAuditWork(audits)
	return controlexperiment.StatelessCampaignExecution{
		Discovery: &discovery, SearchWork: discovery.SearchWork,
		QualifiedExecutionWork: discovery.QualifiedExecutionWork,
		ModelWork:              modelWork, AgentCalls: audits,
	}, nil
}

func failedEtcdraftStatelessAgentCampaignExecution(
	phase string,
	code string,
	search controlexperiment.StatelessDFSWork,
	qualified controlexperiment.WorkLedger,
	journal *statelessAgentCallJournal,
) (controlexperiment.StatelessCampaignExecution, error) {
	audits, err := journal.Audits()
	if err != nil {
		return controlexperiment.StatelessCampaignExecution{}, err
	}
	return controlexperiment.StatelessCampaignExecution{
		SearchWork: search, QualifiedExecutionWork: qualified,
		ModelWork: statelessAgentAuditWork(audits), AgentCalls: audits,
		Failure: &controlexperiment.MethodFailure{Phase: phase, Code: code, Decision: 0},
	}, nil
}

func statelessAgentAuditWork(audits []controlexperiment.StatelessAgentCallAudit) controlexperiment.ModelWork {
	var total controlexperiment.ModelWork
	for _, audit := range audits {
		total.Calls += audit.Work.Calls
		total.InputTokens += audit.Work.InputTokens
		total.OutputTokens += audit.Work.OutputTokens
		total.TotalTokens += audit.Work.TotalTokens
	}
	return total
}
