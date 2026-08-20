package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestAgenticPreparationDeadlineStopsTargetPreparation(t *testing.T) {
	started := time.Now()
	_, err := prepareAgenticCompositionWithinDeadline(
		context.Background(), 10,
		func(ctx context.Context) (agenticEpisodeComposition, error) {
			<-ctx.Done()
			return agenticEpisodeComposition{}, ctx.Err()
		},
	)
	if err == nil || !strings.Contains(err.Error(), "PREPARATION_DEADLINE_EXCEEDED") {
		t.Fatalf("preparation deadline error = %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("bounded preparation did not stop promptly")
	}
}
