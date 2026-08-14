package main

import (
	"errors"
	"reflect"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	omnipaxosMessageLossRiskID          = "message-loss-before-decision"
	omnipaxosMilestoneWorkloadInvoked   = "workload-invoked-at-coordinator"
	omnipaxosMilestoneMessageDropped    = "consensus-message-dropped-while-inflight"
	omnipaxosMilestoneDecisionAfterDrop = "workload-decided-after-drop"
	omnipaxosScenarioProjectorID        = "omnipaxos-v2-message-loss-prefix-v1"
)

func omnipaxosMessageLossWitness() (semantic.RiskWitnessSpec, error) {
	return semantic.NewRiskWitnessSpec(
		"omnipaxos-message-loss-before-decision-v1", "paxos", omnipaxosMessageLossRiskID,
		[]string{
			omnipaxosMilestoneWorkloadInvoked,
			omnipaxosMilestoneMessageDropped,
			omnipaxosMilestoneDecisionAfterDrop,
		},
		[]semantic.RiskWitnessOrder{
			{Before: omnipaxosMilestoneWorkloadInvoked, After: omnipaxosMilestoneMessageDropped},
			{Before: omnipaxosMilestoneMessageDropped, After: omnipaxosMilestoneDecisionAfterDrop},
		},
	)
}

type omnipaxosScenarioProjector struct{}

func (omnipaxosScenarioProjector) ID() string { return omnipaxosScenarioProjectorID }

func (omnipaxosScenarioProjector) Project(
	id string,
	spec semantic.RiskWitnessSpec,
	trace controlruntime.Trace,
) (semantic.RiskWitnessResult, error) {
	want, err := omnipaxosMessageLossWitness()
	if err != nil || spec.Validate() != nil || !reflect.DeepEqual(spec, want) || trace.Validate() != nil {
		return semantic.RiskWitnessResult{}, errors.New("OMNIPAXOS_SCENARIO_PROJECTOR_INPUT_INVALID")
	}
	milestones, err := projectOmnipaxosMessageLossMilestones(trace)
	if err != nil {
		return semantic.RiskWitnessResult{}, err
	}
	return semantic.NewRiskWitnessResult(
		id, spec, trace.ManifestDigest, trace.Digest, omnipaxosScenarioProjectorID, milestones,
	)
}

func projectOmnipaxosMessageLossMilestones(
	trace controlruntime.Trace,
) ([]semantic.RiskWitnessMilestoneEvidence, error) {
	var invoke *controlruntime.ActionRecord
	var decidedAtInvoke uint64
	for index := range trace.Records {
		record := trace.Records[index]
		if record.Action.Kind != control.ActionInvoke || record.Evidence == nil {
			continue
		}
		evidence, err := omnipaxosv2.ProjectEvidence(*record.Evidence)
		if err != nil {
			return nil, err
		}
		for _, node := range evidence.Nodes {
			if node.DecidedIndex > decidedAtInvoke {
				decidedAtInvoke = node.DecidedIndex
			}
		}
		for _, node := range evidence.Nodes {
			if node.Node == record.Action.Node.Node && node.Leader == node.Node {
				copyRecord := record
				invoke = &copyRecord
				break
			}
		}
		if invoke != nil {
			break
		}
	}
	if invoke == nil {
		return []semantic.RiskWitnessMilestoneEvidence{}, nil
	}
	invokeMilestone, err := omnipaxosScenarioMilestone(
		omnipaxosMilestoneWorkloadInvoked, "trace-action", *invoke, invoke.Action.Node, nil,
	)
	if err != nil {
		return nil, err
	}
	milestones := []semantic.RiskWitnessMilestoneEvidence{invokeMilestone}
	var dropped *controlruntime.ActionRecord
	for index := range trace.Records {
		record := trace.Records[index]
		if record.Step <= invoke.Step {
			continue
		}
		if record.Evidence != nil {
			evidence, err := omnipaxosv2.ProjectEvidence(*record.Evidence)
			if err != nil {
				return nil, err
			}
			if dropped == nil && omnipaxosDecidedBeyond(evidence, decidedAtInvoke) {
				return milestones, nil
			}
			if dropped != nil {
				if participant, ok := omnipaxosDecisionParticipant(evidence, decidedAtInvoke); ok {
					decision, err := omnipaxosScenarioMilestone(
						omnipaxosMilestoneDecisionAfterDrop, "target-evidence", record,
						participant, &dropped.Action.Node,
					)
					if err != nil {
						return nil, err
					}
					return append(milestones, decision), nil
				}
			}
		}
		if dropped == nil && record.Action.Kind == control.ActionDropMessage {
			copyRecord := record
			dropped = &copyRecord
			drop, err := omnipaxosScenarioMilestone(
				omnipaxosMilestoneMessageDropped, "message-control", record, record.Action.Node, nil,
			)
			if err != nil {
				return nil, err
			}
			milestones = append(milestones, drop)
		}
	}
	return milestones, nil
}

func omnipaxosScenarioMilestone(
	id string,
	kind string,
	record controlruntime.ActionRecord,
	participant control.NodeRef,
	related *control.NodeRef,
) (semantic.RiskWitnessMilestoneEvidence, error) {
	digest, err := control.CanonicalDigest(record)
	if err != nil {
		return semantic.RiskWitnessMilestoneEvidence{}, err
	}
	value := participant
	return semantic.RiskWitnessMilestoneEvidence{
		MilestoneID: id, Step: record.Step, Kind: kind, EvidenceDigest: digest,
		Participant: &value, RelatedParticipant: related,
	}, nil
}

func omnipaxosDecidedBeyond(evidence omnipaxosv2.Evidence, index uint64) bool {
	_, ok := omnipaxosDecisionParticipant(evidence, index)
	return ok
}

func omnipaxosDecisionParticipant(evidence omnipaxosv2.Evidence, index uint64) (control.NodeRef, bool) {
	for _, node := range evidence.Nodes {
		if node.DecidedIndex > index {
			return control.NodeRef{Node: node.Node, Incarnation: 1}, true
		}
	}
	return control.NodeRef{}, false
}
