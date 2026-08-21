package controlexperiment

import (
	"errors"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const (
	ProtocolKnowledgePackVersion    = "consensus-atlas/protocol-knowledge-pack/v1"
	protocolKnowledgeTextMaxBytes   = 2048
	agentMaterialReferenceMaxBytes  = 128
	targetEvidenceReferenceMaxBytes = 512
	targetMaterialSectionMax        = 64
	targetMaterialEvidenceMax       = 16

	PropertyEvidenceHypothesis   = "hypothesis-only"
	PropertyEvidenceObservable   = "observable-only"
	PropertyEvidenceOracleBacked = "oracle-backed"
)

// KnowledgeStatement is protocol knowledge visible to a restricted planner.
// Text is never interpreted by trusted code and grants no Action authority.
type KnowledgeStatement struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// ProtocolProperty is trusted vocabulary for what an Agent may investigate.
// It names a property but does not let the Agent select or implement an Oracle.
type ProtocolProperty struct {
	ID            string `json:"id"`
	Summary       string `json:"summary"`
	EvidenceLevel string `json:"evidence_level,omitempty"`
}

func cloneProtocolKnowledge(value ProtocolKnowledgePack) ProtocolKnowledgePack {
	value.Knowledge = append([]KnowledgeStatement(nil), value.Knowledge...)
	value.Properties = append([]ProtocolProperty(nil), value.Properties...)
	value.IssuePatterns = append([]HistoricalIssuePattern(nil), value.IssuePatterns...)
	value.TargetDossier = cloneTargetDossier(value.TargetDossier)
	value.Risks = cloneProtocolKnowledgeRisks(value.Risks)
	return value
}

// HistoricalIssuePattern gives the Agent cross-protocol implementation
// experience without embedding a target-specific reproduction schedule.
type HistoricalIssuePattern struct {
	ID            string `json:"id"`
	Summary       string `json:"summary"`
	Mechanism     string `json:"mechanism"`
	Applicability string `json:"applicability,omitempty"`
	Boundary      string `json:"boundary,omitempty"`
}

// TargetMaterial is implementation-facing context for an untrusted Agent.
// EvidenceRefs are reviewable source locations, not execution authority or
// proof that the summarized statement is correct.
type TargetMaterial struct {
	ID           string   `json:"id"`
	Summary      string   `json:"summary"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
}

// TargetDossier supplements protocol theory with the concrete composition
// under investigation. It remains editable authoring material: Runtime
// Actions, observations and verdicts still come from trusted execution.
type TargetDossier struct {
	Scope            string           `json:"scope"`
	Assumptions      []TargetMaterial `json:"assumptions,omitempty"`
	Components       []TargetMaterial `json:"components,omitempty"`
	Contracts        []TargetMaterial `json:"contracts,omitempty"`
	ControlSemantics []TargetMaterial `json:"control_semantics,omitempty"`
	ActiveExperiment []TargetMaterial `json:"active_experiment,omitempty"`
	BlindSpots       []TargetMaterial `json:"blind_spots,omitempty"`
}

// ProtocolRisk is typed, curated context retained in the public knowledge
// identity. Stateless planners cannot modify these requirements or budgets.
type ProtocolRisk struct {
	ID                   string               `json:"id"`
	Summary              string               `json:"summary"`
	RequiredCapabilities []string             `json:"required_capabilities"`
	RequiredActions      []control.ActionKind `json:"required_actions"`
	AllowedBackendIDs    []string             `json:"allowed_backend_ids"`
}

type ProtocolKnowledgePack struct {
	SchemaVersion string                   `json:"schema_version"`
	ID            string                   `json:"id"`
	Family        string                   `json:"family"`
	Protocol      string                   `json:"protocol"`
	Knowledge     []KnowledgeStatement     `json:"knowledge"`
	Properties    []ProtocolProperty       `json:"properties,omitempty"`
	IssuePatterns []HistoricalIssuePattern `json:"issue_patterns,omitempty"`
	TargetDossier *TargetDossier           `json:"target_dossier,omitempty"`
	Risks         []ProtocolRisk           `json:"risks,omitempty"`
	Digest        string                   `json:"digest"`
}

func NewProtocolKnowledgePack(pack ProtocolKnowledgePack) (ProtocolKnowledgePack, error) {
	pack.SchemaVersion = ProtocolKnowledgePackVersion
	sealed, err := pack.seal()
	if err != nil || sealed.Validate() != nil {
		return ProtocolKnowledgePack{}, errors.New("EXPERIMENT_PROTOCOL_KNOWLEDGE_INPUT_INVALID")
	}
	return sealed, nil
}

func (pack ProtocolKnowledgePack) Validate() error {
	if pack.SchemaVersion != ProtocolKnowledgePackVersion || !validMethodToken(pack.ID) ||
		!validMethodToken(pack.Family) || !validMethodToken(pack.Protocol) ||
		len(pack.Knowledge) == 0 {
		return errors.New("EXPERIMENT_PROTOCOL_KNOWLEDGE_INVALID")
	}
	seenKnowledge := make(map[string]bool, len(pack.Knowledge))
	for _, statement := range pack.Knowledge {
		if !validMethodToken(statement.ID) || statement.Text == "" ||
			len(statement.Text) > protocolKnowledgeTextMaxBytes ||
			seenKnowledge[statement.ID] {
			return errors.New("EXPERIMENT_PROTOCOL_KNOWLEDGE_STATEMENT_INVALID")
		}
		seenKnowledge[statement.ID] = true
	}
	seenProperties := make(map[string]bool, len(pack.Properties))
	for _, property := range pack.Properties {
		if !validMethodToken(property.ID) || len(property.ID) > agentMaterialReferenceMaxBytes ||
			property.Summary == "" || len(property.Summary) > protocolKnowledgeTextMaxBytes ||
			!validPropertyEvidenceLevel(property.EvidenceLevel) ||
			seenProperties[property.ID] {
			return errors.New("EXPERIMENT_PROTOCOL_PROPERTY_INVALID")
		}
		seenProperties[property.ID] = true
	}
	seenPatterns := make(map[string]bool, len(pack.IssuePatterns))
	for _, pattern := range pack.IssuePatterns {
		if !validMethodToken(pattern.ID) || len(pattern.ID) > agentMaterialReferenceMaxBytes ||
			pattern.ID == "original" || pattern.Summary == "" ||
			len(pattern.Summary) > protocolKnowledgeTextMaxBytes || pattern.Mechanism == "" ||
			len(pattern.Mechanism) > protocolKnowledgeTextMaxBytes ||
			len(pattern.Applicability) > protocolKnowledgeTextMaxBytes ||
			len(pattern.Boundary) > protocolKnowledgeTextMaxBytes || seenPatterns[pattern.ID] {
			return errors.New("EXPERIMENT_HISTORICAL_ISSUE_PATTERN_INVALID")
		}
		seenPatterns[pattern.ID] = true
	}
	if pack.TargetDossier != nil && !validTargetDossier(*pack.TargetDossier) {
		return errors.New("EXPERIMENT_TARGET_DOSSIER_INVALID")
	}
	seenRisks := make(map[string]bool, len(pack.Risks))
	for _, risk := range pack.Risks {
		if !validMethodToken(risk.ID) || risk.Summary == "" ||
			len(risk.Summary) > protocolKnowledgeTextMaxBytes ||
			seenRisks[risk.ID] || !canonicalStrings(risk.RequiredCapabilities, true) ||
			!canonicalActionKinds(risk.RequiredActions, true) ||
			!canonicalKnowledgeTokens(risk.AllowedBackendIDs) {
			return errors.New("EXPERIMENT_PROTOCOL_RISK_INVALID")
		}
		seenRisks[risk.ID] = true
	}
	want, err := pack.seal()
	if err != nil || !validSHA256(pack.Digest) || want.Digest != pack.Digest {
		return errors.New("EXPERIMENT_PROTOCOL_KNOWLEDGE_DIGEST_MISMATCH")
	}
	return nil
}

// ValidateAgentMaterials is the active Hypothesis-Agent boundary. Seed Risks
// are deliberately absent: they remain supported only by legacy curated
// baselines that construct an explicit TestHypothesis.
func (pack ProtocolKnowledgePack) ValidateAgentMaterials() error {
	if pack.Validate() != nil || len(pack.Properties) == 0 || len(pack.Risks) != 0 {
		return errors.New("EXPERIMENT_AGENT_MATERIALS_INVALID")
	}
	for _, property := range pack.Properties {
		if property.EvidenceLevel == "" {
			return errors.New("EXPERIMENT_AGENT_PROPERTY_EVIDENCE_REQUIRED")
		}
	}
	return nil
}

func (pack ProtocolKnowledgePack) seal() (ProtocolKnowledgePack, error) {
	pack.Knowledge = append([]KnowledgeStatement(nil), pack.Knowledge...)
	pack.Properties = append([]ProtocolProperty(nil), pack.Properties...)
	pack.IssuePatterns = append([]HistoricalIssuePattern(nil), pack.IssuePatterns...)
	pack.TargetDossier = cloneTargetDossier(pack.TargetDossier)
	pack.Risks = cloneProtocolKnowledgeRisks(pack.Risks)
	sort.Slice(pack.Knowledge, func(i, j int) bool { return pack.Knowledge[i].ID < pack.Knowledge[j].ID })
	sort.Slice(pack.Properties, func(i, j int) bool { return pack.Properties[i].ID < pack.Properties[j].ID })
	sort.Slice(pack.IssuePatterns, func(i, j int) bool {
		return pack.IssuePatterns[i].ID < pack.IssuePatterns[j].ID
	})
	if pack.TargetDossier != nil {
		sortTargetDossier(pack.TargetDossier)
	}
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
	pack.Digest = digest
	return pack, err
}

func validPropertyEvidenceLevel(value string) bool {
	return value == "" || value == PropertyEvidenceHypothesis || value == PropertyEvidenceObservable ||
		value == PropertyEvidenceOracleBacked
}

func validTargetDossier(dossier TargetDossier) bool {
	if dossier.Scope == "" || len(dossier.Scope) > protocolKnowledgeTextMaxBytes {
		return false
	}
	sections := [][]TargetMaterial{
		dossier.Assumptions, dossier.Components, dossier.Contracts, dossier.ControlSemantics,
		dossier.ActiveExperiment, dossier.BlindSpots,
	}
	total := 0
	for _, section := range sections {
		if len(section) > targetMaterialSectionMax || !validTargetMaterials(section) {
			return false
		}
		total += len(section)
	}
	return total > 0
}

func validTargetMaterials(materials []TargetMaterial) bool {
	seen := make(map[string]bool, len(materials))
	for _, material := range materials {
		if !validMethodToken(material.ID) || material.Summary == "" ||
			len(material.Summary) > protocolKnowledgeTextMaxBytes || seen[material.ID] ||
			len(material.EvidenceRefs) > targetMaterialEvidenceMax {
			return false
		}
		seen[material.ID] = true
		seenReferences := make(map[string]bool, len(material.EvidenceRefs))
		for _, reference := range material.EvidenceRefs {
			if reference == "" || len(reference) > targetEvidenceReferenceMaxBytes || seenReferences[reference] {
				return false
			}
			seenReferences[reference] = true
		}
	}
	return true
}

func cloneTargetDossier(dossier *TargetDossier) *TargetDossier {
	if dossier == nil {
		return nil
	}
	cloned := *dossier
	cloned.Assumptions = cloneTargetMaterials(dossier.Assumptions)
	cloned.Components = cloneTargetMaterials(dossier.Components)
	cloned.Contracts = cloneTargetMaterials(dossier.Contracts)
	cloned.ControlSemantics = cloneTargetMaterials(dossier.ControlSemantics)
	cloned.ActiveExperiment = cloneTargetMaterials(dossier.ActiveExperiment)
	cloned.BlindSpots = cloneTargetMaterials(dossier.BlindSpots)
	return &cloned
}

func cloneTargetMaterials(materials []TargetMaterial) []TargetMaterial {
	cloned := append([]TargetMaterial(nil), materials...)
	for index := range cloned {
		cloned[index].EvidenceRefs = append([]string(nil), materials[index].EvidenceRefs...)
	}
	return cloned
}

func sortTargetDossier(dossier *TargetDossier) {
	sections := []*[]TargetMaterial{
		&dossier.Assumptions, &dossier.Components, &dossier.Contracts, &dossier.ControlSemantics,
		&dossier.ActiveExperiment, &dossier.BlindSpots,
	}
	for _, section := range sections {
		sort.Slice(*section, func(i, j int) bool { return (*section)[i].ID < (*section)[j].ID })
	}
}

func cloneProtocolKnowledgeRisks(risks []ProtocolRisk) []ProtocolRisk {
	cloned := append([]ProtocolRisk(nil), risks...)
	for index := range cloned {
		cloned[index].RequiredCapabilities = append([]string(nil), cloned[index].RequiredCapabilities...)
		cloned[index].RequiredActions = append([]control.ActionKind(nil), cloned[index].RequiredActions...)
		cloned[index].AllowedBackendIDs = append([]string(nil), cloned[index].AllowedBackendIDs...)
	}
	return cloned
}

func canonicalKnowledgeTokens(values []string) bool {
	if !canonicalStrings(values, true) {
		return false
	}
	for _, value := range values {
		if !validMethodToken(value) {
			return false
		}
	}
	return true
}
