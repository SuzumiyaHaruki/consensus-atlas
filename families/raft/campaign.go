package raft

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
)

const CampaignSpecVersion = 1

const (
	categoryTransition = "transition"
	categoryOrdering   = "ordering"
	categoryFault      = "fault"
	categoryBoundary   = "boundary"
	categoryProperty   = "property"
)

const (
	capCampaign       = "explicit-campaign"
	capMessage        = "message-release-control"
	capMessageFault   = "message-drop-duplicate-partition"
	capStorage        = "visible-write-durable-sync"
	capRestart        = "power-loss-restart"
	capCrashCutpoint  = "ready-crash-cutpoints"
	capApply          = "application-apply"
	capExactBarrier   = "exact-ready-send-barriers"
	capNaturalTimeout = "natural-election-timeout-replay"
)

// CampaignSpec is a small, reviewable Raft Family Pack input. Bounds and
// dimensions are frozen before a campaign; an Agent never writes the compiled
// obligations or their support status directly.
type CampaignSpec struct {
	Schema            string                `json:"$schema,omitempty"`
	Version           int                   `json:"version"`
	ID                string                `json:"id"`
	Family            string                `json:"family"`
	PSSID             string                `json:"pss_id"`
	FaultModel        string                `json:"fault_model"`
	Bounds            CampaignBounds        `json:"bounds"`
	Transitions       []TransitionDimension `json:"transitions"`
	MessageTypes      []string              `json:"message_types"`
	FaultMessageTypes []string              `json:"fault_message_types"`
	CrashRoles        []string              `json:"crash_roles"`
	StorageCutpoints  []string              `json:"storage_cutpoints"`
	LogRelations      []string              `json:"log_relations"`
	PartitionShapes   []string              `json:"partition_shapes"`
	Evaluation        CampaignEvaluation    `json:"evaluation"`
}

type CampaignBounds struct {
	Nodes          int `json:"nodes"`
	ElectionRounds int `json:"election_rounds"`
	Proposals      int `json:"proposals"`
	Crashes        int `json:"crashes"`
	Restarts       int `json:"restarts"`
	Partitions     int `json:"partitions"`
}

type TransitionDimension struct {
	ID          string         `json:"id"`
	From        string         `json:"from"`
	To          string         `json:"to"`
	Trigger     core.EventKind `json:"trigger"`
	MessageType string         `json:"message_type,omitempty"`
	Requires    []string       `json:"requires"`
}

type CampaignEvaluation struct {
	Weights   map[string]float64 `json:"weights"`
	Threshold coverage.Threshold `json:"threshold"`
}

func (spec CampaignSpec) Validate() error {
	if spec.Version != CampaignSpecVersion {
		return fmt.Errorf("unsupported raft campaign spec version %d", spec.Version)
	}
	if spec.ID == "" || spec.Family != "raft" || spec.PSSID != PSSID || spec.FaultModel != "cft" {
		return errors.New("campaign id, raft family, raft PSS, and cft fault model are required")
	}
	if spec.Bounds.Nodes < 3 || spec.Bounds.Nodes > MaxCanonicalNodes {
		return fmt.Errorf("campaign node bound must be between 3 and %d", MaxCanonicalNodes)
	}
	bounded := []struct {
		name  string
		value int
		min   int
		max   int
	}{
		{"election_rounds", spec.Bounds.ElectionRounds, 1, 16},
		{"proposals", spec.Bounds.Proposals, 1, 16},
		{"crashes", spec.Bounds.Crashes, 1, 8},
		{"restarts", spec.Bounds.Restarts, 1, 8},
		{"partitions", spec.Bounds.Partitions, 1, 8},
	}
	for _, bound := range bounded {
		if bound.value < bound.min || bound.value > bound.max {
			return fmt.Errorf("campaign %s bound must be between %d and %d", bound.name, bound.min, bound.max)
		}
	}
	if len(spec.Transitions) == 0 {
		return errors.New("campaign requires at least one role transition")
	}
	transitionIDs := make(map[string]bool, len(spec.Transitions))
	for index, transition := range spec.Transitions {
		if transition.ID == "" || transition.From == "" || transition.To == "" || transition.From == transition.To ||
			!uniqueStrings(transition.Requires) || len(transition.Requires) == 0 {
			return fmt.Errorf("transitions[%d] has an invalid id, role pair, or capability list", index)
		}
		if transitionIDs[transition.ID] {
			return fmt.Errorf("duplicate transition id %q", transition.ID)
		}
		transitionIDs[transition.ID] = true
		switch transition.Trigger {
		case core.EventCampaign, core.EventMessage, core.EventTimeout:
		default:
			return fmt.Errorf("transition %s has unsupported trigger %q", transition.ID, transition.Trigger)
		}
		if transition.Trigger == core.EventMessage && transition.MessageType == "" {
			return fmt.Errorf("message transition %s requires message_type", transition.ID)
		}
		if transition.Trigger != core.EventMessage && transition.MessageType != "" {
			return fmt.Errorf("non-message transition %s cannot select message_type", transition.ID)
		}
	}
	if !uniqueStrings(spec.MessageTypes) || !uniqueStrings(spec.FaultMessageTypes) ||
		!uniqueStrings(spec.CrashRoles) || !uniqueStrings(spec.StorageCutpoints) ||
		!uniqueStrings(spec.LogRelations) || !uniqueStrings(spec.PartitionShapes) {
		return errors.New("campaign dimension arrays must be non-empty, unique strings")
	}
	messages := stringSet(spec.MessageTypes)
	for _, messageType := range spec.FaultMessageTypes {
		if !messages[messageType] {
			return fmt.Errorf("fault message type %q is absent from message_types", messageType)
		}
	}
	roles := map[string]bool{"follower": true, "pre-candidate": true, "candidate": true, "leader": true}
	for _, role := range spec.CrashRoles {
		if !roles[role] {
			return fmt.Errorf("unknown raft crash role %q", role)
		}
	}
	cutpoints := map[string]bool{
		"before-persist": true, "persist-before-sync": true, "sync-before-release": true,
		"release-before-ack": true, "apply-before-ack": true,
	}
	for _, cutpoint := range spec.StorageCutpoints {
		if !cutpoints[cutpoint] {
			return fmt.Errorf("unknown host cutpoint %q", cutpoint)
		}
	}
	for _, relation := range spec.LogRelations {
		if relation != LogSameNonEmpty && relation != LogStrictPrefix {
			return fmt.Errorf("log relation %q is not a safe reachable-shape obligation", relation)
		}
	}
	for _, shape := range spec.PartitionShapes {
		if shape != PartitionMinorityQuorum && shape != PartitionIsolateLeader {
			return fmt.Errorf("unknown partition shape %q", shape)
		}
	}
	if err := validateEvaluation(spec.Evaluation); err != nil {
		return err
	}
	return nil
}

// CompileCampaign deterministically expands a validated integration profile
// into a separate campaign denominator. The integration profile is an input
// identity and capability vocabulary, not a source of coarse obligations.
func CompileCampaign(base coverage.Profile, manifest driver.Manifest, spec CampaignSpec) (coverage.Profile, error) {
	if err := base.Validate(); err != nil {
		return coverage.Profile{}, fmt.Errorf("validate integration profile: %w", err)
	}
	if err := spec.Validate(); err != nil {
		return coverage.Profile{}, fmt.Errorf("validate raft campaign spec: %w", err)
	}
	if base.PSSID != spec.PSSID || len(base.Nodes) != spec.Bounds.Nodes {
		return coverage.Profile{}, fmt.Errorf("campaign scope does not match integration profile PSS/nodes")
	}
	capabilities, err := capabilityMap(manifest)
	if err != nil {
		return coverage.Profile{}, err
	}
	for _, required := range base.RequiredCapabilities {
		if _, ok := capabilities[required]; !ok {
			return coverage.Profile{}, fmt.Errorf("driver manifest omits integration capability %q", required)
		}
	}

	compiler := campaignCompiler{spec: spec, capabilities: capabilities}
	compiler.compileTransitions()
	compiler.compileOrderings()
	compiler.compileFaults()
	compiler.compileBoundaries()
	compiler.compileProperties()
	if compiler.err != nil {
		return coverage.Profile{}, compiler.err
	}
	sort.Slice(compiler.obligations, func(i, j int) bool { return compiler.obligations[i].ID < compiler.obligations[j].ID })
	required := make([]string, 0, len(compiler.required))
	for capability := range compiler.required {
		required = append(required, capability)
	}
	sort.Strings(required)
	profile := coverage.Profile{
		Version:              coverage.ProfileVersion,
		ID:                   "raft-campaign/" + spec.ID,
		Protocol:             base.Protocol,
		PSSID:                spec.PSSID,
		RuntimeProfile:       base.RuntimeProfile,
		Nodes:                append([]string(nil), base.Nodes...),
		RequiredCapabilities: required,
		Coverage: coverage.CoverageDefinition{
			Weights:     cloneWeights(spec.Evaluation.Weights),
			Obligations: compiler.obligations,
		},
		Threshold: spec.Evaluation.Threshold,
	}
	if err := profile.Validate(); err != nil {
		return coverage.Profile{}, fmt.Errorf("validate compiled campaign profile: %w", err)
	}
	return profile, nil
}

type campaignCompiler struct {
	spec         CampaignSpec
	capabilities map[string]driver.Capability
	required     map[string]bool
	obligations  []coverage.Obligation
	ids          map[string]bool
	err          error
}

func (compiler *campaignCompiler) add(item coverage.Obligation) {
	if compiler.err != nil {
		return
	}
	if compiler.ids == nil {
		compiler.ids = make(map[string]bool)
		compiler.required = make(map[string]bool)
	}
	if compiler.ids[item.ID] {
		compiler.err = fmt.Errorf("campaign compiler produced duplicate obligation %q", item.ID)
		return
	}
	compiler.ids[item.ID] = true
	sort.Strings(item.Requires)
	for _, capability := range item.Requires {
		if _, exists := compiler.capabilities[capability]; !exists {
			compiler.err = fmt.Errorf("campaign obligation %s requires undeclared capability %q", item.ID, capability)
			return
		}
		compiler.required[capability] = true
	}
	item.Status = coverage.StatusSupported
	for _, capability := range item.Requires {
		if !compiler.capabilities[capability].Supported {
			item.Status = coverage.StatusUnsupported
		}
	}
	compiler.obligations = append(compiler.obligations, item)
}

func (compiler *campaignCompiler) compileTransitions() {
	for _, dimension := range compiler.spec.Transitions {
		selector := coverage.TracePredicate{
			EventKind: dimension.Trigger, MessageType: dimension.MessageType,
			Semantic: semantic(RelationTargetRoleBefore, dimension.From, nil),
		}
		witness := coverage.TracePredicate{
			EventKind: dimension.Trigger, MessageType: dimension.MessageType,
			ObservationKind: "transition", ObservationLabel: "transition:" + dimension.From + "->" + dimension.To,
			Semantic: semantic(RelationTargetRoleAfter, dimension.To, nil),
		}
		compiler.add(obligation(
			"transition."+slug(dimension.ID), categoryTransition,
			fmt.Sprintf("Reach the Raft role transition %s -> %s through %s.", dimension.From, dimension.To, dimension.Trigger),
			coverage.EvidenceRequirement{Reach: []coverage.TracePredicate{selector}, Observe: []coverage.TracePredicate{witness}},
			[]string{"trace-integrity"}, dimension.Requires, coverage.RiskStandard,
		))
	}
}

func (compiler *campaignCompiler) compileOrderings() {
	hostPairs := []struct {
		id            string
		before, after core.EventKind
		description   string
	}{
		{"persist-sync", core.EventPersist, core.EventSync, "Visible persistence precedes durable synchronization in one output batch."},
		{"sync-release", core.EventSync, core.EventEmit, "Durable synchronization precedes message release in one output batch."},
		{"sync-apply", core.EventSync, core.EventApply, "Durable synchronization precedes application in one output batch."},
		{"release-ack", core.EventEmit, core.EventAcknowledge, "Message release precedes acknowledgement in one output batch."},
		{"apply-ack", core.EventApply, core.EventAcknowledge, "Application precedes acknowledgement in one output batch."},
	}
	for _, pair := range hostPairs {
		before, after := event(pair.before), event(pair.after)
		compiler.add(obligation(
			"ordering.host."+pair.id, categoryOrdering, pair.description,
			orderedEvidence(before, after, coverage.OrderingConstraint{Before: before, After: after, SameGroup: true}),
			[]string{"trace-integrity"}, []string{capStorage}, coverage.RiskStandard,
		))
	}
	for _, messageType := range compiler.spec.MessageTypes {
		before := coverage.TracePredicate{EventKind: core.EventEmit, MessageType: messageType, ObservationLabel: "message:released"}
		after := coverage.TracePredicate{EventKind: core.EventMessage, MessageType: messageType, ObservationLabel: "message:delivered"}
		compiler.add(obligation(
			"ordering.message-release-delivery."+slug(messageType), categoryOrdering,
			fmt.Sprintf("A captured %s message is released before the same message is delivered.", messageType),
			orderedEvidence(before, after, coverage.OrderingConstraint{Before: before, After: after, SameMessage: true}),
			[]string{"trace-integrity"}, []string{capMessage}, coverage.RiskStandard,
		))
	}
	propose := event(core.EventPropose)
	commit := coverage.TracePredicate{ObservationKind: "commit", ObservationLabel: "commit"}
	compiler.add(obligation(
		"ordering.propose-commit", categoryOrdering, "A proposal precedes a non-empty committed application value.",
		orderedEvidence(propose, commit, coverage.OrderingConstraint{Before: propose, After: commit}),
		[]string{"trace-integrity", "agreement"}, []string{capMessage, capApply}, coverage.RiskHigh,
	))
	emit, sync := event(core.EventEmit), event(core.EventSync)
	compiler.add(obligation(
		"ordering.ready-release-before-sync", categoryOrdering,
		"Exercise a legal native Ready overlap in which message release precedes synchronization.",
		orderedEvidence(emit, sync, coverage.OrderingConstraint{Before: emit, After: sync, SameGroup: true}),
		[]string{"trace-integrity"}, []string{capExactBarrier}, coverage.RiskHigh,
	))
	partition, heal := event(core.EventPartition), event(core.EventHeal)
	compiler.add(obligation(
		"ordering.partition-heal", categoryOrdering, "A network partition is installed and later healed.",
		orderedEvidence(partition, heal, coverage.OrderingConstraint{Before: partition, After: heal}),
		[]string{"trace-integrity"}, []string{capMessageFault}, coverage.RiskStandard,
	))
}

func (compiler *campaignCompiler) compileFaults() {
	for _, role := range compiler.spec.CrashRoles {
		crash := coverage.TracePredicate{
			EventKind: core.EventCrash, ObservationKind: "fault", ObservationLabel: "fault:crash",
			Semantic: semantic(RelationTargetRoleBefore, role, nil),
		}
		compiler.add(obligation(
			"fault.crash-role."+slug(role), categoryFault, "Crash a node while it is a Raft "+role+".",
			sameEvidence(crash), []string{"trace-integrity"}, []string{capCrashCutpoint}, coverage.RiskHigh,
		))
	}
	for _, cutpoint := range compiler.spec.StorageCutpoints {
		crash := coverage.TracePredicate{
			EventKind: core.EventCrash, HostCutpoint: cutpoint,
			ObservationKind: "fault", ObservationLabel: "fault:crash",
		}
		compiler.add(obligation(
			"fault.crash-cutpoint."+slug(cutpoint), categoryFault, "Crash at host cutpoint "+cutpoint+".",
			sameEvidence(crash), []string{"trace-integrity"}, []string{capCrashCutpoint, capStorage}, coverage.RiskCritical,
		))
	}
	for _, messageType := range compiler.spec.FaultMessageTypes {
		dropped := coverage.TracePredicate{
			EventKind: core.EventMessage, MessageType: messageType, Outcome: "dropped",
			ObservationKind: "transport", ObservationLabel: "message:dropped",
		}
		compiler.add(obligation(
			"fault.message-drop."+slug(messageType), categoryFault, "Drop one captured "+messageType+" message.",
			sameEvidence(dropped), []string{"trace-integrity"}, []string{capMessage, capMessageFault}, coverage.RiskHigh,
		))
		duplicated := coverage.TracePredicate{
			EventKind: core.EventDuplicate, MessageType: messageType,
			ObservationKind: "transport", ObservationLabel: "message:duplicated",
			ObservationEvidence: map[string]string{"type": messageType},
		}
		compiler.add(obligation(
			"fault.message-duplicate."+slug(messageType), categoryFault, "Duplicate one captured "+messageType+" message.",
			sameEvidence(duplicated), []string{"trace-integrity"}, []string{capMessage, capMessageFault}, coverage.RiskHigh,
		))
	}
	crash := coverage.TracePredicate{EventKind: core.EventCrash, ObservationLabel: "fault:crash"}
	restart := coverage.TracePredicate{EventKind: core.EventRestart, ObservationLabel: "fault:restart"}
	compiler.add(obligation(
		"fault.crash-restart", categoryFault, "Restart the same node after a power-loss crash.",
		orderedEvidence(crash, restart, coverage.OrderingConstraint{Before: crash, After: restart, SameTarget: true}),
		[]string{"trace-integrity"}, []string{capCrashCutpoint, capRestart}, coverage.RiskCritical,
	))
}

func (compiler *campaignCompiler) compileBoundaries() {
	election := coverage.TracePredicate{
		ObservationKind: "transition", ObservationLabel: "transition:follower->candidate",
	}
	for round := 1; round <= compiler.spec.Bounds.ElectionRounds; round++ {
		compiler.add(countObligation(
			fmt.Sprintf("boundary.election-rounds.%d", round),
			fmt.Sprintf("Reach at least %d distinct Raft election terms.", round), election,
			round, "observation_evidence:term", []string{capCampaign}, coverage.RiskHigh,
		))
	}
	proposal := coverage.TracePredicate{EventKind: core.EventPropose, Outcome: string(core.StatusApplied)}
	for depth := 1; depth <= compiler.spec.Bounds.Proposals; depth++ {
		compiler.add(countObligation(
			fmt.Sprintf("boundary.proposals.%d", depth),
			fmt.Sprintf("Execute at least %d application proposals.", depth), proposal,
			depth, "", nil, coverage.RiskStandard,
		))
	}
	compiler.compileFaultCountBoundaries("crashes", core.EventCrash, "fault:crash", compiler.spec.Bounds.Crashes, []string{capCrashCutpoint})
	compiler.compileFaultCountBoundaries("restarts", core.EventRestart, "fault:restart", compiler.spec.Bounds.Restarts, []string{capRestart})
	compiler.compileFaultCountBoundaries("partitions", core.EventPartition, "network:partition", compiler.spec.Bounds.Partitions, []string{capMessageFault})

	quorum := compiler.spec.Bounds.Nodes/2 + 1
	elected := coverage.TracePredicate{
		EventKind: core.EventMessage, MessageType: "MsgVoteResp",
		ObservationKind: "transition", ObservationLabel: "transition:candidate->leader",
	}
	voteResponse := coverage.TracePredicate{
		EventKind: core.EventMessage, MessageType: "MsgVoteResp",
		ObservationKind: "transport", ObservationLabel: "message:delivered",
	}
	compiler.add(obligation(
		"boundary.quorum-election", categoryBoundary, "Elect a leader with a minimal legal quorum of vote responses.",
		coverage.EvidenceRequirement{
			Reach: []coverage.TracePredicate{elected}, Observe: []coverage.TracePredicate{elected},
			Counts: []coverage.CountConstraint{{Predicate: voteResponse, AtLeast: quorum - 1, DistinctBy: "event_source"}},
		}, []string{"trace-integrity", "agreement"}, []string{capCampaign, capMessage}, coverage.RiskCritical,
	))
	compiler.add(obligation(
		"boundary.all-peer-vote-responses", categoryBoundary, "Deliver vote responses from every peer in the bounded cluster.",
		coverage.EvidenceRequirement{
			Reach: []coverage.TracePredicate{voteResponse}, Observe: []coverage.TracePredicate{voteResponse},
			Counts: []coverage.CountConstraint{{Predicate: voteResponse, AtLeast: compiler.spec.Bounds.Nodes - 1, DistinctBy: "event_source"}},
		}, []string{"trace-integrity", "agreement"}, []string{capCampaign, capMessage}, coverage.RiskHigh,
	))
	for _, relation := range compiler.spec.LogRelations {
		for depth := 1; depth <= compiler.spec.Bounds.Proposals; depth++ {
			stable := coverage.TracePredicate{
				EventKind: core.EventAcknowledge,
				Semantic:  semantic(RelationLogAfter, relation, map[string]string{"min_entries": fmt.Sprint(depth)}),
			}
			compiler.add(obligation(
				fmt.Sprintf("boundary.log.%s.%d", slug(relation), depth), categoryBoundary,
				fmt.Sprintf("Observe Raft durable application logs in relation %s at depth %d.", relation, depth),
				sameEvidence(stable), []string{"trace-integrity", "agreement"}, []string{capStorage, capApply}, coverage.RiskCritical,
			))
		}
	}
	for _, shape := range compiler.spec.PartitionShapes {
		partition := coverage.TracePredicate{
			EventKind: core.EventPartition, ObservationLabel: "network:partition",
			Semantic: semantic(RelationPartitionShape, shape, nil),
		}
		compiler.add(obligation(
			"boundary.partition."+slug(shape), categoryBoundary, "Install the Raft partition shape "+shape+".",
			sameEvidence(partition), []string{"trace-integrity", "agreement"}, []string{capMessageFault}, coverage.RiskCritical,
		))
	}
}

func (compiler *campaignCompiler) compileFaultCountBoundaries(id string, kind core.EventKind, label string, maximum int, requires []string) {
	predicate := coverage.TracePredicate{EventKind: kind, ObservationLabel: label}
	for count := 1; count <= maximum; count++ {
		compiler.add(countObligation(
			fmt.Sprintf("boundary.%s.%d", id, count), fmt.Sprintf("Exercise at least %d %s in one campaign.", count, id),
			predicate, count, "", requires, coverage.RiskHigh,
		))
	}
}

func (compiler *campaignCompiler) compileProperties() {
	commit := coverage.TracePredicate{ObservationKind: "commit", ObservationLabel: "commit"}
	compiler.add(obligation(
		"property.agreement-activated", categoryProperty, "Activate agreement checking with a non-empty committed entry.",
		sameEvidence(commit), []string{"agreement"}, []string{capMessage, capApply}, coverage.RiskCritical,
	))
	compiler.add(obligation(
		"property.agreement-multiple-indexes", categoryProperty, "Activate agreement checking at multiple committed log indexes.",
		coverage.EvidenceRequirement{
			Reach: []coverage.TracePredicate{commit}, Observe: []coverage.TracePredicate{commit},
			Counts: []coverage.CountConstraint{{Predicate: commit, AtLeast: 2, DistinctBy: "observation_evidence:index"}},
		}, []string{"agreement"}, []string{capMessage, capApply}, coverage.RiskCritical,
	))
	apply := coverage.TracePredicate{EventKind: core.EventApply, ObservationLabel: "host:apply"}
	compiler.add(obligation(
		"property.apply-activated", categoryProperty, "Drive committed output across the application boundary.",
		sameEvidence(apply), []string{"trace-integrity", "agreement"}, []string{capApply}, coverage.RiskHigh,
	))
	leader := coverage.TracePredicate{
		ObservationKind: "transition", ObservationLabel: "transition:candidate->leader",
		Semantic: semantic(RelationLeaderCountAfter, "", map[string]string{"min": "1", "max": "1"}),
	}
	compiler.add(obligation(
		"property.single-leader-activated", categoryProperty, "Observe a leader transition while the frozen state contains exactly one running leader.",
		sameEvidence(leader), []string{"trace-integrity", "agreement"}, []string{capCampaign, capMessage}, coverage.RiskCritical,
	))
	compiler.add(progressObligation(
		"property.progress-after-heal", "Commit after healing a partition.",
		coverage.TracePredicate{EventKind: core.EventHeal, ObservationLabel: "network:heal"}, commit,
		[]string{capMessageFault, capMessage, capApply}, false,
	))
	compiler.add(progressObligation(
		"property.progress-after-restart", "Commit after restarting a crashed node.",
		coverage.TracePredicate{EventKind: core.EventRestart, ObservationLabel: "fault:restart"}, commit,
		[]string{capRestart, capMessage, capApply}, false,
	))
	for _, messageType := range compiler.spec.FaultMessageTypes {
		dropped := coverage.TracePredicate{EventKind: core.EventMessage, MessageType: messageType, Outcome: "dropped"}
		compiler.add(progressObligation(
			"property.progress-after-drop."+slug(messageType), "Commit after dropping a "+messageType+" message.",
			dropped, commit, []string{capMessage, capMessageFault, capApply}, false,
		))
		duplicated := coverage.TracePredicate{EventKind: core.EventDuplicate, MessageType: messageType}
		compiler.add(progressObligation(
			"property.progress-after-duplicate."+slug(messageType), "Commit after duplicating a "+messageType+" message.",
			duplicated, commit, []string{capMessage, capMessageFault, capApply}, false,
		))
	}
}

func obligation(id, category, description string, evidence coverage.EvidenceRequirement, monitors, requires []string, risk string) coverage.Obligation {
	return coverage.Obligation{
		ID: id, Category: category, Description: description, Evidence: evidence,
		Monitors: append([]string(nil), monitors...), Requires: append([]string(nil), requires...), Risk: risk,
	}
}

func event(kind core.EventKind) coverage.TracePredicate {
	return coverage.TracePredicate{EventKind: kind}
}

func semantic(relation, value string, parameters map[string]string) *coverage.SemanticPredicate {
	return &coverage.SemanticPredicate{Domain: PSSID, Relation: relation, Value: value, Parameters: parameters}
}

func sameEvidence(predicate coverage.TracePredicate) coverage.EvidenceRequirement {
	return coverage.EvidenceRequirement{
		Reach: []coverage.TracePredicate{predicate}, Observe: []coverage.TracePredicate{predicate},
	}
}

func orderedEvidence(before, after coverage.TracePredicate, ordering coverage.OrderingConstraint) coverage.EvidenceRequirement {
	return coverage.EvidenceRequirement{
		Reach: []coverage.TracePredicate{before}, Observe: []coverage.TracePredicate{after},
		Orderings: []coverage.OrderingConstraint{ordering},
	}
}

func countObligation(id, description string, predicate coverage.TracePredicate, atLeast int, distinctBy string, requires []string, risk string) coverage.Obligation {
	return obligation(
		id, categoryBoundary, description,
		coverage.EvidenceRequirement{
			Reach: []coverage.TracePredicate{predicate}, Observe: []coverage.TracePredicate{predicate},
			Counts: []coverage.CountConstraint{{Predicate: predicate, AtLeast: atLeast, DistinctBy: distinctBy}},
		}, []string{"trace-integrity", "agreement"}, requires, risk,
	)
}

func progressObligation(id, description string, before, after coverage.TracePredicate, requires []string, sameTarget bool) coverage.Obligation {
	return obligation(
		id, categoryProperty, description,
		orderedEvidence(before, after, coverage.OrderingConstraint{Before: before, After: after, SameTarget: sameTarget}),
		[]string{"trace-integrity", "agreement"}, requires, coverage.RiskCritical,
	)
}

func capabilityMap(manifest driver.Manifest) (map[string]driver.Capability, error) {
	result := make(map[string]driver.Capability, len(manifest.Capabilities))
	for _, capability := range manifest.Capabilities {
		if capability.ID == "" {
			return nil, errors.New("driver manifest contains an empty capability ID")
		}
		if _, exists := result[capability.ID]; exists {
			return nil, fmt.Errorf("driver manifest repeats capability %q", capability.ID)
		}
		result[capability.ID] = capability
	}
	return result, nil
}

func validateEvaluation(evaluation CampaignEvaluation) error {
	wanted := []string{categoryTransition, categoryOrdering, categoryFault, categoryBoundary, categoryProperty}
	if len(evaluation.Weights) != len(wanted) {
		return errors.New("campaign evaluation requires exactly five category weights")
	}
	total := 0.0
	for _, category := range wanted {
		weight, ok := evaluation.Weights[category]
		if !ok || weight <= 0 {
			return fmt.Errorf("campaign evaluation has no positive %s weight", category)
		}
		total += weight
	}
	if total < 0.999 || total > 1.001 {
		return fmt.Errorf("campaign evaluation weights must sum to 1, got %.4f", total)
	}
	if evaluation.Threshold.Score < 0 || evaluation.Threshold.Score > 100 ||
		evaluation.Threshold.MinCategory < 0 || evaluation.Threshold.MinCategory > 1 {
		return errors.New("campaign evaluation threshold is outside its valid range")
	}
	return nil
}

func uniqueStrings(values []string) bool {
	if len(values) == 0 {
		return false
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func cloneWeights(weights map[string]float64) map[string]float64 {
	result := make(map[string]float64, len(weights))
	for category, weight := range weights {
		result[category] = weight
	}
	return result
}

func slug(value string) string {
	var builder strings.Builder
	lastDash := false
	for _, character := range strings.ToLower(value) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			builder.WriteRune(character)
			lastDash = false
			continue
		}
		if !lastDash && builder.Len() > 0 {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}
