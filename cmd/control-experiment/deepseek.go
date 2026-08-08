package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/modelcommand"
)

const deepSeekPlannerProtocolVersion = 1

type deepSeekPlannerConfig struct {
	RepoRoot string
	KeyPath  string
	Model    string
	Endpoint string
}

type deepSeekScopeView struct {
	Digest          string `json:"digest"`
	Runs            []int  `json:"runs"`
	DecisionsPerRun int    `json:"decisions_per_run"`
	RequireReplay   bool   `json:"require_replay"`
}

type deepSeekTargetView struct {
	Description string   `json:"description"`
	Nodes       []string `json:"nodes"`
}

type deepSeekPlannerRequest struct {
	ProposalSchemaVersion string               `json:"proposal_schema_version"`
	Scope                 deepSeekScopeView    `json:"scope"`
	Target                deepSeekTargetView   `json:"target"`
	ActionKinds           []control.ActionKind `json:"action_kinds"`
}

type deepSeekPlannerResponse struct {
	Version  int                                 `json:"version"`
	Proposal json.RawMessage                     `json:"proposal"`
	Audit    controlexperiment.PlannerModelAudit `json:"audit"`
}

func generateDeepSeekPlannerProposal(
	ctx context.Context,
	config deepSeekPlannerConfig,
	scope controlexperiment.PlannerScope,
) (controlexperiment.PlannerProposal, controlexperiment.PlannerModelAudit, error) {
	if config.RepoRoot == "" || config.Model == "" || !officialDeepSeekEndpoint(config.Endpoint) {
		return controlexperiment.PlannerProposal{}, controlexperiment.PlannerModelAudit{},
			errors.New("DEEPSEEK_PLANNER_CONFIG_INVALID")
	}
	if err := modelcommand.ValidateSecretFile(config.KeyPath); err != nil {
		return controlexperiment.PlannerProposal{}, controlexperiment.PlannerModelAudit{}, err
	}
	request, err := deepSeekRequest(scope)
	if err != nil {
		return controlexperiment.PlannerProposal{}, controlexperiment.PlannerModelAudit{}, err
	}
	encoded, err := json.Marshal(struct {
		Version int                    `json:"version"`
		Request deepSeekPlannerRequest `json:"request"`
	}{Version: deepSeekPlannerProtocolVersion, Request: request})
	if err != nil {
		return controlexperiment.PlannerProposal{}, controlexperiment.PlannerModelAudit{}, err
	}
	encoded = append(encoded, '\n')
	python, err := exec.LookPath("python3")
	if err != nil {
		return controlexperiment.PlannerProposal{}, controlexperiment.PlannerModelAudit{}, err
	}
	stdout, err := modelcommand.Run(ctx, modelcommand.Config{
		Path: python, Args: []string{filepath.Join(config.RepoRoot, "agents", "deepseek_control_planner.py")},
		Dir: config.RepoRoot, Env: []string{
			"CONSENSUS_ATLAS_DEEPSEEK_KEY_FILE=" + config.KeyPath,
			"CONSENSUS_ATLAS_DEEPSEEK_MODEL=" + config.Model,
			"CONSENSUS_ATLAS_DEEPSEEK_ENDPOINT=" + config.Endpoint,
		},
		StdoutLimit: 1 << 20, StderrLimit: 64 << 10,
	}, encoded)
	if err != nil {
		return controlexperiment.PlannerProposal{}, controlexperiment.PlannerModelAudit{}, err
	}
	return decodeDeepSeekPlannerResponse(encoded, stdout, config)
}

func deepSeekRequest(scope controlexperiment.PlannerScope) (deepSeekPlannerRequest, error) {
	digest, err := scope.Digest()
	if err != nil {
		return deepSeekPlannerRequest{}, err
	}
	return deepSeekPlannerRequest{
		ProposalSchemaVersion: controlexperiment.PlannerProposalVersion,
		Scope: deepSeekScopeView{
			Digest: digest, Runs: append([]int(nil), scope.Runs...),
			DecisionsPerRun: scope.DecisionsPerRun, RequireReplay: scope.RequireReplay,
		},
		Target: deepSeekTargetView{
			Description: "three-node etcd/raft cluster controlled through protocol-neutral Actions",
			Nodes:       []string{"n1", "n2", "n3"},
		},
		ActionKinds: []control.ActionKind{
			control.ActionCompleteEffect, control.ActionDeliverMessage, control.ActionDropMessage,
			control.ActionDuplicateMessage, control.ActionFireTemporal, control.ActionCrash, control.ActionRestart,
		},
	}, nil
}

func decodeDeepSeekPlannerResponse(
	request []byte,
	response []byte,
	config deepSeekPlannerConfig,
) (controlexperiment.PlannerProposal, controlexperiment.PlannerModelAudit, error) {
	decoder := json.NewDecoder(bytes.NewReader(response))
	decoder.DisallowUnknownFields()
	var envelope deepSeekPlannerResponse
	if err := decoder.Decode(&envelope); err != nil {
		return controlexperiment.PlannerProposal{}, controlexperiment.PlannerModelAudit{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return controlexperiment.PlannerProposal{}, controlexperiment.PlannerModelAudit{},
			errors.New("DEEPSEEK_PLANNER_TRAILING_JSON")
	}
	if envelope.Version != deepSeekPlannerProtocolVersion {
		return controlexperiment.PlannerProposal{}, controlexperiment.PlannerModelAudit{},
			errors.New("DEEPSEEK_PLANNER_PROTOCOL_MISMATCH")
	}
	proposal, err := controlexperiment.DecodePlannerProposal(envelope.Proposal)
	if err != nil {
		return controlexperiment.PlannerProposal{}, controlexperiment.PlannerModelAudit{}, err
	}
	envelope.Audit.RequestDigest = byteDigest(request)
	envelope.Audit.ResponseDigest = byteDigest(response)
	if envelope.Audit.Provider != "deepseek" || envelope.Audit.RequestedModel != config.Model ||
		envelope.Audit.Endpoint != config.Endpoint {
		return controlexperiment.PlannerProposal{}, controlexperiment.PlannerModelAudit{},
			errors.New("DEEPSEEK_PLANNER_AUDIT_IDENTITY_MISMATCH")
	}
	if err := envelope.Audit.Validate(); err != nil {
		return controlexperiment.PlannerProposal{}, controlexperiment.PlannerModelAudit{}, err
	}
	return proposal, envelope.Audit, nil
}

func officialDeepSeekEndpoint(endpoint string) bool {
	return endpoint == "https://api.deepseek.com/chat/completions" ||
		endpoint == "https://api.deepseek.com/v1/chat/completions"
}

func byteDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
