// Package migration compares externally visible results while an execution
// path is replaced. It does not require the old and new runtimes to share
// event types, scheduling steps, trace schemas, or replay mechanisms.
package migration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

const (
	SummarySchemaVersion = "consensus-atlas/migration-summary/v1"
	CaseSchemaVersion    = "consensus-atlas/migration-case/v1"
	SuiteSchemaVersion   = "consensus-atlas/migration-suite/v1"

	StatusPassed   = "passed"
	StatusMismatch = "mismatch"
	StatusDeferred = "deferred"
)

type Command struct {
	Ordinal      int      `json:"ordinal"`
	ValueDigest  string   `json:"value_digest"`
	AppliedNodes []string `json:"applied_nodes"`
}

type Safety struct {
	AgreementViolations      int `json:"agreement_violations"`
	TraceIntegrityViolations int `json:"trace_integrity_violations"`
}

type Replay struct {
	Mode   string `json:"mode"`
	Stable bool   `json:"stable"`
}

type Summary struct {
	SchemaVersion    string    `json:"schema_version"`
	ScenarioID       string    `json:"scenario_id"`
	PathID           string    `json:"path_id"`
	ImplementationID string    `json:"implementation_id"`
	ConfigurationID  string    `json:"configuration_id"`
	Commands         []Command `json:"commands,omitempty"`
	Safety           Safety    `json:"safety"`
	Replay           Replay    `json:"replay"`
	Witnesses        []string  `json:"witnesses,omitempty"`
	Digest           string    `json:"digest"`
}

func SealSummary(summary Summary) (Summary, error) {
	summary.SchemaVersion = SummarySchemaVersion
	for index := range summary.Commands {
		sort.Strings(summary.Commands[index].AppliedNodes)
	}
	sort.Strings(summary.Witnesses)
	summary.Digest = ""
	if err := validateSummaryBody(summary); err != nil {
		return Summary{}, err
	}
	digest, err := canonicalDigest(summary)
	if err != nil {
		return Summary{}, err
	}
	summary.Digest = digest
	return summary, nil
}

func (summary Summary) Validate() error {
	if summary.SchemaVersion != SummarySchemaVersion || summary.Digest == "" {
		return errors.New("MIGRATION_SUMMARY_IDENTITY_INVALID")
	}
	if err := validateSummaryBody(summary); err != nil {
		return err
	}
	sealed, err := SealSummary(summary)
	if err != nil {
		return err
	}
	if sealed.Digest != summary.Digest {
		return errors.New("MIGRATION_SUMMARY_DIGEST_MISMATCH")
	}
	return nil
}

func validateSummaryBody(summary Summary) error {
	if summary.ScenarioID == "" || summary.PathID == "" || summary.ImplementationID == "" ||
		summary.ConfigurationID == "" || summary.Replay.Mode == "" {
		return errors.New("MIGRATION_SUMMARY_FIELD_REQUIRED")
	}
	if summary.Safety.AgreementViolations < 0 || summary.Safety.TraceIntegrityViolations < 0 {
		return errors.New("MIGRATION_SUMMARY_SAFETY_COUNT_INVALID")
	}
	if !sortedUnique(summary.Witnesses) {
		return errors.New("MIGRATION_SUMMARY_WITNESSES_NOT_CANONICAL")
	}
	for index, command := range summary.Commands {
		if command.Ordinal != index+1 || !isDigest(command.ValueDigest) || len(command.AppliedNodes) == 0 ||
			!sortedUnique(command.AppliedNodes) {
			return fmt.Errorf("MIGRATION_SUMMARY_COMMAND_INVALID: %d", index+1)
		}
	}
	return nil
}

type Case struct {
	SchemaVersion string   `json:"schema_version"`
	ScenarioID    string   `json:"scenario_id"`
	Status        string   `json:"status"`
	Left          *Summary `json:"left,omitempty"`
	Right         *Summary `json:"right,omitempty"`
	ReasonCodes   []string `json:"reason_codes,omitempty"`
	Digest        string   `json:"digest"`
}

type Expectation struct {
	Commands  []Command
	Witnesses []string
}

func Compare(left, right Summary) (Case, error) {
	return CompareExpected(left, right, Expectation{})
}

func CompareExpected(left, right Summary, expected Expectation) (Case, error) {
	if err := left.Validate(); err != nil {
		return Case{}, err
	}
	if err := right.Validate(); err != nil {
		return Case{}, err
	}
	caseReport := Case{
		SchemaVersion: CaseSchemaVersion, ScenarioID: left.ScenarioID,
		Left: cloneSummary(left), Right: cloneSummary(right),
	}
	if left.ScenarioID != right.ScenarioID {
		caseReport.ReasonCodes = append(caseReport.ReasonCodes, "MIGRATION_SCENARIO_ID_MISMATCH")
	}
	if !equalCommands(left.Commands, right.Commands) {
		caseReport.ReasonCodes = append(caseReport.ReasonCodes, "MIGRATION_COMMAND_RESULT_MISMATCH")
	}
	if !equalStrings(left.Witnesses, right.Witnesses) {
		caseReport.ReasonCodes = append(caseReport.ReasonCodes, "MIGRATION_WITNESS_MISMATCH")
	}
	if len(expected.Commands) != 0 {
		expectedCommands := cloneCommands(expected.Commands)
		canonicalizeCommands(expectedCommands)
		if !equalCommands(left.Commands, expectedCommands) {
			caseReport.ReasonCodes = append(caseReport.ReasonCodes, "MIGRATION_LEFT_EXPECTED_COMMAND_MISMATCH")
		}
		if !equalCommands(right.Commands, expectedCommands) {
			caseReport.ReasonCodes = append(caseReport.ReasonCodes, "MIGRATION_RIGHT_EXPECTED_COMMAND_MISMATCH")
		}
	}
	if len(expected.Witnesses) != 0 {
		expectedWitnesses := append([]string(nil), expected.Witnesses...)
		sort.Strings(expectedWitnesses)
		if !equalStrings(left.Witnesses, expectedWitnesses) {
			caseReport.ReasonCodes = append(caseReport.ReasonCodes, "MIGRATION_LEFT_EXPECTED_WITNESS_MISMATCH")
		}
		if !equalStrings(right.Witnesses, expectedWitnesses) {
			caseReport.ReasonCodes = append(caseReport.ReasonCodes, "MIGRATION_RIGHT_EXPECTED_WITNESS_MISMATCH")
		}
	}
	if left.Safety != right.Safety {
		caseReport.ReasonCodes = append(caseReport.ReasonCodes, "MIGRATION_SAFETY_RESULT_MISMATCH")
	}
	if left.Safety.AgreementViolations != 0 || left.Safety.TraceIntegrityViolations != 0 {
		caseReport.ReasonCodes = append(caseReport.ReasonCodes, "MIGRATION_LEFT_SAFETY_VIOLATION")
	}
	if right.Safety.AgreementViolations != 0 || right.Safety.TraceIntegrityViolations != 0 {
		caseReport.ReasonCodes = append(caseReport.ReasonCodes, "MIGRATION_RIGHT_SAFETY_VIOLATION")
	}
	if !left.Replay.Stable {
		caseReport.ReasonCodes = append(caseReport.ReasonCodes, "MIGRATION_LEFT_REPLAY_UNSTABLE")
	}
	if !right.Replay.Stable {
		caseReport.ReasonCodes = append(caseReport.ReasonCodes, "MIGRATION_RIGHT_REPLAY_UNSTABLE")
	}
	caseReport.Status = StatusPassed
	if len(caseReport.ReasonCodes) != 0 {
		caseReport.Status = StatusMismatch
	}
	return sealCase(caseReport)
}

func Defer(scenarioID string, reasonCodes ...string) (Case, error) {
	if scenarioID == "" || len(reasonCodes) == 0 {
		return Case{}, errors.New("MIGRATION_DEFERRED_REASON_REQUIRED")
	}
	return sealCase(Case{
		SchemaVersion: CaseSchemaVersion, ScenarioID: scenarioID,
		Status: StatusDeferred, ReasonCodes: append([]string(nil), reasonCodes...),
	})
}

func sealCase(caseReport Case) (Case, error) {
	caseReport.SchemaVersion = CaseSchemaVersion
	sort.Strings(caseReport.ReasonCodes)
	caseReport.Digest = ""
	if err := validateCaseBody(caseReport); err != nil {
		return Case{}, err
	}
	digest, err := canonicalDigest(caseReport)
	if err != nil {
		return Case{}, err
	}
	caseReport.Digest = digest
	return caseReport, nil
}

func (caseReport Case) Validate() error {
	if caseReport.SchemaVersion != CaseSchemaVersion || caseReport.Digest == "" {
		return errors.New("MIGRATION_CASE_IDENTITY_INVALID")
	}
	if err := validateCaseBody(caseReport); err != nil {
		return err
	}
	sealed, err := sealCase(caseReport)
	if err != nil {
		return err
	}
	if sealed.Digest != caseReport.Digest {
		return errors.New("MIGRATION_CASE_DIGEST_MISMATCH")
	}
	return nil
}

func validateCaseBody(caseReport Case) error {
	if caseReport.ScenarioID == "" || !sortedUnique(caseReport.ReasonCodes) {
		return errors.New("MIGRATION_CASE_FIELD_INVALID")
	}
	switch caseReport.Status {
	case StatusPassed:
		if caseReport.Left == nil || caseReport.Right == nil || len(caseReport.ReasonCodes) != 0 {
			return errors.New("MIGRATION_PASSED_CASE_INVALID")
		}
	case StatusMismatch:
		if caseReport.Left == nil || caseReport.Right == nil || len(caseReport.ReasonCodes) == 0 {
			return errors.New("MIGRATION_MISMATCH_CASE_INVALID")
		}
	case StatusDeferred:
		if caseReport.Left != nil || caseReport.Right != nil || len(caseReport.ReasonCodes) == 0 {
			return errors.New("MIGRATION_DEFERRED_CASE_INVALID")
		}
	default:
		return errors.New("MIGRATION_CASE_STATUS_INVALID")
	}
	if caseReport.Left != nil {
		if err := caseReport.Left.Validate(); err != nil {
			return err
		}
	}
	if caseReport.Right != nil {
		if err := caseReport.Right.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type Suite struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`
	Cases         []Case `json:"cases"`
	Passed        int    `json:"passed"`
	Mismatched    int    `json:"mismatched"`
	Deferred      int    `json:"deferred"`
	Qualified     bool   `json:"qualified"`
	Digest        string `json:"digest"`
}

func SealSuite(id string, cases []Case) (Suite, error) {
	if id == "" || len(cases) == 0 {
		return Suite{}, errors.New("MIGRATION_SUITE_FIELD_REQUIRED")
	}
	suite := Suite{SchemaVersion: SuiteSchemaVersion, ID: id, Cases: append([]Case(nil), cases...)}
	sort.Slice(suite.Cases, func(i, j int) bool { return suite.Cases[i].ScenarioID < suite.Cases[j].ScenarioID })
	for index, caseReport := range suite.Cases {
		if index > 0 && suite.Cases[index-1].ScenarioID == caseReport.ScenarioID {
			return Suite{}, errors.New("MIGRATION_SUITE_SCENARIO_DUPLICATE")
		}
		if err := caseReport.Validate(); err != nil {
			return Suite{}, err
		}
		switch caseReport.Status {
		case StatusPassed:
			suite.Passed++
		case StatusMismatch:
			suite.Mismatched++
		case StatusDeferred:
			suite.Deferred++
		}
	}
	suite.Qualified = suite.Mismatched == 0 && suite.Deferred == 0
	digest, err := canonicalDigest(suite)
	if err != nil {
		return Suite{}, err
	}
	suite.Digest = digest
	return suite, nil
}

func (suite Suite) Validate() error {
	if suite.SchemaVersion != SuiteSchemaVersion || suite.ID == "" || suite.Digest == "" {
		return errors.New("MIGRATION_SUITE_IDENTITY_INVALID")
	}
	sealed, err := SealSuite(suite.ID, suite.Cases)
	if err != nil {
		return err
	}
	if sealed.Digest != suite.Digest || sealed.Passed != suite.Passed ||
		sealed.Mismatched != suite.Mismatched || sealed.Deferred != suite.Deferred ||
		sealed.Qualified != suite.Qualified {
		return errors.New("MIGRATION_SUITE_DIGEST_MISMATCH")
	}
	return nil
}

func ValueDigest(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func canonicalizeCommands(commands []Command) {
	for index := range commands {
		sort.Strings(commands[index].AppliedNodes)
	}
}

func cloneCommands(commands []Command) []Command {
	cloned := make([]Command, len(commands))
	for index, command := range commands {
		cloned[index] = command
		cloned[index].AppliedNodes = append([]string(nil), command.AppliedNodes...)
	}
	return cloned
}

func canonicalDigest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func isDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func sortedUnique(values []string) bool {
	for index, value := range values {
		if value == "" || index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}

func equalCommands(left, right []Command) bool {
	leftEncoded, _ := json.Marshal(left)
	rightEncoded, _ := json.Marshal(right)
	return string(leftEncoded) == string(rightEncoded)
}

func equalStrings(left, right []string) bool {
	leftEncoded, _ := json.Marshal(left)
	rightEncoded, _ := json.Marshal(right)
	return string(leftEncoded) == string(rightEncoded)
}

func cloneSummary(summary Summary) *Summary {
	encoded, _ := json.Marshal(summary)
	var cloned Summary
	_ = json.Unmarshal(encoded, &cloned)
	return &cloned
}
