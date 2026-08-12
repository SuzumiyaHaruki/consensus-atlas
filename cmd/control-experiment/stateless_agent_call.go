package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	statelessAgentPromptVersion = "stateless-frontier-permutation-v1"
	statelessAgentMaxCalls      = 6
)

type statelessAgentCallJournal struct {
	directory string
	rootID    string
	client    deepSeekIntentClient
	key       string
	transport controlexperiment.AgentTransportFreeze
	next      int
}

func newStatelessAgentCallJournal(
	directory string,
	client deepSeekIntentClient,
	key string,
) (*statelessAgentCallJournal, error) {
	clean := filepath.Clean(directory)
	transport := controlexperiment.AgentTransportFreeze{
		Provider: deepSeekProvider, Endpoint: client.Endpoint, Model: client.Model,
		Thinking: "disabled", Temperature: 0, MaxOutputTokens: client.MaxOutputTokens,
		MaxCallsPerArm: 1, MaxRetries: 0,
	}
	if directory == "" || clean == "." || clean == string(filepath.Separator) ||
		client.HTTP == nil || strings.TrimSpace(key) == "" || transport.Validate() != nil {
		return nil, errors.New("STATELESS_AGENT_CALL_JOURNAL_INPUT_INVALID")
	}
	if err := os.MkdirAll(filepath.Dir(clean), 0o700); err != nil {
		return nil, err
	}
	if err := os.Mkdir(clean, 0o700); err != nil {
		if os.IsExist(err) {
			return nil, errors.New("STATELESS_AGENT_CALL_JOURNAL_DIRECTORY_NOT_NEW")
		}
		return nil, err
	}
	if err := syncStatelessAgentDirectory(filepath.Dir(clean)); err != nil {
		return nil, err
	}
	if err := os.Mkdir(filepath.Join(clean, "model-calls"), 0o700); err != nil {
		return nil, err
	}
	if err := syncStatelessAgentDirectory(clean); err != nil {
		return nil, err
	}
	return &statelessAgentCallJournal{
		directory: clean, client: client, key: key, transport: transport,
	}, nil
}

func (journal *statelessAgentCallJournal) SetRoot(rootID string) error {
	if journal == nil || strings.TrimSpace(rootID) == "" {
		return errors.New("STATELESS_AGENT_CALL_ROOT_INVALID")
	}
	journal.rootID = rootID
	return nil
}

func (journal *statelessAgentCallJournal) Calls() int {
	if journal == nil {
		return 0
	}
	return journal.next
}

func (journal *statelessAgentCallJournal) Planner(
	ctx context.Context,
	view controlexperiment.StatelessSearchAgentView,
) ([]byte, controlexperiment.ModelWork, error) {
	if journal == nil || journal.rootID == "" || journal.next >= statelessAgentMaxCalls {
		return nil, controlexperiment.ModelWork{}, errors.New("STATELESS_AGENT_CALL_BUDGET_EXHAUSTED")
	}
	system, user, err := statelessAgentFrontierPrompt(view)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	prepared, err := journal.client.prepare(system, user)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	ordinal := journal.next + 1
	intent, err := controlexperiment.NewStatelessAgentCallIntent(
		fmt.Sprintf("etcdraft-m5-23g-call-%d", ordinal), ordinal, journal.rootID,
		view.Request, journal.transport, prepared.PromptBytes, prepared.RequestBytes,
	)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	callDirectory := filepath.Join(
		journal.directory, "model-calls", fmt.Sprintf("%03d-%s", ordinal, journal.rootID),
	)
	if err := os.Mkdir(callDirectory, 0o700); err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	if err := syncStatelessAgentDirectory(filepath.Dir(callDirectory)); err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	if err := writeStatelessAgentJSON(callDirectory, "intent.json", intent); err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	dispatch, err := controlexperiment.NewStatelessAgentCallDispatch(intent)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	if err := writeStatelessAgentJSON(callDirectory, "dispatch.json", dispatch); err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	// The ordinal is consumed as soon as dispatch becomes durable. No caller can
	// repeat an ambiguous or rejected provider call through this journal.
	journal.next = ordinal
	call, transportErr := journal.client.invokePrepared(ctx, journal.key, prepared)
	result := controlexperiment.StatelessAgentCallResult{
		Status:  controlexperiment.StatelessAgentCallCompleted,
		Content: call.Content, ResponseDigest: call.ResponseDigest, Response: call.Response,
		DurationMillis: call.DurationMillis, Work: call.Work,
	}
	var terminalErr error
	if transportErr != nil || call.FailureCode != "" {
		result.Status = controlexperiment.StatelessAgentCallFailed
		result.FailureCode = "agent-transport-failed"
		if transportErr == nil && call.FailureCode != deepSeekFailureTransport {
			result.FailureCode = "agent-response-rejected"
		}
		result.Content, result.Response = nil, nil
		terminalErr = errors.New(result.FailureCode)
	} else {
		proposal, parseErr := controlexperiment.ParseStatelessFrontierOrderProposal(call.Content)
		if parseErr == nil {
			_, parseErr = controlexperiment.ValidateStatelessFrontierOrderProposal(view.Request, proposal)
		}
		if parseErr != nil {
			result.Status = controlexperiment.StatelessAgentCallRejected
			result.FailureCode = "frontier-proposal-rejected"
			terminalErr = parseErr
		} else {
			result.ProposalDigest = proposal.Digest
		}
	}
	result, err = controlexperiment.NewStatelessAgentCallResult(intent, dispatch, result)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	if err := writeStatelessAgentJSON(callDirectory, "result.json", result); err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	if terminalErr != nil {
		return nil, result.Work, terminalErr
	}
	return append([]byte(nil), result.Content...), result.Work, nil
}

func statelessAgentFrontierPrompt(
	view controlexperiment.StatelessSearchAgentView,
) (string, string, error) {
	if view.Knowledge.Validate() != nil || view.Request.Validate() != nil ||
		view.Request.KnowledgeDigest != view.Knowledge.Digest {
		return "", "", errors.New("STATELESS_AGENT_PROMPT_VIEW_INVALID")
	}
	actionIDs := make([]string, 0, len(view.Request.Frontier.Actions))
	for _, action := range view.Request.Frontier.Actions {
		actionIDs = append(actionIDs, string(action.ActionID))
	}
	template := struct {
		SchemaVersion string   `json:"schema_version"`
		ID            string   `json:"id"`
		RequestDigest string   `json:"request_digest"`
		ViewDigest    string   `json:"view_digest"`
		ActionIDs     []string `json:"action_ids"`
		Digest        string   `json:"digest"`
	}{
		SchemaVersion: controlexperiment.StatelessFrontierOrderProposalVersion,
		ID:            view.Request.ID, RequestDigest: view.Request.Digest,
		ViewDigest: view.Request.Frontier.Digest, ActionIDs: actionIDs,
	}
	input := struct {
		PromptVersion    string                                     `json:"prompt_version"`
		ProposalTemplate any                                        `json:"proposal_template"`
		AgentView        controlexperiment.StatelessSearchAgentView `json:"agent_view"`
	}{statelessAgentPromptVersion, template, view}
	encoded, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return "", "", err
	}
	system := "Return exactly one JSON object and no prose. Copy proposal_template exactly, changing only action_ids. " +
		"action_ids must contain every supplied Action ID exactly once, but may be reordered. Leave digest empty. " +
		"Do not add, remove, invent, or edit an Action ID or any other field."
	user := "Order the current trusted frontier using only the supplied protocol knowledge and completed-history summaries. " +
		"A later trusted validator will reject any authority expansion. Frozen input JSON:\n" + string(encoded)
	return system, user, nil
}

func writeStatelessAgentJSON(directory string, name string, value any) error {
	if filepath.Base(name) != name || name == "" || name == "." {
		return errors.New("STATELESS_AGENT_ARTIFACT_NAME_INVALID")
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(directory, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return syncStatelessAgentDirectory(directory)
}

func syncStatelessAgentDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Sync()
}
