package controlruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlentropy"
)

type Config struct {
	Seed       []byte
	ClockError uint64
	MaxClones  uint64
}

type Runtime struct {
	adapter      control.Adapter
	closeOnce    sync.Once
	closeErr     error
	manifest     control.AdapterManifest
	manifestHash string
	seedDigest   string
	clockError   uint64
	maxClones    uint64
	now          uint64
	step         uint64
	nodes        map[control.NodeID]NodeSnapshot
	items        map[control.ItemID]*itemEntry
	partitions   map[string]*partition
	cloneCounts  map[control.ItemID]uint64
	offered      map[control.ActionID]control.Action
	trace        Trace
	lastEntropy  uint64
	entropyTape  string
	entropyLog   controlentropy.Tape
	adapterState string
	lastEvidence control.EvidenceEnvelope
	lastAudit    control.EntropyAuditEnvelope
	failed       error
	terminal     *TerminalOutcome
}

type cycleResult struct {
	yield    control.Yield
	emission control.Emission
	evidence control.EvidenceEnvelope
	entropy  control.EntropyAuditEnvelope
	tape     controlentropy.Tape
}

type executionMeta struct {
	command      *control.AdapterCommand
	yield        *control.Yield
	emission     string
	evidence     *control.EvidenceEnvelope
	entropy      *control.EntropyAuditEnvelope
	clockAdvance *ClockAdvance
}

type nativeCommit struct {
	action     control.Action
	clone      *control.ProducedItem
	cloneCount uint64
	partition  *control.PartitionParameters
}

// New takes ownership of adapter. If initialization fails, New closes it
// before returning. After a successful initialization, the caller must close
// the returned Runtime when it is no longer needed.
func New(ctx context.Context, adapter control.Adapter, config Config) (_ *Runtime, err error) {
	if adapter == nil {
		return nil, errors.New("ADAPTER_REQUIRED")
	}
	runtime := &Runtime{adapter: adapter}
	defer func() {
		if err != nil {
			err = errors.Join(err, runtime.Close())
		}
	}()
	if len(config.Seed) == 0 {
		return nil, errors.New("RUNTIME_SEED_REQUIRED")
	}
	manifest, err := adapter.Manifest(ctx)
	if err != nil {
		return nil, fmt.Errorf("ADAPTER_MANIFEST_FAILED: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	manifestHash, err := manifest.Digest()
	if err != nil {
		return nil, err
	}
	if config.ClockError > manifest.Capabilities.Temporal.ClockError {
		return nil, fmt.Errorf("CLOCK_ERROR_CAPABILITY_EXCEEDED: %d > %d",
			config.ClockError, manifest.Capabilities.Temporal.ClockError)
	}
	if config.ClockError != 0 {
		return nil, errors.New("CLOCK_ERROR_V2ALPHA1_REQUIRES_ZERO")
	}
	if config.MaxClones == 0 {
		config.MaxClones = 2
	}
	seedSum := sha256.Sum256(config.Seed)
	runtime.manifest = manifest
	runtime.manifestHash = manifestHash
	runtime.seedDigest = hex.EncodeToString(seedSum[:])
	runtime.clockError = config.ClockError
	runtime.maxClones = config.MaxClones
	runtime.nodes = make(map[control.NodeID]NodeSnapshot)
	runtime.items = make(map[control.ItemID]*itemEntry)
	runtime.partitions = make(map[string]*partition)
	runtime.cloneCounts = make(map[control.ItemID]uint64)
	runtime.offered = make(map[control.ActionID]control.Action)
	for _, node := range manifest.Nodes {
		runtime.nodes[node] = NodeSnapshot{
			Ref: control.NodeRef{Node: node, Incarnation: 1}, Lifecycle: control.NodeRunning,
		}
	}
	if err := adapter.Reset(ctx, append([]byte(nil), config.Seed...)); err != nil {
		return nil, fmt.Errorf("ADAPTER_RESET_FAILED: %w", err)
	}
	yield, err := adapter.RunUntilYield(ctx)
	if err != nil {
		return nil, fmt.Errorf("ADAPTER_INITIAL_YIELD_FAILED: %w", err)
	}
	if err := validateYield(yield, control.YieldStable); err != nil {
		return nil, err
	}
	cycle, err := runtime.collect(ctx, yield)
	if err != nil {
		return nil, err
	}
	if err := runtime.validateEmission(cycle.emission, nil); err != nil {
		return nil, err
	}
	runtime.lastEntropy = cycle.entropy.DrawCount
	runtime.entropyTape = cycle.entropy.TapeDigest
	runtime.entropyLog = cycle.tape
	runtime.adapterState = cycle.yield.StateDigest
	runtime.lastEvidence = cycle.evidence
	runtime.lastAudit = cycle.entropy
	runtime.commitEmission(cycle.emission)
	initialStateDigest, err := runtime.stateDigest()
	if err != nil {
		return nil, err
	}
	evidenceDigest, err := control.CanonicalDigest(cycle.evidence)
	if err != nil {
		return nil, err
	}
	runtime.trace = Trace{
		SchemaVersion: TraceSchemaVersion, ManifestDigest: manifestHash,
		SeedDigest: runtime.seedDigest, InitialYield: cycle.yield,
		InitialEmissionDigest: cycle.emission.Digest,
		InitialEvidenceDigest: evidenceDigest,
		InitialEntropyDigest:  cycle.entropy.TapeDigest,
		InitialEvidence:       cycle.evidence, InitialEntropy: cycle.entropy,
		InitialStateDigest: initialStateDigest, FinalStateDigest: initialStateDigest,
	}
	return runtime, nil
}

// Close releases resources owned by the Adapter. Adapters without an optional
// Close method need no special handling. Close is safe to call more than once.
func (runtime *Runtime) Close() error {
	if runtime == nil {
		return nil
	}
	runtime.closeOnce.Do(func() {
		runtime.closeErr = closeAdapter(runtime.adapter)
	})
	return runtime.closeErr
}

func closeAdapter(adapter control.Adapter) error {
	if closer, ok := adapter.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

func (runtime *Runtime) Select(ctx context.Context, id control.ActionID) (ActionRecord, error) {
	if runtime.failed != nil {
		return ActionRecord{}, runtime.failed
	}
	enabled, err := runtime.EnabledActions(ctx)
	if err != nil {
		return ActionRecord{}, runtime.fail(err)
	}
	enabledDigest, err := control.CanonicalDigest(enabled)
	if err != nil {
		return ActionRecord{}, runtime.fail(err)
	}
	var selected *control.Action
	for index := range enabled {
		if enabled[index].ID == id {
			action := enabled[index]
			selected = &action
			break
		}
	}
	if selected == nil {
		return ActionRecord{}, fmt.Errorf("ACTION_NOT_ENABLED: %s", id)
	}
	before := runtime.Snapshot()
	beforeDigest, err := before.Digest()
	if err != nil {
		return ActionRecord{}, runtime.fail(err)
	}
	var meta executionMeta
	if runtime.adapterDirected(selected.Kind) {
		meta, err = runtime.executeAdapter(ctx, *selected)
	} else {
		var commit nativeCommit
		commit, err = runtime.prepareNative(*selected)
		if err == nil {
			if applyErr := runtime.adapter.ApplyRuntimeAction(ctx, *selected); applyErr != nil {
				err = fmt.Errorf("ADAPTER_RUNTIME_ACTION_FAILED: %w", applyErr)
			}
		}
		if err == nil {
			runtime.commitNative(commit)
		}
	}
	if err != nil {
		prefix, traceErr := runtime.successfulPrefixTrace()
		if traceErr != nil {
			return ActionRecord{}, runtime.fail(errors.Join(err, traceErr))
		}
		class, code := control.ClassifyExecutionFailure(err)
		outcome, outcomeErr := newTerminalOutcome(
			class, code, runtime.step+1, prefix, enabledDigest, *selected,
		)
		if outcomeErr != nil {
			return ActionRecord{}, runtime.fail(errors.Join(err, outcomeErr))
		}
		runtime.terminal = &outcome
		return ActionRecord{}, runtime.fail(&TerminalExecutionError{Terminal: outcome, cause: err})
	}
	delete(runtime.offered, selected.ID)
	runtime.refreshItemStates()
	runtime.step++
	after := runtime.Snapshot()
	afterDigest, err := after.Digest()
	if err != nil {
		return ActionRecord{}, runtime.fail(err)
	}
	record := ActionRecord{
		Step: runtime.step, LogicalTime: runtime.now, EnabledSetDigest: enabledDigest,
		Action: *selected, Command: meta.command, Yield: meta.yield,
		EmissionDigest: meta.emission, ClockAdvance: meta.clockAdvance,
		ItemTransitions: itemTransitions(before, after), NodeTransitions: nodeTransitions(before, after),
		BeforeStateDigest: beforeDigest, AfterStateDigest: afterDigest, Outcome: "applied",
	}
	if meta.evidence != nil {
		record.Evidence = meta.evidence
		record.EvidenceDigest, err = control.CanonicalDigest(*meta.evidence)
		if err != nil {
			return ActionRecord{}, runtime.fail(err)
		}
	}
	if meta.entropy != nil {
		record.Entropy = meta.entropy
		record.EntropyTapeDigest = meta.entropy.TapeDigest
	}
	runtime.trace.Records = append(runtime.trace.Records, record)
	runtime.trace.FinalStateDigest = afterDigest
	return record, nil
}

func (runtime *Runtime) executeAdapter(ctx context.Context, action control.Action) (executionMeta, error) {
	command, err := runtime.buildCommand(action)
	if err != nil {
		return executionMeta{}, err
	}
	eligibility, err := runtime.adapter.Check(ctx, command)
	if err != nil {
		return executionMeta{}, fmt.Errorf("ADAPTER_CHECK_FAILED: %w", err)
	}
	if err := eligibility.Validate(); err != nil {
		return executionMeta{}, err
	}
	if !eligibility.Eligible {
		return executionMeta{}, fmt.Errorf("ACTION_BECAME_INELIGIBLE: %s", eligibility.ReasonCode)
	}
	if err := runtime.adapter.Submit(ctx, command); err != nil {
		return executionMeta{}, fmt.Errorf("ADAPTER_SUBMIT_FAILED: %w", err)
	}
	yield, err := runtime.adapter.RunUntilYield(ctx)
	if err != nil {
		return executionMeta{}, fmt.Errorf("ADAPTER_YIELD_FAILED: %w", err)
	}
	expectedYield := control.YieldStable
	if action.Kind == control.ActionCrash {
		expectedYield = control.YieldTerminal
	}
	if err := validateYield(yield, expectedYield); err != nil {
		return executionMeta{}, err
	}
	cycle, err := runtime.collect(ctx, yield)
	if err != nil {
		return executionMeta{}, err
	}
	var futureOwner *control.NodeRef
	if action.Kind == control.ActionRestart {
		futureOwner = &action.Node
	}
	if err := runtime.validateEmission(cycle.emission, futureOwner); err != nil {
		return executionMeta{}, err
	}
	meta := executionMeta{
		command: &command, yield: &cycle.yield, emission: cycle.emission.Digest,
		evidence: &cycle.evidence, entropy: &cycle.entropy,
	}
	if err := runtime.commitAdapterAction(action, cycle.emission, &meta); err != nil {
		return executionMeta{}, err
	}
	runtime.lastEntropy = cycle.entropy.DrawCount
	runtime.entropyTape = cycle.entropy.TapeDigest
	runtime.entropyLog = cycle.tape
	runtime.adapterState = cycle.yield.StateDigest
	runtime.lastEvidence = cycle.evidence
	runtime.lastAudit = cycle.entropy
	return meta, nil
}

func (runtime *Runtime) commitAdapterAction(action control.Action, emission control.Emission, meta *executionMeta) error {
	entry := runtime.items[action.Item]
	switch action.Kind {
	case control.ActionInvoke:
	case control.ActionDropMessage:
		if entry == nil || entry.item.Kind != control.ItemMessage {
			return fmt.Errorf("DROP_ITEM_INVALID: %s", action.Item)
		}
		entry.state = control.ItemDropped
	case control.ActionDeliverMessage:
		if entry == nil || entry.item.Kind != control.ItemMessage {
			return fmt.Errorf("DELIVERY_ITEM_INVALID: %s", action.Item)
		}
		entry.state = control.ItemCompleted
	case control.ActionFireTemporal:
		if entry == nil || entry.item.Kind != control.ItemTemporal {
			return fmt.Errorf("TEMPORAL_ITEM_INVALID: %s", action.Item)
		}
		from := runtime.now
		to := entry.item.Temporal.Deadline
		if to < from {
			to = from
		}
		runtime.now = to
		entry.state = control.ItemCompleted
		meta.clockAdvance = &ClockAdvance{
			From: from, To: to, Reason: string(entry.item.Temporal.Kind), Temporal: entry.item.Temporal.ID,
		}
	case control.ActionCrash:
		node := runtime.nodes[action.Node.Node]
		if node.Ref != action.Node || node.Lifecycle != control.NodeRunning {
			return fmt.Errorf("CRASH_NODE_STATE_INVALID: %s", action.Node.Node)
		}
		node.Lifecycle = control.NodeStopped
		runtime.nodes[action.Node.Node] = node
		for id, offered := range runtime.offered {
			if offered.Node == action.Node {
				delete(runtime.offered, id)
			}
		}
	case control.ActionRestart:
		node := runtime.nodes[action.Node.Node]
		if node.Lifecycle != control.NodeStopped || action.Node.Incarnation != node.Ref.Incarnation+1 {
			return fmt.Errorf("RESTART_NODE_STATE_INVALID: %s", action.Node.Node)
		}
		node.Ref = action.Node
		node.Lifecycle = control.NodeRunning
		runtime.nodes[action.Node.Node] = node
	case control.ActionCompleteEffect:
		if entry == nil || entry.item.Kind != control.ItemEffect {
			return fmt.Errorf("EFFECT_ITEM_INVALID: %s", action.Item)
		}
		entry.state = control.ItemCompleted
		if entry.item.Effect.Durability == control.DurabilityDurable {
			node := runtime.nodes[entry.item.Owner.Node]
			node.DurableEffects++
			runtime.nodes[entry.item.Owner.Node] = node
		}
	case control.ActionFailEffect:
		if entry == nil || entry.item.Kind != control.ItemEffect {
			return fmt.Errorf("EFFECT_ITEM_INVALID: %s", action.Item)
		}
		entry.state = control.ItemFailed
	case control.ActionCompleteCallback:
		if entry == nil || entry.item.Kind != control.ItemCallback {
			return fmt.Errorf("CALLBACK_ITEM_INVALID: %s", action.Item)
		}
		entry.state = control.ItemCompleted
	default:
		return fmt.Errorf("ADAPTER_ACTION_UNSUPPORTED: %s", action.Kind)
	}
	runtime.commitEmission(emission)
	return nil
}

func (runtime *Runtime) prepareNative(action control.Action) (nativeCommit, error) {
	commit := nativeCommit{action: action}
	switch action.Kind {
	case control.ActionDuplicateMessage:
		entry := runtime.items[action.Item]
		if entry == nil || entry.item.Kind != control.ItemMessage || entry.state != control.ItemEnabled {
			return nativeCommit{}, fmt.Errorf("DUPLICATE_ITEM_INVALID: %s", action.Item)
		}
		count := runtime.cloneCounts[action.Item] + 1
		if count > runtime.maxClones {
			return nativeCommit{}, fmt.Errorf("DUPLICATE_BUDGET_EXCEEDED: %s", action.Item)
		}
		itemID, err := control.StableID("item-clone", string(action.Item), fmt.Sprint(count))
		if err != nil {
			return nativeCommit{}, err
		}
		messageID, err := control.StableID("message-clone", string(entry.item.Message.ID), fmt.Sprint(count))
		if err != nil {
			return nativeCommit{}, err
		}
		clone := cloneItem(entry.item)
		clone.ID = control.ItemID(itemID)
		clone.Dependencies = nil
		clone.Message.CloneOf = entry.item.Message.ID
		clone.Message.ID = control.MessageID(messageID)
		commit.clone = &clone
		commit.cloneCount = count
	case control.ActionPartition:
		parameters, err := control.DecodePartitionParameters(action.Parameters)
		if err != nil {
			return nativeCommit{}, err
		}
		if runtime.partitions[parameters.ID] != nil {
			return nativeCommit{}, fmt.Errorf("PARTITION_ALREADY_ACTIVE: %s", parameters.ID)
		}
		commit.partition = &parameters
	case control.ActionHeal:
		parameters, err := control.DecodePartitionParameters(action.Parameters)
		if err != nil {
			return nativeCommit{}, err
		}
		if runtime.partitions[parameters.ID] == nil {
			return nativeCommit{}, fmt.Errorf("PARTITION_NOT_ACTIVE: %s", parameters.ID)
		}
		commit.partition = &parameters
	default:
		return nativeCommit{}, fmt.Errorf("NATIVE_ACTION_UNSUPPORTED: %s", action.Kind)
	}
	return commit, nil
}

func (runtime *Runtime) commitNative(commit nativeCommit) {
	switch commit.action.Kind {
	case control.ActionDuplicateMessage:
		runtime.items[commit.clone.ID] = &itemEntry{item: *commit.clone, state: control.ItemEnabled}
		runtime.cloneCounts[commit.action.Item] = commit.cloneCount
	case control.ActionPartition:
		parameters := commit.partition
		runtime.partitions[parameters.ID] = &partition{
			id: parameters.ID, left: append([]control.NodeID(nil), parameters.Left...),
			right: append([]control.NodeID(nil), parameters.Right...),
		}
	case control.ActionHeal:
		delete(runtime.partitions, commit.partition.ID)
	}
}

func (runtime *Runtime) collect(ctx context.Context, yield control.Yield) (cycleResult, error) {
	emission, err := runtime.adapter.Collect(ctx, yield.ID)
	if err != nil {
		return cycleResult{}, fmt.Errorf("ADAPTER_COLLECT_FAILED: %w", err)
	}
	if emission.Yield != yield.ID {
		return cycleResult{}, fmt.Errorf("EMISSION_YIELD_MISMATCH: %s != %s", emission.Yield, yield.ID)
	}
	if err := emission.Validate(); err != nil {
		return cycleResult{}, err
	}
	evidence, err := runtime.adapter.SnapshotEvidence(ctx)
	if err != nil {
		return cycleResult{}, fmt.Errorf("ADAPTER_EVIDENCE_FAILED: %w", err)
	}
	if evidence.Yield != yield.ID {
		return cycleResult{}, fmt.Errorf("EVIDENCE_YIELD_MISMATCH: %s != %s", evidence.Yield, yield.ID)
	}
	if err := evidence.Payload.Validate(); err != nil {
		return cycleResult{}, err
	}
	entropy, err := runtime.adapter.SnapshotEntropy(ctx)
	if err != nil {
		return cycleResult{}, fmt.Errorf("ADAPTER_ENTROPY_FAILED: %w", err)
	}
	if entropy.Yield != yield.ID {
		return cycleResult{}, fmt.Errorf("ENTROPY_YIELD_MISMATCH: %s != %s", entropy.Yield, yield.ID)
	}
	if entropy.Algorithm == "" || entropy.SeedDigest != runtime.seedDigest || entropy.TapeDigest == "" {
		return cycleResult{}, fmt.Errorf("ENTROPY_AUDIT_INVALID")
	}
	if entropy.Algorithm != runtime.manifest.Capabilities.Entropy.Algorithm {
		return cycleResult{}, fmt.Errorf("ENTROPY_ALGORITHM_MISMATCH: %s != %s",
			entropy.Algorithm, runtime.manifest.Capabilities.Entropy.Algorithm)
	}
	if entropy.DrawCount < runtime.lastEntropy {
		return cycleResult{}, fmt.Errorf("ENTROPY_DRAW_COUNT_REGRESSION: %d < %d", entropy.DrawCount, runtime.lastEntropy)
	}
	if err := entropy.Tape.Validate(); err != nil {
		return cycleResult{}, err
	}
	if entropy.Tape.SchemaVersion != controlentropy.TapeSchemaVersion || entropy.Tape.Encoding != "json" {
		return cycleResult{}, fmt.Errorf("ENTROPY_TAPE_PAYLOAD_UNSUPPORTED: %s/%s",
			entropy.Tape.SchemaVersion, entropy.Tape.Encoding)
	}
	var tape controlentropy.Tape
	if err := json.Unmarshal(entropy.Tape.Bytes, &tape); err != nil {
		return cycleResult{}, fmt.Errorf("ENTROPY_TAPE_DECODE_FAILED: %w", err)
	}
	if err := tape.Validate(); err != nil {
		return cycleResult{}, err
	}
	if tape.Algorithm != entropy.Algorithm || tape.SeedDigest != entropy.SeedDigest ||
		uint64(len(tape.Draws)) != entropy.DrawCount || tape.Digest != entropy.TapeDigest {
		return cycleResult{}, fmt.Errorf("ENTROPY_TAPE_ENVELOPE_MISMATCH")
	}
	if len(runtime.entropyLog.Draws) > len(tape.Draws) {
		return cycleResult{}, fmt.Errorf("ENTROPY_TAPE_PREFIX_TRUNCATED")
	}
	for index := range runtime.entropyLog.Draws {
		previous, err := control.CanonicalDigest(runtime.entropyLog.Draws[index])
		if err != nil {
			return cycleResult{}, err
		}
		current, err := control.CanonicalDigest(tape.Draws[index])
		if err != nil {
			return cycleResult{}, err
		}
		if previous != current {
			return cycleResult{}, fmt.Errorf("ENTROPY_TAPE_PREFIX_REWRITTEN: draw %d", index)
		}
	}
	return cycleResult{yield: yield, emission: emission, evidence: evidence, entropy: entropy, tape: tape}, nil
}

func validateYield(yield control.Yield, expected control.YieldKind) error {
	if yield.ID == "" || yield.StateDigest == "" {
		return fmt.Errorf("YIELD_IDENTITY_REQUIRED")
	}
	if yield.Kind != expected {
		return fmt.Errorf("YIELD_KIND_MISMATCH: got %s, want %s", yield.Kind, expected)
	}
	return nil
}

func (runtime *Runtime) verifyStableAudit(ctx context.Context) error {
	evidence, err := runtime.adapter.SnapshotEvidence(ctx)
	if err != nil {
		return fmt.Errorf("ADAPTER_EVIDENCE_FAILED_DURING_CHECK: %w", err)
	}
	if err := evidence.Payload.Validate(); err != nil {
		return err
	}
	expectedEvidence, err := control.CanonicalDigest(runtime.lastEvidence)
	if err != nil {
		return err
	}
	actualEvidence, err := control.CanonicalDigest(evidence)
	if err != nil {
		return err
	}
	if expectedEvidence != actualEvidence {
		return fmt.Errorf("ADAPTER_CHECK_MUTATED_EVIDENCE")
	}
	audit, err := runtime.adapter.SnapshotEntropy(ctx)
	if err != nil {
		return fmt.Errorf("ADAPTER_ENTROPY_FAILED_DURING_CHECK: %w", err)
	}
	expectedAudit, err := control.CanonicalDigest(runtime.lastAudit)
	if err != nil {
		return err
	}
	actualAudit, err := control.CanonicalDigest(audit)
	if err != nil {
		return err
	}
	if expectedAudit != actualAudit {
		return fmt.Errorf("ADAPTER_CHECK_MUTATED_ENTROPY")
	}
	return nil
}

func (runtime *Runtime) Trace() (Trace, error) {
	if runtime.terminal != nil {
		return runtime.successfulPrefixTrace()
	}
	copyTrace := cloneTrace(runtime.trace)
	var err error
	copyTrace.FinalStateDigest, err = runtime.stateDigest()
	if err != nil {
		return Trace{}, err
	}
	return copyTrace.Seal()
}

func (runtime *Runtime) successfulPrefixTrace() (Trace, error) {
	copyTrace := cloneTrace(runtime.trace)
	return copyTrace.Seal()
}

func (runtime *Runtime) DecisionLog() []control.ActionID {
	result := make([]control.ActionID, len(runtime.trace.Records))
	for index, record := range runtime.trace.Records {
		result[index] = record.Action.ID
	}
	return result
}

func (runtime *Runtime) TerminalOutcome() (TerminalOutcome, bool) {
	if runtime == nil || runtime.terminal == nil {
		return TerminalOutcome{}, false
	}
	outcome := *runtime.terminal
	outcome.AttemptedAction = cloneAction(outcome.AttemptedAction)
	return outcome, true
}

func (runtime *Runtime) fail(err error) error {
	if err != nil && runtime.failed == nil {
		runtime.failed = fmt.Errorf("INVALID_EXECUTION: %w", err)
	}
	return runtime.failed
}

func (runtime *Runtime) supportsAction(kind control.ActionKind) bool {
	for _, supported := range runtime.manifest.Capabilities.Actions {
		if supported == kind {
			return true
		}
	}
	return false
}

func (runtime *Runtime) supportsItem(kind control.ItemKind) bool {
	for _, supported := range runtime.manifest.Capabilities.Items {
		if supported == kind {
			return true
		}
	}
	return false
}

func (runtime *Runtime) supportsTemporal(kind control.TemporalKind) bool {
	for _, supported := range runtime.manifest.Capabilities.Temporal.Kinds {
		if supported == kind {
			return true
		}
	}
	return false
}

func (runtime *Runtime) supportsEffect(kind string) bool {
	for _, supported := range runtime.manifest.Capabilities.EffectKinds {
		if supported == kind {
			return true
		}
	}
	return false
}

func (runtime *Runtime) adapterDirected(kind control.ActionKind) bool {
	switch kind {
	case control.ActionInvoke, control.ActionDropMessage, control.ActionDeliverMessage, control.ActionFireTemporal,
		control.ActionCrash, control.ActionRestart, control.ActionCompleteEffect,
		control.ActionFailEffect, control.ActionCompleteCallback:
		return true
	default:
		return false
	}
}

func (runtime *Runtime) isPartitioned(left, right control.NodeID) bool {
	for _, value := range runtime.partitions {
		if (containsNode(value.left, left) && containsNode(value.right, right)) ||
			(containsNode(value.left, right) && containsNode(value.right, left)) {
			return true
		}
	}
	return false
}

func containsNode(nodes []control.NodeID, target control.NodeID) bool {
	index := sort.Search(len(nodes), func(i int) bool { return nodes[i] >= target })
	return index < len(nodes) && nodes[index] == target
}

func cloneTrace(trace Trace) Trace {
	encoded, err := json.Marshal(trace)
	if err != nil {
		panic(err)
	}
	var result Trace
	if err := json.Unmarshal(encoded, &result); err != nil {
		panic(err)
	}
	return result
}
