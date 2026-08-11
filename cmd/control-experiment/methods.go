package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const etcdraftMethodRuns = 3

type etcdraftMethodExecution struct {
	Reports     []controlexperiment.Report
	Bundles     []controlexperiment.ExecutionBundle
	Observation controlexperiment.MethodObservation
	Failure     *controlexperiment.MethodFailure
}

func etcdraftAdmissibleUniformMethod(
	ctx context.Context,
	decisions int,
	baseSeed uint64,
) (etcdraftMethodExecution, error) {
	return etcdraftCompletedBaselineMethod(
		ctx, decisions, baseSeed, "workload-admissible-uniform", "uniform-method",
		"etcdraft-admissible-uniform-corpus-m5.17c2",
		"etcdraft-admissible-uniform-method-m5.17c2",
		"etcdraft-admissible-uniform-ledger-m5.17c2",
		"admissible-uniform-corpus/v1",
		"etcdraft-admissible-uniform-pss-m5.17c2",
	)

}

func etcdraftActionClassMethod(
	ctx context.Context,
	decisions int,
	baseSeed uint64,
) (etcdraftMethodExecution, error) {
	reports, bundles, entries, records, err := etcdraftMethodSources(
		ctx, "workload-action-class-random", decisions, baseSeed, 1, "action-class-method",
	)
	if err != nil {
		return etcdraftMethodExecution{}, err
	}
	corpus, err := controlexperiment.NewMutationSourceCorpus(
		"etcdraft-action-class-corpus-m5.18b2", entries,
	)
	if err != nil {
		return etcdraftMethodExecution{}, err
	}
	feedback, err := controlexperiment.NewPSSFeedback(
		"etcdraft-action-class-method-m5.18b2/run-01", bundles[0], etcdraftv2.CorePSSMapper{},
	)
	if err != nil {
		return etcdraftMethodExecution{}, err
	}
	allFeedback := []controlexperiment.PSSFeedback{feedback}
	var firstFailure *controlexperiment.MethodFailure
	for index := 1; index < etcdraftMethodRuns; index++ {
		seed := baseSeed + uint64(index)
		proposalDigest, err := control.CanonicalDigest(struct {
			Strategy  string `json:"strategy"`
			Seed      uint64 `json:"seed"`
			Decisions int    `json:"decisions"`
		}{Strategy: "workload-action-class-random", Seed: seed, Decisions: decisions})
		if err != nil {
			return etcdraftMethodExecution{}, err
		}
		records = append(records, controlexperiment.MethodRecord{
			Ordinal: len(records) + 1, Kind: controlexperiment.MethodRecordProposal,
			Outcome:     controlexperiment.MethodOutcomeCompleted,
			InputDigest: entries[0].Digest, OutputDigest: proposalDigest, Work: emptyMethodWork(),
		})
		report, bundle, executionErr := etcdraftExecution(
			ctx, "workload-action-class-random", decisions, seed, true,
		)
		if executionErr != nil {
			var failed *controlexperiment.ExecutionFailure
			if !errors.As(executionErr, &failed) {
				return etcdraftMethodExecution{}, executionErr
			}
			failure := &controlexperiment.MethodFailure{
				Phase: failed.Phase, Code: failed.Code, Decision: failed.Decision,
			}
			if firstFailure == nil {
				copyFailure := *failure
				firstFailure = &copyFailure
			}
			records = append(records, controlexperiment.MethodRecord{
				Ordinal: len(records) + 1, Kind: controlexperiment.MethodRecordExecution,
				Outcome:     controlexperiment.MethodOutcomeExecutionFailed,
				InputDigest: proposalDigest, Failure: failure, Work: failed.Work,
			})
			continue
		}
		outputFeedback, err := controlexperiment.NewPSSFeedback(
			fmt.Sprintf("etcdraft-action-class-method-m5.18b2/run-%02d", index+1),
			bundle, etcdraftv2.CorePSSMapper{},
		)
		if err != nil {
			return etcdraftMethodExecution{}, err
		}
		reports, bundles = append(reports, report), append(bundles, bundle)
		allFeedback = append(allFeedback, outputFeedback)
		records = append(records, controlexperiment.MethodRecord{
			Ordinal: len(records) + 1, Kind: controlexperiment.MethodRecordExecution,
			Outcome:     controlexperiment.MethodOutcomeCompleted,
			InputDigest: proposalDigest, OutputDigest: bundle.Digest,
			ReportDigest: report.Digest, BundleDigest: bundle.Digest, Work: report.Work,
		})
	}
	ledger, err := controlexperiment.NewMethodLedger(
		"etcdraft-action-class-ledger-m5.18b2", "action-class-random-corpus/v1",
		corpus, nil, records,
	)
	if err != nil {
		return etcdraftMethodExecution{}, err
	}
	measurement, err := controlexperiment.NewMethodPSSMeasurement(
		"etcdraft-action-class-pss-m5.18b2", allFeedback,
	)
	if err != nil {
		return etcdraftMethodExecution{}, err
	}
	observation, err := controlexperiment.NewMethodObservation(
		"etcdraft-action-class-method-m5.18b2", etcdraftM517c2MethodBudget(decisions),
		ledger, measurement, nil,
	)
	if err != nil {
		return etcdraftMethodExecution{}, err
	}
	if err := observation.ValidateBundles(bundles, etcdraftv2.CorePSSMapper{}); err != nil {
		return etcdraftMethodExecution{}, err
	}
	return etcdraftMethodExecution{
		Reports: reports, Bundles: bundles, Observation: observation, Failure: firstFailure,
	}, nil
}

func etcdraftActionClassMethodV2(
	ctx context.Context,
	decisions int,
	baseSeed uint64,
) (etcdraftMethodExecution, error) {
	return etcdraftCompletedBaselineMethod(
		ctx, decisions, baseSeed, "workload-action-class-random-v2", "action-class-v2-method",
		"etcdraft-action-class-v2-corpus-m5.18b3",
		"etcdraft-action-class-v2-method-m5.18b3",
		"etcdraft-action-class-v2-ledger-m5.18b3",
		"action-class-random-v2-corpus/v1",
		"etcdraft-action-class-v2-pss-m5.18b3",
	)
}

func etcdraftCompletedBaselineMethod(
	ctx context.Context,
	decisions int,
	baseSeed uint64,
	strategy string,
	sourceIDPrefix string,
	corpusID string,
	observationID string,
	ledgerID string,
	methodID string,
	measurementID string,
) (etcdraftMethodExecution, error) {
	reports, bundles, entries, records, err := etcdraftMethodSources(
		ctx, strategy, decisions, baseSeed, etcdraftMethodRuns, sourceIDPrefix,
	)
	if err != nil {
		return etcdraftMethodExecution{}, err
	}
	corpus, err := controlexperiment.NewMutationSourceCorpus(corpusID, entries)
	if err != nil {
		return etcdraftMethodExecution{}, err
	}
	feedback := make([]controlexperiment.PSSFeedback, len(bundles))
	for index, bundle := range bundles {
		feedback[index], err = controlexperiment.NewPSSFeedback(
			fmt.Sprintf("%s/run-%02d", observationID, index+1),
			bundle, etcdraftv2.CorePSSMapper{},
		)
		if err != nil {
			return etcdraftMethodExecution{}, err
		}
	}
	ledger, err := controlexperiment.NewMethodLedger(
		ledgerID, methodID, corpus, nil, records,
	)
	if err != nil {
		return etcdraftMethodExecution{}, err
	}
	measurement, err := controlexperiment.NewMethodPSSMeasurement(
		measurementID, feedback,
	)
	if err != nil {
		return etcdraftMethodExecution{}, err
	}
	observation, err := controlexperiment.NewMethodObservation(
		observationID, etcdraftM517c2MethodBudget(decisions),
		ledger, measurement, nil,
	)
	if err != nil {
		return etcdraftMethodExecution{}, err
	}
	if err := observation.ValidateBundles(bundles, etcdraftv2.CorePSSMapper{}); err != nil {
		return etcdraftMethodExecution{}, err
	}
	return etcdraftMethodExecution{Reports: reports, Bundles: bundles, Observation: observation}, nil
}

func etcdraftPSSGuidedCorpusMethod(
	ctx context.Context,
	decisions int,
	baseSeed uint64,
) (etcdraftMethodExecution, error) {
	reports, bundles, _, _, err := etcdraftUniformSources(
		ctx, decisions, baseSeed, etcdraftMethodRuns-1, "guided-source",
	)
	if err != nil {
		return etcdraftMethodExecution{}, err
	}
	return etcdraftPSSGuidedCorpusMethodFromUniformSources(
		ctx, decisions, baseSeed, reports, bundles,
	)
}

func etcdraftPSSGuidedCorpusMethodFromUniformSources(
	ctx context.Context,
	decisions int,
	baseSeed uint64,
	reports []controlexperiment.Report,
	bundles []controlexperiment.ExecutionBundle,
) (etcdraftMethodExecution, error) {
	if len(reports) != etcdraftMethodRuns-1 || len(bundles) != len(reports) {
		return etcdraftMethodExecution{}, errors.New("ETCDRAFT_METHOD_SOURCE_COUNT_INVALID")
	}
	for index := range reports {
		if reports[index].Config.DecisionsPerRun != decisions || len(reports[index].Config.Runs) != 1 ||
			reports[index].Config.Runs[0].Policy.Version != controlexperiment.AdmissibleUniformPolicyVersion ||
			reports[index].Config.Runs[0].Policy.SeedHex != randomPolicySeed(baseSeed+uint64(index), 1) ||
			bundles[index].Identity.ReportDigest != reports[index].Digest {
			return etcdraftMethodExecution{}, errors.New("ETCDRAFT_METHOD_SOURCE_IDENTITY_INVALID")
		}
	}
	entries, records, err := etcdraftMethodSourceRecords(
		reports, bundles, "guided-source",
	)
	if err != nil {
		return etcdraftMethodExecution{}, err
	}
	corpus, err := controlexperiment.NewMutationSourceCorpus(
		"etcdraft-pss-guided-source-corpus-m5.17c2", entries,
	)
	if err != nil {
		return etcdraftMethodExecution{}, err
	}
	suffix := []control.ActionKind{
		control.ActionInvoke, control.ActionCompleteEffect,
		control.ActionDeliverMessage, control.ActionFireTemporal,
	}
	choice, err := controlexperiment.NewPSSGuidedMutationChoice(
		"etcdraft-pss-guided-choice-m5.17c2", corpus, bundles,
		etcdraftv2.CorePSSMapper{}, control.ActionDeliverMessage, suffix,
	)
	if err != nil {
		return etcdraftMethodExecution{}, err
	}
	sourceFeedback := make([]controlexperiment.PSSFeedback, len(bundles))
	for index, bundle := range bundles {
		sourceFeedback[index], err = controlexperiment.NewPSSFeedback(
			fmt.Sprintf("%s/source-%02d", choice.ID, index+1), bundle, etcdraftv2.CorePSSMapper{},
		)
		if err != nil || sourceFeedback[index].Digest != choice.FeedbackDigests[index] {
			return etcdraftMethodExecution{}, errors.New("etcdraft guided source feedback drifted")
		}
	}
	selected := choice.SelectedSourceOrdinal - 1
	config := reports[selected].Config
	config.ID = "public-etcdraft-v2-pss-guided-corpus-m5.17c2"
	config.Runs = append([]controlexperiment.RunPlan(nil), config.Runs...)
	config.Runs[0].Policy = controlexperiment.Policy{
		Version:       controlexperiment.TraceMutationPolicyVersionV2,
		ID:            "pss-guided-adjacent-message-swap-v1/run-1",
		TraceMutation: &choice.Mutation,
	}
	factory := func() (control.Adapter, error) {
		return etcdraftv2.NewWithConfig(etcdraftv2.ThreeNodeConfig())
	}
	report, bundle, executionErr := controlexperiment.ExecuteQualifiedBundle(
		ctx, config, bundles[selected].Qualification, factory, etcdraftv2.CorePSSMapper{},
		etcdraftv2.DecisionProjector{}, etcdraftv2.WorkloadRouter{},
	)
	records = append(records, controlexperiment.MethodRecord{
		Ordinal: len(records) + 1, Kind: controlexperiment.MethodRecordProposal,
		Outcome:     controlexperiment.MethodOutcomeCompleted,
		InputDigest: entries[selected].Digest, ContextDigest: choice.Digest,
		OutputDigest: choice.Mutation.Digest, Work: emptyMethodWork(),
	})
	if executionErr != nil {
		var failed *controlexperiment.ExecutionFailure
		if !errors.As(executionErr, &failed) {
			return etcdraftMethodExecution{}, executionErr
		}
		failure := &controlexperiment.MethodFailure{
			Phase: failed.Phase, Code: failed.Code, Decision: failed.Decision,
		}
		records = append(records, controlexperiment.MethodRecord{
			Ordinal: len(records) + 1, Kind: controlexperiment.MethodRecordExecution,
			Outcome:     controlexperiment.MethodOutcomeExecutionFailed,
			InputDigest: choice.Mutation.Digest, Failure: failure, Work: failed.Work,
		})
		ledger, ledgerErr := controlexperiment.NewMethodLedger(
			"etcdraft-pss-guided-ledger-m5.17c2", "pss-guided-corpus/v1",
			corpus, nil, records,
		)
		if ledgerErr != nil {
			return etcdraftMethodExecution{}, ledgerErr
		}
		measurement, measurementErr := controlexperiment.NewMethodPSSMeasurement(
			"etcdraft-pss-guided-pss-m5.17c2", sourceFeedback,
		)
		if measurementErr != nil {
			return etcdraftMethodExecution{}, measurementErr
		}
		observation, observationErr := controlexperiment.NewMethodObservation(
			"etcdraft-pss-guided-method-m5.17c2", etcdraftM517c2MethodBudget(decisions),
			ledger, measurement, &choice,
		)
		if observationErr != nil {
			return etcdraftMethodExecution{}, observationErr
		}
		if validationErr := observation.ValidateBundles(bundles, etcdraftv2.CorePSSMapper{}); validationErr != nil {
			return etcdraftMethodExecution{}, validationErr
		}
		return etcdraftMethodExecution{
			Reports: reports, Bundles: bundles, Observation: observation, Failure: failure,
		}, nil
	}

	outputFeedback, err := controlexperiment.NewPSSFeedback(
		"etcdraft-pss-guided-choice-m5.17c2/output", bundle, etcdraftv2.CorePSSMapper{},
	)
	if err != nil {
		return etcdraftMethodExecution{}, err
	}
	records = append(records, controlexperiment.MethodRecord{
		Ordinal: len(records) + 1, Kind: controlexperiment.MethodRecordExecution,
		Outcome:     controlexperiment.MethodOutcomeCompleted,
		InputDigest: choice.Mutation.Digest, OutputDigest: bundle.Digest,
		ReportDigest: report.Digest, BundleDigest: bundle.Digest, Work: report.Work,
	})
	ledger, err := controlexperiment.NewMethodLedger(
		"etcdraft-pss-guided-ledger-m5.17c2", "pss-guided-corpus/v1",
		corpus, &outputFeedback, records,
	)
	if err != nil {
		return etcdraftMethodExecution{}, err
	}
	allFeedback := append(append([]controlexperiment.PSSFeedback(nil), sourceFeedback...), outputFeedback)
	measurement, err := controlexperiment.NewMethodPSSMeasurement(
		"etcdraft-pss-guided-pss-m5.17c2", allFeedback,
	)
	if err != nil {
		return etcdraftMethodExecution{}, err
	}
	observation, err := controlexperiment.NewMethodObservation(
		"etcdraft-pss-guided-method-m5.17c2", etcdraftM517c2MethodBudget(decisions),
		ledger, measurement, &choice,
	)
	if err != nil {
		return etcdraftMethodExecution{}, err
	}
	allBundles := append(append([]controlexperiment.ExecutionBundle(nil), bundles...), bundle)
	if err := observation.ValidateBundles(allBundles, etcdraftv2.CorePSSMapper{}); err != nil {
		return etcdraftMethodExecution{}, err
	}
	return etcdraftMethodExecution{
		Reports: append(reports, report), Bundles: allBundles, Observation: observation,
	}, nil
}

func etcdraftUniformSources(
	ctx context.Context,
	decisions int,
	baseSeed uint64,
	runs int,
	idPrefix string,
) ([]controlexperiment.Report, []controlexperiment.ExecutionBundle,
	[]controlexperiment.MutationSourceEntry, []controlexperiment.MethodRecord, error) {
	return etcdraftMethodSources(
		ctx, "workload-admissible-uniform", decisions, baseSeed, runs, idPrefix,
	)
}

func etcdraftMethodSources(
	ctx context.Context,
	strategy string,
	decisions int,
	baseSeed uint64,
	runs int,
	idPrefix string,
) ([]controlexperiment.Report, []controlexperiment.ExecutionBundle,
	[]controlexperiment.MutationSourceEntry, []controlexperiment.MethodRecord, error) {
	var reports []controlexperiment.Report
	var bundles []controlexperiment.ExecutionBundle
	for index := 0; index < runs; index++ {
		report, bundle, err := etcdraftExecution(
			ctx, strategy, decisions, baseSeed+uint64(index), true,
		)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		reports = append(reports, report)
		bundles = append(bundles, bundle)
	}
	entries, records, err := etcdraftMethodSourceRecords(reports, bundles, idPrefix)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return reports, bundles, entries, records, nil
}

func etcdraftMethodSourceRecords(
	reports []controlexperiment.Report,
	bundles []controlexperiment.ExecutionBundle,
	idPrefix string,
) ([]controlexperiment.MutationSourceEntry, []controlexperiment.MethodRecord, error) {
	if len(reports) == 0 || len(reports) != len(bundles) || idPrefix == "" {
		return nil, nil, errors.New("ETCDRAFT_METHOD_SOURCE_INPUT_INVALID")
	}
	entries := make([]controlexperiment.MutationSourceEntry, 0, len(bundles))
	records := make([]controlexperiment.MethodRecord, 0, len(bundles))
	for index, bundle := range bundles {
		if reports[index].Digest == "" || bundle.Identity.ReportDigest != reports[index].Digest {
			return nil, nil, errors.New("ETCDRAFT_METHOD_SOURCE_IDENTITY_INVALID")
		}
		entry, err := controlexperiment.NewMutationSourceEntry(
			fmt.Sprintf("etcdraft-%s-%02d", idPrefix, index+1), bundle,
		)
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, entry)
		records = append(records, controlexperiment.MethodRecord{
			Ordinal: index + 1, Kind: controlexperiment.MethodRecordSource,
			Outcome:     controlexperiment.MethodOutcomeCompleted,
			InputDigest: entry.ConfigDigest, OutputDigest: entry.Digest,
			ReportDigest: reports[index].Digest, BundleDigest: bundle.Digest, Work: reports[index].Work,
		})
	}
	return entries, records, nil
}

func emptyMethodWork() controlexperiment.WorkLedger {
	return controlexperiment.WorkLedger{Resources: controlexperiment.ResourceAccounting{
		WallTime: controlexperiment.ResourceNotCollected,
		CPUTime:  controlexperiment.ResourceNotCollected,
		PeakRSS:  controlexperiment.ResourceNotCollected,
	}}
}

func etcdraftM517c2MethodBudget(decisions int) controlexperiment.MethodBudget {
	return controlexperiment.MethodBudget{
		MaxExecutionAttempts: etcdraftMethodRuns,
		MaxPrimaryWorkUnits:  etcdraftMethodRuns * (decisions + 2),
		MaxReplayWorkUnits:   etcdraftMethodRuns * (decisions + 2),
	}
}
