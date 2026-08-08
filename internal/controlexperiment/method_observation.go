package controlexperiment

import (
	"errors"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
)

const (
	MethodPSSMeasurementVersion = "consensus-atlas/method-pss-measurement/v1"
	MethodObservationVersion    = "consensus-atlas/method-observation/v1"
)

type MethodBudget struct {
	MaxExecutionAttempts int `json:"max_execution_attempts"`
	MaxPrimaryWorkUnits  int `json:"max_primary_work_units"`
	MaxReplayWorkUnits   int `json:"max_replay_work_units"`
}

func (budget MethodBudget) validate() error {
	if budget.MaxExecutionAttempts <= 0 || budget.MaxPrimaryWorkUnits <= 0 ||
		budget.MaxReplayWorkUnits <= 0 {
		return errors.New("EXPERIMENT_METHOD_BUDGET_INVALID")
	}
	return nil
}

type MethodPSSRun struct {
	Ordinal        int    `json:"ordinal"`
	BundleDigest   string `json:"bundle_digest"`
	FeedbackDigest string `json:"feedback_digest"`
	Samples        int    `json:"samples"`
	States         int    `json:"states"`
	NewStates      int    `json:"new_states"`
}

// MethodPSSMeasurement reports discovery only. It has no completeness
// denominator and cannot create an Oracle verdict.
type MethodPSSMeasurement struct {
	SchemaVersion  string         `json:"schema_version"`
	ID             string         `json:"id"`
	PSSID          string         `json:"pss_id"`
	Feedback       []PSSFeedback  `json:"feedback"`
	Runs           []MethodPSSRun `json:"runs"`
	TotalSamples   int            `json:"total_samples"`
	UniqueStates   int            `json:"unique_states"`
	StateSetDigest string         `json:"state_set_digest"`
	Digest         string         `json:"digest"`
}

func NewMethodPSSMeasurement(id string, feedback []PSSFeedback) (MethodPSSMeasurement, error) {
	measurement := MethodPSSMeasurement{
		SchemaVersion: MethodPSSMeasurementVersion, ID: id,
		Feedback: clonePSSFeedback(feedback),
	}
	if len(feedback) > 0 {
		measurement.PSSID = feedback[0].MapperID
	}
	seen := make(map[string]bool)
	for index, current := range feedback {
		if err := current.Validate(); err != nil {
			return MethodPSSMeasurement{}, err
		}
		newStates := 0
		for _, state := range current.States {
			if !seen[state.Key] {
				seen[state.Key] = true
				newStates++
			}
		}
		measurement.Runs = append(measurement.Runs, MethodPSSRun{
			Ordinal: index + 1, BundleDigest: current.SourceBundleDigest,
			FeedbackDigest: current.Digest, Samples: current.Samples,
			States: len(current.States), NewStates: newStates,
		})
		measurement.TotalSamples += current.Samples
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	stateSetDigest, err := portableJSONDigest(keys)
	if err != nil {
		return MethodPSSMeasurement{}, err
	}
	measurement.UniqueStates = len(keys)
	measurement.StateSetDigest = stateSetDigest
	measurement, err = measurement.seal()
	if err != nil {
		return MethodPSSMeasurement{}, err
	}
	if err := measurement.Validate(); err != nil {
		return MethodPSSMeasurement{}, err
	}
	return measurement, nil
}

func (measurement MethodPSSMeasurement) Validate() error {
	if measurement.SchemaVersion != MethodPSSMeasurementVersion || measurement.ID == "" ||
		measurement.PSSID == "" || len(measurement.Feedback) == 0 ||
		len(measurement.Feedback) != len(measurement.Runs) ||
		measurement.TotalSamples <= 0 || measurement.UniqueStates <= 0 ||
		!validSHA256(measurement.StateSetDigest) {
		return errors.New("EXPERIMENT_METHOD_PSS_MEASUREMENT_INVALID")
	}
	seen := make(map[string]bool)
	seenFeedback := make(map[string]bool, len(measurement.Feedback))
	seenBundles := make(map[string]bool, len(measurement.Feedback))
	totalSamples := 0
	for index, feedback := range measurement.Feedback {
		if err := feedback.Validate(); err != nil {
			return err
		}
		if feedback.MapperID != measurement.PSSID {
			return errors.New("EXPERIMENT_METHOD_PSS_ID_MISMATCH")
		}
		if seenFeedback[feedback.Digest] || seenBundles[feedback.SourceBundleDigest] {
			return errors.New("EXPERIMENT_METHOD_PSS_FEEDBACK_DUPLICATE")
		}
		seenFeedback[feedback.Digest] = true
		seenBundles[feedback.SourceBundleDigest] = true
		newStates := 0
		for _, state := range feedback.States {
			if !seen[state.Key] {
				seen[state.Key] = true
				newStates++
			}
		}
		run := measurement.Runs[index]
		if run.Ordinal != index+1 || run.BundleDigest != feedback.SourceBundleDigest ||
			run.FeedbackDigest != feedback.Digest || run.Samples != feedback.Samples ||
			run.States != len(feedback.States) || run.NewStates != newStates {
			return errors.New("EXPERIMENT_METHOD_PSS_RUN_MISMATCH")
		}
		totalSamples += feedback.Samples
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	stateSetDigest, err := portableJSONDigest(keys)
	if err != nil || stateSetDigest != measurement.StateSetDigest ||
		totalSamples != measurement.TotalSamples || len(keys) != measurement.UniqueStates {
		return errors.New("EXPERIMENT_METHOD_PSS_TOTAL_MISMATCH")
	}
	sealed, err := measurement.seal()
	if err != nil || sealed.Digest != measurement.Digest {
		return errors.New("EXPERIMENT_METHOD_PSS_DIGEST_MISMATCH")
	}
	return nil
}

func (measurement MethodPSSMeasurement) ValidateBundles(
	bundles []ExecutionBundle,
	mapper psscore.SemanticMapper,
) error {
	if err := measurement.Validate(); err != nil {
		return err
	}
	if len(bundles) != len(measurement.Feedback) {
		return errors.New("EXPERIMENT_METHOD_PSS_BUNDLE_COUNT_MISMATCH")
	}
	for index, feedback := range measurement.Feedback {
		if err := feedback.ValidateBundle(bundles[index], mapper); err != nil {
			return err
		}
	}
	return nil
}

func (measurement MethodPSSMeasurement) seal() (MethodPSSMeasurement, error) {
	measurement.Feedback = clonePSSFeedback(measurement.Feedback)
	measurement.Runs = append([]MethodPSSRun(nil), measurement.Runs...)
	measurement.Digest = ""
	digest, err := portableJSONDigest(measurement)
	if err != nil {
		return MethodPSSMeasurement{}, err
	}
	measurement.Digest = digest
	return measurement, nil
}

// MethodObservation is a small, self-contained method-level artifact. Full
// reports and bundles remain separate reproducible evidence files.
type MethodObservation struct {
	SchemaVersion string                   `json:"schema_version"`
	ID            string                   `json:"id"`
	Budget        MethodBudget             `json:"budget"`
	Ledger        MethodLedger             `json:"ledger"`
	Measurement   MethodPSSMeasurement     `json:"measurement"`
	Guidance      *PSSGuidedMutationChoice `json:"guidance,omitempty"`
	Digest        string                   `json:"digest"`
}

func NewMethodObservation(
	id string,
	budget MethodBudget,
	ledger MethodLedger,
	measurement MethodPSSMeasurement,
	guidance *PSSGuidedMutationChoice,
) (MethodObservation, error) {
	observation := MethodObservation{
		SchemaVersion: MethodObservationVersion, ID: id,
		Budget: budget, Ledger: ledger, Measurement: measurement,
	}
	if guidance != nil {
		copyGuidance := *guidance
		observation.Guidance = &copyGuidance
	}
	sealed, err := observation.seal()
	if err != nil {
		return MethodObservation{}, err
	}
	if err := sealed.Validate(); err != nil {
		return MethodObservation{}, err
	}
	return sealed, nil
}

func (observation MethodObservation) Validate() error {
	if observation.SchemaVersion != MethodObservationVersion || observation.ID == "" {
		return errors.New("EXPERIMENT_METHOD_OBSERVATION_INVALID")
	}
	if err := observation.Budget.validate(); err != nil {
		return err
	}
	if err := observation.Ledger.Validate(); err != nil {
		return err
	}
	if err := observation.Measurement.Validate(); err != nil {
		return err
	}
	executionAttempts := 0
	for _, record := range observation.Ledger.Records {
		if record.Kind == MethodRecordSource || record.Kind == MethodRecordExecution {
			executionAttempts++
		}
	}
	if executionAttempts > observation.Budget.MaxExecutionAttempts ||
		observation.Ledger.Totals.Primary.WorkUnits > observation.Budget.MaxPrimaryWorkUnits ||
		observation.Ledger.Totals.Replay.WorkUnits > observation.Budget.MaxReplayWorkUnits {
		return errors.New("EXPERIMENT_METHOD_BUDGET_EXCEEDED")
	}
	allowedBundles := make(map[string]bool)
	for _, source := range observation.Ledger.SourceCorpus.Entries {
		allowedBundles[source.BundleDigest] = true
	}
	for _, record := range observation.Ledger.Records {
		if record.Kind == MethodRecordExecution && record.Outcome == MethodOutcomeCompleted {
			allowedBundles[record.BundleDigest] = true
		}
	}
	for _, feedback := range observation.Measurement.Feedback {
		if !allowedBundles[feedback.SourceBundleDigest] {
			return errors.New("EXPERIMENT_METHOD_OBSERVATION_FEEDBACK_UNBOUND")
		}
	}
	if observation.Ledger.Feedback != nil {
		found := false
		for _, feedback := range observation.Measurement.Feedback {
			if feedback.Digest == observation.Ledger.Feedback.Digest {
				found = true
			}
		}
		if !found {
			return errors.New("EXPERIMENT_METHOD_OBSERVATION_LEDGER_FEEDBACK_MISSING")
		}
	}
	if observation.Guidance == nil {
		for _, record := range observation.Ledger.Records {
			if record.ContextDigest != "" {
				return errors.New("EXPERIMENT_METHOD_OBSERVATION_GUIDANCE_MISSING")
			}
		}
	} else {
		if err := observation.Guidance.Validate(); err != nil {
			return err
		}
		if observation.Guidance.CorpusDigest != observation.Ledger.SourceCorpusDigest ||
			len(observation.Guidance.FeedbackDigests) != len(observation.Ledger.SourceCorpus.Entries) ||
			len(observation.Measurement.Feedback) < len(observation.Guidance.FeedbackDigests) {
			return errors.New("EXPERIMENT_METHOD_OBSERVATION_GUIDANCE_MISMATCH")
		}
		for index, digest := range observation.Guidance.FeedbackDigests {
			if digest != observation.Measurement.Feedback[index].Digest ||
				observation.Measurement.Feedback[index].SourceBundleDigest !=
					observation.Ledger.SourceCorpus.Entries[index].BundleDigest {
				return errors.New("EXPERIMENT_METHOD_OBSERVATION_GUIDANCE_MISMATCH")
			}
		}
		bound := false
		for _, record := range observation.Ledger.Records {
			if record.Kind == MethodRecordProposal && record.Outcome == MethodOutcomeCompleted &&
				record.ContextDigest == observation.Guidance.Digest &&
				record.InputDigest == observation.Guidance.SelectedSourceEntryDigest &&
				record.OutputDigest == observation.Guidance.Mutation.Digest {
				bound = true
			}
		}
		if !bound {
			return errors.New("EXPERIMENT_METHOD_OBSERVATION_GUIDANCE_UNBOUND")
		}
	}
	sealed, err := observation.seal()
	if err != nil || sealed.Digest != observation.Digest {
		return errors.New("EXPERIMENT_METHOD_OBSERVATION_DIGEST_MISMATCH")
	}
	return nil
}

func (observation MethodObservation) ValidateBundles(
	bundles []ExecutionBundle,
	mapper psscore.SemanticMapper,
) error {
	if err := observation.Validate(); err != nil {
		return err
	}
	if err := observation.Measurement.ValidateBundles(bundles, mapper); err != nil {
		return err
	}
	if len(bundles) < len(observation.Ledger.SourceCorpus.Entries) {
		return errors.New("EXPERIMENT_METHOD_OBSERVATION_SOURCE_BUNDLE_MISSING")
	}
	for index, source := range observation.Ledger.SourceCorpus.Entries {
		if err := source.ValidateBundle(bundles[index]); err != nil {
			return err
		}
	}
	if observation.Guidance != nil {
		if err := observation.Guidance.ValidateInputs(
			observation.Ledger.SourceCorpus,
			bundles[:len(observation.Ledger.SourceCorpus.Entries)], mapper,
		); err != nil {
			return err
		}
	}
	return nil
}

func (observation MethodObservation) seal() (MethodObservation, error) {
	if observation.Guidance != nil {
		guidance := *observation.Guidance
		guidance.FeedbackDigests = append([]string(nil), guidance.FeedbackDigests...)
		guidance.SuffixPriority = append([]control.ActionKind(nil), guidance.SuffixPriority...)
		observation.Guidance = &guidance
	}
	observation.Digest = ""
	digest, err := portableJSONDigest(observation)
	if err != nil {
		return MethodObservation{}, err
	}
	observation.Digest = digest
	return observation, nil
}

func clonePSSFeedback(feedback []PSSFeedback) []PSSFeedback {
	result := make([]PSSFeedback, len(feedback))
	for index, current := range feedback {
		result[index] = current
		result[index].States = append([]PSSFeedbackState(nil), current.States...)
	}
	return result
}
