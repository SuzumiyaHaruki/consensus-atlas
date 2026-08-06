package adapter

import (
	"context"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
)

// Adapter is the engine-facing execution target. New protocol integrations
// should implement driver.ProtocolDriver and use host.Adapter instead of
// reimplementing this interface. Implementations must not read wall-clock time
// or choose random events.
type Adapter interface {
	Protocol() string
	Nodes() []string
	// Enabled is a read-only host/protocol precondition. Returning false keeps
	// an event pending; the engine never consumes temporarily inapplicable work.
	Enabled(core.Event) (bool, string)
	Apply(context.Context, core.Event) (core.ApplyResult, error)
	Snapshot() any
	CheckConformance() error
}

// TimerSource is an optional, side-effect-free declaration boundary for
// virtual time. It returns the complete set of currently live timers at now;
// it neither invokes a protocol API nor schedules transport. The Engine owns
// the resulting pending timeout events.
type TimerSource interface {
	Timers(now uint64) ([]core.Timer, error)
}
