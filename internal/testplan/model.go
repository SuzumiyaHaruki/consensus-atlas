// Package testplan defines the untrusted planning boundary. A plan may choose
// existing coverage debts and bounded runtime actions, but cannot define an
// Oracle, evidence predicate, capability result, or coverage score.
package testplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/explore"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/scenario"
)

const (
	Version             = 1
	MaxPlans            = 32
	MaxTargetsPerPlan   = 24
	MaxPrepareActions   = 96
	MaxStimuliPerPlan   = 24
	MaxPayloadBytes     = 4096
	MaxRunsPerPlan      = 64
	MaxDecisionsPerRun  = 512
	MaxSuiteRuns        = 512
	MaxSuiteDecisions   = 65536
	BootstrapDrainLimit = 256
)

const (
	OpInject          = "inject"
	OpExecute         = "execute"
	OpExecuteOptional = "execute_optional"
	OpDrop            = "drop"
	OpDuplicate       = "duplicate"
	OpPartition       = "partition"
	OpHeal            = "heal"
	OpDrain           = "drain"
	OpAdvance         = "advance"
	// OpCaptureMessage resolves one currently Runtime-owned message through a
	// stable selector and stores its opaque execution-local reference. Plans
	// cannot provide an event ID or payload for the captured message.
	OpCaptureMessage = "capture_message"
	OpExecuteRef     = "execute_ref"
)

var safeID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

type Suite struct {
	Version           int    `json:"version"`
	ID                string `json:"id"`
	ProfileID         string `json:"profile_id"`
	ProfileDigest     string `json:"profile_digest"`
	MaxTotalRuns      int    `json:"max_total_runs"`
	MaxTotalDecisions int    `json:"max_total_decisions"`
	Plans             []Plan `json:"plans"`
}

type Plan struct {
	ID      string   `json:"id"`
	Targets []string `json:"targets"`
	Prepare []Action `json:"prepare,omitempty"`
	Stimuli []Input  `json:"stimuli"`
	Search  Search   `json:"search"`
}

type Search struct {
	Strategy explore.Strategy `json:"strategy"`
	Config   explore.Config   `json:"config"`
}

type Input struct {
	Kind      core.EventKind  `json:"kind"`
	Operation string          `json:"operation,omitempty"`
	Target    string          `json:"target"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type Action struct {
	Op        string             `json:"op"`
	Kind      core.EventKind     `json:"kind,omitempty"`
	Operation string             `json:"operation,omitempty"`
	Target    string             `json:"target,omitempty"`
	Payload   json.RawMessage    `json:"payload,omitempty"`
	Match     *scenario.Selector `json:"match,omitempty"`
	Groups    [][]string         `json:"groups,omitempty"`
	Count     int                `json:"count,omitempty"`
	Ticks     uint64             `json:"ticks,omitempty"`
	Ref       string             `json:"ref,omitempty"`
}

type ConcretePlan struct {
	ID      string        `json:"id"`
	Targets []string      `json:"targets"`
	Setup   scenario.Spec `json:"setup"`
	Search  Search        `json:"search"`
}

func (suite Suite) Validate(profile coverage.Profile) error {
	if err := profile.Validate(); err != nil {
		return fmt.Errorf("validate profile: %w", err)
	}
	if suite.Version != Version {
		return fmt.Errorf("unsupported test plan suite version %d", suite.Version)
	}
	if !safeID.MatchString(suite.ID) {
		return errors.New("suite id must use lowercase letters, digits, dots, underscores, or hyphens")
	}
	digest, err := coverage.Digest(profile)
	if err != nil {
		return err
	}
	if suite.ProfileID != profile.ID || suite.ProfileDigest != digest {
		return fmt.Errorf("suite profile identity does not match frozen profile %s/%s", profile.ID, digest)
	}
	if len(suite.Plans) == 0 || len(suite.Plans) > MaxPlans {
		return fmt.Errorf("suite must contain between 1 and %d plans", MaxPlans)
	}
	if suite.MaxTotalRuns < 1 || suite.MaxTotalRuns > MaxSuiteRuns ||
		suite.MaxTotalDecisions < 1 || suite.MaxTotalDecisions > MaxSuiteDecisions {
		return errors.New("suite run or decision limit is outside the trusted maximum")
	}
	items, _ := profile.Obligations()
	obligations := make(map[string]coverage.Obligation, len(items))
	for _, item := range items {
		obligations[item.ID] = item
	}
	nodes := make(map[string]bool, len(profile.Nodes))
	for _, node := range profile.Nodes {
		nodes[node] = true
	}
	planIDs := make(map[string]bool, len(suite.Plans))
	totalRuns, totalDecisions := 0, 0
	for index, plan := range suite.Plans {
		if err := plan.validate(obligations, nodes, profile.Nodes); err != nil {
			return fmt.Errorf("plans[%d]: %w", index, err)
		}
		if planIDs[plan.ID] {
			return fmt.Errorf("duplicate plan id %q", plan.ID)
		}
		planIDs[plan.ID] = true
		totalRuns += plan.Search.Config.Runs
		totalDecisions += plan.Search.Config.TargetDecisionBudget()
	}
	if totalRuns > suite.MaxTotalRuns {
		return fmt.Errorf("declared plan runs %d exceed suite limit %d", totalRuns, suite.MaxTotalRuns)
	}
	if totalDecisions > suite.MaxTotalDecisions {
		return fmt.Errorf("declared plan decisions %d exceed suite limit %d", totalDecisions, suite.MaxTotalDecisions)
	}
	return nil
}

func (plan Plan) validate(obligations map[string]coverage.Obligation, nodes map[string]bool, nodeOrder []string) error {
	if !safeID.MatchString(plan.ID) {
		return errors.New("plan id must use lowercase letters, digits, dots, underscores, or hyphens")
	}
	if len(plan.Targets) == 0 || len(plan.Targets) > MaxTargetsPerPlan {
		return fmt.Errorf("plan targets must contain between 1 and %d obligation IDs", MaxTargetsPerPlan)
	}
	seenTargets := make(map[string]bool, len(plan.Targets))
	for _, target := range plan.Targets {
		item, exists := obligations[target]
		if !exists {
			return fmt.Errorf("unknown coverage target %q", target)
		}
		if item.Status != coverage.StatusSupported {
			return fmt.Errorf("coverage target %q is not actionable because it is %s", target, item.Status)
		}
		if seenTargets[target] {
			return fmt.Errorf("duplicate coverage target %q", target)
		}
		seenTargets[target] = true
	}
	if len(plan.Prepare) > MaxPrepareActions {
		return fmt.Errorf("plan has more than %d preparation actions", MaxPrepareActions)
	}
	refs := make(map[string]bool)
	for index, action := range plan.Prepare {
		if err := action.validate(nodes, nodeOrder); err != nil {
			return fmt.Errorf("prepare[%d]: %w", index, err)
		}
		switch action.Op {
		case OpCaptureMessage:
			if refs[action.Ref] {
				return fmt.Errorf("prepare[%d]: duplicate captured reference %q", index, action.Ref)
			}
			refs[action.Ref] = true
		case OpExecuteRef:
			if !refs[action.Ref] {
				return fmt.Errorf("prepare[%d]: reference %q was not captured first", index, action.Ref)
			}
			delete(refs, action.Ref)
		}
	}
	if len(plan.Stimuli) == 0 || len(plan.Stimuli) > MaxStimuliPerPlan {
		return fmt.Errorf("plan stimuli must contain between 1 and %d inputs", MaxStimuliPerPlan)
	}
	for index, input := range plan.Stimuli {
		if err := input.validate(nodes); err != nil {
			return fmt.Errorf("stimuli[%d]: %w", index, err)
		}
	}
	if plan.Search.Strategy != explore.StrategyRandom && plan.Search.Strategy != explore.StrategyDFS {
		return fmt.Errorf("search strategy %q is not allowed", plan.Search.Strategy)
	}
	config := plan.Search.Config
	if err := config.Validate(); err != nil {
		return fmt.Errorf("search config: %w", err)
	}
	if config.Runs > MaxRunsPerPlan || config.BudgetPerRun > MaxDecisionsPerRun || config.DecisionBudget <= 0 {
		return errors.New("search config exceeds per-plan limits or omits an explicit decision budget")
	}
	if config.Actions.MaxDuplicates > 4 {
		return errors.New("search allows more than four duplicate decisions per run")
	}
	return nil
}

func (input Input) validate(nodes map[string]bool) error {
	if !allowedInputKind(input.Kind) {
		return fmt.Errorf("input kind %q is not an allowed protocol input", input.Kind)
	}
	if !nodes[input.Target] {
		return fmt.Errorf("input target %q is not a profile node", input.Target)
	}
	if len(input.Payload) > MaxPayloadBytes || (len(input.Payload) > 0 && !json.Valid(input.Payload)) {
		return errors.New("input payload is invalid or too large")
	}
	if input.Kind == core.EventProtocolInput && !safeID.MatchString(input.Operation) {
		return errors.New("protocol-input requires a safe operation ID")
	}
	if input.Kind != core.EventProtocolInput && input.Operation != "" {
		return fmt.Errorf("input kind %q cannot declare a protocol operation", input.Kind)
	}
	if (input.Kind == core.EventPropose || input.Kind == core.EventQuery) && len(input.Payload) == 0 {
		return fmt.Errorf("%s input requires a bounded JSON payload", input.Kind)
	}
	if input.Kind != core.EventProtocolInput && input.Kind != core.EventPropose && input.Kind != core.EventQuery && len(input.Payload) != 0 {
		return fmt.Errorf("input kind %q cannot carry an Agent-defined payload", input.Kind)
	}
	return nil
}

func (action Action) validate(nodes map[string]bool, nodeOrder []string) error {
	switch action.Op {
	case OpInject:
		if action.Match != nil || len(action.Groups) != 0 || action.Count != 0 || action.Ticks != 0 || action.Ref != "" {
			return errors.New("inject contains fields owned by another operation")
		}
		return (Input{Kind: action.Kind, Operation: action.Operation, Target: action.Target, Payload: action.Payload}).validate(nodes)
	case OpExecute, OpExecuteOptional, OpDrop, OpDuplicate:
		if action.Kind != "" || action.Operation != "" || action.Target != "" || len(action.Payload) != 0 || len(action.Groups) != 0 || action.Count != 0 || action.Ticks != 0 || action.Ref != "" {
			return fmt.Errorf("%s contains fields owned by another operation", action.Op)
		}
		if err := validateSelector(action.Match, nodes); err != nil {
			return err
		}
		if action.Op == OpExecuteOptional && !optionalHostOperation(action.Match.Kind) {
			return errors.New("execute_optional may select only a host output operation")
		}
		if (action.Op == OpDrop || action.Op == OpDuplicate) && action.Match.Kind != core.EventMessage {
			return fmt.Errorf("%s may select only a message", action.Op)
		}
		return nil
	case OpCaptureMessage:
		if action.Kind != "" || action.Operation != "" || action.Target != "" || len(action.Payload) != 0 || len(action.Groups) != 0 || action.Count != 0 || action.Ticks != 0 ||
			action.Ref == "" || !safeID.MatchString(action.Ref) {
			return errors.New("capture_message requires only a safe non-empty ref and message selector")
		}
		if err := validateSelector(action.Match, nodes); err != nil {
			return err
		}
		if action.Match.Kind != core.EventMessage {
			return errors.New("capture_message may select only a Runtime-owned message")
		}
		return nil
	case OpExecuteRef:
		if action.Kind != "" || action.Operation != "" || action.Target != "" || len(action.Payload) != 0 || action.Match != nil || len(action.Groups) != 0 || action.Count != 0 || action.Ticks != 0 ||
			action.Ref == "" || !safeID.MatchString(action.Ref) {
			return errors.New("execute_ref requires only a previously captured safe ref")
		}
		return nil
	case OpPartition:
		if action.Kind != "" || action.Operation != "" || action.Target != "" || len(action.Payload) != 0 || action.Match != nil || action.Count != 0 || action.Ticks != 0 || action.Ref != "" {
			return errors.New("partition contains fields owned by another operation")
		}
		return validatePartition(action.Groups, nodeOrder)
	case OpHeal:
		if action.Kind != "" || action.Operation != "" || action.Target != "" || len(action.Payload) != 0 || action.Match != nil || len(action.Groups) != 0 || action.Count != 0 || action.Ticks != 0 || action.Ref != "" {
			return errors.New("heal cannot contain arguments")
		}
		return nil
	case OpDrain:
		if action.Count < 1 || action.Count > 1000 || action.Kind != "" || action.Operation != "" || action.Target != "" || len(action.Payload) != 0 || action.Match != nil || len(action.Groups) != 0 || action.Ticks != 0 || action.Ref != "" {
			return errors.New("drain requires only a count between 1 and 1000")
		}
		return nil
	case OpAdvance:
		if action.Ticks < 1 || action.Ticks > 1_000_000 || action.Kind != "" || action.Operation != "" || action.Target != "" || len(action.Payload) != 0 || action.Match != nil || len(action.Groups) != 0 || action.Count != 0 || action.Ref != "" {
			return errors.New("advance requires only ticks between 1 and 1000000")
		}
		return nil
	default:
		return fmt.Errorf("preparation operation %q is not allowed", action.Op)
	}
}

func optionalHostOperation(kind core.EventKind) bool {
	switch kind {
	case core.EventPersist, core.EventSync, core.EventEmit, core.EventApply, core.EventAcknowledge:
		return true
	default:
		return false
	}
}

func validateSelector(selector *scenario.Selector, nodes map[string]bool) error {
	if selector == nil || selector.Kind == "" {
		return errors.New("selector and selector.kind are required")
	}
	if !knownEventKind(selector.Kind) {
		return fmt.Errorf("selector event kind %q is unknown", selector.Kind)
	}
	if selector.Operation != "" && (selector.Kind != core.EventProtocolInput || !safeID.MatchString(selector.Operation)) {
		return errors.New("selector operation requires protocol-input and a safe operation ID")
	}
	if selector.ID != "" || selector.Group != "" {
		return errors.New("plans cannot select transient event IDs or host batch groups")
	}
	if selector.Source != "" && !nodes[selector.Source] {
		return fmt.Errorf("selector source %q is not a profile node", selector.Source)
	}
	if selector.Target != "" && !nodes[selector.Target] {
		return fmt.Errorf("selector target %q is not a profile node", selector.Target)
	}
	if len(selector.TypeHint) > 128 {
		return errors.New("selector message type is too long")
	}
	return nil
}

func knownEventKind(kind core.EventKind) bool {
	switch kind {
	case core.EventStart, core.EventProtocolInput, core.EventCampaign, core.EventPropose, core.EventQuery, core.EventMessage,
		core.EventTimeout, core.EventPersist, core.EventSync, core.EventEmit,
		core.EventApply, core.EventAcknowledge, core.EventCrash, core.EventRestart,
		core.EventDuplicate, core.EventPartition, core.EventHeal, core.EventClockAdvance:
		return true
	default:
		return false
	}
}

func validatePartition(groups [][]string, nodeOrder []string) error {
	if len(groups) < 2 || len(groups) > len(nodeOrder) {
		return errors.New("partition requires between two and node-count groups")
	}
	wanted := make(map[string]bool, len(nodeOrder))
	for _, node := range nodeOrder {
		wanted[node] = true
	}
	seen := make(map[string]bool, len(nodeOrder))
	for _, group := range groups {
		if len(group) == 0 {
			return errors.New("partition groups cannot be empty")
		}
		for _, node := range group {
			if !wanted[node] || seen[node] {
				return fmt.Errorf("partition contains unknown or duplicate node %q", node)
			}
			seen[node] = true
		}
	}
	if len(seen) != len(wanted) {
		return errors.New("partition must place every profile node in exactly one group")
	}
	return nil
}

func allowedInputKind(kind core.EventKind) bool {
	switch kind {
	case core.EventProtocolInput, core.EventCampaign, core.EventPropose, core.EventQuery, core.EventTimeout, core.EventCrash, core.EventRestart:
		return true
	default:
		return false
	}
}

func Digest(suite Suite) (string, error) {
	encoded, err := json.Marshal(suite)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func SupportedTargetIDs(profile coverage.Profile) []string {
	items, _ := profile.Obligations()
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item.Status == coverage.StatusSupported {
			result = append(result, item.ID)
		}
	}
	sort.Strings(result)
	return result
}

// ValidateInputCapabilities adds the trusted Driver vocabulary check to the
// profile-only schema validation. Keeping it separate preserves replay of
// historical plan files whose old event kinds predate protocol-input.
func ValidateInputCapabilities(plan Plan, manifest driver.Manifest) error {
	for index, action := range plan.Prepare {
		if action.Op != OpInject {
			continue
		}
		if err := validateInputCapability(Input{Kind: action.Kind, Operation: action.Operation, Target: action.Target, Payload: action.Payload}, manifest); err != nil {
			return fmt.Errorf("prepare[%d]: %w", index, err)
		}
	}
	for index, input := range plan.Stimuli {
		if err := validateInputCapability(input, manifest); err != nil {
			return fmt.Errorf("stimuli[%d]: %w", index, err)
		}
	}
	return nil
}

func validateInputCapability(input Input, manifest driver.Manifest) error {
	// A manifest without Inputs predates the extensible input boundary. It is
	// accepted only for historical fixtures and replay; all newly onboarded
	// Drivers must publish a non-empty vocabulary.
	if len(manifest.Inputs) == 0 {
		return nil
	}
	wantedKind, wantedID := input.Kind, input.Operation
	if input.Kind != core.EventProtocolInput {
		// Existing v1 trace and plan files use named inputs. They resolve only
		// through a matching modern protocol-input declaration, never through a
		// Raft check in the generic validator.
		if legacy := legacyProtocolOperation(input.Kind); legacy != "" {
			wantedKind, wantedID = core.EventProtocolInput, legacy
		} else {
			wantedID = string(input.Kind)
		}
	}
	for _, declared := range manifest.Inputs {
		if declared.Kind != wantedKind || declared.ID != wantedID {
			continue
		}
		switch declared.PayloadMode {
		case "required":
			if len(input.Payload) == 0 {
				return fmt.Errorf("input %q requires a payload", declared.ID)
			}
		case "optional":
		case "forbidden":
			if len(input.Payload) != 0 {
				return fmt.Errorf("input %q forbids a payload", declared.ID)
			}
		default:
			return fmt.Errorf("driver declares input %q with invalid payload mode %q", declared.ID, declared.PayloadMode)
		}
		return nil
	}
	return fmt.Errorf("input kind %q operation %q is not declared by the Driver manifest", input.Kind, input.Operation)
}

func legacyProtocolOperation(kind core.EventKind) string {
	switch kind {
	case core.EventCampaign:
		return "campaign"
	case core.EventPropose:
		return "propose"
	case core.EventQuery:
		return "query"
	default:
		return ""
	}
}
