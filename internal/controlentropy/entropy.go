package controlentropy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const (
	Algorithm         = "hmac-sha256-counter-v1"
	TapeSchemaVersion = "consensus-atlas/entropy-tape/v1"
)

type Domain struct {
	Namespace   string         `json:"namespace"`
	Node        control.NodeID `json:"node,omitempty"`
	Incarnation uint64         `json:"incarnation,omitempty"`
	ID          string         `json:"id"`
}

func (domain Domain) Validate() error {
	if domain.Namespace == "" {
		return errors.New("ENTROPY_NAMESPACE_REQUIRED")
	}
	if domain.ID == "" {
		return errors.New("ENTROPY_DOMAIN_REQUIRED")
	}
	if domain.Node == "" && domain.Incarnation != 0 {
		return errors.New("ENTROPY_NODE_REQUIRED_FOR_INCARNATION")
	}
	if domain.Node != "" && domain.Incarnation == 0 {
		return errors.New("ENTROPY_INCARNATION_REQUIRED")
	}
	return nil
}

func (domain Domain) key() string {
	return domain.Namespace + "\x00" + string(domain.Node) + "\x00" +
		strconv.FormatUint(domain.Incarnation, 10) + "\x00" + domain.ID
}

type DrawRecord struct {
	ID         control.EntropyDrawID `json:"id"`
	Sequence   uint64                `json:"sequence"`
	Domain     Domain                `json:"domain"`
	Ordinal    uint64                `json:"ordinal"`
	Operation  string                `json:"operation"`
	Bound      uint64                `json:"bound,omitempty"`
	Length     uint64                `json:"length,omitempty"`
	IntResult  *uint64               `json:"int_result,omitempty"`
	ByteResult []byte                `json:"byte_result,omitempty"`
}

type Tape struct {
	SchemaVersion string       `json:"schema_version"`
	Algorithm     string       `json:"algorithm"`
	SeedDigest    string       `json:"seed_digest"`
	Draws         []DrawRecord `json:"draws"`
	Digest        string       `json:"digest"`
}

func (tape Tape) Seal() (Tape, error) {
	if tape.SchemaVersion == "" {
		tape.SchemaVersion = TapeSchemaVersion
	}
	if tape.SchemaVersion != TapeSchemaVersion {
		return Tape{}, fmt.Errorf("ENTROPY_TAPE_SCHEMA_MISMATCH: %s", tape.SchemaVersion)
	}
	if tape.Algorithm == "" {
		tape.Algorithm = Algorithm
	}
	if tape.Algorithm != Algorithm {
		return Tape{}, fmt.Errorf("ENTROPY_ALGORITHM_UNSUPPORTED: %s", tape.Algorithm)
	}
	if tape.SeedDigest == "" {
		return Tape{}, errors.New("ENTROPY_SEED_DIGEST_REQUIRED")
	}
	ordinals := make(map[string]uint64)
	for index := range tape.Draws {
		if err := tape.Draws[index].validate(); err != nil {
			return Tape{}, fmt.Errorf("draw %d: %w", index, err)
		}
		if tape.Draws[index].Sequence != uint64(index) {
			return Tape{}, fmt.Errorf("ENTROPY_SEQUENCE_MISMATCH: draw %d has sequence %d", index, tape.Draws[index].Sequence)
		}
		key := tape.Draws[index].Domain.key()
		if tape.Draws[index].Ordinal != ordinals[key] {
			return Tape{}, fmt.Errorf("ENTROPY_ORDINAL_MISMATCH: draw %d has ordinal %d, want %d",
				index, tape.Draws[index].Ordinal, ordinals[key])
		}
		ordinals[key]++
	}
	tape.Digest = ""
	digest, err := control.CanonicalDigest(tape)
	if err != nil {
		return Tape{}, err
	}
	tape.Digest = digest
	return tape, nil
}

func (tape Tape) Validate() error {
	sealed, err := tape.Seal()
	if err != nil {
		return err
	}
	if tape.Digest != sealed.Digest {
		return errors.New("ENTROPY_TAPE_DIGEST_MISMATCH")
	}
	return nil
}

func (record DrawRecord) validate() error {
	if record.ID == "" {
		return errors.New("ENTROPY_DRAW_ID_REQUIRED")
	}
	if err := record.Domain.Validate(); err != nil {
		return err
	}
	expectedID, err := control.StableID("entropy-draw", record.Domain.key(), strconv.FormatUint(record.Ordinal, 10), record.Operation)
	if err != nil {
		return err
	}
	if string(record.ID) != expectedID {
		return errors.New("ENTROPY_DRAW_ID_MISMATCH")
	}
	switch record.Operation {
	case "intn":
		if record.Bound == 0 || record.IntResult == nil || len(record.ByteResult) != 0 || record.Length != 0 {
			return errors.New("ENTROPY_INTN_RECORD_INVALID")
		}
		if *record.IntResult >= record.Bound {
			return errors.New("ENTROPY_INTN_RESULT_OUT_OF_RANGE")
		}
	case "bytes":
		if record.Length == 0 || uint64(len(record.ByteResult)) != record.Length || record.IntResult != nil || record.Bound != 0 {
			return errors.New("ENTROPY_BYTES_RECORD_INVALID")
		}
	default:
		return fmt.Errorf("ENTROPY_OPERATION_UNSUPPORTED: %s", record.Operation)
	}
	return nil
}

type Provider struct {
	masterSeed []byte
	seedDigest string
	ordinals   map[string]uint64
	draws      []DrawRecord
}

func New(seed []byte) (*Provider, error) {
	if len(seed) == 0 {
		return nil, errors.New("ENTROPY_SEED_REQUIRED")
	}
	sum := sha256.Sum256(seed)
	return &Provider{
		masterSeed: append([]byte(nil), seed...),
		seedDigest: hex.EncodeToString(sum[:]),
		ordinals:   make(map[string]uint64),
	}, nil
}

func (provider *Provider) Intn(domain Domain, n int) (int, error) {
	if n <= 0 {
		return 0, errors.New("ENTROPY_BOUND_INVALID")
	}
	if err := domain.Validate(); err != nil {
		return 0, err
	}
	ordinal := provider.ordinals[domain.key()]
	bound := uint64(n)
	threshold := (0 - bound) % bound
	var result uint64
	for attempt := uint64(0); ; attempt++ {
		block := provider.block(domain, ordinal, "intn", attempt)
		candidate := binary.BigEndian.Uint64(block[:8])
		if candidate >= threshold {
			result = candidate % bound
			break
		}
	}
	resultCopy := result
	record, err := provider.record(domain, ordinal, "intn")
	if err != nil {
		return 0, err
	}
	record.Bound = bound
	record.IntResult = &resultCopy
	if err := provider.accept(record); err != nil {
		return 0, err
	}
	provider.ordinals[domain.key()] = ordinal + 1
	return int(result), nil
}

func (provider *Provider) Bytes(domain Domain, length int) ([]byte, error) {
	if length <= 0 {
		return nil, errors.New("ENTROPY_LENGTH_INVALID")
	}
	if err := domain.Validate(); err != nil {
		return nil, err
	}
	ordinal := provider.ordinals[domain.key()]
	result := make([]byte, 0, length)
	for blockIndex := uint64(0); len(result) < length; blockIndex++ {
		block := provider.block(domain, ordinal, "bytes", blockIndex)
		remaining := length - len(result)
		if remaining > len(block) {
			remaining = len(block)
		}
		result = append(result, block[:remaining]...)
	}
	record, err := provider.record(domain, ordinal, "bytes")
	if err != nil {
		return nil, err
	}
	record.Length = uint64(length)
	record.ByteResult = append([]byte(nil), result...)
	if err := provider.accept(record); err != nil {
		return nil, err
	}
	provider.ordinals[domain.key()] = ordinal + 1
	return append([]byte(nil), result...), nil
}

func (provider *Provider) record(domain Domain, ordinal uint64, operation string) (DrawRecord, error) {
	id, err := control.StableID("entropy-draw", domain.key(), strconv.FormatUint(ordinal, 10), operation)
	if err != nil {
		return DrawRecord{}, err
	}
	return DrawRecord{
		ID:        control.EntropyDrawID(id),
		Sequence:  uint64(len(provider.draws)),
		Domain:    domain,
		Ordinal:   ordinal,
		Operation: operation,
	}, nil
}

func (provider *Provider) accept(record DrawRecord) error {
	if err := record.validate(); err != nil {
		return err
	}
	provider.draws = append(provider.draws, cloneRecord(record))
	return nil
}

func (provider *Provider) Tape() (Tape, error) {
	return (Tape{
		SchemaVersion: TapeSchemaVersion,
		Algorithm:     Algorithm,
		SeedDigest:    provider.seedDigest,
		Draws:         cloneRecords(provider.draws),
	}).Seal()
}

// SnapshotAudit converts the provider's current tape into the common Adapter
// audit envelope. Adapter-specific availability checks remain at the boundary.
func (provider *Provider) SnapshotAudit(yield control.YieldID) (control.EntropyAuditEnvelope, error) {
	tape, err := provider.Tape()
	if err != nil {
		return control.EntropyAuditEnvelope{}, err
	}
	payload, err := control.NewJSONPayload(TapeSchemaVersion, tape)
	if err != nil {
		return control.EntropyAuditEnvelope{}, err
	}
	return control.EntropyAuditEnvelope{
		Yield: yield, Algorithm: tape.Algorithm, SeedDigest: tape.SeedDigest,
		DrawCount: uint64(len(tape.Draws)), TapeDigest: tape.Digest, Tape: payload,
	}, nil
}

func (provider *Provider) block(domain Domain, ordinal uint64, operation string, counter uint64) []byte {
	domainMAC := hmac.New(sha256.New, provider.masterSeed)
	writePart(domainMAC, "domain-v1")
	writePart(domainMAC, domain.Namespace)
	writePart(domainMAC, string(domain.Node))
	writePart(domainMAC, strconv.FormatUint(domain.Incarnation, 10))
	writePart(domainMAC, domain.ID)
	domainSeed := domainMAC.Sum(nil)

	blockMAC := hmac.New(sha256.New, domainSeed)
	writePart(blockMAC, operation)
	writePart(blockMAC, strconv.FormatUint(ordinal, 10))
	writePart(blockMAC, strconv.FormatUint(counter, 10))
	return blockMAC.Sum(nil)
}

type partWriter interface {
	Write([]byte) (int, error)
}

func writePart(writer partWriter, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = writer.Write(length[:])
	_, _ = writer.Write([]byte(value))
}

func cloneRecord(record DrawRecord) DrawRecord {
	copyRecord := record
	if record.IntResult != nil {
		value := *record.IntResult
		copyRecord.IntResult = &value
	}
	copyRecord.ByteResult = append([]byte(nil), record.ByteResult...)
	return copyRecord
}

func cloneRecords(records []DrawRecord) []DrawRecord {
	result := make([]DrawRecord, len(records))
	for index, record := range records {
		result[index] = cloneRecord(record)
	}
	return result
}
