package targetoracles

import (
	"errors"
	"reflect"
	"sort"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/omnipaxosv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	EtcdraftV2TargetID  = "etcdraft-v2"
	OmnipaxosV2TargetID = "omnipaxos-v2"
)

// Registration is the target-owned single source for both the planning
// capability projection and deterministic monitor execution.
type Registration struct {
	Capability controlexperiment.AgentOracleCapability
	Monitor    oracle.BundleMonitor
}

// Registry binds one target and decision projector to its executable
// monitors. It is composition data, not an Oracle DSL: protocol meaning stays
// in the concrete monitors while callers select existing monitors by ID.
type Registry struct {
	targetID    string
	projectorID string
	entries     []Registration
}

func newRegistry(targetID, projectorID string, entries ...Registration) Registry {
	return Registry{
		targetID: targetID, projectorID: projectorID,
		entries: append([]Registration(nil), entries...),
	}
}

func (registry Registry) TargetID() string    { return registry.targetID }
func (registry Registry) ProjectorID() string { return registry.projectorID }

func (registry Registry) Validate() error {
	if strings.TrimSpace(registry.targetID) == "" ||
		strings.TrimSpace(registry.projectorID) == "" || len(registry.entries) == 0 {
		return errors.New("TARGET_ORACLE_REGISTRY_INVALID")
	}
	seen := make(map[string]bool, len(registry.entries))
	for _, entry := range registry.entries {
		capability := entry.Capability
		if entry.Monitor == nil || strings.TrimSpace(capability.ID) == "" ||
			capability.ID != entry.Monitor.Name() || seen[capability.ID] ||
			(capability.Scope != controlexperiment.AgentOracleScopeGeneric &&
				capability.Scope != controlexperiment.AgentOracleScopeTarget) ||
			!strictlyIncreasingStrings(capability.PropertyIDs) {
			return errors.New("TARGET_ORACLE_REGISTRY_INVALID")
		}
		seen[capability.ID] = true
	}
	if !seen["trace-integrity"] {
		return errors.New("TARGET_ORACLE_REGISTRY_INVALID")
	}
	return nil
}

func (registry Registry) Matches(targetID, projectorID string) bool {
	return registry.Validate() == nil && registry.targetID == targetID &&
		registry.projectorID == projectorID
}

func (registry Registry) Capabilities() []controlexperiment.AgentOracleCapability {
	result := make([]controlexperiment.AgentOracleCapability, len(registry.entries))
	for index, entry := range registry.entries {
		result[index] = entry.Capability
		result[index].PropertyIDs = append([]string(nil), entry.Capability.PropertyIDs...)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (registry Registry) MatchesCapabilities(
	capabilities []controlexperiment.AgentOracleCapability,
) bool {
	return registry.Validate() == nil && reflect.DeepEqual(registry.Capabilities(), capabilities)
}

// EvaluationMonitors returns all contract-selectable monitors. Trace
// integrity is deliberately omitted because the evaluator always executes it
// independently before target verdict monitors.
func (registry Registry) EvaluationMonitors() []oracle.BundleMonitor {
	result := make([]oracle.BundleMonitor, 0, len(registry.entries)-1)
	for _, entry := range registry.entries {
		if entry.Monitor.Name() != "trace-integrity" {
			result = append(result, entry.Monitor)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name() < result[j].Name() })
	return result
}

// Check runs the same monitor set and order used by a formal contract that
// selects every monitor exposed by EvaluationMonitors.
func (registry Registry) Check(bundle controlexperiment.ExecutionBundle) oracle.Result {
	monitors := append([]oracle.BundleMonitor{oracle.BundleTraceIntegrity{}}, registry.EvaluationMonitors()...)
	return oracle.CheckBundle(bundle, monitors...)
}

func Resolve(
	targetID string,
	projectorID string,
) (semantic.DecisionProjector, Registry, error) {
	projector, registry, err := ResolveTarget(targetID)
	if err != nil {
		return nil, Registry{}, err
	}
	if !registry.Matches(targetID, projectorID) || projector.ID() != projectorID {
		return nil, Registry{}, errors.New("TARGET_ORACLE_COMPOSITION_MISMATCH")
	}
	return projector, registry, nil
}

// ResolveTarget returns the registered projector and Oracle registry for one
// concrete Target. Evaluator-side saved-Bundle checks use this entry point so
// the caller cannot pair a Target with a different projector identifier.
func ResolveTarget(targetID string) (semantic.DecisionProjector, Registry, error) {
	var projector semantic.DecisionProjector
	var registry Registry
	switch targetID {
	case EtcdraftV2TargetID:
		projector = etcdraftv2.DecisionProjector{}
		registry = EtcdraftV2Registry()
	case OmnipaxosV2TargetID:
		projector = omnipaxosv2.DecisionProjector{}
		registry = OmnipaxosV2Registry()
	default:
		return nil, Registry{}, errors.New("TARGET_ORACLE_TARGET_UNSUPPORTED")
	}
	if !registry.Matches(targetID, projector.ID()) {
		return nil, Registry{}, errors.New("TARGET_ORACLE_COMPOSITION_MISMATCH")
	}
	return projector, registry, nil
}

func strictlyIncreasingStrings(values []string) bool {
	for index, value := range values {
		if strings.TrimSpace(value) == "" || index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}
