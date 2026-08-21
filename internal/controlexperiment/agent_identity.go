package controlexperiment

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// AgentResponseIdentity records only non-secret provider metadata. Exact
// response bytes are represented separately by a digest.
type AgentResponseIdentity struct {
	ID                string `json:"id"`
	Model             string `json:"model"`
	FinishReason      string `json:"finish_reason"`
	SystemFingerprint string `json:"system_fingerprint,omitempty"`
}

func (identity AgentResponseIdentity) Validate() error {
	if identity.ID == "" || identity.ID != strings.TrimSpace(identity.ID) ||
		identity.Model == "" || identity.Model != strings.TrimSpace(identity.Model) ||
		identity.FinishReason == "" || identity.FinishReason != strings.TrimSpace(identity.FinishReason) ||
		strings.ContainsAny(identity.ID, "\r\n\x00") ||
		strings.ContainsAny(identity.Model, "\r\n\x00") ||
		strings.ContainsAny(identity.FinishReason, "\r\n\x00") ||
		identity.SystemFingerprint != strings.TrimSpace(identity.SystemFingerprint) ||
		strings.ContainsAny(identity.SystemFingerprint, "\r\n\x00") {
		return errors.New("AGENT_RESPONSE_IDENTITY_INVALID")
	}
	return nil
}

// AgentInvocationDigest hashes exact transport bytes without persisting a
// credential or granting any scheduling authority.
func AgentInvocationDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
