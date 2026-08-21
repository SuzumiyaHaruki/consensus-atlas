package etcdraftv2

import (
	"encoding/json"
	"fmt"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
	pb "go.etcd.io/raft/v3/raftpb"
)

const (
	inputSchema            = "consensus-atlas/etcdraft-v2-input/v1"
	proposalEntrySchema    = "consensus-atlas/etcdraft-v2-proposal-entry/v1"
	applicationImageSchema = "consensus-atlas/etcdraft-v2-application-image/v1"
	clientResultSchema     = "consensus-atlas/etcdraft-v2-client-result/v1"
	clientRejectionSchema  = "consensus-atlas/etcdraft-v2-client-rejection/v1"

	OperationPropose = "propose"
)

// Input is an Adapter-specific payload. The Runtime stores and authenticates
// its envelope but does not interpret Operation, RequestID, or Value.
type Input struct {
	Operation string `json:"operation"`
	RequestID string `json:"request_id"`
	Value     []byte `json:"value"`
}

func InputPayload(input Input) (control.PayloadEnvelope, error) {
	if err := input.validate(); err != nil {
		return control.PayloadEnvelope{}, err
	}
	return control.NewJSONPayload(inputSchema, input)
}

func (input Input) validate() error {
	if input.Operation != OperationPropose {
		return fmt.Errorf("ETCDRAFT_V2_INPUT_OPERATION_UNSUPPORTED: %s", input.Operation)
	}
	if input.RequestID == "" {
		return fmt.Errorf("ETCDRAFT_V2_INPUT_REQUEST_ID_REQUIRED")
	}
	return nil
}

func decodeInput(payload control.PayloadEnvelope) (Input, error) {
	if err := payload.Validate(); err != nil {
		return Input{}, err
	}
	if payload.SchemaVersion != inputSchema || payload.Encoding != "json" {
		return Input{}, fmt.Errorf("ETCDRAFT_V2_INPUT_SCHEMA_MISMATCH: %s/%s", payload.SchemaVersion, payload.Encoding)
	}
	var input Input
	if err := json.Unmarshal(payload.Bytes, &input); err != nil {
		return Input{}, err
	}
	if err := input.validate(); err != nil {
		return Input{}, err
	}
	return input, nil
}

type proposalEntry struct {
	SchemaVersion string          `json:"schema_version"`
	RequestID     string          `json:"request_id"`
	Origin        control.NodeRef `json:"origin"`
	Value         []byte          `json:"value"`
}

func encodeProposal(input Input, origin control.NodeRef) ([]byte, error) {
	if err := input.validate(); err != nil {
		return nil, err
	}
	if err := origin.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(proposalEntry{
		SchemaVersion: proposalEntrySchema,
		RequestID:     input.RequestID, Origin: origin, Value: append([]byte(nil), input.Value...),
	})
}

func decodeProposal(encoded []byte) (proposalEntry, error) {
	var proposal proposalEntry
	if err := json.Unmarshal(encoded, &proposal); err != nil {
		return proposalEntry{}, err
	}
	if proposal.SchemaVersion != proposalEntrySchema || proposal.RequestID == "" {
		return proposalEntry{}, fmt.Errorf("ETCDRAFT_V2_PROPOSAL_IDENTITY_INVALID")
	}
	if err := proposal.Origin.Validate(); err != nil {
		return proposalEntry{}, err
	}
	proposal.Value = append([]byte(nil), proposal.Value...)
	return proposal, nil
}

type appliedCommand struct {
	Index     uint64          `json:"index"`
	Term      uint64          `json:"term"`
	RequestID string          `json:"request_id"`
	Origin    control.NodeRef `json:"origin"`
	Value     []byte          `json:"value"`
}

type readyAdvancedObservation struct {
	ReadyID  string           `json:"value"`
	Commands []appliedCommand `json:"commands,omitempty"`
}

type applicationImage struct {
	SchemaVersion string           `json:"schema_version"`
	Applied       uint64           `json:"applied"`
	Commands      []appliedCommand `json:"commands,omitempty"`
	Digest        string           `json:"digest"`
	prefixCache   []ApplicationPrefixEvidence
}

func newApplicationImage() (applicationImage, error) {
	return sealApplicationImage(applicationImage{SchemaVersion: applicationImageSchema})
}

func sealApplicationImage(image applicationImage) (applicationImage, error) {
	image.SchemaVersion = applicationImageSchema
	image.Digest = ""
	digest, err := control.CanonicalDigest(image)
	if err != nil {
		return applicationImage{}, err
	}
	image.Digest = digest
	return image, nil
}

func (image applicationImage) validate() error {
	if image.SchemaVersion != applicationImageSchema || image.Digest == "" {
		return fmt.Errorf("ETCDRAFT_V2_APPLICATION_IMAGE_IDENTITY_INVALID")
	}
	sealed, err := sealApplicationImage(image)
	if err != nil {
		return err
	}
	if sealed.Digest != image.Digest {
		return fmt.Errorf("ETCDRAFT_V2_APPLICATION_IMAGE_DIGEST_MISMATCH")
	}
	lastIndex := uint64(0)
	for _, command := range image.Commands {
		if command.Index == 0 || command.Index <= lastIndex || command.Index > image.Applied || command.RequestID == "" {
			return fmt.Errorf("ETCDRAFT_V2_APPLICATION_COMMAND_INVALID")
		}
		if err := command.Origin.Validate(); err != nil {
			return err
		}
		lastIndex = command.Index
	}
	return nil
}

func (image applicationImage) encode() ([]byte, error) {
	if err := image.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(image)
}

func decodeApplicationImage(encoded []byte) (applicationImage, error) {
	var image applicationImage
	if err := json.Unmarshal(encoded, &image); err != nil {
		return applicationImage{}, err
	}
	if err := image.validate(); err != nil {
		return applicationImage{}, err
	}
	for index := range image.Commands {
		image.Commands[index].Value = append([]byte(nil), image.Commands[index].Value...)
	}
	prefixes, err := image.rebuildPrefixes()
	if err != nil {
		return applicationImage{}, err
	}
	image.prefixCache = prefixes
	return image, nil
}

func (image applicationImage) applyNormal(entry pb.Entry) (applicationImage, *appliedCommand, error) {
	if entry.Index <= image.Applied {
		return image, nil, nil
	}
	previous, err := image.prefixes()
	if err != nil {
		return applicationImage{}, nil, err
	}
	var applied *appliedCommand
	if len(entry.Data) > 0 {
		proposal, err := decodeProposal(entry.Data)
		if err != nil {
			return applicationImage{}, nil, fmt.Errorf("decode proposal at %d: %w", entry.Index, err)
		}
		command := appliedCommand{
			Index: entry.Index, Term: entry.Term, RequestID: proposal.RequestID,
			Origin: proposal.Origin, Value: append([]byte(nil), proposal.Value...),
		}
		image.Commands = append(image.Commands, command)
		applied = &command
	}
	image.Applied = entry.Index
	sealed, err := sealApplicationImage(image)
	if err != nil {
		return applicationImage{}, nil, err
	}
	sealed.prefixCache = append(append(
		[]ApplicationPrefixEvidence(nil), previous...,
	), ApplicationPrefixEvidence{Position: sealed.Applied, Digest: sealed.Digest})
	return sealed, applied, nil
}

func (image applicationImage) advance(index uint64) (applicationImage, error) {
	if index <= image.Applied {
		return image, nil
	}
	previous, err := image.prefixes()
	if err != nil {
		return applicationImage{}, err
	}
	oldApplied := image.Applied
	image.Applied = index
	sealed, err := sealApplicationImage(image)
	if err != nil {
		return applicationImage{}, err
	}
	sealed.prefixCache = append([]ApplicationPrefixEvidence(nil), previous...)
	for position := oldApplied + 1; position <= index; position++ {
		digest := sealed.Digest
		if position != index {
			prefix, sealErr := sealApplicationImage(applicationImage{
				SchemaVersion: applicationImageSchema, Applied: position,
				Commands: append([]appliedCommand(nil), image.Commands...),
			})
			if sealErr != nil {
				return applicationImage{}, sealErr
			}
			digest = prefix.Digest
		}
		sealed.prefixCache = append(sealed.prefixCache, ApplicationPrefixEvidence{
			Position: position, Digest: digest,
		})
	}
	return sealed, nil
}

// prefixes returns one commitment for every applied log position. Each
// commitment is the digest of the application image at that exact frontier,
// allowing a protocol-neutral Agreement monitor to compare the shared prefix
// of nodes that currently have different applied frontiers.
func (image applicationImage) prefixes() ([]ApplicationPrefixEvidence, error) {
	if err := image.validate(); err != nil {
		return nil, err
	}
	if image.prefixCacheValid() {
		return append([]ApplicationPrefixEvidence(nil), image.prefixCache...), nil
	}
	return image.rebuildPrefixes()
}

func (image applicationImage) rebuildPrefixes() ([]ApplicationPrefixEvidence, error) {
	result := make([]ApplicationPrefixEvidence, 0, image.Applied)
	commandEnd := 0
	for position := uint64(1); position <= image.Applied; position++ {
		for commandEnd < len(image.Commands) && image.Commands[commandEnd].Index <= position {
			commandEnd++
		}
		prefix, err := sealApplicationImage(applicationImage{
			SchemaVersion: applicationImageSchema,
			Applied:       position,
			Commands:      append([]appliedCommand(nil), image.Commands[:commandEnd]...),
		})
		if err != nil {
			return nil, err
		}
		result = append(result, ApplicationPrefixEvidence{Position: position, Digest: prefix.Digest})
	}
	if len(result) > 0 && result[len(result)-1].Digest != image.Digest {
		return nil, fmt.Errorf("ETCDRAFT_V2_APPLICATION_PREFIX_FINAL_MISMATCH")
	}
	return result, nil
}

func (image applicationImage) prefixCacheValid() bool {
	if uint64(len(image.prefixCache)) != image.Applied {
		return false
	}
	for index, prefix := range image.prefixCache {
		if prefix.Position != uint64(index+1) || prefix.Digest == "" {
			return false
		}
	}
	return len(image.prefixCache) == 0 || image.prefixCache[len(image.prefixCache)-1].Digest == image.Digest
}

type clientResult struct {
	Index uint64 `json:"index"`
	Term  uint64 `json:"term"`
	Value []byte `json:"value"`
}

type clientRejection struct {
	ReasonCode string `json:"reason_code"`
}
