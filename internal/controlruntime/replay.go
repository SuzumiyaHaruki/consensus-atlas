package controlruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

type ReplayDivergenceError struct {
	Step     uint64
	Expected string
	Actual   string
}

// ReplayProgress reports only work that completed before Replay returned. It
// lets callers charge failed replay attempts without duplicating replay logic.
type ReplayProgress struct {
	RuntimeInitialized bool
	PrepareActions     int
	WorkloadOffers     int
	Decisions          int
}

func (e *ReplayDivergenceError) Error() string {
	return fmt.Sprintf("REPLAY_DIVERGENCE at step %d: expected %s, got %s", e.Step, e.Expected, e.Actual)
}

func Replay(ctx context.Context, adapter control.Adapter, config Config, expected Trace) (*Runtime, error) {
	runtime, _, err := ReplayWithProgress(ctx, adapter, config, expected)
	return runtime, err
}

func ReplayWithProgress(
	ctx context.Context,
	adapter control.Adapter,
	config Config,
	expected Trace,
) (*Runtime, ReplayProgress, error) {
	var progress ReplayProgress
	if err := expected.Validate(); err != nil {
		return nil, progress, errors.Join(err, closeAdapter(adapter))
	}
	runtime, err := New(ctx, adapter, config)
	if err != nil {
		return nil, progress, err
	}
	fail := func(cause error) (*Runtime, ReplayProgress, error) {
		return nil, progress, errors.Join(cause, runtime.Close())
	}
	progress.RuntimeInitialized = true
	actualInitial, err := runtime.Trace()
	if err != nil {
		return fail(err)
	}
	expectedInitial := expected
	expectedInitial.Records = nil
	expectedInitial.FinalStateDigest = expected.InitialStateDigest
	expectedInitial.Digest = ""
	expectedInitial, err = expectedInitial.Seal()
	if err != nil {
		return fail(err)
	}
	if actualInitial.Digest != expectedInitial.Digest {
		return fail(&ReplayDivergenceError{
			Step: 0, Expected: expectedInitial.Digest, Actual: actualInitial.Digest,
		})
	}
	for _, want := range expected.Records {
		if want.Action.Kind == control.ActionInvoke || want.Action.Kind == control.ActionPartition {
			if err := runtime.reofferExpected(ctx, want.Action); err != nil {
				return fail(err)
			}
			progress.PrepareActions++
			if want.Action.Kind == control.ActionInvoke {
				progress.WorkloadOffers++
			}
		}
		got, err := runtime.Select(ctx, want.Action.ID)
		if err != nil {
			return fail(err)
		}
		progress.Decisions++
		wantDigest, err := control.CanonicalDigest(want)
		if err != nil {
			return fail(err)
		}
		gotDigest, err := control.CanonicalDigest(got)
		if err != nil {
			return fail(err)
		}
		if wantDigest != gotDigest {
			return fail(&ReplayDivergenceError{
				Step: want.Step, Expected: wantDigest, Actual: gotDigest,
			})
		}
	}
	actual, err := runtime.Trace()
	if err != nil {
		return fail(err)
	}
	if actual.Digest != expected.Digest {
		return fail(&ReplayDivergenceError{
			Step: uint64(len(expected.Records)), Expected: expected.Digest, Actual: actual.Digest,
		})
	}
	return runtime, progress, nil
}

// reofferExpected reconstructs author-supplied actions through the same public
// validation path used during primary execution. Saved traces are evidence,
// not permission to inject directly into Runtime-owned state.
func (runtime *Runtime) reofferExpected(ctx context.Context, expected control.Action) error {
	var (
		id  control.ActionID
		err error
	)
	switch expected.Kind {
	case control.ActionInvoke:
		var parameters control.AdapterInvokeParameters
		if err := json.Unmarshal(expected.Parameters, &parameters); err != nil {
			return fmt.Errorf("REPLAY_PREPARATION_PARAMETERS_INVALID: %w", err)
		}
		id, err = runtime.OfferInvoke(ctx, expected.Node.Node, parameters.Input)
	case control.ActionPartition:
		var parameters control.PartitionParameters
		parameters, err = control.DecodePartitionParameters(expected.Parameters)
		if err == nil {
			id, err = runtime.OfferPartition(parameters.Left, parameters.Right)
		}
	default:
		return fmt.Errorf("REPLAY_PREPARATION_KIND_UNSUPPORTED: %s", expected.Kind)
	}
	if err != nil {
		return fmt.Errorf("REPLAY_PREPARATION_REJECTED: %s: %w", expected.Kind, err)
	}
	actual := runtime.offered[id]
	wantDigest, digestErr := control.CanonicalDigest(expected)
	if digestErr != nil {
		return digestErr
	}
	actualDigest, digestErr := control.CanonicalDigest(actual)
	if digestErr != nil {
		return digestErr
	}
	if id != expected.ID || actualDigest != wantDigest {
		delete(runtime.offered, id)
		return fmt.Errorf("REPLAY_PREPARATION_MISMATCH: %s", expected.ID)
	}
	return nil
}
