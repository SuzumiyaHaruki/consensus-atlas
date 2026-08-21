package conformance

import (
	"context"
	"errors"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlruntime"
)

func readFactoryManifest(ctx context.Context, factory Factory) (_ control.AdapterManifest, err error) {
	if factory == nil {
		return control.AdapterManifest{}, errors.New("CONFORMANCE_FACTORY_REQUIRED")
	}
	adapter := factory()
	if adapter == nil {
		return control.AdapterManifest{}, errors.New("CONFORMANCE_FACTORY_REQUIRED")
	}
	defer func() {
		err = errors.Join(err, closeAdapter(adapter))
	}()
	return adapter.Manifest(ctx)
}

func replayAndClose(
	ctx context.Context,
	adapter control.Adapter,
	config controlruntime.Config,
	trace controlruntime.Trace,
) (err error) {
	runtime, err := controlruntime.Replay(ctx, adapter, config, trace)
	if err != nil {
		return err
	}
	return runtime.Close()
}

func closeAdapter(adapter control.Adapter) error {
	if closer, ok := adapter.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}
