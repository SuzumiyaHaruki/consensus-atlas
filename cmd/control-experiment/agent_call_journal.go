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

const (
	statelessAgentFailureTransport      = "agent-transport-ambiguous"
	statelessAgentFailureHTTP           = "agent-http-status-rejected"
	statelessAgentFailureFinishLength   = "response-finish-length"
	statelessAgentFailureEmptyContent   = "response-empty-content"
	statelessAgentFailureMalformed      = "response-malformed"
	statelessAgentFailureResponseTooBig = "response-too-large"
)

type statelessAgentTerminalFailure struct{ Code string }

func (failure *statelessAgentTerminalFailure) Error() string {
	if failure == nil {
		return "STATELESS_AGENT_CALL_TERMINAL_FAILURE"
	}
	return failure.Code
}

func statelessAgentFailure(code string) error {
	return &statelessAgentTerminalFailure{Code: code}
}

func statelessAgentFailureCode(err error) string {
	var failure *statelessAgentTerminalFailure
	if errors.As(err, &failure) {
		return failure.Code
	}
	return ""
}

type statelessAgentCallJournal struct {
	directory string
	rootID    string
	client    agentIntentTransport
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
	client agentIntentTransport,
	key string,
) (*statelessAgentCallJournal, error) {
	clean := filepath.Clean(directory)
	if client == nil {
		return nil, errors.New("STATELESS_AGENT_CALL_JOURNAL_INPUT_INVALID")
	}
	transport := client.freeze()
	if directory == "" || clean == "." || clean == string(filepath.Separator) ||
		!client.ready() || transport.Validate() != nil {
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
	client agentIntentTransport,
) (*statelessAgentCallJournal, error) {
	clean := filepath.Clean(directory)
	if client == nil {
		return nil, errors.New("STATELESS_AGENT_CALL_RECOVERY_INPUT_INVALID")
	}
	transport := client.freeze()
	if directory == "" || clean == "." || clean == string(filepath.Separator) ||
		!client.ready() || transport.Validate() != nil {
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
		recovered = append(recovered, call)
	}
	for index := 0; index+1 < len(recovered); index++ {
		if !statelessAgentCallSequenceCanContinue(recovered[index], recovered[index+1]) {
			return nil, errors.New("STATELESS_AGENT_CALL_RECOVERY_TERMINAL_NOT_LAST")
		}
	}
	if !validStatelessRepairSequence(recovered) {
		return nil, errors.New("STATELESS_AGENT_CALL_RECOVERY_REPAIR_SEQUENCE_INVALID")
	}
	return &statelessAgentCallJournal{
		directory: clean, client: client, transport: transport,
		maxCalls: statelessAgentMaxCalls, recovered: recovered,
	}, nil
}

func statelessAgentCallCanContinue(status string) bool {
	return status == controlexperiment.StatelessAgentCallCompleted ||
		status == controlexperiment.StatelessAgentCallContentReady
}

// statelessAgentCallSequenceCanContinue keeps the generic journal terminal by
// default. The only failed result that may have a successor is an exact
// Scenario or Risk length-repair pair with adjacent ordinals and an identity
// bound to the failed provider call.
func statelessAgentCallSequenceCanContinue(
	current statelessAgentRecoveredCall,
	next statelessAgentRecoveredCall,
) bool {
	if current.result != nil && statelessAgentCallCanContinue(current.result.Status) {
		_, _, _, nextIsRepair := parseScenarioRepairIntentID(next.intent.ID)
		_, _, _, nextIsRiskRepair := parseRiskRepairIntentID(next.intent.ID)
		return !nextIsRepair && !nextIsRiskRepair
	}
	if current.result == nil || current.result.Status != controlexperiment.StatelessAgentCallFailed ||
		current.result.FailureCode != statelessAgentFailureFinishLength ||
		current.intent.RootID != next.intent.RootID ||
		current.intent.Ordinal+1 != next.intent.Ordinal {
		return false
	}
	callOrdinal, callBinding, callOK := parseScenarioCallIntentID(current.intent.ID)
	repairOrdinal, repairFrom, repairBinding, repairOK := parseScenarioRepairIntentID(next.intent.ID)
	if callOK && repairOK && current.intent.SearchRequestDigest == next.intent.SearchRequestDigest &&
		callOrdinal == current.intent.Ordinal &&
		repairOrdinal == next.intent.Ordinal && repairFrom == current.intent.Ordinal &&
		callBinding == repairBinding && callBinding == current.intent.SearchRequestDigest {
		return true
	}
	riskCallOrdinal, riskCallOK := parseRiskCallIntentID(current.intent.ID)
	riskRepairOrdinal, riskRepairFrom, sourceIntentDigest, riskRepairOK :=
		parseRiskRepairIntentID(next.intent.ID)
	return riskCallOK && riskRepairOK && riskCallOrdinal == current.intent.Ordinal &&
		riskRepairOrdinal == next.intent.Ordinal && riskRepairFrom == current.intent.Ordinal &&
		sourceIntentDigest == current.intent.Digest
}

func validStatelessRepairSequence(calls []statelessAgentRecoveredCall) bool {
	repairs := 0
	for index, call := range calls {
		_, _, _, repair := parseScenarioRepairIntentID(call.intent.ID)
		_, _, _, riskRepair := parseRiskRepairIntentID(call.intent.ID)
		if !repair && !riskRepair {
			continue
		}
		repairs++
		if repairs > 1 || index == 0 ||
			!statelessAgentCallSequenceCanContinue(calls[index-1], call) {
			return false
		}
	}
	return true
}

func parseRiskCallIntentID(value string) (int, bool) {
	const prefix = "risk-agent-call-"
	if !strings.HasPrefix(value, prefix) {
		return 0, false
	}
	ordinal, err := strconv.Atoi(strings.TrimPrefix(value, prefix))
	return ordinal, err == nil && ordinal > 0
}

func parseRiskRepairIntentID(value string) (int, int, string, bool) {
	const prefix = "risk-agent-repair-call-"
	const fromSeparator = "-from-"
	const intentSeparator = "-intent-"
	if !strings.HasPrefix(value, prefix) {
		return 0, 0, "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(value, prefix), fromSeparator, 2)
	if len(parts) != 2 {
		return 0, 0, "", false
	}
	remainder := strings.SplitN(parts[1], intentSeparator, 2)
	if len(remainder) != 2 || !validAgenticSHA256(remainder[1]) {
		return 0, 0, "", false
	}
	ordinal, ordinalErr := strconv.Atoi(parts[0])
	from, fromErr := strconv.Atoi(remainder[0])
	return ordinal, from, remainder[1], ordinalErr == nil && fromErr == nil &&
		ordinal > 0 && from > 0
}

func parseScenarioCallIntentID(value string) (int, string, bool) {
	const prefix = "scenario-agent-call-"
	const separator = "-binding-"
	if !strings.HasPrefix(value, prefix) {
		return 0, "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(value, prefix), separator, 2)
	if len(parts) != 2 || !validAgenticSHA256(parts[1]) {
		return 0, "", false
	}
	ordinal, err := strconv.Atoi(parts[0])
	return ordinal, parts[1], err == nil && ordinal > 0
}

func parseScenarioRepairIntentID(value string) (int, int, string, bool) {
	const prefix = "scenario-agent-repair-call-"
	const fromSeparator = "-from-"
	const bindingSeparator = "-binding-"
	if !strings.HasPrefix(value, prefix) {
		return 0, 0, "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(value, prefix), fromSeparator, 2)
	if len(parts) != 2 {
		return 0, 0, "", false
	}
	remainder := strings.SplitN(parts[1], bindingSeparator, 2)
	if len(remainder) != 2 || !validAgenticSHA256(remainder[1]) {
		return 0, 0, "", false
	}
	ordinal, ordinalErr := strconv.Atoi(parts[0])
	from, fromErr := strconv.Atoi(remainder[0])
	return ordinal, from, remainder[1], ordinalErr == nil && fromErr == nil &&
		ordinal > 0 && from > 0
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
			filepath.Join(directory, "result.json"), 2<<20, call.result,
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
				return nil, recovered.result.Work, statelessAgentFailure(recovered.result.FailureCode)
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
		TransportAttempts: call.TransportAttempts, ProviderUsageStatus: call.UsageStatus,
	}
	var terminalErr error
	if transportErr != nil || call.FailureCode != "" {
		result.Status = controlexperiment.StatelessAgentCallFailed
		result.FailureCode = statelessAgentFailureTransport
		if transportErr == nil && call.FailureCode == agentFailureHTTP {
			result.FailureCode = statelessAgentFailureHTTP
		} else if transportErr == nil {
			switch call.FailureCode {
			case agentFailureResponseFinishLength:
				result.FailureCode = statelessAgentFailureFinishLength
			case agentFailureResponseEmptyContent:
				result.FailureCode = statelessAgentFailureEmptyContent
			case agentFailureResponseTooLarge:
				result.FailureCode = statelessAgentFailureResponseTooBig
			case agentFailureResponseMalformed:
				result.FailureCode = statelessAgentFailureMalformed
			default:
				result.FailureCode = statelessAgentFailureMalformed
			}
		}
		result.Content = nil
		terminalErr = statelessAgentFailure(result.FailureCode)
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
