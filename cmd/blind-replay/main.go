// blind-replay deterministically reruns a trusted Blind Planner bundle. It
// has no model client and is the evaluator-compatible replay binary for a
// candidate-specific agent-campaign build.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/bindings"
	"github.com/SuzumiyaHaruki/consensus-atlas/families"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/adapter"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/agentcampaign"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/campaign"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

func main() {
	profilePath := flag.String("profile", "", "frozen Campaign Profile v2")
	bundlePath := flag.String("plans", "", "private trusted replay bundle")
	outPath := flag.String("out", "", "campaign report output")
	artifact := flag.String("artifact", "", "must equal trial://<opaque trial id>")
	flag.Parse()
	if err := run(context.Background(), *profilePath, *bundlePath, *outPath, *artifact); err != nil {
		fmt.Fprintln(os.Stderr, "blind-replay:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, profilePath, bundlePath, outPath, artifact string) error {
	if profilePath == "" || bundlePath == "" || outPath == "" {
		return errors.New("-profile, -plans, and -out are required")
	}
	var profile coverage.Profile
	if err := readStrictJSON(profilePath, &profile); err != nil {
		return err
	}
	var bundle agentcampaign.TrustedReplayBundle
	if err := readStrictJSON(bundlePath, &bundle); err != nil {
		return err
	}
	if artifact == "" {
		artifact = "trial://" + bundle.Scope.TrialID
	}
	if artifact != "trial://"+bundle.Scope.TrialID {
		return errors.New("artifact does not match trusted replay trial")
	}
	_, manifest, err := bindings.New(profile)
	if err != nil {
		return err
	}
	var matcher coverage.SemanticMatcher
	var monitors []oracle.Monitor
	if profile.PSSID != "" {
		matcher, err = families.CoverageMatcher(profile.PSSID)
		if err != nil {
			return err
		}
		monitors, err = families.CampaignMonitors(profile.PSSID, manifest)
		if err != nil {
			return err
		}
	}
	newAdapter := func() (adapter.Adapter, error) {
		current, _, createErr := bindings.New(profile)
		return current, createErr
	}
	report, err := agentcampaign.ReplayTrusted(ctx, profile, manifest, matcher, bundle, newAdapter, campaign.Options{Artifact: artifact, Monitors: monitors})
	if err != nil {
		return err
	}
	return writeJSON(outPath, report)
}

func readStrictJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode %s: trailing JSON data", path)
	}
	return nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if directory := filepath.Dir(path); directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
