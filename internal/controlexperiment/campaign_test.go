package controlexperiment

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCampaignCheckpointIsAppendOnlyBudgetedAndResumeBound(t *testing.T) {
	budget := CampaignLogicalBudget{
		MaxAttempts: 2, MaxPrimarySchedulerDecisions: 20,
		MaxPrimaryWorkUnits: 24, MaxReplayWorkUnits: 24,
		MaxModelCalls: 2, MaxModelTokens: 200,
	}
	config := campaignTestConfig(t, "campaign", strings.Repeat("a", 64), budget, 1_000)
	initial, err := NewCampaignCheckpoint(config)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Sequence != 0 || initial.StopReason != CampaignStopRunning ||
		initial.Totals != emptyWork() || initial.PreviousDigest != "" {
		t.Fatalf("unexpected initial checkpoint: %#v", initial)
	}

	rejected := campaignTestAttempt(
		t, 1, "rejected-attempt", CampaignAttemptRejected, campaignTestWork(5, 5, 1, 50),
	)
	first, err := initial.AppendAttempt(config, rejected, 100)
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence != 1 || first.PreviousDigest != initial.Digest ||
		first.StopReason != CampaignStopRunning || first.Record == nil || first.Record.Failure == nil ||
		initial.Sequence != 0 || initial.Record != nil {
		t.Fatalf("append mutated history or lost failure: initial=%#v first=%#v", initial, first)
	}
	if err := first.ValidatePrevious(config, initial); err != nil {
		t.Fatal(err)
	}
	wrongOrdinal := campaignTestAttempt(
		t, 1, "wrong-ordinal", CampaignAttemptCompleted, campaignTestWork(1, 1, 0, 0),
	)
	if _, err := first.AppendAttempt(config, wrongOrdinal, 200); err == nil ||
		!strings.Contains(err.Error(), "APPEND_ORDER_INVALID") {
		t.Fatalf("non-monotonic ordinal was accepted: %v", err)
	}
	nextOrdinal := campaignTestAttempt(
		t, 2, "backward-clock", CampaignAttemptCompleted, campaignTestWork(1, 1, 0, 0),
	)
	if _, err := first.AppendAttempt(config, nextOrdinal, 99); err == nil ||
		!strings.Contains(err.Error(), "APPEND_ORDER_INVALID") {
		t.Fatalf("backward elapsed time was accepted: %v", err)
	}

	completed := campaignTestAttempt(
		t, 2, "completed-attempt", CampaignAttemptCompleted, campaignTestWork(5, 5, 1, 50),
	)
	second, err := first.AppendAttempt(config, completed, 200)
	if err != nil {
		t.Fatal(err)
	}
	if second.StopReason != CampaignStopAttemptLimit || second.Sequence != 2 ||
		second.Totals.Primary.SchedulerDecisions != 10 ||
		second.Totals.Primary.WorkUnits != 12 || second.Totals.Replay.WorkUnits != 12 ||
		second.Totals.Model.Calls != 2 || second.Totals.Model.TotalTokens != 100 {
		t.Fatalf("campaign totals or stop reason drifted: %#v", second)
	}
	if err := second.ValidatePrevious(config, first); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCampaignCheckpointChain(config, []CampaignCheckpoint{initial, first, second}); err != nil {
		t.Fatal(err)
	}
	if _, err := second.AppendAttempt(config, completed, 300); err == nil ||
		!strings.Contains(err.Error(), "ALREADY_STOPPED") {
		t.Fatalf("stopped campaign accepted append: %v", err)
	}

	other := campaignTestConfig(t, "campaign", strings.Repeat("b", 64), budget, 1_000)
	if err := second.ValidateInputs(other); err == nil || !strings.Contains(err.Error(), "IDENTITY_MISMATCH") {
		t.Fatalf("resume accepted another target: %v", err)
	}
	tampered := second
	tampered.Record = cloneCampaignRecord(second.Record)
	tampered.Record.ArtifactDigest = strings.Repeat("f", 64)
	if err := tampered.Validate(); err == nil {
		t.Fatal("tampered attempt remained a valid checkpoint")
	}
	broken := second
	broken.PreviousDigest = strings.Repeat("e", 64)
	broken, err = broken.seal()
	if err != nil || broken.Validate() != nil {
		t.Fatalf("self-contained broken-chain witness invalid: %v", err)
	}
	if err := broken.ValidatePrevious(config, first); err == nil ||
		!strings.Contains(err.Error(), "CHAIN_MISMATCH") {
		t.Fatalf("checkpoint chain break was accepted: %v", err)
	}
	brokenTotals := second
	brokenTotals.Totals.Primary.SchedulerDecisions++
	brokenTotals.Totals.Primary.WorkUnits++
	brokenTotals, err = brokenTotals.seal()
	if err != nil || brokenTotals.Validate() != nil {
		t.Fatalf("self-contained cumulative-total witness invalid: %v", err)
	}
	if err := brokenTotals.ValidatePrevious(config, first); err == nil ||
		!strings.Contains(err.Error(), "TOTAL_MISMATCH") {
		t.Fatalf("cumulative total break was accepted: %v", err)
	}
	if err := ValidateCampaignCheckpointChain(config, []CampaignCheckpoint{initial, second}); err == nil {
		t.Fatal("checkpoint chain with a missing link was accepted")
	}
}

func TestCampaignObjectsRoundTripAndRejectMalformedTerminalRecord(t *testing.T) {
	budget := CampaignLogicalBudget{
		MaxAttempts: 2, MaxPrimarySchedulerDecisions: 20,
		MaxPrimaryWorkUnits: 24, MaxReplayWorkUnits: 24,
	}
	config := campaignTestConfig(t, "round-trip", strings.Repeat("a", 64), budget, 1_000)
	initial, err := NewCampaignCheckpoint(config)
	if err != nil {
		t.Fatal(err)
	}
	record := campaignTestAttempt(
		t, 1, "terminal-attempt", CampaignAttemptFailed, campaignTestWork(5, 5, 0, 0),
	)
	checkpoint, err := initial.AppendAttempt(config, record, 10)
	if err != nil {
		t.Fatal(err)
	}

	encoded, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	var decoded CampaignCheckpoint
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Digest != checkpoint.Digest || decoded.Record == nil ||
		decoded.Record.Digest != record.Digest || decoded.ValidatePrevious(config, initial) != nil {
		t.Fatalf("campaign checkpoint did not survive JSON round trip: %#v", decoded)
	}

	malformed := record
	malformed.ArtifactDigest = ""
	if _, err := NewCampaignAttemptRecord(malformed); err == nil {
		t.Fatal("terminal attempt without durable artifact digest was accepted")
	}
	malformed = record
	malformed.Outcome = CampaignAttemptCompleted
	if _, err := NewCampaignAttemptRecord(malformed); err == nil {
		t.Fatal("completed attempt with failure was accepted")
	}
}

func TestCampaignWallClockStopsWithoutChangingLogicalWork(t *testing.T) {
	budget := CampaignLogicalBudget{
		MaxAttempts: 5, MaxPrimarySchedulerDecisions: 100,
		MaxPrimaryWorkUnits: 120, MaxReplayWorkUnits: 120,
	}
	config := campaignTestConfig(t, "wall-clock", strings.Repeat("a", 64), budget, 100)
	initial, err := NewCampaignCheckpoint(config)
	if err != nil {
		t.Fatal(err)
	}
	record := campaignTestAttempt(
		t, 1, "wall-clock-attempt", CampaignAttemptCompleted, campaignTestWork(5, 5, 0, 0),
	)
	stopped, err := initial.AppendAttempt(config, record, 100)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.StopReason != CampaignStopWallClock || stopped.Totals != record.Work ||
		stopped.ElapsedMillis != 100 {
		t.Fatalf("wall clock rewrote logical work: %#v", stopped)
	}

	logicalBudget := budget
	logicalBudget.MaxPrimarySchedulerDecisions = 5
	logical := campaignTestConfig(t, "logical", strings.Repeat("a", 64), logicalBudget, 10_000)
	logicalInitial, err := NewCampaignCheckpoint(logical)
	if err != nil {
		t.Fatal(err)
	}
	logicalStop, err := logicalInitial.AppendAttempt(logical, record, 1)
	if err != nil || logicalStop.StopReason != CampaignStopLogicalBudget {
		t.Fatalf("logical ceiling did not stop campaign: %#v/%v", logicalStop, err)
	}

	over := campaignTestAttempt(
		t, 1, "over-budget", CampaignAttemptCompleted, campaignTestWork(101, 5, 0, 0),
	)
	if _, err := initial.AppendAttempt(config, over, 1); err == nil ||
		!strings.Contains(err.Error(), "BUDGET_EXCEEDED") {
		t.Fatalf("over-budget attempt was appended: %v", err)
	}
}

func campaignTestConfig(
	t *testing.T,
	id string,
	targetDigest string,
	budget CampaignLogicalBudget,
	wallClockMillis int64,
) CampaignConfig {
	t.Helper()
	config, err := NewCampaignConfig(
		id, "fixture-target", targetDigest, strings.Repeat("c", 64), budget, wallClockMillis,
	)
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func campaignTestAttempt(
	t *testing.T,
	ordinal int,
	id string,
	outcome string,
	work WorkLedger,
) CampaignAttemptRecord {
	t.Helper()
	record := CampaignAttemptRecord{
		Ordinal: ordinal, ID: id, InputDigest: strings.Repeat("d", 64),
		ArtifactDigest: strings.Repeat(string(rune('0'+ordinal)), 64),
		Outcome:        outcome, Work: work,
	}
	if outcome != CampaignAttemptCompleted {
		record.Failure = &MethodFailure{Phase: "attempt", Code: "FIXTURE_REJECTED"}
	}
	sealed, err := NewCampaignAttemptRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

func campaignTestWork(primary int, replay int, modelCalls int, modelTokens int) WorkLedger {
	work := emptyWork()
	work.Primary = PhaseWork{
		SetupAttempts: 1, RuntimeInitializations: 1,
		SchedulerDecisions: primary, WorkUnits: primary + 1,
	}
	work.Replay = PhaseWork{
		SetupAttempts: 1, RuntimeInitializations: 1,
		SchedulerDecisions: replay, WorkUnits: replay + 1,
	}
	work.Model = ModelWork{
		Calls: modelCalls, InputTokens: modelTokens, TotalTokens: modelTokens,
	}
	return work
}
