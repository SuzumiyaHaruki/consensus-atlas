// Package families registers protocol-family semantic projectors at the CLI
// composition boundary.
package families

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	raftfamily "github.com/SuzumiyaHaruki/consensus-atlas/families/raft"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/coverage"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/driver"
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/protocolstate"
)

func Projector(pssID string) (protocolstate.Projector, error) {
	switch pssID {
	case raftfamily.PSSID:
		return raftfamily.Projector{}, nil
	default:
		return nil, fmt.Errorf("no protocol state projector registered for PSS %q", pssID)
	}
}

func CoverageMatcher(pssID string) (coverage.SemanticMatcher, error) {
	switch pssID {
	case raftfamily.PSSID:
		return raftfamily.CoverageMatcher{}, nil
	default:
		return nil, fmt.Errorf("no coverage semantic matcher registered for PSS %q", pssID)
	}
}

// CompileCampaign is the composition boundary for family-specific bounded
// denominator compilers. Generic CLIs and runtimes do not import a protocol
// family directly; adding another family extends this registry.
func CompileCampaign(base coverage.Profile, manifest driver.Manifest, specJSON []byte) (coverage.Profile, error) {
	switch base.PSSID {
	case raftfamily.PSSID:
		var spec raftfamily.CampaignSpec
		if err := decodeStrict(specJSON, &spec); err != nil {
			return coverage.Profile{}, fmt.Errorf("decode Raft campaign spec: %w", err)
		}
		return raftfamily.CompileCampaign(base, manifest, spec)
	default:
		return coverage.Profile{}, fmt.Errorf("no campaign compiler registered for PSS %q", base.PSSID)
	}
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return fmt.Errorf("trailing JSON data: %w", err)
	}
	return nil
}
