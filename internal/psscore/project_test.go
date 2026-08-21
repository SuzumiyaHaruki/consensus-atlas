package psscore

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

func TestProjectIgnoresNativeIdentityAbsoluteTimeAndPayload(t *testing.T) {
	leftSnapshot, leftGraph := equivalentFixture("alpha", "beta", 3, 5, 100, "left-value")
	rightSnapshot, rightGraph := equivalentFixture("zeta", "eta", 7, 10, 900, "right-value")

	left, err := Project(leftSnapshot, "fixture/core-v1", SemanticObservation{LogicalTime: leftSnapshot.LogicalTime, Graph: leftGraph})
	if err != nil {
		t.Fatal(err)
	}
	right, err := Project(rightSnapshot, "fixture/core-v1", SemanticObservation{LogicalTime: rightSnapshot.LogicalTime, Graph: rightGraph})
	if err != nil {
		t.Fatal(err)
	}
	if left.Digest != right.Digest {
		t.Fatalf("equivalent projections differ:\nleft  %s\nright %s", left.Digest, right.Digest)
	}
	if left.Control.Participants[0].ID != "n1" || left.Control.Participants[1].ID != "n2" {
		t.Fatalf("participants were not canonicalized: %+v", left.Control.Participants)
	}
	if err := left.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestProjectSeparatesFundamentalControlState(t *testing.T) {
	snapshot, graph := equivalentFixture("alpha", "beta", 3, 5, 100, "value")
	baseline, err := Project(snapshot, "fixture/core-v1", SemanticObservation{LogicalTime: snapshot.LogicalTime, Graph: graph})
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Items = append(snapshot.Items, controlruntime.ItemSnapshot{
		ID: "another-message", Kind: control.ItemMessage,
		Owner: control.NodeRef{Node: "alpha", Incarnation: 3}, State: control.ItemEnabled,
		Value: control.ProducedItem{
			ID: "opaque-item-2", Kind: control.ItemMessage,
			Owner: control.NodeRef{Node: "alpha", Incarnation: 3},
			Message: &control.MessageEnvelope{
				ID: "opaque-message-2", Source: control.NodeRef{Node: "alpha", Incarnation: 3},
				Target: "beta", TypeHint: "ignored-native-type",
			},
		},
	})
	changed, err := Project(snapshot, "fixture/core-v1", SemanticObservation{LogicalTime: snapshot.LogicalTime, Graph: graph})
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Digest == changed.Digest {
		t.Fatal("an additional pending message did not change Core PSS")
	}
	baselineViews, err := Keys(baseline)
	if err != nil {
		t.Fatal(err)
	}
	changedViews, err := Keys(changed)
	if err != nil {
		t.Fatal(err)
	}
	if baselineViews.Protocol != changedViews.Protocol ||
		baselineViews.Control == changedViews.Control || baselineViews.Joint == changedViews.Joint {
		t.Fatalf("pending message was not isolated to control/joint views: %#v/%#v",
			baselineViews, changedViews)
	}
}

func TestViewKeysSeparateSemanticProgressFromStableControl(t *testing.T) {
	snapshot, graph := equivalentFixture("alpha", "beta", 3, 5, 100, "value")
	baseline, err := Project(snapshot, "fixture/core-v1", SemanticObservation{
		LogicalTime: snapshot.LogicalTime, Graph: graph,
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := range graph.Entities {
		if graph.Entities[index].Kind == EntityDecision {
			graph.Entities[index].Stage = StageDecided
		}
	}
	changed, err := Project(snapshot, "fixture/core-v1", SemanticObservation{
		LogicalTime: snapshot.LogicalTime, Graph: graph,
	})
	if err != nil {
		t.Fatal(err)
	}
	baselineViews, err := Keys(baseline)
	if err != nil {
		t.Fatal(err)
	}
	changedViews, err := Keys(changed)
	if err != nil {
		t.Fatal(err)
	}
	if baselineViews.Protocol == changedViews.Protocol ||
		baselineViews.Control != changedViews.Control || baselineViews.Joint == changedViews.Joint {
		t.Fatalf("semantic change was not isolated to protocol/joint views: %#v/%#v",
			baselineViews, changedViews)
	}
}

func TestProjectSeparatesMessageFromPriorIncarnation(t *testing.T) {
	snapshot, graph := equivalentFixture("alpha", "beta", 3, 5, 100, "value")
	current, err := Project(snapshot, "fixture/core-v1", SemanticObservation{LogicalTime: snapshot.LogicalTime, Graph: graph})
	if err != nil {
		t.Fatal(err)
	}
	for index := range snapshot.Nodes {
		if snapshot.Nodes[index].Ref.Node == "alpha" {
			snapshot.Nodes[index].Ref.Incarnation++
		}
	}
	prior, err := Project(snapshot, "fixture/core-v1", SemanticObservation{LogicalTime: snapshot.LogicalTime, Graph: graph})
	if err != nil {
		t.Fatal(err)
	}
	if current.Digest == prior.Digest {
		t.Fatal("a released message from a prior incarnation was merged with a current-incarnation message")
	}
}

func TestProjectRejectsSemanticParticipantMismatch(t *testing.T) {
	snapshot, graph := equivalentFixture("alpha", "beta", 3, 5, 100, "value")
	graph.Entities = graph.Entities[1:]
	if _, err := Project(snapshot, "fixture/core-v1", SemanticObservation{LogicalTime: snapshot.LogicalTime, Graph: graph}); err == nil {
		t.Fatal("Project accepted a missing Participant entity")
	}
}

func TestProjectRejectsStaleSemanticObservation(t *testing.T) {
	snapshot, graph := equivalentFixture("alpha", "beta", 3, 5, 100, "value")
	if _, err := Project(snapshot, "fixture/core-v1", SemanticObservation{
		LogicalTime: snapshot.LogicalTime + 1, Graph: graph,
	}); err == nil {
		t.Fatal("Project accepted evidence from a different logical instant")
	}
}

func TestValidateRejectsDigestMutation(t *testing.T) {
	snapshot, graph := equivalentFixture("alpha", "beta", 3, 5, 100, "value")
	state, err := Project(snapshot, "fixture/core-v1", SemanticObservation{LogicalTime: snapshot.LogicalTime, Graph: graph})
	if err != nil {
		t.Fatal(err)
	}
	state.Digest = "not-the-sealed-digest"
	if err := state.Validate(); err == nil {
		t.Fatal("Validate accepted a mutated digest")
	}
}

func equivalentFixture(
	passive, coordinating control.NodeID,
	passiveIncarnation, coordinatingIncarnation, deadline uint64,
	valueID string,
) (controlruntime.Snapshot, SemanticGraph) {
	passiveRef := control.NodeRef{Node: passive, Incarnation: passiveIncarnation}
	coordinatingRef := control.NodeRef{Node: coordinating, Incarnation: coordinatingIncarnation}
	snapshot := controlruntime.Snapshot{
		Step: 44, LogicalTime: deadline - 10, AdapterStateDigest: "ignored-adapter-state",
		Nodes: []controlruntime.NodeSnapshot{
			{Ref: passiveRef, Lifecycle: control.NodeRunning},
			{Ref: coordinatingRef, Lifecycle: control.NodeRunning},
		},
		Partitions: []controlruntime.PartitionSnapshot{{
			ID: "ignored-partition-id", Left: []control.NodeID{passive}, Right: []control.NodeID{coordinating},
		}},
		Items: []controlruntime.ItemSnapshot{
			{
				ID: "ignored-message-item", Kind: control.ItemMessage, Owner: passiveRef, State: control.ItemEnabled,
				Value: control.ProducedItem{
					ID: "different-native-item-id", Kind: control.ItemMessage, Owner: passiveRef,
					Message: &control.MessageEnvelope{
						ID: "different-native-message-id", Source: passiveRef, Target: coordinating,
						TypeHint: "ignored", Payload: control.PayloadEnvelope{Bytes: []byte("opaque payload is excluded")},
					},
				},
			},
			{
				ID: "ignored-temporal-item", Kind: control.ItemTemporal, Owner: coordinatingRef, State: control.ItemBlocked,
				Value: control.ProducedItem{
					ID: "different-native-temporal-item", Kind: control.ItemTemporal, Owner: coordinatingRef,
					Temporal: &control.TemporalItem{
						ID: "different-native-timer-id", Kind: control.TemporalPeriodicPulse,
						Owner: coordinatingRef, ClockDomain: "ignored-clock", Deadline: deadline, Period: 7,
					},
				},
			},
		},
	}
	graph := SemanticGraph{
		Entities: []Entity{
			{ID: string(passive), Kind: EntityParticipant, Mode: ModePassive},
			{ID: string(coordinating), Kind: EntityParticipant, Mode: ModeCoordinating},
			{ID: "epoch-1", Kind: EntityEpoch, Stage: StageUnknown},
			{ID: "decision-1", Kind: EntityDecision, Stage: StageApplied},
			{ID: valueID, Kind: EntityValue, Stage: StageApplied},
		},
		Relations: []Relation{
			{Kind: RelationBelongsTo, From: string(passive), To: "epoch-1"},
			{Kind: RelationBelongsTo, From: string(coordinating), To: "epoch-1"},
			{Kind: RelationApplies, From: string(passive), To: "decision-1"},
			{Kind: RelationBelongsTo, From: "decision-1", To: valueID},
		},
	}
	return snapshot, graph
}
