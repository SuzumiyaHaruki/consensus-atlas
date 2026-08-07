package fixture_test

import (
	"context"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/fixture"
)

func TestCollectIsIdempotent(t *testing.T) {
	ctx := context.Background()
	adapter := fixture.New()
	if err := adapter.Reset(ctx, []byte("seed")); err != nil {
		t.Fatal(err)
	}
	yield, err := adapter.RunUntilYield(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, err := adapter.Collect(ctx, yield.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := adapter.Collect(ctx, yield.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("Collect digest differs: %s != %s", first.Digest, second.Digest)
	}
	first.Items[0].Temporal.Deadline++
	third, err := adapter.Collect(ctx, yield.ID)
	if err != nil {
		t.Fatal(err)
	}
	if third.Digest != second.Digest || third.Items[0].Temporal.Deadline != second.Items[0].Temporal.Deadline {
		t.Fatal("caller mutation changed frozen emission")
	}
}
