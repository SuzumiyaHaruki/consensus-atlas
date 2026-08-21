package controlexperiment

import (
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

type ScenarioSemanticExposureMode string

const (
	ScenarioSemanticExposureFull   ScenarioSemanticExposureMode = "full"
	ScenarioSemanticExposureMasked ScenarioSemanticExposureMode = "masked"

	ConsensusActorLeader     = "leader"
	ConsensusActorReplica    = "replica"
	ConsensusActorContender  = "contender"
	ConsensusSemanticUnknown = "unknown"

	ConsensusMessageVote        = "vote"
	ConsensusMessageProposal    = "proposal"
	ConsensusMessageReplication = "replication"
	ConsensusMessageHeartbeat   = "heartbeat"
	ConsensusMessageRecovery    = "recovery"

	ConsensusEpochStale   = "stale"
	ConsensusEpochCurrent = "current"
	ConsensusEpochFuture  = "future"

	ConsensusOperationNone              = "none"
	ConsensusOperationInflight          = "inflight"
	ConsensusOperationDecidedNotApplied = "decided-not-applied"

	ConsensusCoordinatorAbsent    = "absent"
	ConsensusCoordinatorPresent   = "present"
	ConsensusCoordinatorAmbiguous = "ambiguous"
)

// ConsensusElectionProgress is a compact, protocol-neutral projection of
// startup progress. A Target derives it from its own trusted evidence; it does
// not grant an Action or assert that an election will eventually complete.
type ConsensusElectionProgress struct {
	TermOrBallotChanged bool             `json:"term_or_ballot_changed"`
	CandidateNodes      []control.NodeID `json:"candidate_nodes,omitempty"`
}

// ConsensusCoordinationStatus explains whether the configured single-
// coordinator workload can currently be materialized. It deliberately avoids
// Raft/Paxos-specific epoch values and remains optional for targets without a
// coordinator concept.
type ConsensusCoordinationStatus struct {
	Status           string                    `json:"coordinator_status"`
	CoordinatorNode  control.NodeID            `json:"coordinator_node,omitempty"`
	InvokeReady      bool                      `json:"invoke_ready"`
	ElectionProgress ConsensusElectionProgress `json:"election_progress"`
}

// ConsensusActionHint is a closed semantic classification of one current
// trusted Action. It carries no raw message, absolute epoch, future fact, or
// verdict and grants no additional selector authority.
type ConsensusActionHint struct {
	ActionID       control.ActionID `json:"action_id"`
	ActionDigest   string           `json:"action_digest"`
	ActorRole      string           `json:"actor_role"`
	MessageClass   string           `json:"message_class"`
	EpochRelation  string           `json:"epoch_relation"`
	OperationState string           `json:"operation_state"`
}

type ScenarioSemanticExposure struct {
	Mode              ScenarioSemanticExposureMode `json:"mode"`
	PrefixTraceDigest string                       `json:"prefix_trace_digest"`
	SnapshotDigest    string                       `json:"snapshot_digest"`
	ActionHints       []ConsensusActionHint        `json:"action_hints"`
	Coordination      *ConsensusCoordinationStatus `json:"coordination,omitempty"`
}

func NewScenarioSemanticExposure(
	mode ScenarioSemanticExposureMode,
	frontier RiskFrontierView,
	hints []ConsensusActionHint,
) (ScenarioSemanticExposure, error) {
	exposure := ScenarioSemanticExposure{
		Mode: mode, PrefixTraceDigest: frontier.PrefixTraceDigest,
		SnapshotDigest: frontier.SnapshotDigest,
		ActionHints:    append([]ConsensusActionHint(nil), hints...),
	}
	if err := exposure.Validate(frontier); err != nil {
		return ScenarioSemanticExposure{}, err
	}
	return exposure, nil
}

func MaskScenarioSemanticExposure(
	exposure ScenarioSemanticExposure,
	frontier RiskFrontierView,
) (ScenarioSemanticExposure, error) {
	if exposure.Mode != ScenarioSemanticExposureFull || exposure.Validate(frontier) != nil {
		return ScenarioSemanticExposure{}, errors.New("EXPERIMENT_SCENARIO_SEMANTICS_MASK_INPUT_INVALID")
	}
	exposure.Mode = ScenarioSemanticExposureMasked
	exposure.ActionHints = append([]ConsensusActionHint(nil), exposure.ActionHints...)
	exposure.Coordination = nil
	for index := range exposure.ActionHints {
		exposure.ActionHints[index].ActorRole = ConsensusSemanticUnknown
		exposure.ActionHints[index].MessageClass = ConsensusSemanticUnknown
		exposure.ActionHints[index].EpochRelation = ConsensusSemanticUnknown
		exposure.ActionHints[index].OperationState = ConsensusSemanticUnknown
	}
	if err := exposure.Validate(frontier); err != nil {
		return ScenarioSemanticExposure{}, err
	}
	return exposure, nil
}

func (mode ScenarioSemanticExposureMode) Validate() error {
	if mode != ScenarioSemanticExposureFull && mode != ScenarioSemanticExposureMasked {
		return errors.New("EXPERIMENT_SCENARIO_SEMANTIC_MODE_INVALID")
	}
	return nil
}

func (exposure ScenarioSemanticExposure) Validate(frontier RiskFrontierView) error {
	if exposure.Mode.Validate() != nil || exposure.PrefixTraceDigest != frontier.PrefixTraceDigest ||
		exposure.SnapshotDigest != frontier.SnapshotDigest || len(exposure.ActionHints) != len(frontier.Actions) {
		return errors.New("EXPERIMENT_SCENARIO_SEMANTICS_INVALID")
	}
	for index, hint := range exposure.ActionHints {
		action := frontier.Actions[index]
		if hint.ActionID != action.ActionID || hint.ActionDigest != action.ActionDigest ||
			!validConsensusActorRole(hint.ActorRole) || !validConsensusMessageClass(hint.MessageClass) ||
			!validConsensusEpochRelation(hint.EpochRelation) ||
			!validConsensusOperationState(hint.OperationState) {
			return errors.New("EXPERIMENT_SCENARIO_ACTION_HINT_INVALID")
		}
		if exposure.Mode == ScenarioSemanticExposureMasked &&
			(hint.ActorRole != ConsensusSemanticUnknown || hint.MessageClass != ConsensusSemanticUnknown ||
				hint.EpochRelation != ConsensusSemanticUnknown || hint.OperationState != ConsensusSemanticUnknown) {
			return errors.New("EXPERIMENT_SCENARIO_MASKED_HINT_EXPOSED")
		}
	}
	if exposure.Coordination != nil && exposure.Coordination.Validate() != nil {
		return errors.New("EXPERIMENT_SCENARIO_COORDINATION_INVALID")
	}
	return nil
}

func (status ConsensusCoordinationStatus) Validate() error {
	switch status.Status {
	case ConsensusCoordinatorAbsent:
		if status.CoordinatorNode != "" || status.InvokeReady {
			return errors.New("EXPERIMENT_SCENARIO_COORDINATION_ABSENT_INVALID")
		}
	case ConsensusCoordinatorPresent:
		if status.CoordinatorNode == "" {
			return errors.New("EXPERIMENT_SCENARIO_COORDINATION_PRESENT_INVALID")
		}
	case ConsensusCoordinatorAmbiguous:
		if status.CoordinatorNode != "" || status.InvokeReady {
			return errors.New("EXPERIMENT_SCENARIO_COORDINATION_AMBIGUOUS_INVALID")
		}
	default:
		return errors.New("EXPERIMENT_SCENARIO_COORDINATION_STATUS_INVALID")
	}
	seen := make(map[control.NodeID]struct{}, len(status.ElectionProgress.CandidateNodes))
	for _, node := range status.ElectionProgress.CandidateNodes {
		if node == "" {
			return errors.New("EXPERIMENT_SCENARIO_COORDINATION_CANDIDATE_INVALID")
		}
		if _, duplicate := seen[node]; duplicate {
			return errors.New("EXPERIMENT_SCENARIO_COORDINATION_CANDIDATE_DUPLICATE")
		}
		seen[node] = struct{}{}
	}
	return nil
}

func validConsensusActorRole(value string) bool {
	return value == ConsensusActorLeader || value == ConsensusActorReplica ||
		value == ConsensusActorContender || value == ConsensusSemanticUnknown
}

func validConsensusMessageClass(value string) bool {
	return value == ConsensusMessageVote || value == ConsensusMessageProposal ||
		value == ConsensusMessageReplication || value == ConsensusMessageHeartbeat ||
		value == ConsensusMessageRecovery || value == ConsensusSemanticUnknown
}

func validConsensusEpochRelation(value string) bool {
	return value == ConsensusEpochStale || value == ConsensusEpochCurrent ||
		value == ConsensusEpochFuture || value == ConsensusSemanticUnknown
}

func validConsensusOperationState(value string) bool {
	return value == ConsensusOperationNone || value == ConsensusOperationInflight ||
		value == ConsensusOperationDecidedNotApplied || value == ConsensusSemanticUnknown
}
