package conformance

import (
	"context"
	"errors"
	"sync"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

// QualificationWork records the protocol-neutral control work actually
// attempted by conformance runners. A Reset creates one isolated Adapter
// execution; Submit and ApplyRuntimeAction each correspond to one selected
// scheduler Action. Read-only Manifest/Check/Snapshot calls are deliberately
// excluded from scheduler work and remain represented by wall-clock time.
type QualificationWork struct {
	SetupAttempts          int `json:"setup_attempts"`
	RuntimeInitializations int `json:"runtime_initializations"`
	SchedulerDecisions     int `json:"scheduler_decisions"`
	WorkUnits              int `json:"work_units"`
}

func (work QualificationWork) Validate() error {
	maxInt := int(^uint(0) >> 1)
	if work.SetupAttempts < 0 || work.RuntimeInitializations < 0 ||
		work.RuntimeInitializations != work.SetupAttempts || work.SchedulerDecisions < 0 ||
		work.SetupAttempts > maxInt-work.SchedulerDecisions ||
		work.WorkUnits != work.SetupAttempts+work.SchedulerDecisions {
		return errors.New("CONFORMANCE_QUALIFICATION_WORK_INVALID")
	}
	return nil
}

// WorkMeter is shared by every Adapter instance created during one complete
// qualification. It measures real calls without teaching conformance about a
// concrete protocol or changing the Adapter contract.
type WorkMeter struct {
	mu   sync.Mutex
	work QualificationWork
}

func (meter *WorkMeter) Snapshot() QualificationWork {
	meter.mu.Lock()
	defer meter.mu.Unlock()
	return meter.work
}

func (meter *WorkMeter) reset() {
	meter.mu.Lock()
	defer meter.mu.Unlock()
	meter.work.SetupAttempts++
	meter.work.RuntimeInitializations++
	meter.work.WorkUnits++
}

func (meter *WorkMeter) selectAction() {
	meter.mu.Lock()
	defer meter.mu.Unlock()
	meter.work.SchedulerDecisions++
	meter.work.WorkUnits++
}

// MeterFactory wraps an existing trusted Adapter factory. The returned meter
// belongs to that factory invocation set and must not be reused across two
// independent qualifications.
func MeterFactory(factory Factory) (Factory, *WorkMeter) {
	meter := &WorkMeter{}
	return func() control.Adapter {
		return &meteredAdapter{Adapter: factory(), meter: meter}
	}, meter
}

type meteredAdapter struct {
	control.Adapter
	meter *WorkMeter
}

func (adapter *meteredAdapter) Reset(ctx context.Context, seed []byte) error {
	adapter.meter.reset()
	return adapter.Adapter.Reset(ctx, seed)
}

func (adapter *meteredAdapter) Submit(ctx context.Context, command control.AdapterCommand) error {
	adapter.meter.selectAction()
	return adapter.Adapter.Submit(ctx, command)
}

func (adapter *meteredAdapter) ApplyRuntimeAction(ctx context.Context, action control.Action) error {
	adapter.meter.selectAction()
	return adapter.Adapter.ApplyRuntimeAction(ctx, action)
}

func (adapter *meteredAdapter) Close() error {
	if closer, ok := adapter.Adapter.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}
