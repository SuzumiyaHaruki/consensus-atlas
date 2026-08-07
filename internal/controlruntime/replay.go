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

func (e *ReplayDivergenceError) Error() string {
	return fmt.Sprintf("REPLAY_DIVERGENCE at step %d: expected %s, got %s", e.Step, e.Expected, e.Actual)
}

func Replay(ctx context.Context, adapter control.Adapter, config Config, expected Trace) (*Runtime, error) {
	if err := expected.Validate(); err != nil {
		return nil, err
	}
	runtime, err := New(ctx, adapter, config)
	if err != nil {
		return nil, err
	}
	actualInitial, err := runtime.Trace()
	if err != nil {
		return nil, err
	}
	expectedInitial := expected
	expectedInitial.Records = nil
	expectedInitial.FinalStateDigest = expected.InitialStateDigest
	expectedInitial.Digest = ""
	expectedInitial, err = expectedInitial.Seal()
	if err != nil {
		return nil, err
	}
	if actualInitial.Digest != expectedInitial.Digest {
		return nil, &ReplayDivergenceError{Step: 0, Expected: expectedInitial.Digest, Actual: actualInitial.Digest}
	}
	for _, want := range expected.Records {
		if want.Action.Kind == control.ActionInvoke || want.Action.Kind == control.ActionPartition {
			runtime.offered[want.Action.ID] = want.Action
		}
		got, err := runtime.Select(ctx, want.Action.ID)
		if err != nil {
			return nil, err
		}
		wantDigest, err := control.CanonicalDigest(want)
		if err != nil {
			return nil, err
		}
		gotDigest, err := control.CanonicalDigest(got)
		if err != nil {
			return nil, err
		}
		if wantDigest != gotDigest {
			return nil, &ReplayDivergenceError{Step: want.Step, Expected: wantDigest, Actual: gotDigest}
		}
	}
	actual, err := runtime.Trace()
	if err != nil {
		return nil, err
	}
	if actual.Digest != expected.Digest {
		return nil, &ReplayDivergenceError{
			Step: uint64(len(expected.Records)), Expected: expected.Digest, Actual: actual.Digest,
		}
	}
	return runtime, nil
}
