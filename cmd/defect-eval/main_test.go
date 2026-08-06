package main

import (
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
)

func TestFrozenPSSIDUsesManifestAndRestrictsLegacyFallback(t *testing.T) {
	manifest := defectbench.Manifest{PSSID: "frozen-pss-v1"}
	got, err := frozenPSSID(manifest, "ignored-legacy")
	if err != nil || got != manifest.PSSID {
		t.Fatalf("manifest PSS = %q, %v", got, err)
	}
	got, err = frozenPSSID(defectbench.Manifest{}, "archived-pss-v1")
	if err != nil || got != "archived-pss-v1" {
		t.Fatalf("legacy PSS = %q, %v", got, err)
	}
	if _, err := frozenPSSID(defectbench.Manifest{}, ""); err == nil {
		t.Fatal("missing PSS identity was accepted")
	}
}
