package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/controlexperiment"
)

const (
	statelessAgentMaxCalls = controlexperiment.ScenarioAgentMaxCalls
)

var errStatelessAgentCallKeyRequired = errors.New("STATELESS_AGENT_CALL_KEY_REQUIRED")

type statelessAgentCallJournal struct {
	directory string
	rootID    string
	client    openRouterIntentClient
	key       string
	transport controlexperiment.AgentTransportFreeze
	next      int
	maxCalls  int
	recovered []statelessAgentRecoveredCall
}

type statelessAgentRecoveredCall struct {
	intent   controlexperiment.StatelessAgentCallIntent
	dispatch *controlexperiment.StatelessAgentCallDispatch
	result   *controlexperiment.StatelessAgentCallResult
}

// planningAgentCallPlan supplies only request-specific semantics to the shared
// durable journal. Provider preparation, dispatch, recovery, identity and work
// accounting remain single-owner infrastructure.
type planningAgentCallPlan struct {
	intentID       string
	requestDigest  string
	prepared       agentPreparedRequest
	contentReady   bool
	rejectionCode  string
	validateOutput func([]byte) (string, error)
}

func (plan planningAgentCallPlan) successStatus() string {
	if plan.contentReady {
		return controlexperiment.StatelessAgentCallContentReady
	}
	return controlexperiment.StatelessAgentCallCompleted
}

func (plan planningAgentCallPlan) validate() error {
	if strings.TrimSpace(plan.intentID) == "" || plan.requestDigest == "" ||
		len(plan.prepared.PromptBytes) == 0 || len(plan.prepared.RequestBytes) == 0 {
		return errors.New("STATELESS_AGENT_CALL_PLAN_INVALID")
	}
	if plan.contentReady {
		if plan.validateOutput != nil || plan.rejectionCode != "" {
			return errors.New("STATELESS_AGENT_CALL_PLAN_INVALID")
		}
		return nil
	}
	if plan.validateOutput == nil || strings.TrimSpace(plan.rejectionCode) == "" {
		return errors.New("STATELESS_AGENT_CALL_PLAN_INVALID")
	}
	return nil
}

func newStatelessAgentCallJournal(
	directory string,
	client openRouterIntentClient,
	key string,
) (*statelessAgentCallJournal, error) {
	clean := filepath.Clean(directory)
	transport := openRouterTransportFreeze(client)
	if directory == "" || clean == "." || clean == string(filepath.Separator) ||
		client.HTTP == nil || transport.Validate() != nil {
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
		directory: clean, client: client, key: strings.TrimSpace(key), transport: transport,
		maxCalls: statelessAgentMaxCalls,
	}, nil
}

func recoverStatelessAgentCallJournal(
	directory string,
	client openRouterIntentClient,
) (*statelessAgentCallJournal, error) {
	clean := filepath.Clean(directory)
	transport := openRouterTransportFreeze(client)
	if directory == "" || clean == "." || clean == string(filepath.Separator) ||
		client.HTTP == nil || transport.Validate() != nil {
		return nil, errors.New("STATELESS_AGENT_CALL_RECOVERY_INPUT_INVALID")
	}
	info, err := os.Lstat(clean)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("STATELESS_AGENT_CALL_RECOVERY_DIRECTORY_INVALID")
	}
	callRoot := filepath.Join(clean, "model-calls")
	callRootInfo, err := os.Lstat(callRoot)
	if err != nil || !callRootInfo.IsDir() || callRootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("STATELESS_AGENT_CALL_RECOVERY_LAYOUT_INVALID")
	}
	entries, err := os.ReadDir(callRoot)
	if err != nil || len(entries) > statelessAgentMaxCalls {
		return nil, errors.New("STATELESS_AGENT_CALL_RECOVERY_LAYOUT_INVALID")
	}
	recovered := make([]statelessAgentRecoveredCall, 0, len(entries))
	for index, entry := range entries {
		ordinal, rootID, ok := parseStatelessAgentCallDirectory(entry.Name())
		if !ok || ordinal != index+1 || entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			return nil, errors.New("STATELESS_AGENT_CALL_RECOVERY_SEQUENCE_INVALID")
		}
		call, err := recoverStatelessAgentCall(
			filepath.Join(callRoot, entry.Name()), ordinal, rootID,
		)
		if err != nil {
			return nil, err
		}
		if index+1 < len(entries) && (call.result == nil || !statelessAgentCallCanContinue(call.result.Status)) {
			return nil, errors.New("STATELESS_AGENT_CALL_RECOVERY_TERMINAL_NOT_LAST")
		}
		recovered = append(recovered, call)
	}
	return &statelessAgentCallJournal{
		directory: clean, client: client, transport: transport,
		maxCalls: statelessAgentMaxCalls, recovered: recovered,
	}, nil
}

func openRouterTransportFreeze(client openRouterIntentClient) controlexperiment.AgentTransportFreeze {
	return controlexperiment.AgentTransportFreeze{
		Provider: openRouterProvider, Endpoint: client.Endpoint, Model: client.Model,
		Thinking: client.ReasoningEffort, ExcludeReasoning: client.ExcludeReasoning,
		Temperature: 0, MaxOutputTokens: client.MaxOutputTokens,
		MaxCallsPerArm: 1, MaxRetries: client.MaxRetries,
	}
}

func statelessAgentCallCanContinue(status string) bool {
	return status == controlexperiment.StatelessAgentCallCompleted ||
		status == controlexperiment.StatelessAgentCallContentReady
}

func recoverStatelessAgentCall(
	directory string,
	ordinal int,
	rootID string,
) (statelessAgentRecoveredCall, error) {
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) == 0 || len(entries) > 3 {
		return statelessAgentRecoveredCall{}, errors.New("STATELESS_AGENT_CALL_RECOVERY_CALL_INVALID")
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() ||
			(entry.Name() != "intent.json" && entry.Name() != "dispatch.json" && entry.Name() != "result.json") {
			return statelessAgentRecoveredCall{}, errors.New("STATELESS_AGENT_CALL_RECOVERY_FILE_INVALID")
		}
		seen[entry.Name()] = true
	}
	if !seen["intent.json"] {
		return statelessAgentRecoveredCall{}, errors.New("STATELESS_AGENT_CALL_RECOVERY_INTENT_MISSING")
	}
	var call statelessAgentRecoveredCall
	if err := readStrictJSONFile(
		filepath.Join(directory, "intent.json"), 128<<10, &call.intent,
	); err != nil || call.intent.Validate() != nil || call.intent.Ordinal != ordinal || call.intent.RootID != rootID {
		return statelessAgentRecoveredCall{}, errors.New("STATELESS_AGENT_CALL_RECOVERY_INTENT_INVALID")
	}
	if seen["dispatch.json"] {
		call.dispatch = new(controlexperiment.StatelessAgentCallDispatch)
		if err := readStrictJSONFile(
			filepath.Join(directory, "dispatch.json"), 16<<10, call.dispatch,
		); err != nil || call.dispatch.ValidateIntent(call.intent) != nil {
			return statelessAgentRecoveredCall{}, errors.New("STATELESS_AGENT_CALL_RECOVERY_DISPATCH_INVALID")
		}
	}
	if seen["result.json"] {
		if call.dispatch == nil {
			return statelessAgentRecoveredCall{}, errors.New("STATELESS_AGENT_CALL_RECOVERY_RESULT_WITHOUT_DISPATCH")
		}
		call.result = new(controlexperiment.StatelessAgentCallResult)
		if err := readStrictJSONFile(
			filepath.Join(directory, "result.json"), 32<<10, call.result,
		); err != nil || call.result.ValidateInputs(call.intent, *call.dispatch) != nil {
			return statelessAgentRecoveredCall{}, errors.New("STATELESS_AGENT_CALL_RECOVERY_RESULT_INVALID")
		}
	}
	return call, nil
}

func parseStatelessAgentCallDirectory(name string) (int, string, bool) {
	parts := strings.SplitN(name, "-", 2)
	if len(parts) != 2 || len(parts[0]) != 3 || parts[1] == "" || filepath.Base(parts[1]) != parts[1] {
		return 0, "", false
	}
	ordinal, err := strconv.Atoi(parts[0])
	return ordinal, parts[1], err == nil && ordinal > 0
}

func (journal *statelessAgentCallJournal) ActivateKey(key string) error {
	if journal == nil || strings.TrimSpace(key) == "" {
		return errors.New("STATELESS_AGENT_CALL_KEY_INVALID")
	}
	journal.key = strings.TrimSpace(key)
	return nil
}

func (journal *statelessAgentCallJournal) ClearKey() {
	if journal != nil {
		journal.key = ""
	}
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

func (journal *statelessAgentCallJournal) Audits() ([]controlexperiment.StatelessAgentCallAudit, error) {
	if journal == nil {
		return nil, errors.New("STATELESS_AGENT_CALL_JOURNAL_INVALID")
	}
	audits := make([]controlexperiment.StatelessAgentCallAudit, 0, len(journal.recovered))
	for _, recovered := range journal.recovered {
		audit, err := controlexperiment.NewStatelessAgentCallAudit(
			recovered.intent, recovered.dispatch, recovered.result,
		)
		if err != nil {
			return nil, err
		}
		audits = append(audits, audit)
	}
	return audits, nil
}

func (journal *statelessAgentCallJournal) planningCall(
	ctx context.Context,
	plan planningAgentCallPlan,
) ([]byte, controlexperiment.ModelWork, error) {
	if journal == nil || journal.rootID == "" || journal.next >= journal.maxCalls {
		return nil, controlexperiment.ModelWork{}, errors.New("STATELESS_AGENT_CALL_BUDGET_EXHAUSTED")
	}
	if err := plan.validate(); err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	ordinal := journal.next + 1
	intent, err := controlexperiment.NewPlanningAgentCallIntent(
		plan.intentID, ordinal, journal.rootID, plan.requestDigest,
		journal.transport, plan.prepared.PromptBytes, plan.prepared.RequestBytes,
	)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	if ordinal <= len(journal.recovered) {
		recovered := journal.recovered[ordinal-1]
		if recovered.intent.Digest != intent.Digest {
			return nil, controlexperiment.ModelWork{}, errors.New("STATELESS_AGENT_CALL_RECOVERY_REQUEST_DRIFT")
		}
		if recovered.result != nil {
			journal.next = ordinal
			if recovered.result.Status != plan.successStatus() {
				return nil, recovered.result.Work, errors.New("STATELESS_AGENT_CALL_RECOVERED_TERMINAL_FAILURE")
			}
			return append([]byte(nil), recovered.result.Content...), recovered.result.Work, nil
		}
		if recovered.dispatch != nil {
			journal.next = ordinal
			return nil, controlexperiment.ModelWork{Calls: 1},
				errors.New("STATELESS_AGENT_CALL_RECOVERED_AMBIGUOUS")
		}
		if journal.key == "" {
			return nil, controlexperiment.ModelWork{}, errStatelessAgentCallKeyRequired
		}
		return journal.dispatch(ctx, ordinal, recovered.intent, plan,
			filepath.Join(journal.directory, "model-calls", fmt.Sprintf("%03d-%s", ordinal, journal.rootID)))
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
	if journal.key == "" {
		journal.recovered = append(journal.recovered, statelessAgentRecoveredCall{intent: intent})
		return nil, controlexperiment.ModelWork{}, errStatelessAgentCallKeyRequired
	}
	return journal.dispatch(ctx, ordinal, intent, plan, callDirectory)
}

func (journal *statelessAgentCallJournal) dispatch(
	ctx context.Context,
	ordinal int,
	intent controlexperiment.StatelessAgentCallIntent,
	plan planningAgentCallPlan,
	callDirectory string,
) ([]byte, controlexperiment.ModelWork, error) {
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
	activeKey := journal.key
	journal.key = ""
	call, transportErr := journal.client.invokePrepared(ctx, activeKey, plan.prepared)
	result := controlexperiment.StatelessAgentCallResult{
		Status:  plan.successStatus(),
		Content: call.Content, ResponseDigest: call.ResponseDigest, Response: call.Response,
		DurationMillis: call.DurationMillis, Work: call.Work,
		TransportAttempts: call.TransportAttempts,
	}
	var terminalErr error
	if transportErr != nil || call.FailureCode != "" {
		result.Status = controlexperiment.StatelessAgentCallFailed
		result.FailureCode = "agent-transport-failed"
		if transportErr == nil && call.FailureCode == agentFailureHTTP {
			result.FailureCode = "agent-http-status-rejected"
		} else if transportErr == nil && call.FailureCode != agentFailureTransport {
			result.FailureCode = "agent-response-rejected"
		}
		result.Content, result.Response = nil, nil
		terminalErr = errors.New(result.FailureCode)
	} else if !plan.contentReady {
		proposalDigest, parseErr := plan.validateOutput(call.Content)
		if parseErr != nil {
			result.Status = controlexperiment.StatelessAgentCallRejected
			result.FailureCode = plan.rejectionCode
			terminalErr = parseErr
		} else {
			result.ProposalDigest = proposalDigest
		}
	}
	result, err = controlexperiment.NewStatelessAgentCallResult(intent, dispatch, result)
	if err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	if err := writeStatelessAgentJSON(callDirectory, "result.json", result); err != nil {
		return nil, controlexperiment.ModelWork{}, err
	}
	recoveredCall := statelessAgentRecoveredCall{intent: intent, dispatch: &dispatch, result: &result}
	if ordinal <= len(journal.recovered) {
		journal.recovered[ordinal-1] = recoveredCall
	} else {
		journal.recovered = append(journal.recovered, recoveredCall)
	}
	if terminalErr != nil {
		return nil, result.Work, terminalErr
	}
	return append([]byte(nil), result.Content...), result.Work, nil
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
