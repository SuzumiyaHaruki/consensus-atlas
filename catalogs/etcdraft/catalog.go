// Package etcdraft is the concrete composition layer for the public etcd/raft
// defect candidate catalog. Generic qualification remains in defectbench.
package etcdraft

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	driveretcdraft "github.com/SuzumiyaHaruki/consensus-atlas/drivers/etcdraft"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
)

const SnapshotID = "etcdraft-v3.6-capability-snapshot-v2"

type CampaignSpec struct {
	Version    int            `json:"version"`
	ID         string         `json:"id"`
	Family     string         `json:"family"`
	PSSID      string         `json:"pss_id"`
	FaultModel string         `json:"fault_model"`
	Bounds     CampaignBounds `json:"bounds"`
}

type CampaignBounds struct {
	Nodes          int `json:"nodes"`
	ElectionRounds int `json:"election_rounds"`
	Proposals      int `json:"proposals"`
	Crashes        int `json:"crashes"`
	Restarts       int `json:"restarts"`
	Partitions     int `json:"partitions"`
}

func BuildCapabilitySnapshot(
	profile coverage.Profile,
	driverManifest driver.Manifest,
	trustedMonitors []string,
	spec CampaignSpec,
) (defectbench.CapabilitySnapshot, error) {
	if err := profile.Validate(); err != nil {
		return defectbench.CapabilitySnapshot{}, err
	}
	if profile.Protocol != driveretcdraft.Protocol || driverManifest.SUT != "go.etcd.io/raft/v3" {
		return defectbench.CapabilitySnapshot{}, errors.New("etcdraft catalog requires the official etcd/raft binding")
	}
	if spec.Version != 1 || spec.ID == "" || profile.ID != "raft-campaign/"+spec.ID ||
		spec.Family != "raft" || spec.PSSID != profile.PSSID ||
		spec.FaultModel != "cft" || spec.Bounds.Nodes != len(profile.Nodes) {
		return defectbench.CapabilitySnapshot{}, errors.New("Raft campaign bounds do not match the current profile")
	}
	if err := validateCompiledBounds(profile, spec.Bounds); err != nil {
		return defectbench.CapabilitySnapshot{}, err
	}
	profileDigest, err := coverage.Digest(profile)
	if err != nil {
		return defectbench.CapabilitySnapshot{}, err
	}
	driverDigest, err := defectbench.DriverManifestDigest(driverManifest)
	if err != nil {
		return defectbench.CapabilitySnapshot{}, err
	}

	// This list comes from the concrete Runtime/host/Driver composition, never
	// from Profile obligations. A generic query event becomes the concrete
	// read-index fact only when this Driver explicitly supports that API.
	controllable := []string{
		string(core.EventStart), string(core.EventCampaign), string(core.EventPropose),
		string(core.EventMessage), string(core.EventCrash), string(core.EventRestart),
		string(core.EventDuplicate), string(core.EventPartition), string(core.EventHeal),
	}
	observable, err := observableEvidence(profile)
	if err != nil {
		return defectbench.CapabilitySnapshot{}, err
	}
	capabilitySet := make(map[string]bool, len(driverManifest.Capabilities))
	var capabilities []string
	for _, capability := range driverManifest.Capabilities {
		if capability.Supported {
			capabilities = append(capabilities, capability.ID)
			capabilitySet[capability.ID] = true
		}
	}
	if capabilitySet["read-index-input"] {
		controllable = append(controllable, string(core.EventQuery), "read-index")
	}
	if capabilitySet["read-state-observation"] {
		observable = append(observable, "read-state")
	}
	// Native Ready.MustSync evidence enters the snapshot only through the
	// opt-in Driver mode. Reaching a Ready with an unchanged HardState is an
	// execution precondition, not a directly controllable protocol input; it is
	// therefore proved by a deterministic witness trace rather than advertised
	// as a static capability.
	if capabilitySet["ready-must-sync-observation"] {
		observable = append(observable, "ready-must-sync")
	}
	executionOutcomes := []string{
		"campaign-report-v2",
		"deterministic-trace-replay",
		"in-process-error-classified",
		"oracle-from-saved-trace",
	}
	profileBounds := []string{
		fmt.Sprintf("nodes=%d", spec.Bounds.Nodes),
		"fault-model=" + spec.FaultModel,
	}
	profileBounds = appendCapacity(profileBounds, "election-rounds", spec.Bounds.ElectionRounds)
	profileBounds = appendCapacity(profileBounds, "proposals", spec.Bounds.Proposals)
	profileBounds = appendCapacity(profileBounds, "crashes", spec.Bounds.Crashes)
	profileBounds = appendCapacity(profileBounds, "restarts", spec.Bounds.Restarts)
	profileBounds = appendCapacity(profileBounds, "partitions", spec.Bounds.Partitions)
	snapshot := defectbench.CapabilitySnapshot{
		Version: defectbench.CapabilitySnapshotVersion, ID: SnapshotID,
		Protocol: profile.Protocol, Family: spec.Family, ProfileID: profile.ID, ProfileDigest: profileDigest,
		DriverManifestHash: driverDigest,
		ControllableInputs: normalized(controllable), ObservableEvents: observable,
		DriverCapabilities: normalized(capabilities), TrustedMonitors: normalized(trustedMonitors),
		ExecutionOutcomes: normalized(executionOutcomes), ProfileBounds: normalized(profileBounds),
	}
	if err := snapshot.Validate(); err != nil {
		return defectbench.CapabilitySnapshot{}, err
	}
	return snapshot, nil
}

func appendCapacity(values []string, name string, bound int) []string {
	for current := 1; current <= bound; current++ {
		values = append(values, fmt.Sprintf("%s>=%d", name, current))
	}
	return values
}

func validateCompiledBounds(profile coverage.Profile, bounds CampaignBounds) error {
	if bounds.Nodes < 1 || bounds.ElectionRounds < 1 || bounds.Proposals < 1 ||
		bounds.Crashes < 0 || bounds.Restarts < 0 || bounds.Partitions < 0 {
		return errors.New("Raft campaign bounds are outside the supported finite domain")
	}
	obligations, err := profile.Obligations()
	if err != nil {
		return err
	}
	ids := make(map[string]bool, len(obligations))
	for _, obligation := range obligations {
		ids[obligation.ID] = true
	}
	checks := []struct {
		prefix string
		bound  int
	}{
		{"boundary.election-rounds.", bounds.ElectionRounds},
		{"boundary.proposals.", bounds.Proposals},
		{"boundary.crashes.", bounds.Crashes},
		{"boundary.restarts.", bounds.Restarts},
		{"boundary.partitions.", bounds.Partitions},
	}
	for _, check := range checks {
		for current := 1; current <= check.bound; current++ {
			id := check.prefix + strconv.Itoa(current)
			if !ids[id] {
				return fmt.Errorf("compiled Profile is missing bound obligation %q", id)
			}
		}
		for id := range ids {
			if !strings.HasPrefix(id, check.prefix) {
				continue
			}
			value, err := strconv.Atoi(strings.TrimPrefix(id, check.prefix))
			if err != nil || value < 1 || value > check.bound {
				return fmt.Errorf("compiled Profile contains out-of-bound obligation %q", id)
			}
		}
	}
	return nil
}

func observableEvidence(profile coverage.Profile) ([]string, error) {
	obligations, err := profile.Obligations()
	if err != nil {
		return nil, err
	}
	values := make(map[string]bool)
	add := func(predicate coverage.TracePredicate) {
		if predicate.EventKind != "" {
			values["event:"+string(predicate.EventKind)] = true
		}
		if predicate.ObservationKind != "" {
			values["observation-kind:"+predicate.ObservationKind] = true
		}
		if predicate.ObservationLabel != "" {
			values["observation-label:"+predicate.ObservationLabel] = true
		}
		if predicate.Semantic != nil {
			values["semantic:"+predicate.Semantic.Domain+"/"+predicate.Semantic.Relation] = true
		}
	}
	for _, obligation := range obligations {
		if obligation.Status != coverage.StatusSupported {
			continue
		}
		for _, predicate := range obligation.Evidence.Reach {
			add(predicate)
		}
		for _, predicate := range obligation.Evidence.Observe {
			add(predicate)
		}
		for _, ordering := range obligation.Evidence.Orderings {
			add(ordering.Before)
			add(ordering.After)
			for _, predicate := range ordering.Without {
				add(predicate)
			}
		}
		for _, count := range obligation.Evidence.Counts {
			add(count.Predicate)
		}
	}
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func normalized(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
