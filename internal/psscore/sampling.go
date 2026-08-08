package psscore

import (
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate"
)

type SemanticMapper interface {
	ID() string
	Map(control.EvidenceEnvelope) (SemanticObservation, error)
}

// OnlineSampler observes post-decision state but cannot schedule an Action.
type OnlineSampler struct {
	mapper    SemanticMapper
	mappingID string
	evidence  control.EvidenceEnvelope
	lastStep  uint64
	samples   []protocolstate.Sample
}

func NewOnlineSampler(mapper SemanticMapper, snapshot controlruntime.Snapshot, evidence control.EvidenceEnvelope) (*OnlineSampler, error) {
	if mapper == nil || mapper.ID() == "" {
		return nil, fmt.Errorf("CORE_PSS_SAMPLER_MAPPING_REQUIRED")
	}
	if err := evidence.Payload.Validate(); err != nil {
		return nil, err
	}
	evidence.Payload.Bytes = append([]byte(nil), evidence.Payload.Bytes...)
	sampler := &OnlineSampler{mapper: mapper, mappingID: mapper.ID(), evidence: evidence, lastStep: snapshot.Step}
	sample, err := sampler.project(snapshot, sampler.evidence)
	if err != nil {
		return nil, err
	}
	sampler.samples = append(sampler.samples, sample)
	return sampler, nil
}

func (sampler *OnlineSampler) Capture(record controlruntime.ActionRecord, snapshot controlruntime.Snapshot) error {
	if record.Outcome != "applied" {
		return fmt.Errorf("CORE_PSS_SAMPLE_OUTCOME_INVALID: %s", record.Outcome)
	}
	if record.Step != snapshot.Step || record.LogicalTime != snapshot.LogicalTime {
		return fmt.Errorf("CORE_PSS_SAMPLE_RECORD_SNAPSHOT_MISMATCH: record=%d/%d snapshot=%d/%d", record.Step,
			record.LogicalTime, snapshot.Step, snapshot.LogicalTime)
	}
	if snapshot.Step != sampler.lastStep+1 {
		return fmt.Errorf("CORE_PSS_SAMPLE_STEP_GAP: got=%d want=%d", snapshot.Step, sampler.lastStep+1)
	}
	nextEvidence := sampler.evidence
	if record.Evidence != nil {
		if err := record.Evidence.Payload.Validate(); err != nil {
			return err
		}
		digest, err := control.CanonicalDigest(*record.Evidence)
		if err != nil {
			return err
		}
		if digest != record.EvidenceDigest {
			return fmt.Errorf("CORE_PSS_SAMPLE_EVIDENCE_DIGEST_MISMATCH")
		}
		nextEvidence = *record.Evidence
		nextEvidence.Payload.Bytes = append([]byte(nil), record.Evidence.Payload.Bytes...)
	}
	sample, err := sampler.project(snapshot, nextEvidence)
	if err != nil {
		return err
	}
	sampler.samples = append(sampler.samples, sample)
	sampler.evidence = nextEvidence
	sampler.lastStep = snapshot.Step
	return nil
}

func (sampler *OnlineSampler) Samples() []protocolstate.Sample {
	return append([]protocolstate.Sample(nil), sampler.samples...)
}

func (sampler *OnlineSampler) Discovery() (protocolstate.DiscoverySummary, error) {
	return protocolstate.Discover(sampler.mappingID, sampler.samples)
}

func (sampler *OnlineSampler) project(snapshot controlruntime.Snapshot, evidence control.EvidenceEnvelope) (protocolstate.Sample, error) {
	step := int(snapshot.Step)
	if step < 0 || uint64(step) != snapshot.Step {
		return protocolstate.Sample{}, fmt.Errorf("CORE_PSS_SAMPLE_STEP_OVERFLOW: %d", snapshot.Step)
	}
	observation, err := sampler.mapper.Map(evidence)
	if err != nil {
		return protocolstate.Sample{}, err
	}
	state, err := Project(snapshot, sampler.mappingID, observation)
	if err != nil {
		return protocolstate.Sample{}, err
	}
	return protocolstate.Sample{Step: step, Key: state.Digest, State: state}, nil
}
