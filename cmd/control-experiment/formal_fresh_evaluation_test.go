package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/SuzumiyaHaruki/consensus-atlas/adapters/etcdraftv2"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/defectbench"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/sutbuild"
)

func checkFormalFreshEvaluation(
	t *testing.T,
	spec controlexperiment.MethodSpec,
	report controlexperiment.Report,
	bundle controlexperiment.ExecutionBundle,
) {
	t.Helper()
	var baseAudit sutbuild.Audit
	auditBytes, err := os.ReadFile("../../benchmarks/pilots/etcdraft-v2-method-evaluation-m5.18a/build-audit/control.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(auditBytes, &baseAudit); err != nil {
		t.Fatal(err)
	}
	if err := baseAudit.Validate(); err != nil {
		t.Fatal(err)
	}
	// This public plumbing fixture rebinds the internally valid archived audit
	// to the in-process official bundle identity; it is not binary provenance
	// evidence and never enters a formal holdout denominator.
	baseAudit.SUTBuildIdentity = bundle.Qualification.Manifest.BuildID

	pairs := make([]defectbench.FormalPair, 0, 3)
	evidence := make(map[string]defectbench.FreshBundleEvidence, 6)
	for pairIndex := 1; pairIndex <= 3; pairIndex++ {
		pair := defectbench.FormalPair{
			PairID:      "private-pair-" + string(rune('a'+pairIndex-1)),
			RootCauseID: "private-root-" + string(rune('a'+pairIndex-1)),
		}
		for candidateIndex, candidate := range []bool{false, true} {
			ordinal := (pairIndex-1)*2 + candidateIndex + 1
			trialID := "opaque-0" + string(rune('0'+ordinal))
			kind := "control"
			if candidate {
				kind = "candidate"
			}
			variantID := "private-" + kind + "-" + string(rune('a'+pairIndex-1))
			audit := baseAudit
			audit.TrialID = trialID
			encodedAudit, err := json.Marshal(audit)
			if err != nil {
				t.Fatal(err)
			}
			auditDigest := formalTestBytesDigest(encodedAudit)
			variant := defectbench.FormalVariant{
				TrialID: trialID, VariantID: variantID,
				ExpectedBuildID:          bundle.Qualification.Manifest.BuildID,
				ExpectedConfigDigest:     bundle.Identity.ConfigDigest,
				ExpectedBuildAuditDigest: auditDigest, ExpectedBinaryDigest: audit.BinaryDigest,
			}
			if candidate {
				pair.Candidate = variant
			} else {
				pair.Control = variant
			}
			evidence[trialID] = defectbench.FreshBundleEvidence{
				Report: report, Bundle: bundle, BuildAudit: audit,
				BuildAuditDigest: auditDigest, BinaryDigest: audit.BinaryDigest,
			}
		}
		pairs = append(pairs, pair)
	}
	contract, err := (defectbench.FormalBenchmarkContract{
		ID: "synthetic-formal-fresh-evaluation", FamilyID: "leader-cft",
		ProfileDigest: bundle.Qualification.Profile.Digest,
		BlindingNonce: formalTestStringDigest("m5.21n-nonce"), MethodSpecDigest: spec.Digest,
		RequiredBundleSchema: spec.RequiredBundleSchema,
		Budget:               defectbench.BundleBudget{MaxDecisions: spec.Decisions, MaxPrimaryWorkUnits: spec.Budget.MaxPrimaryWorkUnits},
		Composition: defectbench.FormalCompositionSpec{
			ProjectorID: etcdraftv2.DecisionProjectionID, MonitorIDs: []string{"agreement"},
		},
		Pairs: pairs,
	}).Seal()
	if err != nil {
		t.Fatal(err)
	}
	view, err := contract.OpaqueView()
	if err != nil {
		t.Fatal(err)
	}
	exposure, err := defectbench.AuditFormalExposure(contract, view, []defectbench.FormalPublicArtifact{
		{Bytes: []byte(`{"trial_id":"opaque-01","method":"frozen"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := defectbench.EvaluateFormalFreshBundles(
		contract, exposure, spec, evidence, etcdraftv2.DecisionProjector{}, oracle.BundleAgreement{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := evaluation.Validate(); err != nil {
		t.Fatal(err)
	}
	tamperedEvaluation := evaluation
	tamperedEvaluation.ExposureAuditDigest = formalTestStringDigest("another-exposure-audit")
	if err := tamperedEvaluation.Validate(); err == nil || !strings.Contains(err.Error(), "DIGEST_MISMATCH") {
		t.Fatalf("tampered exposure binding error = %v", err)
	}
	want := defectbench.FormalEvaluationSummary{Controls: 3, Candidates: 3, RootCauses: 3}
	if evaluation.Summary != want || len(evaluation.Pairs) != 3 || len(evaluation.Results) != 6 {
		t.Fatalf("formal evaluation = %#v", evaluation)
	}

	missing := make(map[string]defectbench.FreshBundleEvidence, len(evidence)-1)
	for trialID, current := range evidence {
		if trialID != "opaque-06" {
			missing[trialID] = current
		}
	}
	if _, err := defectbench.EvaluateFormalFreshBundles(
		contract, exposure, spec, missing, etcdraftv2.DecisionProjector{}, oracle.BundleAgreement{},
	); err == nil || !strings.Contains(err.Error(), "EVIDENCE_SET_MISMATCH") {
		t.Fatalf("missing evidence error = %v", err)
	}
	leakBytes, err := json.Marshal(map[string]string{"target": contract.Pairs[0].RootCauseID})
	if err != nil {
		t.Fatal(err)
	}
	failedExposure, err := defectbench.AuditFormalExposure(
		contract, view, []defectbench.FormalPublicArtifact{{Bytes: leakBytes}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := defectbench.EvaluateFormalFreshBundles(
		contract, failedExposure, spec, evidence, etcdraftv2.DecisionProjector{}, oracle.BundleAgreement{},
	); err == nil || !strings.Contains(err.Error(), "EXPOSURE_AUDIT_REQUIRED") {
		t.Fatalf("failed exposure error = %v", err)
	}

}

func formalTestStringDigest(value string) string { return formalTestBytesDigest([]byte(value)) }

func formalTestBytesDigest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
