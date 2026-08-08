package controlexperiment

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/conformance"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const (
	ProtocolKnowledgePackVersion = "consensus-atlas/protocol-knowledge-pack/v1"
	IntentCompilerCatalogVersion = "consensus-atlas/intent-compiler-catalog/v1"
	AgentSemanticViewVersion     = "consensus-atlas/agent-semantic-view/v1"
	GuardedTestIntentVersion     = "consensus-atlas/guarded-test-intent/v1"
	CompiledIntentPlanVersion    = "consensus-atlas/compiled-intent-plan/v1"
)

// KnowledgeStatement is protocol knowledge for an Agent. Text is never
// interpreted by the compiler and therefore cannot grant a capability.
type KnowledgeStatement struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// ProtocolRisk is a curated semantic hypothesis. Its typed requirements are
// compiler inputs; the Agent may select a Risk ID but cannot weaken it.
type ProtocolRisk struct {
	ID                   string               `json:"id"`
	Summary              string               `json:"summary"`
	RequiredCapabilities []string             `json:"required_capabilities"`
	RequiredActions      []control.ActionKind `json:"required_actions"`
	AllowedBackendIDs    []string             `json:"allowed_backend_ids"`
}

type ProtocolKnowledgePack struct {
	SchemaVersion string               `json:"schema_version"`
	ID            string               `json:"id"`
	Family        string               `json:"family"`
	Protocol      string               `json:"protocol"`
	Knowledge     []KnowledgeStatement `json:"knowledge"`
	Risks         []ProtocolRisk       `json:"risks"`
	Digest        string               `json:"digest"`
}

func NewProtocolKnowledgePack(pack ProtocolKnowledgePack) (ProtocolKnowledgePack, error) {
	pack.SchemaVersion = ProtocolKnowledgePackVersion
	sealed, err := pack.seal()
	if err != nil {
		return ProtocolKnowledgePack{}, err
	}
	if err := sealed.Validate(); err != nil {
		return ProtocolKnowledgePack{}, err
	}
	return sealed, nil
}

func (pack ProtocolKnowledgePack) Validate() error {
	if pack.SchemaVersion != ProtocolKnowledgePackVersion || !validMethodToken(pack.ID) ||
		!validMethodToken(pack.Family) || !validMethodToken(pack.Protocol) ||
		len(pack.Knowledge) == 0 || len(pack.Risks) == 0 {
		return errors.New("EXPERIMENT_PROTOCOL_KNOWLEDGE_INVALID")
	}
	seenKnowledge := make(map[string]bool, len(pack.Knowledge))
	for _, statement := range pack.Knowledge {
		if !validMethodToken(statement.ID) || statement.Text == "" || len(statement.Text) > 2048 ||
			seenKnowledge[statement.ID] {
			return errors.New("EXPERIMENT_PROTOCOL_KNOWLEDGE_STATEMENT_INVALID")
		}
		seenKnowledge[statement.ID] = true
	}
	seenRisks := make(map[string]bool, len(pack.Risks))
	for _, risk := range pack.Risks {
		if !validMethodToken(risk.ID) || risk.Summary == "" || len(risk.Summary) > 2048 ||
			seenRisks[risk.ID] || len(risk.RequiredCapabilities) == 0 ||
			len(risk.RequiredActions) == 0 || len(risk.AllowedBackendIDs) == 0 {
			return errors.New("EXPERIMENT_PROTOCOL_RISK_INVALID")
		}
		seenRisks[risk.ID] = true
		if !canonicalStrings(risk.RequiredCapabilities, true) ||
			!canonicalActionKinds(risk.RequiredActions, true) ||
			!canonicalTokens(risk.AllowedBackendIDs, true) {
			return errors.New("EXPERIMENT_PROTOCOL_RISK_REQUIREMENTS_INVALID")
		}
	}
	sealed, err := pack.seal()
	if err != nil || !validSHA256(pack.Digest) || sealed.Digest != pack.Digest {
		return errors.New("EXPERIMENT_PROTOCOL_KNOWLEDGE_DIGEST_MISMATCH")
	}
	return nil
}

func (pack ProtocolKnowledgePack) seal() (ProtocolKnowledgePack, error) {
	pack.Knowledge = append([]KnowledgeStatement(nil), pack.Knowledge...)
	sort.Slice(pack.Knowledge, func(i, j int) bool { return pack.Knowledge[i].ID < pack.Knowledge[j].ID })
	pack.Risks = cloneProtocolRisks(pack.Risks)
	for index := range pack.Risks {
		sort.Strings(pack.Risks[index].RequiredCapabilities)
		sort.Slice(pack.Risks[index].RequiredActions, func(i, j int) bool {
			return pack.Risks[index].RequiredActions[i] < pack.Risks[index].RequiredActions[j]
		})
		sort.Strings(pack.Risks[index].AllowedBackendIDs)
	}
	sort.Slice(pack.Risks, func(i, j int) bool { return pack.Risks[i].ID < pack.Risks[j].ID })
	pack.Digest = ""
	digest, err := portableJSONDigest(pack)
	if err != nil {
		return ProtocolKnowledgePack{}, err
	}
	pack.Digest = digest
	return pack, nil
}

// IntentBackendTemplate is trusted compiler configuration. SupportedActions
// describes selector classes, not a promise that an action occurs in a trace.
type IntentBackendTemplate struct {
	ID                   string               `json:"id"`
	Strategy             string               `json:"strategy"`
	PolicySeed           uint64               `json:"policy_seed"`
	RequiredCapabilities []string             `json:"required_capabilities"`
	SupportedActions     []control.ActionKind `json:"supported_actions"`
	MinDecisions         int                  `json:"min_decisions"`
	MaxDecisions         int                  `json:"max_decisions"`
	MaxFaultEnvelope     FaultEnvelope        `json:"max_fault_envelope"`
	FallbackRank         int                  `json:"fallback_rank"`
}

type IntentCompilerCatalog struct {
	SchemaVersion string                  `json:"schema_version"`
	ID            string                  `json:"id"`
	Templates     []IntentBackendTemplate `json:"templates"`
	Digest        string                  `json:"digest"`
}

func NewIntentCompilerCatalog(catalog IntentCompilerCatalog) (IntentCompilerCatalog, error) {
	catalog.SchemaVersion = IntentCompilerCatalogVersion
	sealed, err := catalog.seal()
	if err != nil {
		return IntentCompilerCatalog{}, err
	}
	if err := sealed.Validate(); err != nil {
		return IntentCompilerCatalog{}, err
	}
	return sealed, nil
}

func (catalog IntentCompilerCatalog) Validate() error {
	if catalog.SchemaVersion != IntentCompilerCatalogVersion || !validMethodToken(catalog.ID) ||
		len(catalog.Templates) == 0 {
		return errors.New("EXPERIMENT_INTENT_CATALOG_INVALID")
	}
	seenIDs := make(map[string]bool, len(catalog.Templates))
	seenRanks := make(map[int]bool, len(catalog.Templates))
	for _, template := range catalog.Templates {
		if !validMethodToken(template.ID) || !validMethodToken(template.Strategy) ||
			seenIDs[template.ID] || template.FallbackRank <= 0 || seenRanks[template.FallbackRank] ||
			template.MinDecisions <= 0 || template.MaxDecisions < template.MinDecisions ||
			!canonicalStrings(template.RequiredCapabilities, true) ||
			!canonicalActionKinds(template.SupportedActions, true) ||
			template.MaxFaultEnvelope.Validate() != nil {
			return errors.New("EXPERIMENT_INTENT_BACKEND_TEMPLATE_INVALID")
		}
		seenIDs[template.ID], seenRanks[template.FallbackRank] = true, true
	}
	sealed, err := catalog.seal()
	if err != nil || !validSHA256(catalog.Digest) || sealed.Digest != catalog.Digest {
		return errors.New("EXPERIMENT_INTENT_CATALOG_DIGEST_MISMATCH")
	}
	return nil
}

func (catalog IntentCompilerCatalog) seal() (IntentCompilerCatalog, error) {
	catalog.Templates = cloneIntentTemplates(catalog.Templates)
	for index := range catalog.Templates {
		sort.Strings(catalog.Templates[index].RequiredCapabilities)
		sort.Slice(catalog.Templates[index].SupportedActions, func(i, j int) bool {
			return catalog.Templates[index].SupportedActions[i] < catalog.Templates[index].SupportedActions[j]
		})
	}
	sort.Slice(catalog.Templates, func(i, j int) bool { return catalog.Templates[i].ID < catalog.Templates[j].ID })
	catalog.Digest = ""
	digest, err := portableJSONDigest(catalog)
	if err != nil {
		return IntentCompilerCatalog{}, err
	}
	catalog.Digest = digest
	return catalog, nil
}

type IntentBackendView struct {
	ID               string               `json:"id"`
	SupportedActions []control.ActionKind `json:"supported_actions"`
	MinDecisions     int                  `json:"min_decisions"`
	MaxDecisions     int                  `json:"max_decisions"`
	MaxFaultEnvelope FaultEnvelope        `json:"max_fault_envelope"`
}

// AgentSemanticView intentionally omits build, candidate, root-cause and
// Oracle identities. It is recomputed from trusted qualification inputs before
// an intent is compiled.
type AgentSemanticView struct {
	SchemaVersion         string                `json:"schema_version"`
	ID                    string                `json:"id"`
	KnowledgePack         ProtocolKnowledgePack `json:"knowledge_pack"`
	CatalogDigest         string                `json:"catalog_digest"`
	ProfileID             string                `json:"profile_id"`
	ProfileDigest         string                `json:"profile_digest"`
	AdapterID             string                `json:"adapter_id"`
	ValidatedCapabilities []string              `json:"validated_capabilities"`
	AvailableActions      []control.ActionKind  `json:"available_actions"`
	EligibleBackends      []IntentBackendView   `json:"eligible_backends"`
	Digest                string                `json:"digest"`
}

func NewAgentSemanticView(
	id string,
	pack ProtocolKnowledgePack,
	catalog IntentCompilerCatalog,
	manifest control.AdapterManifest,
	qualification conformance.QualificationReport,
) (AgentSemanticView, error) {
	if !validMethodToken(id) {
		return AgentSemanticView{}, errors.New("EXPERIMENT_AGENT_VIEW_ID_INVALID")
	}
	if err := pack.Validate(); err != nil {
		return AgentSemanticView{}, err
	}
	if err := catalog.Validate(); err != nil {
		return AgentSemanticView{}, err
	}
	if err := validateViewQualification(manifest, qualification); err != nil {
		return AgentSemanticView{}, err
	}
	validated := make([]string, 0, len(qualification.Capabilities))
	validatedSet := make(map[string]bool)
	for _, capability := range qualification.Capabilities {
		if capability.Status == conformance.CapabilityValidated {
			validated = append(validated, capability.ID)
			validatedSet[capability.ID] = true
		}
	}
	sort.Strings(validated)
	actions := append([]control.ActionKind(nil), manifest.Capabilities.Actions...)
	sort.Slice(actions, func(i, j int) bool { return actions[i] < actions[j] })
	actionSet := actionKindSet(actions)
	view := AgentSemanticView{
		SchemaVersion: AgentSemanticViewVersion, ID: id, KnowledgePack: pack,
		CatalogDigest: catalog.Digest, ProfileID: qualification.ProfileID,
		ProfileDigest: qualification.ProfileDigest, AdapterID: qualification.AdapterID,
		ValidatedCapabilities: validated, AvailableActions: actions,
	}
	for _, template := range catalog.Templates {
		if stringSubset(template.RequiredCapabilities, validatedSet) &&
			actionSubset(template.SupportedActions, actionSet) {
			view.EligibleBackends = append(view.EligibleBackends, IntentBackendView{
				ID: template.ID, SupportedActions: append([]control.ActionKind(nil), template.SupportedActions...),
				MinDecisions: template.MinDecisions, MaxDecisions: template.MaxDecisions,
				MaxFaultEnvelope: template.MaxFaultEnvelope,
			})
		}
	}
	if len(view.EligibleBackends) == 0 {
		return AgentSemanticView{}, errors.New("EXPERIMENT_AGENT_VIEW_NO_ELIGIBLE_BACKEND")
	}
	sealed, err := view.seal()
	if err != nil {
		return AgentSemanticView{}, err
	}
	if err := sealed.Validate(); err != nil {
		return AgentSemanticView{}, err
	}
	return sealed, nil
}

func (view AgentSemanticView) Validate() error {
	if view.SchemaVersion != AgentSemanticViewVersion || !validMethodToken(view.ID) ||
		view.ProfileID == "" || !validSHA256(view.ProfileDigest) || view.AdapterID == "" ||
		!validSHA256(view.CatalogDigest) || len(view.ValidatedCapabilities) == 0 ||
		len(view.AvailableActions) == 0 || len(view.EligibleBackends) == 0 {
		return errors.New("EXPERIMENT_AGENT_VIEW_INVALID")
	}
	if err := view.KnowledgePack.Validate(); err != nil {
		return err
	}
	if !canonicalStrings(view.ValidatedCapabilities, true) ||
		!canonicalActionKinds(view.AvailableActions, true) {
		return errors.New("EXPERIMENT_AGENT_VIEW_CAPABILITIES_INVALID")
	}
	seen := make(map[string]bool, len(view.EligibleBackends))
	for _, backend := range view.EligibleBackends {
		if !validMethodToken(backend.ID) || seen[backend.ID] || backend.MinDecisions <= 0 ||
			backend.MaxDecisions < backend.MinDecisions ||
			backend.MaxFaultEnvelope.Validate() != nil ||
			!canonicalActionKinds(backend.SupportedActions, true) {
			return errors.New("EXPERIMENT_AGENT_VIEW_BACKEND_INVALID")
		}
		seen[backend.ID] = true
	}
	sealed, err := view.seal()
	if err != nil || !validSHA256(view.Digest) || sealed.Digest != view.Digest {
		return errors.New("EXPERIMENT_AGENT_VIEW_DIGEST_MISMATCH")
	}
	return nil
}

func (view AgentSemanticView) ValidateInputs(
	pack ProtocolKnowledgePack,
	catalog IntentCompilerCatalog,
	manifest control.AdapterManifest,
	qualification conformance.QualificationReport,
) error {
	want, err := NewAgentSemanticView(view.ID, pack, catalog, manifest, qualification)
	if err != nil {
		return err
	}
	if want.Digest != view.Digest {
		return errors.New("EXPERIMENT_AGENT_VIEW_INPUT_MISMATCH")
	}
	return nil
}

func (view AgentSemanticView) seal() (AgentSemanticView, error) {
	view.KnowledgePack = cloneKnowledgePack(view.KnowledgePack)
	view.ValidatedCapabilities = append([]string(nil), view.ValidatedCapabilities...)
	view.AvailableActions = append([]control.ActionKind(nil), view.AvailableActions...)
	view.EligibleBackends = cloneBackendViews(view.EligibleBackends)
	sort.Slice(view.EligibleBackends, func(i, j int) bool { return view.EligibleBackends[i].ID < view.EligibleBackends[j].ID })
	view.Digest = ""
	digest, err := portableJSONDigest(view)
	if err != nil {
		return AgentSemanticView{}, err
	}
	view.Digest = digest
	return view, nil
}

type IntentMust struct {
	Decisions            int                  `json:"decisions"`
	RequiredCapabilities []string             `json:"required_capabilities,omitempty"`
	RequiredActions      []control.ActionKind `json:"required_actions,omitempty"`
	FaultEnvelope        FaultEnvelope        `json:"fault_envelope"`
}

type IntentPrefer struct {
	BackendIDs []string             `json:"backend_ids,omitempty"`
	Actions    []control.ActionKind `json:"actions,omitempty"`
}

type GuardedTestIntent struct {
	SchemaVersion string       `json:"schema_version"`
	ID            string       `json:"id"`
	ViewDigest    string       `json:"view_digest"`
	RiskID        string       `json:"risk_id"`
	Must          IntentMust   `json:"must"`
	Prefer        IntentPrefer `json:"prefer"`
	Digest        string       `json:"digest"`
}

func NewGuardedTestIntent(intent GuardedTestIntent) (GuardedTestIntent, error) {
	intent.SchemaVersion = GuardedTestIntentVersion
	intent.Must.RequiredCapabilities = sortedUniqueStrings(intent.Must.RequiredCapabilities)
	intent.Must.RequiredActions = sortedUniqueActions(intent.Must.RequiredActions)
	intent.Digest = ""
	if err := intent.validateContent(); err != nil {
		return GuardedTestIntent{}, err
	}
	digest, err := portableJSONDigest(intent)
	if err != nil {
		return GuardedTestIntent{}, err
	}
	intent.Digest = digest
	return intent, nil
}

// ParseGuardedTestIntentProposal is the future model boundary. The proposer
// supplies typed fields only; the trusted coordinator assigns the digest.
func ParseGuardedTestIntentProposal(data []byte) (GuardedTestIntent, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var proposal GuardedTestIntent
	if err := decoder.Decode(&proposal); err != nil {
		return GuardedTestIntent{}, errors.New("EXPERIMENT_GUARDED_INTENT_JSON_INVALID")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return GuardedTestIntent{}, errors.New("EXPERIMENT_GUARDED_INTENT_JSON_TRAILING")
	}
	if proposal.SchemaVersion != GuardedTestIntentVersion || proposal.Digest != "" {
		return GuardedTestIntent{}, errors.New("EXPERIMENT_GUARDED_INTENT_PROPOSAL_IDENTITY_INVALID")
	}
	return NewGuardedTestIntent(proposal)
}

func (intent GuardedTestIntent) Validate() error {
	if err := intent.validateContent(); err != nil {
		return err
	}
	want, err := NewGuardedTestIntent(intent)
	if err != nil || !validSHA256(intent.Digest) || want.Digest != intent.Digest {
		return errors.New("EXPERIMENT_GUARDED_INTENT_DIGEST_MISMATCH")
	}
	return nil
}

func (intent GuardedTestIntent) validateContent() error {
	if intent.SchemaVersion != GuardedTestIntentVersion || !validMethodToken(intent.ID) ||
		!validSHA256(intent.ViewDigest) || !validMethodToken(intent.RiskID) || intent.Must.Decisions <= 0 ||
		intent.Must.FaultEnvelope.Validate() != nil ||
		!canonicalStrings(intent.Must.RequiredCapabilities, false) ||
		!canonicalActionKinds(intent.Must.RequiredActions, false) ||
		!uniqueTokensInOrder(intent.Prefer.BackendIDs) || !uniqueActionsInOrder(intent.Prefer.Actions) {
		return errors.New("EXPERIMENT_GUARDED_INTENT_INVALID")
	}
	return nil
}

type IntentPreferenceMiss struct {
	Code  string `json:"code"`
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type IntentCompilerWork struct {
	CandidatesEvaluated int `json:"candidates_evaluated"`
	PreferenceChecks    int `json:"preference_checks"`
	WorkUnits           int `json:"work_units"`
}

type CompiledIntentPlan struct {
	SchemaVersion        string                 `json:"schema_version"`
	ID                   string                 `json:"id"`
	ViewDigest           string                 `json:"view_digest"`
	IntentDigest         string                 `json:"intent_digest"`
	CatalogDigest        string                 `json:"catalog_digest"`
	RiskID               string                 `json:"risk_id"`
	BackendID            string                 `json:"backend_id"`
	Strategy             string                 `json:"strategy"`
	PolicySeed           uint64                 `json:"policy_seed"`
	Decisions            int                    `json:"decisions"`
	FaultEnvelope        FaultEnvelope          `json:"fault_envelope"`
	RequiredCapabilities []string               `json:"required_capabilities"`
	RequiredActions      []control.ActionKind   `json:"required_actions"`
	PreferenceMisses     []IntentPreferenceMiss `json:"preference_misses,omitempty"`
	FallbackUsed         bool                   `json:"fallback_used"`
	CompilerWork         IntentCompilerWork     `json:"compiler_work"`
	Digest               string                 `json:"digest"`
}

// CompileGuardedTestIntent is a macro compiler. It never sees Runtime
// ActionIDs and never executes a target. Every semantic view is first
// recomputed from the trusted pack/catalog/manifest/qualification inputs.
func CompileGuardedTestIntent(
	view AgentSemanticView,
	pack ProtocolKnowledgePack,
	catalog IntentCompilerCatalog,
	manifest control.AdapterManifest,
	qualification conformance.QualificationReport,
	intent GuardedTestIntent,
) (CompiledIntentPlan, error) {
	if err := view.ValidateInputs(pack, catalog, manifest, qualification); err != nil {
		return CompiledIntentPlan{}, err
	}
	if err := intent.Validate(); err != nil {
		return CompiledIntentPlan{}, err
	}
	if intent.ViewDigest != view.Digest {
		return CompiledIntentPlan{}, errors.New("EXPERIMENT_GUARDED_INTENT_VIEW_MISMATCH")
	}
	risk, ok := findProtocolRisk(pack.Risks, intent.RiskID)
	if !ok {
		return CompiledIntentPlan{}, errors.New("EXPERIMENT_GUARDED_INTENT_RISK_UNKNOWN")
	}
	requiredCapabilities := unionStrings(risk.RequiredCapabilities, intent.Must.RequiredCapabilities)
	requiredActions := unionActions(risk.RequiredActions, intent.Must.RequiredActions)
	if !stringSubset(requiredCapabilities, stringSet(view.ValidatedCapabilities)) ||
		!actionSubset(requiredActions, actionKindSet(view.AvailableActions)) {
		return CompiledIntentPlan{}, errors.New("EXPERIMENT_INTENT_HARD_CONSTRAINT_UNVALIDATED")
	}
	if !faultEnvelopeEnablesActions(intent.Must.FaultEnvelope, requiredActions) {
		return CompiledIntentPlan{}, errors.New("EXPERIMENT_INTENT_HARD_CONSTRAINT_UNSATISFIED")
	}
	eligibleView := make(map[string]bool, len(view.EligibleBackends))
	for _, backend := range view.EligibleBackends {
		eligibleView[backend.ID] = true
	}
	allowed := stringSet(risk.AllowedBackendIDs)
	candidates := make([]IntentBackendTemplate, 0, len(catalog.Templates))
	for _, template := range catalog.Templates {
		if !eligibleView[template.ID] || !allowed[template.ID] ||
			intent.Must.Decisions < template.MinDecisions || intent.Must.Decisions > template.MaxDecisions ||
			!faultEnvelopeWithin(intent.Must.FaultEnvelope, template.MaxFaultEnvelope) ||
			!actionSubset(requiredActions, actionKindSet(template.SupportedActions)) {
			continue
		}
		candidates = append(candidates, template)
	}
	if len(candidates) == 0 {
		return CompiledIntentPlan{}, errors.New("EXPERIMENT_INTENT_HARD_CONSTRAINT_UNSATISFIED")
	}
	selected := selectIntentBackend(candidates, intent.Prefer)
	misses, fallback := preferenceMisses(selected, intent.Prefer)
	work := IntentCompilerWork{
		CandidatesEvaluated: len(candidates),
		PreferenceChecks:    len(intent.Prefer.BackendIDs) + len(intent.Prefer.Actions),
	}
	work.WorkUnits = work.CandidatesEvaluated + work.PreferenceChecks
	plan := CompiledIntentPlan{
		SchemaVersion: CompiledIntentPlanVersion, ID: intent.ID + "-compiled",
		ViewDigest: view.Digest, IntentDigest: intent.Digest, CatalogDigest: catalog.Digest,
		RiskID: intent.RiskID, BackendID: selected.ID, Strategy: selected.Strategy,
		PolicySeed: selected.PolicySeed, Decisions: intent.Must.Decisions,
		FaultEnvelope:        intent.Must.FaultEnvelope,
		RequiredCapabilities: requiredCapabilities, RequiredActions: requiredActions,
		PreferenceMisses: misses, FallbackUsed: fallback, CompilerWork: work,
	}
	sealed, err := plan.seal()
	if err != nil {
		return CompiledIntentPlan{}, err
	}
	if err := sealed.Validate(); err != nil {
		return CompiledIntentPlan{}, err
	}
	return sealed, nil
}

func (plan CompiledIntentPlan) Validate() error {
	if plan.SchemaVersion != CompiledIntentPlanVersion || !validMethodToken(plan.ID) ||
		!validSHA256(plan.ViewDigest) || !validSHA256(plan.IntentDigest) ||
		!validSHA256(plan.CatalogDigest) || !validMethodToken(plan.RiskID) ||
		!validMethodToken(plan.BackendID) || !validMethodToken(plan.Strategy) || plan.Decisions <= 0 ||
		plan.FaultEnvelope.Validate() != nil || !canonicalStrings(plan.RequiredCapabilities, true) ||
		!canonicalActionKinds(plan.RequiredActions, true) || plan.CompilerWork.CandidatesEvaluated <= 0 ||
		plan.CompilerWork.PreferenceChecks < 0 ||
		plan.CompilerWork.WorkUnits != plan.CompilerWork.CandidatesEvaluated+plan.CompilerWork.PreferenceChecks {
		return errors.New("EXPERIMENT_COMPILED_INTENT_INVALID")
	}
	for _, miss := range plan.PreferenceMisses {
		if miss.Code != "INTENT_PREFERENCE_UNAVAILABLE" ||
			(miss.Kind != "backend" && miss.Kind != "action") || miss.Value == "" {
			return errors.New("EXPERIMENT_COMPILED_INTENT_PREFERENCE_MISS_INVALID")
		}
	}
	sealed, err := plan.seal()
	if err != nil || !validSHA256(plan.Digest) || sealed.Digest != plan.Digest {
		return errors.New("EXPERIMENT_COMPILED_INTENT_DIGEST_MISMATCH")
	}
	return nil
}

func (plan CompiledIntentPlan) ValidateInputs(
	view AgentSemanticView,
	pack ProtocolKnowledgePack,
	catalog IntentCompilerCatalog,
	manifest control.AdapterManifest,
	qualification conformance.QualificationReport,
	intent GuardedTestIntent,
) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	want, err := CompileGuardedTestIntent(view, pack, catalog, manifest, qualification, intent)
	if err != nil {
		return err
	}
	if want.Digest != plan.Digest {
		return errors.New("EXPERIMENT_COMPILED_INTENT_INPUT_MISMATCH")
	}
	return nil
}

// ValidateExecution turns RequiredActions from a compile-time selector check
// into a trace-backed hard constraint. A backend that can select an ActionKind
// still fails the intent when the actual execution never takes that action.
func (plan CompiledIntentPlan) ValidateExecution(report Report, bundle ExecutionBundle) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	if err := report.Validate(); err != nil {
		return err
	}
	if err := bundle.Validate(); err != nil {
		return err
	}
	if bundle.Identity.ReportDigest != report.Digest || report.Config.DecisionsPerRun != plan.Decisions ||
		report.Config.FaultEnvelope == nil || *report.Config.FaultEnvelope != plan.FaultEnvelope ||
		report.Config.Admission == nil ||
		!stringSubset(plan.RequiredCapabilities, stringSet(report.Config.Admission.RequiredCapabilities)) {
		return errors.New("EXPERIMENT_COMPILED_INTENT_EXECUTION_MISMATCH")
	}
	observed := make(map[control.ActionKind]bool)
	for _, record := range bundle.Trace.Records {
		observed[record.Action.Kind] = true
	}
	if !actionSubset(plan.RequiredActions, observed) {
		return errors.New("EXPERIMENT_COMPILED_INTENT_HARD_ACTION_MISSING")
	}
	return nil
}

func (plan CompiledIntentPlan) seal() (CompiledIntentPlan, error) {
	plan.RequiredCapabilities = append([]string(nil), plan.RequiredCapabilities...)
	plan.RequiredActions = append([]control.ActionKind(nil), plan.RequiredActions...)
	plan.PreferenceMisses = append([]IntentPreferenceMiss(nil), plan.PreferenceMisses...)
	plan.Digest = ""
	digest, err := portableJSONDigest(plan)
	if err != nil {
		return CompiledIntentPlan{}, err
	}
	plan.Digest = digest
	return plan, nil
}

func validateViewQualification(manifest control.AdapterManifest, report conformance.QualificationReport) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	if err := report.Validate(); err != nil {
		return err
	}
	digest, err := manifest.Digest()
	if err != nil {
		return err
	}
	if digest != report.ManifestDigest || manifest.AdapterID != report.AdapterID ||
		manifest.ImplementationID != report.ImplementationID || manifest.BuildID != report.BuildID ||
		manifest.ConfigurationDigest != report.ConfigurationDigest {
		return errors.New("EXPERIMENT_AGENT_VIEW_QUALIFICATION_MISMATCH")
	}
	return nil
}

func selectIntentBackend(candidates []IntentBackendTemplate, prefer IntentPrefer) IntentBackendTemplate {
	backendRank := func(id string) int {
		for index, preferred := range prefer.BackendIDs {
			if id == preferred {
				return index
			}
		}
		return len(prefer.BackendIDs) + 1
	}
	actionMisses := func(template IntentBackendTemplate) int {
		available := actionKindSet(template.SupportedActions)
		misses := 0
		for _, action := range prefer.Actions {
			if !available[action] {
				misses++
			}
		}
		return misses
	}
	sorted := cloneIntentTemplates(candidates)
	sort.Slice(sorted, func(i, j int) bool {
		leftRank, rightRank := backendRank(sorted[i].ID), backendRank(sorted[j].ID)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		leftMiss, rightMiss := actionMisses(sorted[i]), actionMisses(sorted[j])
		if leftMiss != rightMiss {
			return leftMiss < rightMiss
		}
		if sorted[i].FallbackRank != sorted[j].FallbackRank {
			return sorted[i].FallbackRank < sorted[j].FallbackRank
		}
		return sorted[i].ID < sorted[j].ID
	})
	return sorted[0]
}

func preferenceMisses(selected IntentBackendTemplate, prefer IntentPrefer) ([]IntentPreferenceMiss, bool) {
	selectedRank := -1
	for index, backend := range prefer.BackendIDs {
		if backend == selected.ID {
			selectedRank = index
			break
		}
	}
	misses := make([]IntentPreferenceMiss, 0)
	limit := len(prefer.BackendIDs)
	if selectedRank >= 0 {
		limit = selectedRank
	}
	for index := 0; index < limit; index++ {
		misses = append(misses, IntentPreferenceMiss{
			Code: "INTENT_PREFERENCE_UNAVAILABLE", Kind: "backend", Value: prefer.BackendIDs[index],
		})
	}
	actions := actionKindSet(selected.SupportedActions)
	for _, action := range prefer.Actions {
		if !actions[action] {
			misses = append(misses, IntentPreferenceMiss{
				Code: "INTENT_PREFERENCE_UNAVAILABLE", Kind: "action", Value: string(action),
			})
		}
	}
	return misses, len(prefer.BackendIDs) > 0 && selectedRank < 0
}

func findProtocolRisk(risks []ProtocolRisk, id string) (ProtocolRisk, bool) {
	for _, risk := range risks {
		if risk.ID == id {
			return risk, true
		}
	}
	return ProtocolRisk{}, false
}

func faultEnvelopeWithin(current, ceiling FaultEnvelope) bool {
	return current.MaxCrashes <= ceiling.MaxCrashes &&
		current.MaxConcurrentCrashes <= ceiling.MaxConcurrentCrashes &&
		current.MaxMessageDrops <= ceiling.MaxMessageDrops &&
		current.MaxMessageDuplicates <= ceiling.MaxMessageDuplicates &&
		current.MaxPartitions <= ceiling.MaxPartitions &&
		current.MaxActivePartitions <= ceiling.MaxActivePartitions
}

func faultEnvelopeEnablesActions(envelope FaultEnvelope, actions []control.ActionKind) bool {
	for _, action := range actions {
		switch action {
		case control.ActionCrash, control.ActionRestart:
			if envelope.MaxCrashes == 0 || envelope.MaxConcurrentCrashes == 0 {
				return false
			}
		case control.ActionDropMessage:
			if envelope.MaxMessageDrops == 0 {
				return false
			}
		case control.ActionDuplicateMessage:
			if envelope.MaxMessageDuplicates == 0 {
				return false
			}
		case control.ActionPartition, control.ActionHeal:
			if envelope.MaxPartitions == 0 || envelope.MaxActivePartitions == 0 {
				return false
			}
		}
	}
	return true
}

func cloneKnowledgePack(pack ProtocolKnowledgePack) ProtocolKnowledgePack {
	pack.Knowledge = append([]KnowledgeStatement(nil), pack.Knowledge...)
	pack.Risks = cloneProtocolRisks(pack.Risks)
	return pack
}

func cloneProtocolRisks(risks []ProtocolRisk) []ProtocolRisk {
	result := append([]ProtocolRisk(nil), risks...)
	for index := range result {
		result[index].RequiredCapabilities = append([]string(nil), result[index].RequiredCapabilities...)
		result[index].RequiredActions = append([]control.ActionKind(nil), result[index].RequiredActions...)
		result[index].AllowedBackendIDs = append([]string(nil), result[index].AllowedBackendIDs...)
	}
	return result
}

func cloneIntentTemplates(templates []IntentBackendTemplate) []IntentBackendTemplate {
	result := append([]IntentBackendTemplate(nil), templates...)
	for index := range result {
		result[index].RequiredCapabilities = append([]string(nil), result[index].RequiredCapabilities...)
		result[index].SupportedActions = append([]control.ActionKind(nil), result[index].SupportedActions...)
	}
	return result
}

func cloneBackendViews(backends []IntentBackendView) []IntentBackendView {
	result := append([]IntentBackendView(nil), backends...)
	for index := range result {
		result[index].SupportedActions = append([]control.ActionKind(nil), result[index].SupportedActions...)
	}
	return result
}

func canonicalStrings(values []string, require bool) bool {
	if require && len(values) == 0 {
		return false
	}
	for index, value := range values {
		if value == "" || (index > 0 && values[index-1] >= value) {
			return false
		}
	}
	return true
}

func canonicalTokens(values []string, require bool) bool {
	if !canonicalStrings(values, require) {
		return false
	}
	for _, value := range values {
		if !validMethodToken(value) {
			return false
		}
	}
	return true
}

func canonicalActionKinds(values []control.ActionKind, require bool) bool {
	if require && len(values) == 0 {
		return false
	}
	for index, value := range values {
		if value.Validate() != nil || (index > 0 && values[index-1] >= value) {
			return false
		}
	}
	return true
}

func uniqueTokensInOrder(values []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if !validMethodToken(value) || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func uniqueActionsInOrder(values []control.ActionKind) bool {
	seen := make(map[control.ActionKind]bool, len(values))
	for _, value := range values {
		if value.Validate() != nil || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func sortedUniqueStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func sortedUniqueActions(values []control.ActionKind) []control.ActionKind {
	result := append([]control.ActionKind(nil), values...)
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func actionKindSet(values []control.ActionKind) map[control.ActionKind]bool {
	result := make(map[control.ActionKind]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func stringSubset(values []string, set map[string]bool) bool {
	for _, value := range values {
		if !set[value] {
			return false
		}
	}
	return true
}

func actionSubset(values []control.ActionKind, set map[control.ActionKind]bool) bool {
	for _, value := range values {
		if !set[value] {
			return false
		}
	}
	return true
}

func unionStrings(left, right []string) []string {
	set := stringSet(left)
	for _, value := range right {
		set[value] = true
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func unionActions(left, right []control.ActionKind) []control.ActionKind {
	set := actionKindSet(left)
	for _, value := range right {
		set[value] = true
	}
	result := make([]control.ActionKind, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
