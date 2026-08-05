package protocolstate_test

import (
	"errors"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate"
)

type fixtureProjector struct{}

func (fixtureProjector) ID() string { return "fixture-v1" }

func (fixtureProjector) IsSample(record core.TraceRecord) bool {
	return record.Event.Kind == core.EventAcknowledge
}

func (fixtureProjector) Project(snapshot any) (any, string, error) {
	key, ok := snapshot.(string)
	if !ok {
		return nil, "", errors.New("snapshot is not a string")
	}
	return map[string]string{"key": key}, key, nil
}

func TestDiscoverCountsUniqueKeysAndRetainsFirstWitness(t *testing.T) {
	trace := []core.TraceRecord{
		{Step: 1, Event: core.Event{Kind: core.EventPersist}, After: "ignored"},
		{Step: 2, Event: core.Event{Kind: core.EventAcknowledge}, After: "a"},
		{Step: 3, Event: core.Event{Kind: core.EventAcknowledge}, After: "a"},
		{Step: 4, Event: core.Event{Kind: core.EventAcknowledge}, After: "b"},
	}
	summary, err := protocolstate.Discover(trace, fixtureProjector{})
	if err != nil {
		t.Fatal(err)
	}
	if summary.PSSID != "fixture-v1" || summary.Samples != 3 || summary.UniqueStates != 2 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if len(summary.States) != 2 || summary.States[0].FirstStep != 2 || summary.States[1].FirstStep != 4 {
		t.Fatalf("unexpected witnesses: %#v", summary.States)
	}
	if len(summary.Curve) != 3 || summary.Curve[1].NewState || !summary.Curve[2].NewState {
		t.Fatalf("unexpected curve: %#v", summary.Curve)
	}
}

func TestDiscoverRejectsEmptyKeys(t *testing.T) {
	_, err := protocolstate.Discover([]core.TraceRecord{{
		Step: 1, Event: core.Event{Kind: core.EventAcknowledge}, After: "",
	}}, fixtureProjector{})
	if err == nil {
		t.Fatal("empty projected key was accepted")
	}
}
