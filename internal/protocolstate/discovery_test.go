package protocolstate_test

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate"
)

func TestDiscoverCountsUniqueKeysAndRetainsFirstWitness(t *testing.T) {
	samples := []protocolstate.Sample{
		{Step: 2, Key: "a", State: map[string]string{"key": "a"}},
		{Step: 3, Key: "a", State: map[string]string{"key": "a"}},
		{Step: 4, Key: "b", State: map[string]string{"key": "b"}},
	}
	summary, err := protocolstate.Discover("fixture-v1", samples)
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
	_, err := protocolstate.Discover("fixture-v1", []protocolstate.Sample{{Step: 1}})
	if err == nil {
		t.Fatal("empty sample key was accepted")
	}
}

func TestDiscoverRejectsEmptyPSSID(t *testing.T) {
	if _, err := protocolstate.Discover("", nil); err == nil {
		t.Fatal("empty PSS ID was accepted")
	}
}

func TestDiscoverRejectsNonIncreasingSteps(t *testing.T) {
	_, err := protocolstate.Discover("fixture-v1", []protocolstate.Sample{
		{Step: 2, Key: "a"}, {Step: 2, Key: "b"},
	})
	if err == nil {
		t.Fatal("duplicate sample step was accepted")
	}
}
