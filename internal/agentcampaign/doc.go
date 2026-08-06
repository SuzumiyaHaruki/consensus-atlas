// Package agentcampaign separates untrusted blind test-plan proposals from
// trusted Campaign execution. Only opaque target references and the frozen,
// Driver-declared input vocabulary cross this boundary; the Coordinator alone
// resolves targets and accepts deterministic execution evidence.
package agentcampaign
