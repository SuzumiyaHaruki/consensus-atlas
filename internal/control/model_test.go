package control_test

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

func TestStableIDPreservesComponentBoundaries(t *testing.T) {
	left, err := control.StableID("item", "ab", "c")
	if err != nil {
		t.Fatal(err)
	}
	right, err := control.StableID("item", "a", "bc")
	if err != nil {
		t.Fatal(err)
	}
	if left == right {
		t.Fatalf("boundary collision: %q", left)
	}
}

func TestPayloadDetectsMutation(t *testing.T) {
	payload, err := control.NewPayload("fixture/v1", "bytes", []byte("before"))
	if err != nil {
		t.Fatal(err)
	}
	payload.Bytes[0] = 'a'
	if err := payload.Validate(); control.ValidationCode(err) != "PAYLOAD_DIGEST_MISMATCH" {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestJSONPayloadMatchesEncodedPayload(t *testing.T) {
	value := struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}{Name: "fixture", Count: 2}
	got, err := control.NewJSONPayload("fixture/v1", value)
	if err != nil {
		t.Fatal(err)
	}
	want, err := control.NewPayload("fixture/v1", "json", []byte(`{"name":"fixture","count":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest != want.Digest || string(got.Bytes) != string(want.Bytes) {
		t.Fatalf("JSON payload changed: got %#v, want %#v", got, want)
	}
}

func TestProducedItemRejectsSelfDependency(t *testing.T) {
	payload, err := control.NewPayload("fixture/v1", "bytes", []byte("m"))
	if err != nil {
		t.Fatal(err)
	}
	owner := control.NodeRef{Node: "n1", Incarnation: 1}
	item := control.ProducedItem{
		ID: "item-1", Kind: control.ItemMessage, Owner: owner,
		Dependencies: []control.ItemID{"item-1"},
		Message: &control.MessageEnvelope{
			ID: "message-1", Source: owner, Target: "n2", Payload: payload,
		},
	}
	if err := item.Validate(); control.ValidationCode(err) != "ITEM_SELF_DEPENDENCY" {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestEmissionSealAndValidate(t *testing.T) {
	payload, err := control.NewPayload("fixture/v1", "bytes", []byte("wake"))
	if err != nil {
		t.Fatal(err)
	}
	owner := control.NodeRef{Node: "n1", Incarnation: 1}
	emission, err := (control.Emission{
		Yield: "yield-1",
		Items: []control.ProducedItem{{
			ID: "item-1", Kind: control.ItemTemporal, Owner: owner,
			Temporal: &control.TemporalItem{
				ID: "temporal-1", Kind: control.TemporalPeriodicPulse,
				Owner: owner, ClockDomain: "cluster", Deadline: 1, Period: 1,
				Callback: payload,
			},
		}},
	}).Seal()
	if err != nil {
		t.Fatal(err)
	}
	if err := emission.Validate(); err != nil {
		t.Fatal(err)
	}
	emission.Items[0].Temporal.Deadline++
	if err := emission.Validate(); control.ValidationCode(err) != "EMISSION_DIGEST_MISMATCH" {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestManifestDigestIgnoresCapabilityOrder(t *testing.T) {
	left := fixtureManifest()
	right := fixtureManifest()
	right.Nodes[0], right.Nodes[1] = right.Nodes[1], right.Nodes[0]
	right.Capabilities.Actions[0], right.Capabilities.Actions[1] =
		right.Capabilities.Actions[1], right.Capabilities.Actions[0]
	leftDigest, err := left.Digest()
	if err != nil {
		t.Fatal(err)
	}
	rightDigest, err := right.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if leftDigest != rightDigest {
		t.Fatalf("digest differs: %s != %s", leftDigest, rightDigest)
	}
}

func fixtureManifest() control.AdapterManifest {
	return control.AdapterManifest{
		SchemaVersion: control.SchemaVersion,
		AdapterID:     "fixture", ImplementationID: "fixture/v1", BuildID: "fixture-build",
		ConfigurationDigest: "fixture-configuration-digest",
		Nodes:               []control.NodeID{"n1", "n2"},
		Capabilities: control.CapabilityManifest{
			Actions:      []control.ActionKind{control.ActionInvoke, control.ActionCrash},
			Items:        []control.ItemKind{control.ItemMessage, control.ItemTemporal},
			StrictYield:  true,
			StrictReplay: true,
			Temporal: control.TemporalCapability{
				Kinds: []control.TemporalKind{control.TemporalPeriodicPulse},
			},
		},
	}
}
