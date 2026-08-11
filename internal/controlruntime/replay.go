package controlruntime

import (
	"context"
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
		return nil, progress, err
	}
	runtime, err := New(ctx, adapter, config)
	if err != nil {
		return nil, progress, err
	}
	progress.RuntimeInitialized = true
	actualInitial, err := runtime.Trace()
	if err != nil {
		return nil, progress, err
	}
	expectedInitial := expected
	expectedInitial.Records = nil
	expectedInitial.FinalStateDigest = expected.InitialStateDigest
	expectedInitial.Digest = ""
	expectedInitial, err = expectedInitial.Seal()
	if err != nil {
		return nil, progress, err
	}
	if actualInitial.Digest != expectedInitial.Digest {
		return nil, progress, &ReplayDivergenceError{
			Step: 0, Expected: expectedInitial.Digest, Actual: actualInitial.Digest,
		}
	}
	for _, want := range expected.Records {
		if want.Action.Kind == control.ActionInvoke || want.Action.Kind == control.ActionPartition {
			runtime.offered[want.Action.ID] = want.Action
			progress.PrepareActions++
			if want.Action.Kind == control.ActionInvoke {
				progress.WorkloadOffers++
			}
		}
		got, err := runtime.Select(ctx, want.Action.ID)
		if err != nil {
			return nil, progress, err
		}
		progress.Decisions++
		wantDigest, err := control.CanonicalDigest(want)
		if err != nil {
			return nil, progress, err
		}
		gotDigest, err := control.CanonicalDigest(got)
		if err != nil {
			return nil, progress, err
		}
		if wantDigest != gotDigest {
			return nil, progress, &ReplayDivergenceError{
				Step: want.Step, Expected: wantDigest, Actual: gotDigest,
			}
		}
	}
	actual, err := runtime.Trace()
	if err != nil {
		return nil, progress, err
	}
	if actual.Digest != expected.Digest {
		return nil, progress, &ReplayDivergenceError{
			Step: uint64(len(expected.Records)), Expected: expected.Digest, Actual: actual.Digest,
		}
	}
	return runtime, progress, nil
}
