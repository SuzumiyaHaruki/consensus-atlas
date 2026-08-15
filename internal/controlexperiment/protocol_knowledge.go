package controlexperiment

import (
	"errors"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const (
	ProtocolKnowledgePackVersion   = "consensus-atlas/protocol-knowledge-pack/v1"
	protocolKnowledgeTextMaxBytes  = 2048
	agentMaterialReferenceMaxBytes = 128
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
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

// HistoricalIssuePattern gives the Agent cross-protocol implementation
// experience without embedding a target-specific reproduction schedule.
type HistoricalIssuePattern struct {
	ID        string `json:"id"`
	Summary   string `json:"summary"`
	Mechanism string `json:"mechanism"`
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
			len(pattern.Mechanism) > protocolKnowledgeTextMaxBytes || seenPatterns[pattern.ID] {
			return errors.New("EXPERIMENT_HISTORICAL_ISSUE_PATTERN_INVALID")
		}
		seenPatterns[pattern.ID] = true
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
	if pack.Validate() != nil || len(pack.Properties) == 0 || len(pack.IssuePatterns) == 0 || len(pack.Risks) != 0 {
		return errors.New("EXPERIMENT_AGENT_MATERIALS_INVALID")
	}
	return nil
}

func (pack ProtocolKnowledgePack) seal() (ProtocolKnowledgePack, error) {
	pack.Knowledge = append([]KnowledgeStatement(nil), pack.Knowledge...)
	pack.Properties = append([]ProtocolProperty(nil), pack.Properties...)
	pack.IssuePatterns = append([]HistoricalIssuePattern(nil), pack.IssuePatterns...)
	pack.Risks = cloneProtocolKnowledgeRisks(pack.Risks)
	sort.Slice(pack.Knowledge, func(i, j int) bool { return pack.Knowledge[i].ID < pack.Knowledge[j].ID })
	sort.Slice(pack.Properties, func(i, j int) bool { return pack.Properties[i].ID < pack.Properties[j].ID })
	sort.Slice(pack.IssuePatterns, func(i, j int) bool {
		return pack.IssuePatterns[i].ID < pack.IssuePatterns[j].ID
	})
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
