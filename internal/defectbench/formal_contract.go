package defectbench

import (
	"errors"
	"sort"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/semantic"
)

const (
	FormalBenchmarkContractSchemaVersion = "consensus-atlas/formal-benchmark-contract/v1"
	FormalOpaqueViewSchemaVersion        = "consensus-atlas/formal-benchmark-opaque-view/v1"
)

// FormalCompositionSpec binds private evaluator semantics by identity without
// importing a target implementation into the generic benchmark contract.
type FormalCompositionSpec struct {
	ProjectorID string   `json:"projector_id"`
	MonitorIDs  []string `json:"monitor_ids"`
}

// FormalVariant is private curator input. TrialID is the only field from this
// structure that crosses into the Agent-facing view.
type FormalVariant struct {
	TrialID                  string `json:"trial_id"`
	VariantID                string `json:"variant_id"`
	ExpectedBuildID          string `json:"expected_build_id"`
	ExpectedConfigDigest     string `json:"expected_config_digest,omitempty"`
	ExpectedBuildAuditDigest string `json:"expected_build_audit_digest"`
	ExpectedBinaryDigest     string `json:"expected_binary_digest"`
}

// FormalPair makes candidate/control matching explicit. Pair order has no
// semantic meaning; Seal canonicalizes it by private PairID.
type FormalPair struct {
	PairID      string        `json:"pair_id"`
	RootCauseID string        `json:"root_cause_id"`
	Control     FormalVariant `json:"control"`
	Candidate   FormalVariant `json:"candidate"`
}

// FormalBenchmarkContract is private evaluator input. Version 1 intentionally
// requires at least three matching pairs and three distinct root-cause labels;
// label independence still requires curator review outside this mechanism.
type FormalBenchmarkContract struct {
	SchemaVersion        string                                  `json:"schema_version"`
	ID                   string                                  `json:"id"`
	FamilyID             string                                  `json:"family_id"`
	ProfileDigest        string                                  `json:"profile_digest"`
	BlindingNonce        string                                  `json:"blinding_nonce"`
	MethodSpecDigest     string                                  `json:"method_spec_digest"`
	RequiredBundleSchema string                                  `json:"required_bundle_schema"`
	Budget               BundleBudget                            `json:"budget"`
	AgenticBudget        *controlexperiment.AgenticLogicalBudget `json:"agentic_budget,omitempty"`
	Composition          FormalCompositionSpec                   `json:"composition"`
	Pairs                []FormalPair                            `json:"pairs"`
	Digest               string                                  `json:"digest"`
}

type FormalOpaqueTrial struct {
	TrialID string `json:"trial_id"`
}

// FormalOpaqueView is the complete Agent-facing benchmark projection. It does
// not expose pair membership, candidate/control kind, root cause, build, or
// trusted projector/monitor identity.
type FormalOpaqueView struct {
	SchemaVersion        string                                  `json:"schema_version"`
	BenchmarkID          string                                  `json:"benchmark_id"`
	BenchmarkCommitment  string                                  `json:"benchmark_commitment"`
	FamilyID             string                                  `json:"family_id"`
	ProfileDigest        string                                  `json:"profile_digest"`
	MethodSpecDigest     string                                  `json:"method_spec_digest"`
	RequiredBundleSchema string                                  `json:"required_bundle_schema"`
	Budget               BundleBudget                            `json:"budget"`
	AgenticBudget        *controlexperiment.AgenticLogicalBudget `json:"agentic_budget,omitempty"`
	Trials               []FormalOpaqueTrial                     `json:"trials"`
	Digest               string                                  `json:"digest"`
}

func (contract FormalBenchmarkContract) Seal() (FormalBenchmarkContract, error) {
	contract.SchemaVersion = FormalBenchmarkContractSchemaVersion
	if contract.AgenticBudget != nil {
		copy := *contract.AgenticBudget
		contract.AgenticBudget = &copy
	}
	contract.Composition.MonitorIDs = append([]string(nil), contract.Composition.MonitorIDs...)
	sort.Strings(contract.Composition.MonitorIDs)
	contract.Pairs = append([]FormalPair(nil), contract.Pairs...)
	sort.Slice(contract.Pairs, func(i, j int) bool { return contract.Pairs[i].PairID < contract.Pairs[j].PairID })
	contract.Digest = ""
	if err := contract.validateContent(); err != nil {
		return FormalBenchmarkContract{}, err
	}
	digest, err := control.CanonicalDigest(contract)
	if err != nil {
		return FormalBenchmarkContract{}, err
	}
	contract.Digest = digest
	return contract, nil
}

func (contract FormalBenchmarkContract) Validate() error {
	if contract.SchemaVersion != FormalBenchmarkContractSchemaVersion {
		return errors.New("FORMAL_BENCHMARK_SCHEMA_MISMATCH")
	}
	sealed, err := contract.Seal()
	if err != nil {
		return err
	}
	stored := contract
	stored.Digest = ""
	storedDigest, err := control.CanonicalDigest(stored)
	if err != nil {
		return err
	}
	if sealed.Digest != contract.Digest || storedDigest != contract.Digest {
		return errors.New("FORMAL_BENCHMARK_DIGEST_MISMATCH")
	}
	return nil
}

func (contract FormalBenchmarkContract) validateContent() error {
	if !validBundleID(contract.ID) || !validBundleID(contract.FamilyID) ||
		!bundleDigestValid(contract.ProfileDigest) || !bundleDigestValid(contract.BlindingNonce) ||
		!bundleDigestValid(contract.MethodSpecDigest) || contract.RequiredBundleSchema == "" {
		return errors.New("FORMAL_BENCHMARK_IDENTITY_INVALID")
	}
	if contract.Budget.MaxDecisions <= 0 || contract.Budget.MaxPrimaryWorkUnits <= 0 {
		return errors.New("FORMAL_BENCHMARK_BUDGET_INVALID")
	}
	if contract.AgenticBudget != nil && (contract.AgenticBudget.Validate() != nil ||
		contract.AgenticBudget.MaxPrimarySchedulerDecisions != contract.Budget.MaxDecisions ||
		contract.AgenticBudget.MaxPrimaryWorkUnits != contract.Budget.MaxPrimaryWorkUnits) {
		return errors.New("FORMAL_BENCHMARK_AGENTIC_BUDGET_INVALID")
	}
	if strings.TrimSpace(contract.Composition.ProjectorID) == "" ||
		contract.Composition.ProjectorID != strings.TrimSpace(contract.Composition.ProjectorID) ||
		len(contract.Composition.MonitorIDs) == 0 {
		return errors.New("FORMAL_BENCHMARK_COMPOSITION_INVALID")
	}
	monitors := map[string]bool{}
	for _, monitor := range contract.Composition.MonitorIDs {
		if monitor == "" || monitor == "trace-integrity" || monitor != strings.TrimSpace(monitor) || monitors[monitor] {
			return errors.New("FORMAL_BENCHMARK_MONITOR_INVALID")
		}
		monitors[monitor] = true
	}
	if len(contract.Pairs) < 3 {
		return errors.New("FORMAL_BENCHMARK_MINIMUM_PAIRS_REQUIRED")
	}

	privateAtoms := map[string]bool{
		contract.BlindingNonce: true, contract.Composition.ProjectorID: true,
	}
	for monitor := range monitors {
		privateAtoms[monitor] = true
	}
	pairIDs, variantIDs, roots, trials := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	privateIDs := map[string]bool{}
	for _, pair := range contract.Pairs {
		if !validBundleID(pair.PairID) || pairIDs[pair.PairID] || privateIDs[pair.PairID] ||
			!validBundleID(pair.RootCauseID) || (!roots[pair.RootCauseID] && privateIDs[pair.RootCauseID]) {
			return errors.New("FORMAL_BENCHMARK_PAIR_IDENTITY_INVALID")
		}
		pairIDs[pair.PairID], roots[pair.RootCauseID] = true, true
		privateIDs[pair.PairID], privateIDs[pair.RootCauseID] = true, true
		privateAtoms[pair.PairID], privateAtoms[pair.RootCauseID] = true, true
		for _, variant := range []FormalVariant{pair.Control, pair.Candidate} {
			if err := validateFormalVariant(variant); err != nil {
				return err
			}
			if variantIDs[variant.VariantID] || privateIDs[variant.VariantID] || trials[variant.TrialID] {
				return errors.New("FORMAL_BENCHMARK_VARIANT_DUPLICATE")
			}
			variantIDs[variant.VariantID], trials[variant.TrialID] = true, true
			privateIDs[variant.VariantID] = true
			for _, atom := range formalVariantPrivateAtoms(variant) {
				privateAtoms[atom] = true
			}
		}
		if pair.Control.TrialID == pair.Candidate.TrialID || pair.Control.VariantID == pair.Candidate.VariantID {
			return errors.New("FORMAL_BENCHMARK_PAIR_VARIANTS_IDENTICAL")
		}
	}
	if len(roots) < 3 {
		return errors.New("FORMAL_BENCHMARK_MINIMUM_ROOT_CAUSES_REQUIRED")
	}
	for trial := range trials {
		if privateAtoms[trial] {
			return errors.New("FORMAL_BENCHMARK_OPAQUE_TRIAL_COLLISION")
		}
	}
	return nil
}

func validateFormalVariant(variant FormalVariant) error {
	if !validBundleID(variant.TrialID) || !validBundleID(variant.VariantID) ||
		strings.TrimSpace(variant.ExpectedBuildID) == "" ||
		!bundleDigestValid(variant.ExpectedBuildAuditDigest) ||
		!bundleDigestValid(variant.ExpectedBinaryDigest) ||
		(variant.ExpectedConfigDigest != "" && !bundleDigestValid(variant.ExpectedConfigDigest)) {
		return errors.New("FORMAL_BENCHMARK_VARIANT_INVALID")
	}
	return nil
}

func formalVariantPrivateAtoms(variant FormalVariant) []string {
	values := []string{
		variant.VariantID, variant.ExpectedBuildID, variant.ExpectedConfigDigest,
		variant.ExpectedBuildAuditDigest, variant.ExpectedBinaryDigest,
	}
	result := values[:0]
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func (contract FormalBenchmarkContract) OpaqueView() (FormalOpaqueView, error) {
	if err := contract.Validate(); err != nil {
		return FormalOpaqueView{}, err
	}
	view := FormalOpaqueView{
		SchemaVersion: FormalOpaqueViewSchemaVersion, BenchmarkID: contract.ID,
		BenchmarkCommitment: contract.Digest, FamilyID: contract.FamilyID,
		ProfileDigest: contract.ProfileDigest, MethodSpecDigest: contract.MethodSpecDigest,
		RequiredBundleSchema: contract.RequiredBundleSchema, Budget: contract.Budget,
	}
	if contract.AgenticBudget != nil {
		copy := *contract.AgenticBudget
		view.AgenticBudget = &copy
	}
	for _, pair := range contract.Pairs {
		view.Trials = append(view.Trials,
			FormalOpaqueTrial{TrialID: pair.Control.TrialID},
			FormalOpaqueTrial{TrialID: pair.Candidate.TrialID},
		)
	}
	return view.Seal()
}

func (view FormalOpaqueView) Seal() (FormalOpaqueView, error) {
	view.SchemaVersion = FormalOpaqueViewSchemaVersion
	if view.AgenticBudget != nil {
		copy := *view.AgenticBudget
		view.AgenticBudget = &copy
	}
	view.Trials = append([]FormalOpaqueTrial(nil), view.Trials...)
	sort.Slice(view.Trials, func(i, j int) bool { return view.Trials[i].TrialID < view.Trials[j].TrialID })
	view.Digest = ""
	if err := view.validateContent(); err != nil {
		return FormalOpaqueView{}, err
	}
	digest, err := control.CanonicalDigest(view)
	if err != nil {
		return FormalOpaqueView{}, err
	}
	view.Digest = digest
	return view, nil
}

func (view FormalOpaqueView) Validate() error {
	if view.SchemaVersion != FormalOpaqueViewSchemaVersion {
		return errors.New("FORMAL_OPAQUE_VIEW_SCHEMA_MISMATCH")
	}
	sealed, err := view.Seal()
	if err != nil {
		return err
	}
	stored := view
	stored.Digest = ""
	storedDigest, err := control.CanonicalDigest(stored)
	if err != nil {
		return err
	}
	if sealed.Digest != view.Digest || storedDigest != view.Digest {
		return errors.New("FORMAL_OPAQUE_VIEW_DIGEST_MISMATCH")
	}
	return nil
}

func (view FormalOpaqueView) validateContent() error {
	if !validBundleID(view.BenchmarkID) || !validBundleID(view.FamilyID) ||
		!bundleDigestValid(view.BenchmarkCommitment) || !bundleDigestValid(view.ProfileDigest) ||
		!bundleDigestValid(view.MethodSpecDigest) || view.RequiredBundleSchema == "" ||
		view.Budget.MaxDecisions <= 0 || view.Budget.MaxPrimaryWorkUnits <= 0 || len(view.Trials) < 6 {
		return errors.New("FORMAL_OPAQUE_VIEW_INVALID")
	}
	if view.AgenticBudget != nil && (view.AgenticBudget.Validate() != nil ||
		view.AgenticBudget.MaxPrimarySchedulerDecisions != view.Budget.MaxDecisions ||
		view.AgenticBudget.MaxPrimaryWorkUnits != view.Budget.MaxPrimaryWorkUnits) {
		return errors.New("FORMAL_OPAQUE_AGENTIC_BUDGET_INVALID")
	}
	seen := map[string]bool{}
	for _, trial := range view.Trials {
		if !validBundleID(trial.TrialID) || seen[trial.TrialID] {
			return errors.New("FORMAL_OPAQUE_TRIAL_INVALID")
		}
		seen[trial.TrialID] = true
	}
	return nil
}

func (contract FormalBenchmarkContract) ValidateOpaqueView(view FormalOpaqueView) error {
	expected, err := contract.OpaqueView()
	if err != nil {
		return err
	}
	if err := view.Validate(); err != nil {
		return err
	}
	if expected.Digest != view.Digest {
		return errors.New("FORMAL_OPAQUE_VIEW_PROJECTION_MISMATCH")
	}
	return nil
}

// ResolveFormalComposition selects the exact trusted implementations named by
// the private contract. Extra registered monitors are ignored; missing or
// duplicate identities fail before any SUT execution.
func ResolveFormalComposition(
	contract FormalBenchmarkContract,
	projector semantic.DecisionProjector,
	monitors ...oracle.BundleMonitor,
) ([]oracle.BundleMonitor, error) {
	if err := contract.Validate(); err != nil {
		return nil, err
	}
	if projector == nil || projector.ID() != contract.Composition.ProjectorID {
		return nil, errors.New("FORMAL_COMPOSITION_PROJECTOR_MISMATCH")
	}
	registered := make(map[string]oracle.BundleMonitor, len(monitors))
	for _, monitor := range monitors {
		if monitor == nil || monitor.Name() == "" || monitor.Name() == "trace-integrity" || registered[monitor.Name()] != nil {
			return nil, errors.New("FORMAL_COMPOSITION_MONITOR_REGISTRY_INVALID")
		}
		registered[monitor.Name()] = monitor
	}
	selected := make([]oracle.BundleMonitor, 0, len(contract.Composition.MonitorIDs))
	for _, id := range contract.Composition.MonitorIDs {
		monitor := registered[id]
		if monitor == nil {
			return nil, errors.New("FORMAL_COMPOSITION_MONITOR_MISSING")
		}
		selected = append(selected, monitor)
	}
	return selected, nil
}
