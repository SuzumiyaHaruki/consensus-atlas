// Package raft contains frozen semantic definitions shared by strict Raft
// implementations. Target adapters remain responsible for projecting their
// own opaque evidence into these family milestones.
package raft

import "github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"

const (
	LeaderChangeWithInflightProposalRiskID = "leader-change-with-inflight-proposal"

	MilestoneWorkloadInvokedAtCoordinator = "workload-invoked-at-coordinator"
	MilestoneCoordinatorChangedInflight   = "coordinator-changed-while-inflight"
	MilestoneOldCoordinatorRestarted      = "old-coordinator-restarted-after-change"
)

// LeaderChangeWithInflightProposalWitness freezes the smallest currently
// needed family partial order. It is intentionally not an open temporal DSL.
func LeaderChangeWithInflightProposalWitness() (semantic.RiskWitnessSpec, error) {
	return semantic.NewRiskWitnessSpec(
		"raft-leader-change-with-inflight-proposal-v1",
		"raft",
		LeaderChangeWithInflightProposalRiskID,
		[]string{
			MilestoneWorkloadInvokedAtCoordinator,
			MilestoneCoordinatorChangedInflight,
			MilestoneOldCoordinatorRestarted,
		},
		[]semantic.RiskWitnessOrder{
			{
				Before: MilestoneWorkloadInvokedAtCoordinator,
				After:  MilestoneCoordinatorChangedInflight,
			},
			{
				Before: MilestoneCoordinatorChangedInflight,
				After:  MilestoneOldCoordinatorRestarted,
			},
		},
	)
}
