package controlexperiment

import (
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/psscore"
)

const PSSFeedbackVersion = "consensus-atlas/pss-feedback/v1"

type PSSFeedbackState struct {
	Key       string `json:"key"`
	FirstStep int    `json:"first_step"`
	Visits    int    `json:"visits"`
}

// PSSFeedback is deliberately small. It contains no externally supplied state
// keys: NewPSSFeedback first reconstructs every sampled Runtime control state
// and re-runs the bound SemanticMapper over Trace Evidence.
type PSSFeedback struct {
	SchemaVersion      string             `json:"schema_version"`
	ID                 string             `json:"id"`
	SourceBundleDigest string             `json:"source_bundle_digest"`
	TraceDigest        string             `json:"trace_digest"`
	RuntimeDigest      string             `json:"runtime_digest"`
	EvidenceDigest     string             `json:"evidence_digest"`
	ManifestDigest     string             `json:"manifest_digest"`
	MapperID           string             `json:"mapper_id"`
	SamplesDigest      string             `json:"samples_digest"`
	Samples            int                `json:"samples"`
	States             []PSSFeedbackState `json:"states"`
	StatesDigest       string             `json:"states_digest"`
	Digest             string             `json:"digest"`
}

func NewPSSFeedback(
	id string,
	bundle ExecutionBundle,
	mapper psscore.SemanticMapper,
) (PSSFeedback, error) {
	if id == "" || mapper == nil || mapper.ID() == "" || mapper.ID() != bundle.Identity.PSSID {
		return PSSFeedback{}, errors.New("EXPERIMENT_PSS_FEEDBACK_COMPOSITION_INVALID")
	}
	projected, err := reprojectBundleCorePSS(bundle, mapper)
	if err != nil {
		return PSSFeedback{}, err
	}
	samplesDigest, err := portableJSONDigest(projected)
	if err != nil {
		return PSSFeedback{}, err
	}
	if samplesDigest != bundle.Run.CorePSSSamplesDigest {
		return PSSFeedback{}, errors.New("EXPERIMENT_PSS_FEEDBACK_SAMPLE_DIGEST_MISMATCH")
	}
	states := summarizeFeedbackStates(projected)
	statesDigest, err := portableJSONDigest(states)
	if err != nil {
		return PSSFeedback{}, err
	}
	runtimeDigest, evidenceDigest, err := feedbackProvenanceDigests(bundle)
	if err != nil {
		return PSSFeedback{}, err
	}
	feedback := PSSFeedback{
		SchemaVersion: PSSFeedbackVersion, ID: id,
		SourceBundleDigest: bundle.Digest, TraceDigest: bundle.Trace.Digest,
		RuntimeDigest: runtimeDigest, EvidenceDigest: evidenceDigest,
		ManifestDigest: bundle.Identity.ManifestDigest, MapperID: mapper.ID(),
		SamplesDigest: samplesDigest, Samples: len(projected),
		States: states, StatesDigest: statesDigest,
	}
	feedback, err = feedback.seal()
	if err != nil {
		return PSSFeedback{}, err
	}
	if err := feedback.Validate(); err != nil {
		return PSSFeedback{}, err
	}
	return feedback, nil
}

func (feedback PSSFeedback) Validate() error {
	if feedback.SchemaVersion != PSSFeedbackVersion || feedback.ID == "" ||
		!validSHA256(feedback.SourceBundleDigest) || !validSHA256(feedback.TraceDigest) ||
		!validSHA256(feedback.RuntimeDigest) || !validSHA256(feedback.EvidenceDigest) ||
		!validSHA256(feedback.ManifestDigest) || feedback.MapperID == "" ||
		!validSHA256(feedback.SamplesDigest) || feedback.Samples <= 0 ||
		len(feedback.States) == 0 || len(feedback.States) > feedback.Samples ||
		!validSHA256(feedback.StatesDigest) {
		return errors.New("EXPERIMENT_PSS_FEEDBACK_INVALID")
	}
	seen := make(map[string]bool, len(feedback.States))
	lastFirst := -1
	visits := 0
	for _, state := range feedback.States {
		if !validSHA256(state.Key) || state.FirstStep < 0 || state.FirstStep < lastFirst ||
			state.Visits <= 0 || seen[state.Key] {
			return errors.New("EXPERIMENT_PSS_FEEDBACK_STATE_INVALID")
		}
		seen[state.Key] = true
		lastFirst = state.FirstStep
		visits += state.Visits
	}
	if visits != feedback.Samples {
		return errors.New("EXPERIMENT_PSS_FEEDBACK_VISIT_COUNT_MISMATCH")
	}
	statesDigest, err := portableJSONDigest(feedback.States)
	if err != nil || statesDigest != feedback.StatesDigest {
		return errors.New("EXPERIMENT_PSS_FEEDBACK_STATES_DIGEST_MISMATCH")
	}
	sealed, err := feedback.seal()
	if err != nil || sealed.Digest != feedback.Digest {
		return errors.New("EXPERIMENT_PSS_FEEDBACK_DIGEST_MISMATCH")
	}
	return nil
}

func (feedback PSSFeedback) ValidateBundle(
	bundle ExecutionBundle,
	mapper psscore.SemanticMapper,
) error {
	if err := feedback.Validate(); err != nil {
		return err
	}
	want, err := NewPSSFeedback(feedback.ID, bundle, mapper)
	if err != nil {
		return err
	}
	if want.Digest != feedback.Digest {
		return errors.New("EXPERIMENT_PSS_FEEDBACK_BUNDLE_MISMATCH")
	}
	return nil
}

func (feedback PSSFeedback) seal() (PSSFeedback, error) {
	feedback.States = append([]PSSFeedbackState(nil), feedback.States...)
	feedback.Digest = ""
	digest, err := portableJSONDigest(feedback)
	if err != nil {
		return PSSFeedback{}, err
	}
	feedback.Digest = digest
	return feedback, nil
}

func summarizeFeedbackStates(samples []CorePSSSample) []PSSFeedbackState {
	indexes := make(map[string]int)
	states := make([]PSSFeedbackState, 0)
	for _, sample := range samples {
		if index, exists := indexes[sample.Key]; exists {
			states[index].Visits++
			continue
		}
		indexes[sample.Key] = len(states)
		states = append(states, PSSFeedbackState{Key: sample.Key, FirstStep: sample.Step, Visits: 1})
	}
	return states
}

func feedbackProvenanceDigests(bundle ExecutionBundle) (string, string, error) {
	runtimeDigest, err := control.CanonicalDigest(struct {
		ConfigDigest    string `json:"config_digest"`
		ManifestDigest  string `json:"manifest_digest"`
		TraceSchema     string `json:"trace_schema"`
		SeedDigest      string `json:"seed_digest"`
		InitialState    string `json:"initial_state_digest"`
		QualificationID string `json:"qualification_digest"`
	}{
		ConfigDigest: bundle.Identity.ConfigDigest, ManifestDigest: bundle.Identity.ManifestDigest,
		TraceSchema: bundle.Trace.SchemaVersion, SeedDigest: bundle.Trace.SeedDigest,
		InitialState:    bundle.Trace.InitialStateDigest,
		QualificationID: bundle.Qualification.Qualification.Digest,
	})
	if err != nil {
		return "", "", err
	}
	type evidencePoint struct {
		Step   uint64 `json:"step"`
		Yield  string `json:"yield"`
		Schema string `json:"schema"`
		Digest string `json:"digest"`
	}
	points := []evidencePoint{{
		Step: 0, Yield: string(bundle.Trace.InitialEvidence.Yield),
		Schema: bundle.Trace.InitialEvidence.Payload.SchemaVersion,
		Digest: bundle.Trace.InitialEvidenceDigest,
	}}
	for _, record := range bundle.Trace.Records {
		if record.Evidence == nil {
			continue
		}
		points = append(points, evidencePoint{
			Step: record.Step, Yield: string(record.Evidence.Yield),
			Schema: record.Evidence.Payload.SchemaVersion, Digest: record.EvidenceDigest,
		})
	}
	evidenceDigest, err := control.CanonicalDigest(points)
	if err != nil {
		return "", "", err
	}
	return runtimeDigest, evidenceDigest, nil
}

func reprojectBundleCorePSS(
	bundle ExecutionBundle,
	mapper psscore.SemanticMapper,
) ([]CorePSSSample, error) {
	if err := bundle.Validate(); err != nil {
		return nil, fmt.Errorf("EXPERIMENT_PSS_FEEDBACK_BUNDLE_INVALID: %w", err)
	}
	if mapper == nil || mapper.ID() == "" || mapper.ID() != bundle.Identity.PSSID {
		return nil, errors.New("EXPERIMENT_PSS_FEEDBACK_MAPPER_MISMATCH")
	}
	snapshots, err := reconstructControlSnapshots(bundle.Trace, bundle.FinalSnapshot)
	if err != nil {
		return nil, err
	}
	evidence := bundle.Trace.InitialEvidence
	projected := make([]CorePSSSample, 0, len(snapshots))
	for step, snapshot := range snapshots {
		observation, err := mapper.Map(evidence)
		if err != nil {
			return nil, err
		}
		state, err := psscore.Project(snapshot, mapper.ID(), observation)
		if err != nil {
			return nil, err
		}
		sample := CorePSSSample{Step: step, Key: state.Digest, State: state}
		if !reflect.DeepEqual(sample, bundle.CorePSS[step]) {
			return nil, fmt.Errorf("EXPERIMENT_PSS_FEEDBACK_REPROJECTION_MISMATCH: %d", step)
		}
		projected = append(projected, sample)
		if step < len(bundle.Trace.Records) && bundle.Trace.Records[step].Evidence != nil {
			evidence = *bundle.Trace.Records[step].Evidence
			evidence.Payload.Bytes = append([]byte(nil), bundle.Trace.Records[step].Evidence.Payload.Bytes...)
		}
	}
	return projected, nil
}

// reconstructControlSnapshots reverses only fields consumed by psscore.Project.
// Adapter state, entropy and offered actions are deliberately not treated as
// protocol state by Core PSS.
func reconstructControlSnapshots(
	trace controlruntime.Trace,
	final controlruntime.Snapshot,
) ([]controlruntime.Snapshot, error) {
	if len(trace.Records) != int(final.Step) {
		return nil, errors.New("EXPERIMENT_PSS_FEEDBACK_FINAL_STEP_MISMATCH")
	}
	snapshots := make([]controlruntime.Snapshot, len(trace.Records)+1)
	current := cloneControlSnapshot(final)
	snapshots[len(trace.Records)] = cloneControlSnapshot(current)
	for index := len(trace.Records) - 1; index >= 0; index-- {
		record := trace.Records[index]
		if record.Step != uint64(index+1) || current.Step != record.Step ||
			current.LogicalTime != record.LogicalTime {
			return nil, fmt.Errorf("EXPERIMENT_PSS_FEEDBACK_TRACE_STEP_MISMATCH: %d", index+1)
		}
		before := cloneControlSnapshot(current)
		before.Step = uint64(index)
		if record.ClockAdvance != nil {
			before.LogicalTime = record.ClockAdvance.From
		}
		if err := reverseNodeTransitions(&before, record.NodeTransitions); err != nil {
			return nil, err
		}
		if err := reverseItemTransitions(&before, record.ItemTransitions); err != nil {
			return nil, err
		}
		if err := reversePartitionAction(&before, record.Action); err != nil {
			return nil, err
		}
		before.Offered = nil
		current = before
		snapshots[index] = cloneControlSnapshot(current)
	}
	return snapshots, nil
}

func reverseNodeTransitions(
	snapshot *controlruntime.Snapshot,
	transitions []controlruntime.NodeTransition,
) error {
	for _, transition := range transitions {
		found := false
		for index := range snapshot.Nodes {
			if snapshot.Nodes[index].Ref.Node != transition.Node {
				continue
			}
			if snapshot.Nodes[index] != transition.After {
				return errors.New("EXPERIMENT_PSS_FEEDBACK_NODE_TRANSITION_MISMATCH")
			}
			snapshot.Nodes[index] = transition.Before
			found = true
			break
		}
		if !found {
			return errors.New("EXPERIMENT_PSS_FEEDBACK_NODE_TRANSITION_MISSING")
		}
	}
	return nil
}

func reverseItemTransitions(
	snapshot *controlruntime.Snapshot,
	transitions []controlruntime.ItemTransition,
) error {
	for _, transition := range transitions {
		index := -1
		for candidate := range snapshot.Items {
			if snapshot.Items[candidate].ID == transition.Item {
				index = candidate
				break
			}
		}
		if index < 0 || snapshot.Items[index].State != transition.After {
			return errors.New("EXPERIMENT_PSS_FEEDBACK_ITEM_TRANSITION_MISMATCH")
		}
		if transition.Before == "" {
			snapshot.Items = append(snapshot.Items[:index], snapshot.Items[index+1:]...)
			continue
		}
		snapshot.Items[index].State = transition.Before
	}
	return nil
}

func reversePartitionAction(snapshot *controlruntime.Snapshot, action control.Action) error {
	if action.Kind != control.ActionPartition && action.Kind != control.ActionHeal {
		return nil
	}
	parameters, err := control.DecodePartitionParameters(action.Parameters)
	if err != nil {
		return err
	}
	index := -1
	for candidate := range snapshot.Partitions {
		if snapshot.Partitions[candidate].ID == parameters.ID {
			index = candidate
			break
		}
	}
	if action.Kind == control.ActionPartition {
		if index < 0 {
			return errors.New("EXPERIMENT_PSS_FEEDBACK_PARTITION_MISSING")
		}
		snapshot.Partitions = append(snapshot.Partitions[:index], snapshot.Partitions[index+1:]...)
		return nil
	}
	if index >= 0 {
		return errors.New("EXPERIMENT_PSS_FEEDBACK_PARTITION_DUPLICATE")
	}
	snapshot.Partitions = append(snapshot.Partitions, controlruntime.PartitionSnapshot{
		ID: parameters.ID, Left: append([]control.NodeID(nil), parameters.Left...),
		Right: append([]control.NodeID(nil), parameters.Right...),
	})
	sort.Slice(snapshot.Partitions, func(i, j int) bool {
		return snapshot.Partitions[i].ID < snapshot.Partitions[j].ID
	})
	return nil
}

func cloneControlSnapshot(snapshot controlruntime.Snapshot) controlruntime.Snapshot {
	copySnapshot := snapshot
	copySnapshot.Nodes = append([]controlruntime.NodeSnapshot(nil), snapshot.Nodes...)
	copySnapshot.Items = make([]controlruntime.ItemSnapshot, len(snapshot.Items))
	for index, item := range snapshot.Items {
		copySnapshot.Items[index] = item
		copySnapshot.Items[index].Value = cloneProducedItem(item.Value)
	}
	copySnapshot.Partitions = make([]controlruntime.PartitionSnapshot, len(snapshot.Partitions))
	for index, partition := range snapshot.Partitions {
		copySnapshot.Partitions[index] = partition
		copySnapshot.Partitions[index].Left = append([]control.NodeID(nil), partition.Left...)
		copySnapshot.Partitions[index].Right = append([]control.NodeID(nil), partition.Right...)
	}
	copySnapshot.CloneCounts = append([]controlruntime.CloneCounter(nil), snapshot.CloneCounts...)
	copySnapshot.Offered = append([]control.Action(nil), snapshot.Offered...)
	return copySnapshot
}

func cloneProducedItem(item control.ProducedItem) control.ProducedItem {
	copyItem := item
	copyItem.Dependencies = append([]control.ItemID(nil), item.Dependencies...)
	if item.Message != nil {
		value := *item.Message
		value.Payload.Bytes = append([]byte(nil), item.Message.Payload.Bytes...)
		if item.Message.Metadata != nil {
			value.Metadata = make(map[string]string, len(item.Message.Metadata))
			for key, entry := range item.Message.Metadata {
				value.Metadata[key] = entry
			}
		}
		copyItem.Message = &value
	}
	if item.Temporal != nil {
		value := *item.Temporal
		value.Callback.Bytes = append([]byte(nil), item.Temporal.Callback.Bytes...)
		copyItem.Temporal = &value
	}
	if item.Effect != nil {
		value := *item.Effect
		value.Request.Bytes = append([]byte(nil), item.Effect.Request.Bytes...)
		value.AllowedResults = append([]string(nil), item.Effect.AllowedResults...)
		value.AllowedFailures = append([]string(nil), item.Effect.AllowedFailures...)
		copyItem.Effect = &value
	}
	if item.Callback != nil {
		value := *item.Callback
		value.Request.Bytes = append([]byte(nil), item.Callback.Request.Bytes...)
		value.AllowedResults = append([]string(nil), item.Callback.AllowedResults...)
		copyItem.Callback = &value
	}
	if item.Response != nil {
		value := *item.Response
		value.Payload.Bytes = append([]byte(nil), item.Response.Payload.Bytes...)
		copyItem.Response = &value
	}
	if item.Observation != nil {
		value := *item.Observation
		value.Payload.Bytes = append([]byte(nil), item.Observation.Payload.Bytes...)
		copyItem.Observation = &value
	}
	return copyItem
}
