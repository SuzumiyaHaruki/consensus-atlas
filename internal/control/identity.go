package control

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// ValidationError carries a stable machine-readable code. Detail is only for
// diagnostics and must not be used as an execution identity.
type ValidationError struct {
	Code   string
	Field  string
	Detail string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Detail)
	}
	return fmt.Sprintf("%s: %s: %s", e.Code, e.Field, e.Detail)
}

func invalid(code, field, detail string) error {
	return &ValidationError{Code: code, Field: field, Detail: detail}
}

// ValidationCode extracts a stable validation code from err.
func ValidationCode(err error) string {
	var target *ValidationError
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}

// StableID hashes length-delimited components, so different component
// boundaries cannot alias one another.
func StableID(prefix string, parts ...string) (string, error) {
	if prefix == "" {
		return "", invalid("ID_PREFIX_REQUIRED", "prefix", "must not be empty")
	}
	h := sha256.New()
	for _, part := range append([]string{prefix}, parts...) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		_, _ = h.Write(length[:])
		_, _ = h.Write([]byte(part))
	}
	return prefix + "-" + hex.EncodeToString(h.Sum(nil)), nil
}

// CanonicalDigest returns the SHA-256 identity of a JSON-serializable value.
// encoding/json sorts string map keys, and control types reject opaque
// non-canonical values at their boundaries.
func CanonicalDigest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("canonical JSON: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// PayloadEnvelope carries bytes without giving the Runtime permission to
// interpret their protocol-specific meaning.
type PayloadEnvelope struct {
	SchemaVersion string `json:"schema_version"`
	Encoding      string `json:"encoding"`
	Bytes         []byte `json:"bytes"`
	Digest        string `json:"digest"`
}

func NewPayload(schemaVersion, encoding string, data []byte) (PayloadEnvelope, error) {
	payload := PayloadEnvelope{
		SchemaVersion: schemaVersion,
		Encoding:      encoding,
		Bytes:         append([]byte(nil), data...),
	}
	sum := sha256.Sum256(payload.Bytes)
	payload.Digest = hex.EncodeToString(sum[:])
	if err := payload.Validate(); err != nil {
		return PayloadEnvelope{}, err
	}
	return payload, nil
}

func NewJSONPayload(schemaVersion string, value any) (PayloadEnvelope, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return PayloadEnvelope{}, err
	}
	return NewPayload(schemaVersion, "json", encoded)
}

func (p PayloadEnvelope) Validate() error {
	if p.SchemaVersion == "" {
		return invalid("PAYLOAD_SCHEMA_REQUIRED", "schema_version", "must not be empty")
	}
	if p.Encoding == "" {
		return invalid("PAYLOAD_ENCODING_REQUIRED", "encoding", "must not be empty")
	}
	if p.Digest == "" {
		return invalid("PAYLOAD_DIGEST_REQUIRED", "digest", "must not be empty")
	}
	sum := sha256.Sum256(p.Bytes)
	if got := hex.EncodeToString(sum[:]); got != p.Digest {
		return invalid("PAYLOAD_DIGEST_MISMATCH", "digest", "does not match bytes")
	}
	return nil
}
