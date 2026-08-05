package adapter

import (
	"context"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/core"
)

// Adapter is the only protocol-specific component in the trusted execution
// path. Implementations must not read wall-clock time or choose random events.
type Adapter interface {
	Protocol() string
	Nodes() []string
	Apply(context.Context, core.Event) (core.ApplyResult, error)
	Snapshot() any
	CheckConformance() error
}
