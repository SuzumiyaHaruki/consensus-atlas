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
		prepared, err := runtime.PrepareRecordedAction(ctx, want.Action)
		if err != nil {
			return fail(err)
		}
		if prepared {
			progress.PrepareActions++
			if want.Action.Kind == control.ActionInvoke {
				progress.WorkloadOffers++
			}
		}
		progress.Decisions++
		got, err := runtime.Select(ctx, want.Action.ID)
		if err != nil {
			return fail(err)
		}
		if err := ValidateRecordedActionRecord(want, got); err != nil {
			return fail(&ReplayDivergenceError{
				Step: want.Step, Expected: string(want.Action.ID), Actual: string(got.Action.ID),
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

// PrepareRecordedAction reconstructs an author-supplied action through the
// same public validation path used during ordinary execution. A recorded
// schedule is evidence, not permission to inject directly into Runtime-owned
// state. Runtime-owned actions need no preparation and return false.
func (runtime *Runtime) PrepareRecordedAction(
	ctx context.Context,
	expected control.Action,
) (bool, error) {
	if expected.Kind != control.ActionInvoke && expected.Kind != control.ActionPartition {
		return false, nil
	}
	var (
		id  control.ActionID
		err error
	)
	switch expected.Kind {
	case control.ActionInvoke:
		var parameters control.AdapterInvokeParameters
		if err := json.Unmarshal(expected.Parameters, &parameters); err != nil {
			return false, fmt.Errorf("REPLAY_PREPARATION_PARAMETERS_INVALID: %w", err)
		}
		id, err = runtime.OfferInvoke(ctx, expected.Node.Node, parameters.Input)
	case control.ActionPartition:
		var parameters control.PartitionParameters
		parameters, err = control.DecodePartitionParameters(expected.Parameters)
		if err == nil {
			id, err = runtime.OfferPartition(parameters.Left, parameters.Right)
		}
	default:
		return false, fmt.Errorf("REPLAY_PREPARATION_KIND_UNSUPPORTED: %s", expected.Kind)
	}
	if err != nil {
		return false, fmt.Errorf("REPLAY_PREPARATION_REJECTED: %s: %w", expected.Kind, err)
	}
	actual := runtime.offered[id]
	wantDigest, digestErr := control.CanonicalDigest(expected)
	if digestErr != nil {
		return false, digestErr
	}
	actualDigest, digestErr := control.CanonicalDigest(actual)
	if digestErr != nil {
		return false, digestErr
	}
	if id != expected.ID || actualDigest != wantDigest {
		delete(runtime.offered, id)
		return false, fmt.Errorf("REPLAY_PREPARATION_MISMATCH: %s", expected.ID)
	}
	return true, nil
}

// ValidateRecordedActionRecord is shared by primary recorded-schedule
// execution and fresh Replay. It compares the complete canonical record, not
// only ActionID, so parameter, evidence and state-digest drift are rejected.
func ValidateRecordedActionRecord(expected ActionRecord, actual ActionRecord) error {
	wantDigest, err := control.CanonicalDigest(expected)
	if err != nil {
		return err
	}
	gotDigest, err := control.CanonicalDigest(actual)
	if err != nil {
		return err
	}
	if wantDigest != gotDigest {
		return fmt.Errorf("RECORDED_ACTION_RECORD_MISMATCH: expected=%s actual=%s", wantDigest, gotDigest)
	}
	return nil
}
