package control

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

const SchemaVersion = "consensus-atlas/control/v2alpha1"

type NodeID string
type ActionID string
type CommandID string
type YieldID string
type ItemID string
type MessageID string
type TemporalID string
type EffectID string
type CallbackID string
type EntropyDrawID string
type ObservationID string

type NodeRef struct {
	Node        NodeID `json:"node"`
	Incarnation uint64 `json:"incarnation"`
}

func (r NodeRef) Validate() error {
	if r.Node == "" {
		return invalid("NODE_REQUIRED", "node", "must not be empty")
	}
	if r.Incarnation == 0 {
		return invalid("INCARNATION_REQUIRED", "incarnation", "must be greater than zero")
	}
	return nil
}

type NodeLifecycle string

const (
	NodeStopped NodeLifecycle = "stopped"
	NodeRunning NodeLifecycle = "running"
)

type ActionKind string

const (
	ActionDropMessage      ActionKind = "drop-message"
	ActionDuplicateMessage ActionKind = "duplicate-message"
	ActionPartition        ActionKind = "partition"
	ActionHeal             ActionKind = "heal"
	ActionInvoke           ActionKind = "invoke"
	ActionDeliverMessage   ActionKind = "deliver-message"
	ActionFireTemporal     ActionKind = "fire-temporal-event"
	ActionCrash            ActionKind = "crash"
	ActionRestart          ActionKind = "restart"
	ActionCompleteEffect   ActionKind = "complete-effect"
	ActionFailEffect       ActionKind = "fail-effect"
	ActionCompleteCallback ActionKind = "complete-callback"
)

var allActionKinds = map[ActionKind]struct{}{
	ActionDropMessage: {}, ActionDuplicateMessage: {}, ActionPartition: {},
	ActionHeal: {}, ActionInvoke: {},
	ActionDeliverMessage: {}, ActionFireTemporal: {}, ActionCrash: {},
	ActionRestart: {}, ActionCompleteEffect: {}, ActionFailEffect: {},
	ActionCompleteCallback: {},
}

func (k ActionKind) Validate() error {
	if _, ok := allActionKinds[k]; !ok {
		return invalid("ACTION_KIND_UNSUPPORTED", "kind", fmt.Sprintf("%q", k))
	}
	return nil
}

type Action struct {
	ID         ActionID        `json:"id"`
	Kind       ActionKind      `json:"kind"`
	Node       NodeRef         `json:"node_ref,omitempty"`
	Item       ItemID          `json:"item_id,omitempty"`
	Parameters json.RawMessage `json:"parameters,omitempty"`
}

type AdapterCommand struct {
	ID      CommandID       `json:"id"`
	Action  ActionID        `json:"action_id"`
	Kind    ActionKind      `json:"kind"`
	Node    NodeRef         `json:"node_ref,omitempty"`
	Item    ItemID          `json:"item_id,omitempty"`
	Payload PayloadEnvelope `json:"payload,omitempty"`
}

type ItemKind string

const (
	ItemMessage      ItemKind = "message"
	ItemTemporal     ItemKind = "temporal"
	ItemEffect       ItemKind = "host-effect"
	ItemCallback     ItemKind = "host-callback"
	ItemClientResult ItemKind = "client-response"
	ItemObservation  ItemKind = "typed-observation"
)

var allItemKinds = map[ItemKind]struct{}{
	ItemMessage: {}, ItemTemporal: {}, ItemEffect: {}, ItemCallback: {},
	ItemClientResult: {}, ItemObservation: {},
}

func (k ItemKind) Validate() error {
	if _, ok := allItemKinds[k]; !ok {
		return invalid("ITEM_KIND_UNSUPPORTED", "kind", fmt.Sprintf("%q", k))
	}
	return nil
}

type ItemState string

const (
	ItemProduced  ItemState = "produced"
	ItemBlocked   ItemState = "blocked"
	ItemEnabled   ItemState = "enabled"
	ItemCompleted ItemState = "completed"
	ItemCanceled  ItemState = "canceled"
	ItemFailed    ItemState = "failed"
	ItemDropped   ItemState = "dropped"
)

type MessageEnvelope struct {
	ID       MessageID         `json:"id"`
	CloneOf  MessageID         `json:"clone_of,omitempty"`
	Source   NodeRef           `json:"source"`
	Target   NodeID            `json:"target"`
	TypeHint string            `json:"type_hint,omitempty"`
	Payload  PayloadEnvelope   `json:"payload"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type TemporalKind string

const (
	TemporalOneShotTimer  TemporalKind = "one-shot-timer"
	TemporalPeriodicPulse TemporalKind = "periodic-pulse"
	TemporalSleepWakeup   TemporalKind = "sleep-wakeup"
)

var allTemporalKinds = map[TemporalKind]struct{}{
	TemporalOneShotTimer: {}, TemporalPeriodicPulse: {}, TemporalSleepWakeup: {},
}

func (k TemporalKind) Validate() error {
	if _, ok := allTemporalKinds[k]; !ok {
		return invalid("TEMPORAL_KIND_UNSUPPORTED", "kind", fmt.Sprintf("%q", k))
	}
	return nil
}

type TemporalItem struct {
	ID          TemporalID      `json:"id"`
	Kind        TemporalKind    `json:"kind"`
	Owner       NodeRef         `json:"owner"`
	ClockDomain string          `json:"clock_domain"`
	Deadline    uint64          `json:"deadline"`
	Period      uint64          `json:"period,omitempty"`
	Callback    PayloadEnvelope `json:"callback"`
}

type DurabilityClass string

const (
	DurabilityVolatile DurabilityClass = "volatile"
	DurabilityVisible  DurabilityClass = "visible"
	DurabilityDurable  DurabilityClass = "durable"
	DurabilityApplied  DurabilityClass = "applied"
)

type HostEffect struct {
	ID              EffectID        `json:"id"`
	Kind            string          `json:"kind"`
	Owner           NodeRef         `json:"owner"`
	Request         PayloadEnvelope `json:"request"`
	AllowedResults  []string        `json:"allowed_results,omitempty"`
	AllowedFailures []string        `json:"allowed_failures,omitempty"`
	Durability      DurabilityClass `json:"durability"`
}

type HostCallback struct {
	ID             CallbackID      `json:"id"`
	Kind           string          `json:"kind"`
	Owner          NodeRef         `json:"owner"`
	Request        PayloadEnvelope `json:"request"`
	AllowedResults []string        `json:"allowed_results,omitempty"`
}

type ClientResponse struct {
	RequestID string          `json:"request_id"`
	Owner     NodeRef         `json:"owner"`
	Status    string          `json:"status"`
	Payload   PayloadEnvelope `json:"payload"`
}

type TypedObservation struct {
	ID      ObservationID   `json:"id"`
	Owner   NodeRef         `json:"owner"`
	Kind    string          `json:"kind"`
	Payload PayloadEnvelope `json:"payload"`
}

type ProducedItem struct {
	ID           ItemID            `json:"id"`
	Kind         ItemKind          `json:"kind"`
	Owner        NodeRef           `json:"owner"`
	Dependencies []ItemID          `json:"dependencies,omitempty"`
	Message      *MessageEnvelope  `json:"message,omitempty"`
	Temporal     *TemporalItem     `json:"temporal,omitempty"`
	Effect       *HostEffect       `json:"effect,omitempty"`
	Callback     *HostCallback     `json:"callback,omitempty"`
	Response     *ClientResponse   `json:"response,omitempty"`
	Observation  *TypedObservation `json:"observation,omitempty"`
}

func (item ProducedItem) Validate() error {
	if item.ID == "" {
		return invalid("ITEM_ID_REQUIRED", "id", "must not be empty")
	}
	if _, ok := allItemKinds[item.Kind]; !ok {
		return invalid("ITEM_KIND_UNSUPPORTED", "kind", fmt.Sprintf("%q", item.Kind))
	}
	if err := item.Owner.Validate(); err != nil {
		return err
	}
	seen := make(map[ItemID]struct{}, len(item.Dependencies))
	for _, dependency := range item.Dependencies {
		if dependency == "" {
			return invalid("ITEM_DEPENDENCY_REQUIRED", "dependencies", "must not contain empty ID")
		}
		if dependency == item.ID {
			return invalid("ITEM_SELF_DEPENDENCY", "dependencies", string(dependency))
		}
		if _, ok := seen[dependency]; ok {
			return invalid("ITEM_DEPENDENCY_DUPLICATE", "dependencies", string(dependency))
		}
		seen[dependency] = struct{}{}
	}
	present := 0
	for _, value := range []bool{
		item.Message != nil, item.Temporal != nil, item.Effect != nil,
		item.Callback != nil, item.Response != nil, item.Observation != nil,
	} {
		if value {
			present++
		}
	}
	if present != 1 {
		return invalid("ITEM_PAYLOAD_CARDINALITY", "payload", "exactly one typed payload is required")
	}
	if err := item.validateTypedPayload(); err != nil {
		return err
	}
	return nil
}

func (item ProducedItem) validateTypedPayload() error {
	switch item.Kind {
	case ItemMessage:
		if item.Message == nil {
			return invalid("ITEM_PAYLOAD_KIND_MISMATCH", "message", "is required")
		}
		if item.Message.ID == "" || item.Message.Target == "" {
			return invalid("MESSAGE_IDENTITY_REQUIRED", "message", "id and target are required")
		}
		if err := item.Message.Source.Validate(); err != nil {
			return err
		}
		if item.Message.Source != item.Owner {
			return invalid("ITEM_OWNER_MISMATCH", "message.source", "must equal item owner")
		}
		if item.Message.CloneOf != "" && item.Message.CloneOf == item.Message.ID {
			return invalid("MESSAGE_CLONE_SELF_REFERENCE", "message.clone_of", string(item.Message.ID))
		}
		return item.Message.Payload.Validate()
	case ItemTemporal:
		if item.Temporal == nil {
			return invalid("ITEM_PAYLOAD_KIND_MISMATCH", "temporal", "is required")
		}
		if item.Temporal.ID == "" || item.Temporal.ClockDomain == "" {
			return invalid("TEMPORAL_IDENTITY_REQUIRED", "temporal", "id and clock domain are required")
		}
		if _, ok := allTemporalKinds[item.Temporal.Kind]; !ok {
			return invalid("TEMPORAL_KIND_UNSUPPORTED", "temporal.kind", string(item.Temporal.Kind))
		}
		if item.Temporal.Owner != item.Owner {
			return invalid("ITEM_OWNER_MISMATCH", "temporal.owner", "must equal item owner")
		}
		if item.Temporal.Kind == TemporalPeriodicPulse && item.Temporal.Period == 0 {
			return invalid("TEMPORAL_PERIOD_REQUIRED", "temporal.period", "must be greater than zero")
		}
		if item.Temporal.Kind != TemporalPeriodicPulse && item.Temporal.Period != 0 {
			return invalid("TEMPORAL_PERIOD_UNEXPECTED", "temporal.period", "only periodic pulse has a period")
		}
		return item.Temporal.Callback.Validate()
	case ItemEffect:
		if item.Effect == nil || item.Effect.ID == "" || item.Effect.Kind == "" {
			return invalid("EFFECT_IDENTITY_REQUIRED", "effect", "id and kind are required")
		}
		if item.Effect.Owner != item.Owner {
			return invalid("ITEM_OWNER_MISMATCH", "effect.owner", "must equal item owner")
		}
		if item.Effect.Durability == "" {
			return invalid("EFFECT_DURABILITY_REQUIRED", "effect.durability", "must not be empty")
		}
		switch item.Effect.Durability {
		case DurabilityVolatile, DurabilityVisible, DurabilityDurable, DurabilityApplied:
		default:
			return invalid("EFFECT_DURABILITY_UNSUPPORTED", "effect.durability", string(item.Effect.Durability))
		}
		if len(item.Effect.AllowedResults)+len(item.Effect.AllowedFailures) == 0 {
			return invalid("EFFECT_OUTCOME_REQUIRED", "effect", "at least one result or failure is required")
		}
		if err := uniqueStrings("effect.allowed_results", item.Effect.AllowedResults); err != nil && len(item.Effect.AllowedResults) > 0 {
			return err
		}
		if err := uniqueStrings("effect.allowed_failures", item.Effect.AllowedFailures); err != nil && len(item.Effect.AllowedFailures) > 0 {
			return err
		}
		return item.Effect.Request.Validate()
	case ItemCallback:
		if item.Callback == nil || item.Callback.ID == "" || item.Callback.Kind == "" {
			return invalid("CALLBACK_IDENTITY_REQUIRED", "callback", "id and kind are required")
		}
		if item.Callback.Owner != item.Owner {
			return invalid("ITEM_OWNER_MISMATCH", "callback.owner", "must equal item owner")
		}
		if len(item.Callback.AllowedResults) == 0 {
			return invalid("CALLBACK_RESULT_REQUIRED", "callback.allowed_results", "must not be empty")
		}
		if err := uniqueStrings("callback.allowed_results", item.Callback.AllowedResults); err != nil {
			return err
		}
		return item.Callback.Request.Validate()
	case ItemClientResult:
		if item.Response == nil || item.Response.RequestID == "" || item.Response.Status == "" {
			return invalid("RESPONSE_IDENTITY_REQUIRED", "response", "request and status are required")
		}
		if item.Response.Owner != item.Owner {
			return invalid("ITEM_OWNER_MISMATCH", "response.owner", "must equal item owner")
		}
		return item.Response.Payload.Validate()
	case ItemObservation:
		if item.Observation == nil || item.Observation.ID == "" || item.Observation.Kind == "" {
			return invalid("OBSERVATION_IDENTITY_REQUIRED", "observation", "id and kind are required")
		}
		if item.Observation.Owner != item.Owner {
			return invalid("ITEM_OWNER_MISMATCH", "observation.owner", "must equal item owner")
		}
		return item.Observation.Payload.Validate()
	default:
		return invalid("ITEM_KIND_UNSUPPORTED", "kind", string(item.Kind))
	}
}

type YieldKind string

const (
	YieldStable   YieldKind = "stable"
	YieldTerminal YieldKind = "terminal"
)

type Yield struct {
	ID          YieldID   `json:"id"`
	Kind        YieldKind `json:"kind"`
	StateDigest string    `json:"state_digest"`
}

type Emission struct {
	SchemaVersion string         `json:"schema_version"`
	Yield         YieldID        `json:"yield_id"`
	Items         []ProducedItem `json:"items,omitempty"`
	Digest        string         `json:"digest"`
}

func (emission Emission) Seal() (Emission, error) {
	emission.SchemaVersion = SchemaVersion
	if emission.Yield == "" {
		return Emission{}, invalid("EMISSION_YIELD_REQUIRED", "yield_id", "must not be empty")
	}
	seen := make(map[ItemID]struct{}, len(emission.Items))
	for _, item := range emission.Items {
		if err := item.Validate(); err != nil {
			return Emission{}, fmt.Errorf("item %q: %w", item.ID, err)
		}
		if _, ok := seen[item.ID]; ok {
			return Emission{}, invalid("EMISSION_ITEM_DUPLICATE", "items", string(item.ID))
		}
		seen[item.ID] = struct{}{}
	}
	emission.Digest = ""
	digest, err := CanonicalDigest(emission)
	if err != nil {
		return Emission{}, err
	}
	emission.Digest = digest
	return emission, nil
}

func (emission Emission) Validate() error {
	sealed, err := emission.Seal()
	if err != nil {
		return err
	}
	if emission.SchemaVersion != SchemaVersion {
		return invalid("EMISSION_SCHEMA_MISMATCH", "schema_version", emission.SchemaVersion)
	}
	if emission.Digest != sealed.Digest {
		return invalid("EMISSION_DIGEST_MISMATCH", "digest", "does not match emission")
	}
	return nil
}

type EvidenceEnvelope struct {
	Yield   YieldID         `json:"yield_id"`
	Payload PayloadEnvelope `json:"payload"`
}

type EntropyAuditEnvelope struct {
	Yield      YieldID         `json:"yield_id"`
	Algorithm  string          `json:"algorithm"`
	SeedDigest string          `json:"seed_digest"`
	DrawCount  uint64          `json:"draw_count"`
	TapeDigest string          `json:"tape_digest"`
	Tape       PayloadEnvelope `json:"tape"`
}

type CommandEligibility struct {
	Eligible   bool   `json:"eligible"`
	ReasonCode string `json:"reason_code,omitempty"`
}

func (e CommandEligibility) Validate() error {
	if e.Eligible && e.ReasonCode != "" {
		return invalid("ELIGIBILITY_REASON_UNEXPECTED", "reason_code", "eligible command cannot have a reason")
	}
	if !e.Eligible && e.ReasonCode == "" {
		return invalid("ELIGIBILITY_REASON_REQUIRED", "reason_code", "ineligible command requires a reason")
	}
	return nil
}

type TemporalCapability struct {
	Kinds      []TemporalKind `json:"kinds,omitempty"`
	ClockError uint64         `json:"clock_error"`
}

type EntropyCapability struct {
	Provider     string `json:"provider,omitempty"`
	Algorithm    string `json:"algorithm,omitempty"`
	DomainPolicy string `json:"domain_policy,omitempty"`
	ResetPolicy  string `json:"reset_policy,omitempty"`
	StrictReplay bool   `json:"strict_replay"`
}

type CapabilityManifest struct {
	Actions            []ActionKind       `json:"actions,omitempty"`
	Items              []ItemKind         `json:"items,omitempty"`
	Temporal           TemporalCapability `json:"temporal"`
	Entropy            EntropyCapability  `json:"entropy"`
	CrashModes         []string           `json:"crash_modes,omitempty"`
	EffectKinds        []string           `json:"effect_kinds,omitempty"`
	StrictYield        bool               `json:"strict_yield"`
	StrictReplay       bool               `json:"strict_replay"`
	DurableCheckpoints bool               `json:"durable_checkpoints"`
}

type AdapterManifest struct {
	SchemaVersion       string             `json:"schema_version"`
	AdapterID           string             `json:"adapter_id"`
	ImplementationID    string             `json:"implementation_id"`
	BuildID             string             `json:"build_id"`
	ConfigurationDigest string             `json:"configuration_digest"`
	Nodes               []NodeID           `json:"nodes"`
	Capabilities        CapabilityManifest `json:"capabilities"`
	EvidenceSchemas     []string           `json:"evidence_schemas,omitempty"`
}

func (manifest AdapterManifest) Validate() error {
	if manifest.SchemaVersion != SchemaVersion {
		return invalid("MANIFEST_SCHEMA_MISMATCH", "schema_version", manifest.SchemaVersion)
	}
	if manifest.AdapterID == "" || manifest.ImplementationID == "" || manifest.BuildID == "" ||
		manifest.ConfigurationDigest == "" {
		return invalid("MANIFEST_IDENTITY_REQUIRED", "identity", "adapter, implementation, build, and configuration IDs are required")
	}
	if len(manifest.Nodes) == 0 {
		return invalid("MANIFEST_NODES_REQUIRED", "nodes", "must not be empty")
	}
	if err := uniqueStrings("nodes", nodeStrings(manifest.Nodes)); err != nil {
		return err
	}
	for _, action := range manifest.Capabilities.Actions {
		if err := action.Validate(); err != nil {
			return err
		}
	}
	actionNames := make([]string, len(manifest.Capabilities.Actions))
	for index, action := range manifest.Capabilities.Actions {
		actionNames[index] = string(action)
	}
	if err := uniqueStrings("capabilities.actions", actionNames); err != nil {
		return err
	}
	itemNames := make([]string, len(manifest.Capabilities.Items))
	for _, item := range manifest.Capabilities.Items {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	for index, item := range manifest.Capabilities.Items {
		itemNames[index] = string(item)
	}
	if err := uniqueStrings("capabilities.items", itemNames); err != nil {
		return err
	}
	temporalNames := make([]string, len(manifest.Capabilities.Temporal.Kinds))
	for _, kind := range manifest.Capabilities.Temporal.Kinds {
		if err := kind.Validate(); err != nil {
			return err
		}
	}
	for index, kind := range manifest.Capabilities.Temporal.Kinds {
		temporalNames[index] = string(kind)
	}
	if err := uniqueStrings("capabilities.temporal.kinds", temporalNames); err != nil && len(temporalNames) > 0 {
		return err
	}
	if err := uniqueStrings("capabilities.crash_modes", manifest.Capabilities.CrashModes); err != nil && len(manifest.Capabilities.CrashModes) > 0 {
		return err
	}
	if err := uniqueStrings("capabilities.effect_kinds", manifest.Capabilities.EffectKinds); err != nil && len(manifest.Capabilities.EffectKinds) > 0 {
		return err
	}
	if err := uniqueStrings("evidence_schemas", manifest.EvidenceSchemas); err != nil && len(manifest.EvidenceSchemas) > 0 {
		return err
	}
	if manifest.Capabilities.StrictReplay && !manifest.Capabilities.StrictYield {
		return invalid("STRICT_REPLAY_REQUIRES_YIELD", "capabilities.strict_replay", "strict yield is required")
	}
	if manifest.Capabilities.Entropy.StrictReplay &&
		(manifest.Capabilities.Entropy.Provider == "" || manifest.Capabilities.Entropy.Algorithm == "" ||
			manifest.Capabilities.Entropy.DomainPolicy == "" || manifest.Capabilities.Entropy.ResetPolicy == "") {
		return invalid("STRICT_ENTROPY_REPLAY_IDENTITY_REQUIRED", "capabilities.entropy", "provider, algorithm, domain, and reset policies are required")
	}
	return nil
}

func (manifest AdapterManifest) Digest() (string, error) {
	if err := manifest.Validate(); err != nil {
		return "", err
	}
	copyManifest := manifest
	copyManifest.Nodes = append([]NodeID(nil), manifest.Nodes...)
	sort.Slice(copyManifest.Nodes, func(i, j int) bool { return copyManifest.Nodes[i] < copyManifest.Nodes[j] })
	copyManifest.Capabilities.Actions = append([]ActionKind(nil), manifest.Capabilities.Actions...)
	sort.Slice(copyManifest.Capabilities.Actions, func(i, j int) bool {
		return copyManifest.Capabilities.Actions[i] < copyManifest.Capabilities.Actions[j]
	})
	copyManifest.Capabilities.Items = append([]ItemKind(nil), manifest.Capabilities.Items...)
	sort.Slice(copyManifest.Capabilities.Items, func(i, j int) bool {
		return copyManifest.Capabilities.Items[i] < copyManifest.Capabilities.Items[j]
	})
	copyManifest.Capabilities.Temporal.Kinds = append([]TemporalKind(nil), manifest.Capabilities.Temporal.Kinds...)
	sort.Slice(copyManifest.Capabilities.Temporal.Kinds, func(i, j int) bool {
		return copyManifest.Capabilities.Temporal.Kinds[i] < copyManifest.Capabilities.Temporal.Kinds[j]
	})
	copyManifest.Capabilities.CrashModes = append([]string(nil), manifest.Capabilities.CrashModes...)
	sort.Strings(copyManifest.Capabilities.CrashModes)
	copyManifest.Capabilities.EffectKinds = append([]string(nil), manifest.Capabilities.EffectKinds...)
	sort.Strings(copyManifest.Capabilities.EffectKinds)
	copyManifest.EvidenceSchemas = append([]string(nil), manifest.EvidenceSchemas...)
	sort.Strings(copyManifest.EvidenceSchemas)
	return CanonicalDigest(copyManifest)
}

func uniqueStrings(field string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			return invalid("VALUE_REQUIRED", field, "must not contain empty values")
		}
		if _, ok := seen[value]; ok {
			return invalid("VALUE_DUPLICATE", field, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func nodeStrings(nodes []NodeID) []string {
	values := make([]string, len(nodes))
	for i, node := range nodes {
		values[i] = string(node)
	}
	return values
}

// Adapter is the single engine-facing v2 contract. Optional native features
// are normalized behind this interface rather than discovered by type asserts.
type Adapter interface {
	Manifest(context.Context) (AdapterManifest, error)
	Reset(context.Context, []byte) error
	Check(context.Context, AdapterCommand) (CommandEligibility, error)
	Submit(context.Context, AdapterCommand) error
	CheckRuntimeAction(context.Context, Action) (CommandEligibility, error)
	// ApplyRuntimeAction mirrors a Runtime-owned action into an external
	// mechanism. The Runtime remains the sole owner of the semantic state.
	ApplyRuntimeAction(context.Context, Action) error
	RunUntilYield(context.Context) (Yield, error)
	Collect(context.Context, YieldID) (Emission, error)
	SnapshotEvidence(context.Context) (EvidenceEnvelope, error)
	SnapshotEntropy(context.Context) (EntropyAuditEnvelope, error)
}
