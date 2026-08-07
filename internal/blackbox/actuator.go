package blackbox

import (
	"errors"
	"fmt"
	"sync"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

// GatewayActuator applies typed Partition/Heal actions to a fixed binding.
// It serializes gate changes and preserves overlapping partition references.
type GatewayActuator struct {
	mu      sync.Mutex
	binding *GatewayBinding
	active  map[string][]GatewayLink
	refs    map[string]int
}

func NewGatewayActuator(binding *GatewayBinding) (*GatewayActuator, error) {
	if binding == nil {
		return nil, errors.New("BLACKBOX_GATEWAY_BINDING_REQUIRED")
	}
	return &GatewayActuator{
		binding: binding, active: make(map[string][]GatewayLink), refs: make(map[string]int),
	}, nil
}

func (actuator *GatewayActuator) Check(action control.Action) (control.CommandEligibility, error) {
	actuator.mu.Lock()
	defer actuator.mu.Unlock()
	_, reason := actuator.checkLocked(action)
	if reason != "" {
		return control.CommandEligibility{ReasonCode: reason}, nil
	}
	return control.CommandEligibility{Eligible: true}, nil
}

func (actuator *GatewayActuator) Apply(action control.Action) error {
	actuator.mu.Lock()
	defer actuator.mu.Unlock()
	resolution, reason := actuator.checkLocked(action)
	if reason != "" {
		return errors.New(reason)
	}
	if action.Kind == control.ActionPartition {
		return actuator.partition(resolution)
	}
	return actuator.heal(resolution)
}

func (actuator *GatewayActuator) checkLocked(action control.Action) (GatewayResolution, string) {
	resolution, err := actuator.binding.Resolve(action)
	if err != nil {
		return GatewayResolution{}, err.Error()
	}
	_, active := actuator.active[resolution.PartitionID]
	if action.Kind == control.ActionPartition && active {
		return GatewayResolution{}, "BLACKBOX_GATEWAY_PARTITION_ALREADY_ACTIVE"
	}
	if action.Kind == control.ActionHeal && !active {
		return GatewayResolution{}, "BLACKBOX_GATEWAY_PARTITION_NOT_ACTIVE"
	}
	for _, link := range resolution.Links {
		identity := link.Gateway.Identity()
		snapshot := link.Gateway.Snapshot()
		if !snapshot.Started || snapshot.Closed || snapshot.Partitioned != (actuator.refs[identity] > 0) {
			return GatewayResolution{}, "BLACKBOX_GATEWAY_ACTUATOR_STATE_MISMATCH"
		}
	}
	return resolution, ""
}

func (actuator *GatewayActuator) partition(resolution GatewayResolution) error {
	changed := make([]GatewayControl, 0, len(resolution.Links))
	for _, link := range resolution.Links {
		identity := link.Gateway.Identity()
		if actuator.refs[identity] == 0 {
			if err := link.Gateway.Partition(); err != nil {
				return rollback(changed, func(gateway GatewayControl) error { return gateway.Heal() }, err)
			}
			changed = append(changed, link.Gateway)
		}
	}
	for _, link := range resolution.Links {
		actuator.refs[link.Gateway.Identity()]++
	}
	actuator.active[resolution.PartitionID] = append([]GatewayLink(nil), resolution.Links...)
	return nil
}

func (actuator *GatewayActuator) heal(resolution GatewayResolution) error {
	links := actuator.active[resolution.PartitionID]
	changed := make([]GatewayControl, 0, len(links))
	for _, link := range links {
		if actuator.refs[link.Gateway.Identity()] == 1 {
			if err := link.Gateway.Heal(); err != nil {
				return rollback(changed, func(gateway GatewayControl) error { return gateway.Partition() }, err)
			}
			changed = append(changed, link.Gateway)
		}
	}
	for _, link := range links {
		identity := link.Gateway.Identity()
		actuator.refs[identity]--
		if actuator.refs[identity] == 0 {
			delete(actuator.refs, identity)
		}
	}
	delete(actuator.active, resolution.PartitionID)
	return nil
}

func rollback(changed []GatewayControl, undo func(GatewayControl) error, cause error) error {
	for index := len(changed) - 1; index >= 0; index-- {
		if err := undo(changed[index]); err != nil {
			return fmt.Errorf("BLACKBOX_GATEWAY_ROLLBACK_FAILED: %v; cause: %w", err, cause)
		}
	}
	return fmt.Errorf("BLACKBOX_GATEWAY_ACTUATION_FAILED: %w", cause)
}
