package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

func TestEtcdraftM518b0GuardedIntentCompilesAndUsesQualifiedExecutor(t *testing.T) {
	inputs, err := newEtcdraftIntentInputs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	intent, err := controlexperiment.NewGuardedTestIntent(controlexperiment.GuardedTestIntent{
		ID: "etcdraft-one-shot-intent-m5-18b0", ViewDigest: inputs.View.Digest,
		RiskID: etcdraftIntentRiskLeaderChange,
		Must: controlexperiment.IntentMust{
			Decisions: 96,
			FaultEnvelope: controlexperiment.FaultEnvelope{
				MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
				MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
			},
		},
		Prefer: controlexperiment.IntentPrefer{
			BackendIDs: []string{"dpor"},
			Actions:    []control.ActionKind{control.ActionPartition},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := controlexperiment.CompileGuardedTestIntent(
		inputs.View, inputs.Knowledge, inputs.Catalog, inputs.Qualification.Manifest,
		inputs.Qualification.Qualification, intent,
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.BackendID != etcdraftBackendActionClass ||
		plan.Strategy != "workload-action-class-random" || !plan.FallbackUsed ||
		len(plan.PreferenceMisses) != 1 || plan.PreferenceMisses[0].Value != "dpor" ||
		plan.CompilerWork.CandidatesEvaluated != 2 || plan.CompilerWork.PreferenceChecks != 2 ||
		plan.CompilerWork.WorkUnits != 4 {
		t.Fatalf("unexpected compiled plan: %#v", plan)
	}

	report, bundle, err := executeEtcdraftCompiledIntent(
		context.Background(), inputs, intent, plan,
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Config.Runs[0].Policy.Version != controlexperiment.ActionClassPolicyVersion ||
		report.Work.Primary.WorkUnits != 98 || report.Work.Replay.WorkUnits != 98 ||
		len(report.Runs) != 1 || !report.Runs[0].Replay.Stable || bundle.Digest == "" {
		t.Fatalf("unexpected guarded execution: report=%#v bundle=%s", report, bundle.Digest)
	}
	wantIdentities := map[string]string{
		"knowledge": "1921bdc375f9618436d7ae31d426ba2087dc715443ec0f822628ce0e51199980",
		"catalog":   "76ab1542d5d37fcde02b0913f7080197db8c2fa16aa05c513142a05d659f89dd",
		"view":      "63ccd9fdaf71b6e64942e4611b4b5a49238f6a4eab9fed60982ef07ed8f8b28d",
		"intent":    "e18af54b7f65412cab4b8a44abf46ca1947840b0586d6901cc207e9f2f2745b1",
		"plan":      "d98fc488f0b9d9ae7de56c47355ac3a38b8ce520b17adf035f879e32c055166d",
		"report":    "fc0cb500876b1d3d75dc7d8f83dd672513d1c010c523069f12e3be2f6472260a",
		"bundle":    "b766e3f13e8be015b950032df449762b2ee90e3c80d87e87383c8ff80ac6c9b1",
	}
	gotIdentities := map[string]string{
		"knowledge": inputs.Knowledge.Digest, "catalog": inputs.Catalog.Digest,
		"view": inputs.View.Digest, "intent": intent.Digest, "plan": plan.Digest,
		"report": report.Digest, "bundle": bundle.Digest,
	}
	for name, want := range wantIdentities {
		if gotIdentities[name] != want {
			t.Fatalf("%s identity = %s, want %s", name, gotIdentities[name], want)
		}
	}

	encodedView, err := json.Marshal(inputs.View)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"build_id", "candidate", "root_cause", "oracle"} {
		if bytes.Contains(bytes.ToLower(encodedView), []byte(forbidden)) {
			t.Fatalf("Agent view contains forbidden field/token %q", forbidden)
		}
	}
}

func TestEtcdraftM518b0ViewIsBuildBlindAndHardConstraintsDoNotFallback(t *testing.T) {
	inputs, err := newEtcdraftIntentInputs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variantManifest := inputs.Qualification.Manifest
	variantManifest.BuildID = "opaque-alternate-build"
	manifestDigest, err := variantManifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	variantQualification := inputs.Qualification.Qualification
	variantQualification.BuildID = variantManifest.BuildID
	variantQualification.ManifestDigest = manifestDigest
	variantQualification, err = variantQualification.Seal()
	if err != nil {
		t.Fatal(err)
	}
	variantView, err := controlexperiment.NewAgentSemanticView(
		inputs.View.ID, inputs.Knowledge, inputs.Catalog, variantManifest, variantQualification,
	)
	if err != nil {
		t.Fatal(err)
	}
	if variantView.Digest != inputs.View.Digest {
		t.Fatalf("build identity changed semantic view: %s != %s", variantView.Digest, inputs.View.Digest)
	}

	intent, err := controlexperiment.NewGuardedTestIntent(controlexperiment.GuardedTestIntent{
		ID: "unsupported-hard-action", ViewDigest: inputs.View.Digest,
		RiskID: etcdraftIntentRiskLeaderChange,
		Must: controlexperiment.IntentMust{
			Decisions: 96, RequiredActions: []control.ActionKind{control.ActionFailEffect},
			FaultEnvelope: controlexperiment.FaultEnvelope{
				MaxCrashes: 1, MaxConcurrentCrashes: 1, MaxMessageDrops: 2,
				MaxMessageDuplicates: 1, MaxPartitions: 1, MaxActivePartitions: 1,
			},
		},
		Prefer: controlexperiment.IntentPrefer{BackendIDs: []string{etcdraftBackendUniform}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controlexperiment.CompileGuardedTestIntent(
		inputs.View, inputs.Knowledge, inputs.Catalog, inputs.Qualification.Manifest,
		inputs.Qualification.Qualification, intent,
	); err == nil || !strings.Contains(err.Error(), "HARD_CONSTRAINT_UNVALIDATED") {
		t.Fatalf("unsupported hard action error = %v", err)
	}
}

func TestEtcdraftGuardedIntentRejectsFaultEnvelopeContradictingHardActions(t *testing.T) {
	inputs, err := newEtcdraftIntentInputs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	intent, err := controlexperiment.NewGuardedTestIntent(controlexperiment.GuardedTestIntent{
		ID: "zero-fault-envelope", ViewDigest: inputs.View.Digest,
		RiskID: etcdraftIntentRiskLeaderChange,
		Must: controlexperiment.IntentMust{
			Decisions: 96, FaultEnvelope: controlexperiment.FaultEnvelope{},
		},
		Prefer: controlexperiment.IntentPrefer{BackendIDs: []string{etcdraftBackendUniform}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controlexperiment.CompileGuardedTestIntent(
		inputs.View, inputs.Knowledge, inputs.Catalog, inputs.Qualification.Manifest,
		inputs.Qualification.Qualification, intent,
	); err == nil || !strings.Contains(err.Error(), "HARD_CONSTRAINT_UNSATISFIED") {
		t.Fatalf("contradictory hard fault envelope error = %v", err)
	}
}

func TestM518b0AgentProposalParserRejectsAuthorityExpansion(t *testing.T) {
	viewDigest := strings.Repeat("a", 64)
	proposal := `{
  "schema_version":"consensus-atlas/guarded-test-intent/v1",
  "id":"strict-json",
  "view_digest":"` + viewDigest + `",
  "risk_id":"risk",
  "must":{"decisions":1,"fault_envelope":{"max_crashes":0,"max_concurrent_crashes":0,"max_message_drops":0,"max_message_duplicates":0,"max_partitions":0,"max_active_partitions":0}},
  "prefer":{"backend_ids":["admissible-uniform"]}
}`
	parsed, err := controlexperiment.ParseGuardedTestIntentProposal([]byte(proposal))
	if err != nil {
		t.Fatal(err)
	}
	if err := parsed.Validate(); err != nil {
		t.Fatal(err)
	}
	withAuthority := strings.Replace(proposal, `"prefer":{`, `"oracle":"pass","prefer":{`, 1)
	if _, err := controlexperiment.ParseGuardedTestIntentProposal([]byte(withAuthority)); err == nil ||
		!strings.Contains(err.Error(), "JSON_INVALID") {
		t.Fatalf("unknown authority field error = %v", err)
	}
	withDigest := strings.Replace(proposal, `"prefer":{`, `"digest":"`+viewDigest+`","prefer":{`, 1)
	if _, err := controlexperiment.ParseGuardedTestIntentProposal([]byte(withDigest)); err == nil ||
		!strings.Contains(err.Error(), "PROPOSAL_IDENTITY_INVALID") {
		t.Fatalf("proposer-supplied digest error = %v", err)
	}
	if _, err := controlexperiment.ParseGuardedTestIntentProposal([]byte(proposal + `{}`)); err == nil ||
		!strings.Contains(err.Error(), "JSON_TRAILING") {
		t.Fatalf("trailing JSON error = %v", err)
	}
}
