package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

type etcdraftSemanticCalibrationRunOptions struct {
	Directory         string
	CorpusPath        string
	SemanticInputPath string
	Resume            bool
	AgentKeyFile      string
	Client            openRouterIntentClient
	ReadKey           agentKeyReader
}

func runEtcdraftSemanticCalibration(
	ctx context.Context,
	options etcdraftSemanticCalibrationRunOptions,
) (etcdraftSemanticCalibrationArtifact, error) {
	clean := filepath.Clean(options.Directory)
	if options.Directory == "" || clean == "." || clean == string(filepath.Separator) ||
		options.CorpusPath == "" || options.SemanticInputPath == "" ||
		!validateAgentKeyFileName(options.AgentKeyFile) ||
		options.Client.HTTP == nil ||
		options.ReadKey == nil {
		return etcdraftSemanticCalibrationArtifact{}, errors.New("ETCDRAFT_SEMANTIC_RUN_OPTIONS_INVALID")
	}
	inputs, err := prepareEtcdraftSemanticCalibration(
		ctx, options.CorpusPath, options.SemanticInputPath, options.Client,
	)
	if err != nil {
		return etcdraftSemanticCalibrationArtifact{}, err
	}
	providerDirectory := filepath.Join(clean, "provider")
	var journal *semanticExplorerCallJournal
	if options.Resume {
		if err := validateEtcdraftSemanticCalibrationDirectory(clean, true); err != nil {
			return etcdraftSemanticCalibrationArtifact{}, err
		}
		var persisted etcdraftSemanticCalibrationSpec
		if err := readStrictJSONFile(filepath.Join(clean, "spec.json"), 256<<10, &persisted); err != nil ||
			!reflect.DeepEqual(persisted, inputs.spec) {
			return etcdraftSemanticCalibrationArtifact{}, errors.New("ETCDRAFT_SEMANTIC_RUN_SPEC_DRIFT")
		}
		journal, err = recoverSemanticExplorerCallJournal(providerDirectory, inputs.client)
	} else {
		if err := os.MkdirAll(filepath.Dir(clean), 0o700); err != nil {
			return etcdraftSemanticCalibrationArtifact{}, err
		}
		if err := os.Mkdir(clean, 0o700); err != nil {
			return etcdraftSemanticCalibrationArtifact{}, err
		}
		if err := syncStatelessAgentDirectory(filepath.Dir(clean)); err != nil {
			return etcdraftSemanticCalibrationArtifact{}, err
		}
		if err := writeStatelessAgentJSON(clean, "spec.json", inputs.spec); err != nil {
			return etcdraftSemanticCalibrationArtifact{}, err
		}
		journal, err = newSemanticExplorerCallJournal(providerDirectory, inputs.client, "")
	}
	if err != nil || journal.SetRoot(inputs.spec.RootID) != nil {
		return etcdraftSemanticCalibrationArtifact{}, errors.New("ETCDRAFT_SEMANTIC_RUN_JOURNAL_INVALID")
	}
	artifactPath := filepath.Join(clean, "artifact.json")
	if options.Resume {
		if _, statErr := os.Lstat(artifactPath); statErr == nil {
			var artifact etcdraftSemanticCalibrationArtifact
			if err := readStrictJSONFile(artifactPath, 8<<20, &artifact); err != nil ||
				artifact.ValidateSources(ctx, inputs) != nil ||
				validateEtcdraftSemanticJournalEvidence(artifact, journal) != nil {
				return etcdraftSemanticCalibrationArtifact{}, errors.New("ETCDRAFT_SEMANTIC_RUN_ARTIFACT_INVALID")
			}
			return artifact, nil
		} else if !os.IsNotExist(statErr) {
			return etcdraftSemanticCalibrationArtifact{}, statErr
		}
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(inputs.experiment.AdapterConfig)
	}
	baseline, err := controlexperiment.ExploreBoundedSemanticBestFirst(
		ctx, inputs.searchSpec, inputs.root, inputs.riskSpec, factory,
		etcdraftSemanticPrefixProjector{}, controlexperiment.NewDeterministicSemanticBestFirstGuidance(),
	)
	if err != nil {
		return etcdraftSemanticCalibrationArtifact{}, err
	}
	planner := func(
		ctx context.Context,
		view controlexperiment.SemanticExplorerAgentView,
	) ([]byte, controlexperiment.ModelWork, error) {
		content, work, callErr := journal.Planner(ctx, view)
		if !errors.Is(callErr, errStatelessAgentCallKeyRequired) {
			return content, work, callErr
		}
		if err := ctx.Err(); err != nil {
			return nil, work, controlexperiment.NewCampaignAttemptDeferredError("context-before-key", err)
		}
		key, keyErr := options.ReadKey(options.AgentKeyFile)
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
	explorer, exploreErr := controlexperiment.ExploreBoundedSemanticBestFirstWithExplorer(
		ctx, inputs.spec.ExplorerGuidanceID, inputs.spec.ExplorerBudget,
		inputs.knowledge, inputs.hypothesis, inputs.searchSpec, inputs.root, inputs.riskSpec,
		factory, etcdraftSemanticPrefixProjector{}, planner,
	)
	if exploreErr != nil {
		var deferred *controlexperiment.CampaignAttemptDeferredError
		if errors.As(exploreErr, &deferred) {
			return etcdraftSemanticCalibrationArtifact{}, deferred
		}
	}
	audits, err := journal.Audits()
	if err != nil {
		return etcdraftSemanticCalibrationArtifact{}, err
	}
	var artifact etcdraftSemanticCalibrationArtifact
	if exploreErr == nil {
		testing, testingErr := executeEtcdraftSemanticTesting(ctx, inputs, explorer)
		if testingErr != nil {
			return etcdraftSemanticCalibrationArtifact{}, testingErr
		}
		artifact, err = newEtcdraftSemanticCalibrationArtifact(
			inputs, baseline, &explorer, nil, audits, &testing,
		)
	} else {
		var failure *controlexperiment.SemanticExplorerExecutionError
		if !errors.As(exploreErr, &failure) {
			return etcdraftSemanticCalibrationArtifact{}, exploreErr
		}
		artifact, err = newEtcdraftSemanticCalibrationArtifact(
			inputs, baseline, nil, &failure.Failure, audits, nil,
		)
	}
	if err != nil || validateEtcdraftSemanticJournalEvidence(artifact, journal) != nil {
		return etcdraftSemanticCalibrationArtifact{}, errors.New("ETCDRAFT_SEMANTIC_RUN_RESULT_INVALID")
	}
	if err := writeStatelessAgentJSON(clean, "artifact.json", artifact); err != nil {
		return etcdraftSemanticCalibrationArtifact{}, err
	}
	return artifact, exploreErr
}

func validateEtcdraftSemanticCalibrationDirectory(directory string, resume bool) error {
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("ETCDRAFT_SEMANTIC_RUN_DIRECTORY_INVALID")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) < 2 || len(entries) > 3 {
		return errors.New("ETCDRAFT_SEMANTIC_RUN_LAYOUT_INVALID")
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 ||
			(entry.Name() != "spec.json" && entry.Name() != "provider" && entry.Name() != "artifact.json") {
			return errors.New("ETCDRAFT_SEMANTIC_RUN_LAYOUT_INVALID")
		}
		if entry.Name() == "provider" && !entry.IsDir() {
			return errors.New("ETCDRAFT_SEMANTIC_RUN_PROVIDER_INVALID")
		}
		if entry.Name() != "provider" && entry.IsDir() {
			return errors.New("ETCDRAFT_SEMANTIC_RUN_FILE_INVALID")
		}
		seen[entry.Name()] = true
	}
	if !seen["spec.json"] || !seen["provider"] || (!resume && seen["artifact.json"]) {
		return errors.New("ETCDRAFT_SEMANTIC_RUN_LAYOUT_INVALID")
	}
	return nil
}

func validateEtcdraftSemanticJournalEvidence(
	artifact etcdraftSemanticCalibrationArtifact,
	journal *semanticExplorerCallJournal,
) error {
	if journal == nil || journal.core == nil {
		return errors.New("ETCDRAFT_SEMANTIC_JOURNAL_INVALID")
	}
	audits, err := journal.Audits()
	if err != nil || !reflect.DeepEqual(audits, artifact.ProviderCalls) ||
		len(journal.core.recovered) != len(audits) {
		return errors.New("ETCDRAFT_SEMANTIC_JOURNAL_AUDIT_MISMATCH")
	}
	calls := []controlexperiment.SemanticExplorerCall(nil)
	if artifact.Explorer != nil {
		calls = artifact.Explorer.Calls
	} else if artifact.Failure != nil {
		calls = artifact.Failure.Calls
	}
	for index, call := range calls {
		recovered := journal.core.recovered[index]
		if recovered.result == nil || recovered.result.Status != controlexperiment.StatelessAgentCallContentReady ||
			recovered.intent.SearchRequestDigest != call.Request.Digest ||
			!bytes.Equal(recovered.result.Content, call.ResponseBytes) || recovered.result.Work != call.ModelWork {
			return errors.New("ETCDRAFT_SEMANTIC_JOURNAL_CONTENT_MISMATCH")
		}
	}
	if len(journal.core.recovered) > len(calls) {
		terminal := journal.core.recovered[len(calls)]
		if terminal.result == nil || terminal.result.Status != controlexperiment.StatelessAgentCallFailed {
			return errors.New("ETCDRAFT_SEMANTIC_JOURNAL_TERMINAL_MISMATCH")
		}
	}
	return nil
}

func validateAgentKeyFileName(value string) bool {
	return strings.TrimSpace(value) != "" && !strings.ContainsAny(value, "\r\n\x00")
}
