package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	etcdqualification "github.com/SuzumiyaHaruki/consensus-atlas/qualifications/etcdraftv2"
)

const (
	etcdraftStatelessCanonicalCampaignStrategy = "campaign-etcdraft-stateless-canonical-v1"
	etcdraftStatelessUniformCampaignStrategy   = "campaign-etcdraft-stateless-uniform-v1"

	etcdraftStatelessCampaignMaxDepth      = 2
	etcdraftStatelessCampaignItemsPerRoot  = 6
	etcdraftStatelessCampaignSearchPerRoot = 1500
)

type etcdraftStatelessCampaignInputs struct {
	source        controlexperiment.ExecutionBundle
	corpus        controlexperiment.StatelessRootCorpus
	qualification etcdqualification.Bundle
	admission     controlexperiment.ExecutionAdmission
	workload      controlexperiment.WorkloadPlan
}

type etcdraftStatelessCampaignRunOptions struct {
	Directory              string
	SummaryOut             string
	ObservationOut         string
	Resume                 bool
	Strategy               string
	Attempts               int
	FirstSeed              uint64
	WallClockCeilingMillis int64
	CorpusPath             string
}

func runEtcdraftStatelessCampaign(
	ctx context.Context,
	options etcdraftStatelessCampaignRunOptions,
	stdout io.Writer,
) error {
	directory, summaryOut, observationOut, err := prepareEtcdraftCampaignPaths(
		etcdraftCampaignRunOptions{
			Directory: options.Directory, SummaryOut: options.SummaryOut,
			ObservationOut: options.ObservationOut, Resume: options.Resume,
		},
	)
	if err != nil {
		return err
	}
	if stdout == nil || options.Attempts <= 0 || options.WallClockCeilingMillis <= 0 ||
		options.CorpusPath == "" ||
		(options.Strategy == etcdraftStatelessCanonicalCampaignStrategy && options.FirstSeed != 1) ||
		(options.Strategy == etcdraftStatelessUniformCampaignStrategy && options.FirstSeed == 0) {
		return errors.New("ETCDRAFT_STATELESS_CAMPAIGN_OPTIONS_INVALID")
	}
	inputs, err := loadEtcdraftStatelessCampaignInputs(ctx, options.CorpusPath)
	if err != nil {
		return err
	}
	methods, err := newEtcdraftStatelessCampaignMethods(
		options.Strategy, options.Attempts, options.FirstSeed,
	)
	if err != nil {
		return err
	}
	spec, err := newEtcdraftStatelessCampaignSpec(inputs, methods)
	if err != nil {
		return err
	}
	campaignID := "etcdraft-stateless-canonical-campaign"
	if options.Strategy == etcdraftStatelessUniformCampaignStrategy {
		campaignID = "etcdraft-stateless-uniform-campaign"
	}
	config, err := controlexperiment.NewStatelessCampaignConfig(
		campaignID, spec, options.WallClockCeilingMillis,
	)
	if err != nil {
		return err
	}
	var recovered controlexperiment.CampaignRecovery
	if options.Resume {
		recovered, err = controlexperiment.RecoverCampaignDirectory(directory, config)
	} else {
		recovered, err = controlexperiment.CreateCampaignDirectory(directory, config)
	}
	if err != nil {
		return err
	}
	var stageErr error
	if recovered.Failure != nil {
		stageErr = errors.New("ETCDRAFT_STATELESS_CAMPAIGN_DURABLY_FAILED")
	} else {
		provider, providerErr := controlexperiment.NewStatelessCampaignAttemptProvider(
			spec,
			func(ctx context.Context, request controlexperiment.CampaignAttemptRequest) (controlexperiment.StatelessCampaignExecution, error) {
				return executeEtcdraftStatelessCampaignAttempt(ctx, request, spec, inputs), nil
			},
		)
		if providerErr != nil {
			stageErr = providerErr
		} else {
			coordinator, coordinatorErr := controlexperiment.NewCampaignCoordinator(&recovered, provider)
			if coordinatorErr != nil {
				stageErr = coordinatorErr
			} else {
				_, stageErr = coordinator.Run(ctx)
			}
		}
	}
	return finishEtcdraftStatelessCampaign(
		&recovered, spec, summaryOut, observationOut, stdout, stageErr,
	)
}

func finishEtcdraftStatelessCampaign(
	recovered *controlexperiment.CampaignRecovery,
	spec controlexperiment.StatelessCampaignSpec,
	summaryOut string,
	observationOut string,
	stdout io.Writer,
	stageErr error,
) error {
	summary, summaryErr := controlexperiment.NewCampaignSummary(recovered)
	if summaryErr != nil {
		if stageErr != nil {
			return fmt.Errorf("%v; ETCDRAFT_STATELESS_SUMMARY_FAILED: %w", stageErr, summaryErr)
		}
		return summaryErr
	}
	var deferred *controlexperiment.CampaignAttemptDeferredError
	if errors.As(stageErr, &deferred) {
		// A deferred attempt has no trusted terminal artifact. Keep the final
		// no-replace output paths unused so the exact CLI invocation can resume;
		// config/checkpoint and target-owned sidecars are already durable.
		return stageErr
	}
	projections := make([]controlexperiment.StatelessCampaignArtifactProjection, 0, len(summary.Attempts))
	for index, attempt := range summary.Attempts {
		request, err := controlexperiment.NewCampaignAttemptRequest(
			recovered.Config, recovered.Checkpoints[index],
		)
		if err != nil {
			return err
		}
		encoded, err := recovered.ReadAttemptArtifact(attempt.Ordinal)
		if err != nil {
			return err
		}
		artifact, err := controlexperiment.DecodeStatelessCampaignAttemptArtifact(encoded, request)
		if err != nil {
			return err
		}
		projections = append(projections, controlexperiment.StatelessCampaignArtifactProjection{
			ArtifactDigest: attempt.Record.ArtifactDigest, Request: request, Artifact: artifact,
		})
	}
	observation, observationErr := controlexperiment.NewStatelessCampaignObservation(
		summary, spec, projections,
	)
	if err := persistCampaignSummaryNoReplace(summaryOut, summary); err != nil {
		return err
	}
	if observationErr != nil {
		if stageErr != nil {
			return fmt.Errorf("%v; ETCDRAFT_STATELESS_OBSERVATION_FAILED: %w", stageErr, observationErr)
		}
		return observationErr
	}
	if err := persistStatelessCampaignObservationNoReplace(observationOut, observation); err != nil {
		return err
	}
	fmt.Fprintf(
		stdout,
		"wrote %s\nwrote %s\nstatus=%s attempts=%d primary=%d replay=%d model_calls=%d corpus_novel_pss=%d summary=%s observation=%s\n",
		summaryOut, observationOut, summary.Status, summary.Sequence,
		summary.Totals.Primary.WorkUnits, summary.Totals.Replay.WorkUnits,
		summary.Totals.Model.Calls, observation.UnionCorpusNovelPSSStates,
		summary.Digest, observation.Digest,
	)
	return stageErr
}

func persistStatelessCampaignObservationNoReplace(
	path string,
	observation controlexperiment.StatelessCampaignObservation,
) error {
	if err := observation.Validate(); err != nil {
		return err
	}
	return persistCampaignOutputNoReplace(path, observation, "STATELESS_OBSERVATION")
}

func loadEtcdraftStatelessCampaignInputs(
	ctx context.Context,
	corpusPath string,
) (etcdraftStatelessCampaignInputs, error) {
	workload, err := etcdraftCampaignWorkload()
	if err != nil {
		return etcdraftStatelessCampaignInputs{}, err
	}
	return loadEtcdraftStatelessCampaignInputsWithWorkload(ctx, corpusPath, workload)
}

func loadEtcdraftStatelessCampaignInputsWithWorkload(
	ctx context.Context,
	corpusPath string,
	workload controlexperiment.WorkloadPlan,
) (etcdraftStatelessCampaignInputs, error) {
	if corpusPath == "" {
		return etcdraftStatelessCampaignInputs{}, errors.New("ETCDRAFT_STATELESS_CORPUS_PATH_INVALID")
	}
	if err := workload.Validate(); err != nil {
		return etcdraftStatelessCampaignInputs{}, errors.New("ETCDRAFT_STATELESS_WORKLOAD_INVALID")
	}
	_, source, err := etcdraftExecutionConfigured(
		ctx, "workload-risk-witness-calibration", 64, 1, true, "", &workload,
	)
	if err != nil {
		return etcdraftStatelessCampaignInputs{}, err
	}
	var corpus controlexperiment.StatelessRootCorpus
	if err := readStrictJSONFile(corpusPath, 64<<10, &corpus); err != nil ||
		corpus.Validate(source) != nil {
		return etcdraftStatelessCampaignInputs{}, errors.New("ETCDRAFT_STATELESS_CORPUS_INVALID")
	}
	qualification, admission, _, err := etcdraftQualifiedWorkload(ctx)
	if err != nil {
		return etcdraftStatelessCampaignInputs{}, err
	}
	return etcdraftStatelessCampaignInputs{
		source: source, corpus: corpus, qualification: qualification,
		admission: admission, workload: workload,
	}, nil
}

func newEtcdraftStatelessCampaignMethods(
	strategy string,
	attempts int,
	firstSeed uint64,
) ([]controlexperiment.StatelessTraversalMethod, error) {
	if attempts <= 0 {
		return nil, errors.New("ETCDRAFT_STATELESS_METHOD_COUNT_INVALID")
	}
	methods := make([]controlexperiment.StatelessTraversalMethod, 0, attempts)
	for ordinal := 1; ordinal <= attempts; ordinal++ {
		var (
			method controlexperiment.StatelessTraversalMethod
			err    error
		)
		switch strategy {
		case etcdraftStatelessCanonicalCampaignStrategy:
			method, err = controlexperiment.NewStatelessTraversalMethod(
				"etcdraft-stateless-canonical", controlexperiment.StatelessTraversalCanonical, "",
			)
		case etcdraftStatelessUniformCampaignStrategy:
			seed := firstSeed + uint64(ordinal-1)
			if seed < firstSeed {
				return nil, errors.New("ETCDRAFT_STATELESS_METHOD_SEED_OVERFLOW")
			}
			seedHex := strconv.FormatUint(seed, 16)
			if len(seedHex)%2 != 0 {
				seedHex = "0" + seedHex
			}
			method, err = controlexperiment.NewStatelessTraversalMethod(
				fmt.Sprintf("etcdraft-stateless-uniform-%d", seed),
				controlexperiment.StatelessTraversalSeededUniform, seedHex,
			)
		default:
			return nil, errors.New("ETCDRAFT_STATELESS_CAMPAIGN_STRATEGY_INVALID")
		}
		if err != nil {
			return nil, err
		}
		methods = append(methods, method)
	}
	return methods, nil
}

func newEtcdraftStatelessCampaignSpec(
	inputs etcdraftStatelessCampaignInputs,
	methods []controlexperiment.StatelessTraversalMethod,
) (controlexperiment.StatelessCampaignSpec, error) {
	if len(methods) == 0 || inputs.source.Validate() != nil ||
		inputs.corpus.Validate(inputs.source) != nil {
		return controlexperiment.StatelessCampaignSpec{},
			errors.New("ETCDRAFT_STATELESS_CAMPAIGN_INPUT_INVALID")
	}
	maxRootDecisions := 0
	for _, root := range inputs.corpus.Roots {
		if root.Decisions > maxRootDecisions {
			maxRootDecisions = root.Decisions
		}
	}
	qualifiedWorkCeiling := len(inputs.corpus.Roots) * etcdraftStatelessCampaignItemsPerRoot *
		(maxRootDecisions + etcdraftStatelessCampaignMaxDepth + 2)
	searchWorkCeiling := len(inputs.corpus.Roots) * etcdraftStatelessCampaignSearchPerRoot
	primaryWorkCeiling := inputs.source.Work.Primary.WorkUnits + searchWorkCeiling + qualifiedWorkCeiling
	primaryDecisionCeiling := inputs.source.Work.Primary.SchedulerDecisions +
		searchWorkCeiling + qualifiedWorkCeiling
	replayWorkCeiling := inputs.source.Work.Replay.WorkUnits + qualifiedWorkCeiling
	spec, err := controlexperiment.NewStatelessCampaignSpec(
		controlexperiment.StatelessCampaignSpec{
			ID: "etcdraft-stateless-campaign-spec", TargetID: etcdraftCampaignTargetID,
			TargetIdentityDigest: inputs.source.Identity.ManifestDigest, Methods: methods,
			CorpusDigest: inputs.corpus.Digest, SourceBundleDigest: inputs.source.Digest,
			SourceManifestDigest:      inputs.source.Identity.ManifestDigest,
			SourceWork:                inputs.source.Work,
			RootCount:                 len(inputs.corpus.Roots),
			MaxDepth:                  etcdraftStatelessCampaignMaxDepth,
			MaxWorkItemsPerRoot:       etcdraftStatelessCampaignItemsPerRoot,
			MaxSearchWorkUnitsPerRoot: etcdraftStatelessCampaignSearchPerRoot,
			Budget: controlexperiment.StatelessCampaignAttemptBudget{
				MaxPrimarySchedulerDecisions: primaryDecisionCeiling,
				MaxPrimaryWorkUnits:          primaryWorkCeiling,
				MaxReplayWorkUnits:           replayWorkCeiling,
				MaxModelCalls:                0,
				MaxModelTokens:               0,
			},
		},
	)
	if err != nil || spec.ValidateInputs(methods, inputs.corpus, inputs.source) != nil {
		return controlexperiment.StatelessCampaignSpec{},
			errors.New("ETCDRAFT_STATELESS_CAMPAIGN_SPEC_INVALID")
	}
	return spec, nil
}

func executeEtcdraftStatelessCampaignAttempt(
	ctx context.Context,
	request controlexperiment.CampaignAttemptRequest,
	spec controlexperiment.StatelessCampaignSpec,
	inputs etcdraftStatelessCampaignInputs,
) controlexperiment.StatelessCampaignExecution {
	if request.Ordinal <= 0 || request.Ordinal > len(spec.Methods) ||
		spec.ValidateInputs(spec.Methods, inputs.corpus, inputs.source) != nil {
		return failedEtcdraftStatelessCampaignExecution(
			"input", "ETCDRAFT_STATELESS_ATTEMPT_INPUT_INVALID", 0,
			controlexperiment.StatelessDFSWork{}, controlexperiment.WorkLedger{},
		)
	}
	method := spec.Methods[request.Ordinal-1]
	envelope := &controlexperiment.FaultEnvelope{
		MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
		MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	evidence := make([]controlexperiment.StatelessCorpusRootEvidence, 0, len(inputs.corpus.Roots))
	var searchWork controlexperiment.StatelessDFSWork
	var qualifiedWork controlexperiment.WorkLedger
	for rootIndex, entry := range inputs.corpus.Roots {
		root, err := inputs.corpus.Prefix(inputs.source, entry.ID)
		if err != nil {
			return failedEtcdraftStatelessCampaignExecution(
				"root", "ETCDRAFT_STATELESS_ROOT_INVALID", 0,
				searchWork, qualifiedWork,
			)
		}
		searchSpec, err := controlexperiment.NewStatelessDFSSpec(
			fmt.Sprintf("etcdraft-stateless-root-%d", rootIndex+1), root,
			etcdraftCampaignRuntimeConfig(), envelope,
			spec.MaxDepth, spec.MaxWorkItemsPerRoot, spec.MaxSearchWorkUnitsPerRoot,
		)
		if err != nil {
			return failedEtcdraftStatelessCampaignExecution(
				"search-spec", "ETCDRAFT_STATELESS_SEARCH_SPEC_INVALID", 0,
				searchWork, qualifiedWork,
			)
		}
		result, err := controlexperiment.ExploreBoundedStatelessDFSWithMethod(
			ctx, method, searchSpec, root, factory,
		)
		if err != nil {
			var searchFailure *controlexperiment.StatelessDFSExecutionError
			if errors.As(err, &searchFailure) {
				addEtcdraftStatelessSearchWork(&searchWork, searchFailure.Work)
			}
			return failedEtcdraftStatelessCampaignExecution(
				"search", "ETCDRAFT_STATELESS_SEARCH_FAILED", 0,
				searchWork, qualifiedWork,
			)
		}
		addEtcdraftStatelessSearchWork(&searchWork, result.Search.Work)
		if len(result.Search.Items) != searchSpec.MaxWorkItems ||
			result.Search.StopReason != controlexperiment.StatelessDFSStopItems {
			return failedEtcdraftStatelessCampaignExecution(
				"search", "ETCDRAFT_STATELESS_SEARCH_BOUND_NOT_REACHED", 0,
				searchWork, qualifiedWork,
			)
		}
		bundles := make([]controlexperiment.ExecutionBundle, 0, len(result.Search.Items))
		for _, item := range result.Search.Items {
			_, bundle, err := executeEtcdraftStatelessPrefix(
				ctx,
				fmt.Sprintf("etcdraft-stateless-%s-%s-%d", method.ID, entry.ID, item.Ordinal),
				result.Search, root, item, etcdraftCampaignRuntimeConfig(), etcdraftv2.ThreeNodeConfig(),
				envelope, inputs.qualification, inputs.admission, inputs.workload,
			)
			if err != nil {
				decision := item.Path.Decision
				var executionFailure *controlexperiment.ExecutionFailure
				if errors.As(err, &executionFailure) {
					decision = executionFailure.Decision
					addEtcdraftStatelessWork(&qualifiedWork, executionFailure.Work)
				}
				return failedEtcdraftStatelessCampaignExecution(
					"qualified-execution", "ETCDRAFT_STATELESS_QUALIFIED_EXECUTION_FAILED",
					decision, searchWork, qualifiedWork,
				)
			}
			bundles = append(bundles, bundle)
			addEtcdraftStatelessWork(&qualifiedWork, bundle.Work)
		}
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
		return failedEtcdraftStatelessCampaignExecution(
			"discovery", "ETCDRAFT_STATELESS_DISCOVERY_INVALID", 0,
			searchWork, qualifiedWork,
		)
	}
	return controlexperiment.StatelessCampaignExecution{
		Discovery: &discovery, SearchWork: discovery.SearchWork,
		QualifiedExecutionWork: discovery.QualifiedExecutionWork,
	}
}

func addEtcdraftStatelessSearchWork(
	total *controlexperiment.StatelessDFSWork,
	delta controlexperiment.StatelessDFSWork,
) {
	addPhase := func(target *controlexperiment.PhaseWork, source controlexperiment.PhaseWork) {
		target.SetupAttempts += source.SetupAttempts
		target.RuntimeInitializations += source.RuntimeInitializations
		target.PrepareActions += source.PrepareActions
		target.SchedulerDecisions += source.SchedulerDecisions
		target.WorkUnits = target.SetupAttempts + target.PrepareActions + target.SchedulerDecisions
	}
	addPhase(&total.FrontierReconstruction, delta.FrontierReconstruction)
	addPhase(&total.ChildMaterialization, delta.ChildMaterialization)
	addPhase(&total.ChildVerification, delta.ChildVerification)
	total.TotalWorkUnits = total.FrontierReconstruction.WorkUnits +
		total.ChildMaterialization.WorkUnits + total.ChildVerification.WorkUnits
}

func addEtcdraftStatelessWork(
	total *controlexperiment.WorkLedger,
	delta controlexperiment.WorkLedger,
) {
	addPhase := func(target *controlexperiment.PhaseWork, source controlexperiment.PhaseWork) {
		target.SetupAttempts += source.SetupAttempts
		target.RuntimeInitializations += source.RuntimeInitializations
		target.PrepareActions += source.PrepareActions
		target.SchedulerDecisions += source.SchedulerDecisions
		target.WorkUnits = target.SetupAttempts + target.PrepareActions + target.SchedulerDecisions
	}
	addPhase(&total.Primary, delta.Primary)
	addPhase(&total.Replay, delta.Replay)
	if total.Resources.WallTime == "" {
		total.Resources = delta.Resources
	}
}

func failedEtcdraftStatelessCampaignExecution(
	phase string,
	code string,
	decision int,
	search controlexperiment.StatelessDFSWork,
	qualified controlexperiment.WorkLedger,
) controlexperiment.StatelessCampaignExecution {
	return controlexperiment.StatelessCampaignExecution{
		SearchWork: search, QualifiedExecutionWork: qualified,
		Failure: &controlexperiment.MethodFailure{Phase: phase, Code: code, Decision: decision},
	}
}
