package agentcampaign

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

const BlackboardVersion = 1

const (
	RecordRequest   = "request"
	RecordProposal  = "proposal"
	RecordAudit     = "generation_audit"
	RecordExecution = "execution"
	RecordFinding   = "finding"
)

type BlackboardRecord struct {
	Sequence      int             `json:"sequence"`
	Kind          string          `json:"kind"`
	Attempt       int             `json:"attempt"`
	ParentDigest  string          `json:"parent_digest,omitempty"`
	PayloadDigest string          `json:"payload_digest"`
	Digest        string          `json:"digest"`
	Payload       json.RawMessage `json:"payload"`
}

type BlackboardReport struct {
	Version int                `json:"version"`
	ID      string             `json:"id"`
	Head    string             `json:"head"`
	Records []BlackboardRecord `json:"records"`
}

type blackboard struct {
	id      string
	head    string
	records []BlackboardRecord
}

func newBlackboard(id string) (*blackboard, error) {
	if id == "" {
		return nil, errors.New("blackboard id is required")
	}
	return &blackboard{id: id}, nil
}

func (board *blackboard) append(kind string, attempt int, payload any) error {
	if board == nil || board.id == "" || kind == "" || attempt < 1 {
		return errors.New("invalid blackboard append")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	payloadDigest := sha256Hex(encoded)
	record := BlackboardRecord{
		Sequence: len(board.records) + 1, Kind: kind, Attempt: attempt,
		ParentDigest: board.head, PayloadDigest: payloadDigest, Payload: append([]byte(nil), encoded...),
	}
	record.Digest, err = recordDigest(record)
	if err != nil {
		return err
	}
	board.records = append(board.records, record)
	board.head = record.Digest
	return nil
}

func (board *blackboard) report() BlackboardReport {
	if board == nil {
		return BlackboardReport{}
	}
	report := BlackboardReport{Version: BlackboardVersion, ID: board.id, Head: board.head}
	report.Records = make([]BlackboardRecord, len(board.records))
	for index, record := range board.records {
		report.Records[index] = record
		report.Records[index].Payload = append([]byte(nil), record.Payload...)
	}
	return report
}

func VerifyBlackboard(report BlackboardReport) error {
	if report.Version != BlackboardVersion || report.ID == "" {
		return errors.New("invalid blackboard identity")
	}
	parent := ""
	for index, record := range report.Records {
		if record.Sequence != index+1 || record.ParentDigest != parent {
			return fmt.Errorf("blackboard record %d breaks sequence or parent chain", index+1)
		}
		if sha256Hex(record.Payload) != record.PayloadDigest {
			return fmt.Errorf("blackboard record %d payload digest mismatch", index+1)
		}
		digest, err := recordDigest(record)
		if err != nil || digest != record.Digest {
			return fmt.Errorf("blackboard record %d digest mismatch", index+1)
		}
		parent = record.Digest
	}
	if report.Head != parent {
		return errors.New("blackboard head does not match final record")
	}
	return nil
}

func recordDigest(record BlackboardRecord) (string, error) {
	identity := struct {
		Sequence      int    `json:"sequence"`
		Kind          string `json:"kind"`
		Attempt       int    `json:"attempt"`
		ParentDigest  string `json:"parent_digest,omitempty"`
		PayloadDigest string `json:"payload_digest"`
	}{record.Sequence, record.Kind, record.Attempt, record.ParentDigest, record.PayloadDigest}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	return sha256Hex(encoded), nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
