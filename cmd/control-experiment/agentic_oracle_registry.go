package main

import (
	"errors"
	"reflect"
	"sort"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
)

// agenticOracleRegistration is the target-local single source for both the
// planning capability projection and the deterministic monitor execution.
// Common orchestration only compares IDs; protocol meaning remains here.
type agenticOracleRegistration struct {
	Capability controlexperiment.AgentOracleCapability
	Monitor    oracle.BundleMonitor
}

type agenticOracleRegistry struct {
	registrations []agenticOracleRegistration
}

func (registry agenticOracleRegistry) validate() error {
	if len(registry.registrations) == 0 {
		return errors.New("AGENTIC_ORACLE_REGISTRY_INVALID")
	}
	seen := make(map[string]bool, len(registry.registrations))
	for _, registration := range registry.registrations {
		capability := registration.Capability
		if registration.Monitor == nil || strings.TrimSpace(capability.ID) == "" ||
			capability.ID != registration.Monitor.Name() || seen[capability.ID] ||
			(capability.Scope != controlexperiment.AgentOracleScopeGeneric &&
				capability.Scope != controlexperiment.AgentOracleScopeTarget) ||
			!strictlyIncreasingStrings(capability.PropertyIDs) {
			return errors.New("AGENTIC_ORACLE_REGISTRY_INVALID")
		}
		seen[capability.ID] = true
	}
	return nil
}

func (registry agenticOracleRegistry) Capabilities() []controlexperiment.AgentOracleCapability {
	result := make([]controlexperiment.AgentOracleCapability, len(registry.registrations))
	for index, registration := range registry.registrations {
		result[index] = registration.Capability
		result[index].PropertyIDs = append([]string(nil), registration.Capability.PropertyIDs...)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (registry agenticOracleRegistry) Check(
	bundle controlexperiment.ExecutionBundle,
) oracle.Result {
	monitors := make([]oracle.BundleMonitor, len(registry.registrations))
	for index, registration := range registry.registrations {
		monitors[index] = registration.Monitor
	}
	return oracle.CheckBundle(bundle, monitors...)
}

func (registry agenticOracleRegistry) MatchesCapabilities(
	capabilities []controlexperiment.AgentOracleCapability,
) bool {
	return registry.validate() == nil && reflect.DeepEqual(registry.Capabilities(), capabilities)
}

func strictlyIncreasingStrings(values []string) bool {
	for index, value := range values {
		if strings.TrimSpace(value) == "" || index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}

func etcdraftAgenticOracleRegistry() agenticOracleRegistry {
	return agenticOracleRegistry{registrations: []agenticOracleRegistration{
		agenticOracleRegistration{
			Capability: controlexperiment.AgentOracleCapability{
				ID: "trace-integrity", Scope: controlexperiment.AgentOracleScopeGeneric,
			},
			Monitor: oracle.BundleTraceIntegrity{},
		},
		agenticOracleRegistration{
			Capability: controlexperiment.AgentOracleCapability{
				ID: "agreement", Scope: controlexperiment.AgentOracleScopeGeneric,
				PropertyIDs: []string{"consensus-agreement"},
			},
			Monitor: oracle.BundleAgreement{},
		},
		agenticOracleRegistration{
			Capability: controlexperiment.AgentOracleCapability{
				ID: etcdraftLogProgressMonitorID, Scope: controlexperiment.AgentOracleScopeTarget,
				PropertyIDs: []string{"applied-prefix-consistency", "recovery-progress-monotonicity"},
			},
			Monitor: etcdraftLogProgressMonitor{},
		},
		agenticOracleRegistration{
			Capability: controlexperiment.AgentOracleCapability{
				ID:          etcdraftClientApplicationBindingMonitorID,
				Scope:       controlexperiment.AgentOracleScopeTarget,
				PropertyIDs: []string{"client-operation-continuity"},
			},
			Monitor: etcdraftClientApplicationBindingMonitor{},
		},
	}}
}

func omnipaxosAgenticOracleRegistry() agenticOracleRegistry {
	return agenticOracleRegistry{registrations: []agenticOracleRegistration{
		agenticOracleRegistration{
			Capability: controlexperiment.AgentOracleCapability{
				ID: "trace-integrity", Scope: controlexperiment.AgentOracleScopeGeneric,
			},
			Monitor: oracle.BundleTraceIntegrity{},
		},
		agenticOracleRegistration{
			Capability: controlexperiment.AgentOracleCapability{
				ID: "agreement", Scope: controlexperiment.AgentOracleScopeGeneric,
				PropertyIDs: []string{"consensus-agreement", "decided-prefix-consistency"},
			},
			Monitor: oracle.BundleAgreement{},
		},
	}}
}
