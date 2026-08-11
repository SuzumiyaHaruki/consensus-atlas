package controlexperiment

import (
	"errors"
	"slices"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const CrossTargetPlannerViewVersion = "consensus-atlas/cross-target-planner-view/v1"

// CrossTargetPlannerView is an Agent-facing intersection of already validated
// target views. SourceViewDigests are opaque: target build, Adapter, protocol,
// Oracle, PSS and candidate identities do not cross this boundary.
type CrossTargetPlannerView struct {
	SchemaVersion         string                `json:"schema_version"`
	ID                    string                `json:"id"`
	KnowledgePack         ProtocolKnowledgePack `json:"knowledge_pack"`
	CatalogDigest         string                `json:"catalog_digest"`
	SourceViewDigests     []string              `json:"source_view_digests"`
	ValidatedCapabilities []string              `json:"validated_capabilities"`
	AvailableActions      []control.ActionKind  `json:"available_actions"`
	EligibleBackends      []IntentBackendView   `json:"eligible_backends"`
	Digest                string                `json:"digest"`
}

func NewCrossTargetPlannerView(
	id string,
	sources []AgentSemanticView,
) (CrossTargetPlannerView, error) {
	if !validMethodToken(id) || len(sources) < 2 {
		return CrossTargetPlannerView{}, errors.New("EXPERIMENT_CROSS_TARGET_VIEW_INPUT_INVALID")
	}
	for _, source := range sources {
		if err := source.Validate(); err != nil {
			return CrossTargetPlannerView{}, err
		}
	}
	first := sources[0]
	view := CrossTargetPlannerView{
		SchemaVersion: CrossTargetPlannerViewVersion, ID: id,
		KnowledgePack: cloneKnowledgePack(first.KnowledgePack), CatalogDigest: first.CatalogDigest,
		ValidatedCapabilities: append([]string(nil), first.ValidatedCapabilities...),
		AvailableActions:      append([]control.ActionKind(nil), first.AvailableActions...),
		EligibleBackends:      cloneBackendViews(first.EligibleBackends),
	}
	seenSources := make(map[string]bool, len(sources))
	for index, source := range sources {
		if seenSources[source.Digest] || source.KnowledgePack.Digest != first.KnowledgePack.Digest ||
			source.CatalogDigest != first.CatalogDigest {
			return CrossTargetPlannerView{}, errors.New("EXPERIMENT_CROSS_TARGET_VIEW_SOURCE_MISMATCH")
		}
		seenSources[source.Digest] = true
		view.SourceViewDigests = append(view.SourceViewDigests, source.Digest)
		if index == 0 {
			continue
		}
		view.ValidatedCapabilities = intersectStrings(
			view.ValidatedCapabilities, source.ValidatedCapabilities,
		)
		view.AvailableActions = intersectActions(view.AvailableActions, source.AvailableActions)
		view.EligibleBackends = intersectBackends(view.EligibleBackends, source.EligibleBackends)
	}
	if len(view.ValidatedCapabilities) == 0 || len(view.AvailableActions) == 0 ||
		len(view.EligibleBackends) == 0 {
		return CrossTargetPlannerView{}, errors.New("EXPERIMENT_CROSS_TARGET_VIEW_INTERSECTION_EMPTY")
	}
	backendActions := make(map[control.ActionKind]bool)
	for _, backend := range view.EligibleBackends {
		for _, action := range backend.SupportedActions {
			backendActions[action] = true
		}
	}
	selectable := make([]control.ActionKind, 0, len(view.AvailableActions))
	for _, action := range view.AvailableActions {
		if backendActions[action] {
			selectable = append(selectable, action)
		}
	}
	view.AvailableActions = selectable
	if len(view.AvailableActions) == 0 {
		return CrossTargetPlannerView{}, errors.New("EXPERIMENT_CROSS_TARGET_VIEW_INTERSECTION_EMPTY")
	}
	commonActions := actionKindSet(view.AvailableActions)
	for _, backend := range view.EligibleBackends {
		if !actionSubset(backend.SupportedActions, commonActions) {
			return CrossTargetPlannerView{}, errors.New("EXPERIMENT_CROSS_TARGET_VIEW_BACKEND_OUTSIDE_INTERSECTION")
		}
	}
	sealed, err := view.seal()
	if err != nil {
		return CrossTargetPlannerView{}, err
	}
	if err := sealed.Validate(); err != nil {
		return CrossTargetPlannerView{}, err
	}
	return sealed, nil
}

func (view CrossTargetPlannerView) Validate() error {
	if view.SchemaVersion != CrossTargetPlannerViewVersion || !validMethodToken(view.ID) ||
		!validSHA256(view.CatalogDigest) || len(view.SourceViewDigests) < 2 ||
		!canonicalStrings(view.SourceViewDigests, true) ||
		!canonicalStrings(view.ValidatedCapabilities, true) ||
		!canonicalActionKinds(view.AvailableActions, true) || len(view.EligibleBackends) == 0 {
		return errors.New("EXPERIMENT_CROSS_TARGET_VIEW_INVALID")
	}
	if err := view.KnowledgePack.Validate(); err != nil {
		return err
	}
	seenBackends := make(map[string]bool, len(view.EligibleBackends))
	commonActions := actionKindSet(view.AvailableActions)
	for _, backend := range view.EligibleBackends {
		if !validMethodToken(backend.ID) || seenBackends[backend.ID] || backend.MinDecisions <= 0 ||
			backend.MaxDecisions < backend.MinDecisions || backend.MaxFaultEnvelope.Validate() != nil ||
			!canonicalActionKinds(backend.SupportedActions, true) ||
			!actionSubset(backend.SupportedActions, commonActions) {
			return errors.New("EXPERIMENT_CROSS_TARGET_VIEW_BACKEND_INVALID")
		}
		seenBackends[backend.ID] = true
	}
	sealed, err := view.seal()
	if err != nil || !validSHA256(view.Digest) || sealed.Digest != view.Digest {
		return errors.New("EXPERIMENT_CROSS_TARGET_VIEW_DIGEST_MISMATCH")
	}
	return nil
}

func (view CrossTargetPlannerView) ValidateInputs(sources []AgentSemanticView) error {
	if err := view.Validate(); err != nil {
		return err
	}
	want, err := NewCrossTargetPlannerView(view.ID, sources)
	if err != nil {
		return err
	}
	if want.Digest != view.Digest {
		return errors.New("EXPERIMENT_CROSS_TARGET_VIEW_INPUT_MISMATCH")
	}
	return nil
}

// ProjectCrossTargetIntent binds one common intent to one source target. It
// only changes the view-bound identity; hard requirements, preferences,
// budget, fault envelope and risk remain byte-for-byte equivalent fields.
func ProjectCrossTargetIntent(
	view CrossTargetPlannerView,
	sources []AgentSemanticView,
	intent GuardedTestIntent,
	target AgentSemanticView,
) (GuardedTestIntent, error) {
	if err := view.ValidateInputs(sources); err != nil {
		return GuardedTestIntent{}, err
	}
	if err := intent.Validate(); err != nil {
		return GuardedTestIntent{}, err
	}
	if err := target.Validate(); err != nil {
		return GuardedTestIntent{}, err
	}
	if intent.ViewDigest != view.Digest || target.KnowledgePack.Digest != view.KnowledgePack.Digest ||
		target.CatalogDigest != view.CatalogDigest || !containsString(view.SourceViewDigests, target.Digest) {
		return GuardedTestIntent{}, errors.New("EXPERIMENT_CROSS_TARGET_INTENT_BINDING_MISMATCH")
	}
	if err := validateCrossTargetIntent(view, intent); err != nil {
		return GuardedTestIntent{}, err
	}
	projected := intent
	projected.ID = intent.ID + "-target-" + target.Digest[:12]
	projected.ViewDigest = target.Digest
	projected.Digest = ""
	return NewGuardedTestIntent(projected)
}

// NewCrossTargetPlannerProposalContract exposes the same preference-only wire
// shape as a single-target Campaign without revealing any source target.
func NewCrossTargetPlannerProposalContract(
	view CrossTargetPlannerView,
	baseline GuardedTestIntent,
) (CampaignPlannerProposalContract, error) {
	if err := view.Validate(); err != nil {
		return CampaignPlannerProposalContract{}, err
	}
	if err := baseline.Validate(); err != nil {
		return CampaignPlannerProposalContract{}, err
	}
	if baseline.ViewDigest != view.Digest {
		return CampaignPlannerProposalContract{}, errors.New("EXPERIMENT_CROSS_TARGET_PROPOSAL_BASELINE_MISMATCH")
	}
	risk, ok := findProtocolRisk(view.KnowledgePack.Risks, baseline.RiskID)
	if !ok {
		return CampaignPlannerProposalContract{}, errors.New("EXPERIMENT_CROSS_TARGET_INTENT_RISK_UNKNOWN")
	}
	allowedRiskBackends := stringSet(risk.AllowedBackendIDs)
	backends := make([]string, 0, len(view.EligibleBackends))
	actions := make(map[control.ActionKind]bool)
	for _, backend := range view.EligibleBackends {
		if !allowedRiskBackends[backend.ID] || baseline.Must.Decisions < backend.MinDecisions ||
			baseline.Must.Decisions > backend.MaxDecisions ||
			!faultEnvelopeWithin(baseline.Must.FaultEnvelope, backend.MaxFaultEnvelope) {
			continue
		}
		backends = append(backends, backend.ID)
		for _, action := range backend.SupportedActions {
			actions[action] = true
		}
	}
	if len(backends) == 0 {
		return CampaignPlannerProposalContract{}, errors.New("EXPERIMENT_CROSS_TARGET_PROPOSAL_NO_BACKEND")
	}
	allowedActions := make([]control.ActionKind, 0, len(actions))
	for _, action := range view.AvailableActions {
		if actions[action] {
			allowedActions = append(allowedActions, action)
		}
	}
	template := CampaignPlannerProposalTemplate{
		SchemaVersion: GuardedTestIntentVersion, ID: baseline.ID,
		ViewDigest: baseline.ViewDigest, RiskID: baseline.RiskID, Must: baseline.Must,
	}
	template.Prefer.BackendIDs = make([]string, 0)
	template.Prefer.Actions = make([]control.ActionKind, 0)
	return CampaignPlannerProposalContract{
		SchemaVersion:     CampaignPlannerProposalContractVersion,
		MutableFields:     []string{"prefer.backend_ids", "prefer.actions"},
		AllowedBackendIDs: backends, AllowedActions: allowedActions, Template: template,
	}, nil
}

func ValidateCrossTargetPlannerProposal(
	view CrossTargetPlannerView,
	baseline GuardedTestIntent,
	proposal GuardedTestIntent,
) error {
	contract, err := NewCrossTargetPlannerProposalContract(view, baseline)
	if err != nil {
		return err
	}
	if err := proposal.Validate(); err != nil {
		return err
	}
	if proposal.ID != baseline.ID || proposal.ViewDigest != baseline.ViewDigest ||
		proposal.RiskID != baseline.RiskID || proposal.Must.Decisions != baseline.Must.Decisions ||
		proposal.Must.FaultEnvelope != baseline.Must.FaultEnvelope ||
		!slices.Equal(proposal.Must.RequiredCapabilities, baseline.Must.RequiredCapabilities) ||
		!slices.Equal(proposal.Must.RequiredActions, baseline.Must.RequiredActions) {
		return errors.New("EXPERIMENT_CROSS_TARGET_PROPOSAL_AUTHORITY_CHANGED")
	}
	allowedBackends := stringSet(contract.AllowedBackendIDs)
	for _, backend := range proposal.Prefer.BackendIDs {
		if !allowedBackends[backend] {
			return errors.New("EXPERIMENT_CROSS_TARGET_PROPOSAL_BACKEND_INVALID")
		}
	}
	allowedActions := actionKindSet(contract.AllowedActions)
	for _, action := range proposal.Prefer.Actions {
		if !allowedActions[action] {
			return errors.New("EXPERIMENT_CROSS_TARGET_PROPOSAL_ACTION_INVALID")
		}
	}
	return validateCrossTargetIntent(view, proposal)
}

func validateCrossTargetIntent(view CrossTargetPlannerView, intent GuardedTestIntent) error {
	risk, ok := findProtocolRisk(view.KnowledgePack.Risks, intent.RiskID)
	if !ok {
		return errors.New("EXPERIMENT_CROSS_TARGET_INTENT_RISK_UNKNOWN")
	}
	requiredCapabilities := unionStrings(risk.RequiredCapabilities, intent.Must.RequiredCapabilities)
	requiredActions := unionActions(risk.RequiredActions, intent.Must.RequiredActions)
	if !stringSubset(requiredCapabilities, stringSet(view.ValidatedCapabilities)) ||
		!actionSubset(requiredActions, actionKindSet(view.AvailableActions)) ||
		!faultEnvelopeEnablesActions(intent.Must.FaultEnvelope, requiredActions) {
		return errors.New("EXPERIMENT_CROSS_TARGET_INTENT_HARD_CONSTRAINT_UNSATISFIED")
	}
	allowed := stringSet(risk.AllowedBackendIDs)
	for _, backend := range view.EligibleBackends {
		if allowed[backend.ID] && intent.Must.Decisions >= backend.MinDecisions &&
			intent.Must.Decisions <= backend.MaxDecisions &&
			faultEnvelopeWithin(intent.Must.FaultEnvelope, backend.MaxFaultEnvelope) &&
			actionSubset(requiredActions, actionKindSet(backend.SupportedActions)) {
			return nil
		}
	}
	return errors.New("EXPERIMENT_CROSS_TARGET_INTENT_NO_COMMON_BACKEND")
}

func (view CrossTargetPlannerView) seal() (CrossTargetPlannerView, error) {
	view.KnowledgePack = cloneKnowledgePack(view.KnowledgePack)
	view.SourceViewDigests = append([]string(nil), view.SourceViewDigests...)
	view.ValidatedCapabilities = append([]string(nil), view.ValidatedCapabilities...)
	view.AvailableActions = append([]control.ActionKind(nil), view.AvailableActions...)
	view.EligibleBackends = cloneBackendViews(view.EligibleBackends)
	sort.Strings(view.SourceViewDigests)
	sort.Strings(view.ValidatedCapabilities)
	sort.Slice(view.AvailableActions, func(i, j int) bool { return view.AvailableActions[i] < view.AvailableActions[j] })
	sort.Slice(view.EligibleBackends, func(i, j int) bool { return view.EligibleBackends[i].ID < view.EligibleBackends[j].ID })
	view.Digest = ""
	digest, err := portableJSONDigest(view)
	if err != nil {
		return CrossTargetPlannerView{}, err
	}
	view.Digest = digest
	return view, nil
}

func intersectStrings(left, right []string) []string {
	available := stringSet(right)
	result := make([]string, 0, len(left))
	for _, value := range left {
		if available[value] {
			result = append(result, value)
		}
	}
	return result
}

func intersectActions(left, right []control.ActionKind) []control.ActionKind {
	available := actionKindSet(right)
	result := make([]control.ActionKind, 0, len(left))
	for _, value := range left {
		if available[value] {
			result = append(result, value)
		}
	}
	return result
}

func intersectBackends(left, right []IntentBackendView) []IntentBackendView {
	byID := make(map[string]IntentBackendView, len(right))
	for _, backend := range right {
		byID[backend.ID] = backend
	}
	result := make([]IntentBackendView, 0, len(left))
	for _, backend := range left {
		candidate, ok := byID[backend.ID]
		if ok && sameBackendView(backend, candidate) {
			result = append(result, backend)
		}
	}
	return result
}

func sameBackendView(left, right IntentBackendView) bool {
	if left.ID != right.ID || left.MinDecisions != right.MinDecisions ||
		left.MaxDecisions != right.MaxDecisions || left.MaxFaultEnvelope != right.MaxFaultEnvelope ||
		len(left.SupportedActions) != len(right.SupportedActions) {
		return false
	}
	for index := range left.SupportedActions {
		if left.SupportedActions[index] != right.SupportedActions[index] {
			return false
		}
	}
	return true
}

func containsString(values []string, value string) bool {
	index := sort.SearchStrings(values, value)
	return index < len(values) && values[index] == value
}
