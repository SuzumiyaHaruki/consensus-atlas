package controlexperiment

import (
	"crypto/sha256"
	"encoding/hex"
)

// AgentResponseIdentity records only non-secret provider metadata. Exact
// response bytes are represented separately by a digest.
type AgentResponseIdentity struct {
	ID                string `json:"id"`
	Model             string `json:"model"`
	FinishReason      string `json:"finish_reason"`
	SystemFingerprint string `json:"system_fingerprint,omitempty"`
}

// AgentInvocationDigest hashes exact transport bytes without persisting a
// credential or granting any scheduling authority.
func AgentInvocationDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
