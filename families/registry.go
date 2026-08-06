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
	"github.com/SuzumiyaHaruki/consensus-atlas/internal/oracle"
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

// TrustedMonitors is the composition boundary for safety monitors. The
// generic Campaign runtime executes only monitors selected here; an Agent can
// neither register a monitor nor suppress one through its Test Plan.
func TrustedMonitors(pssID string) ([]oracle.Monitor, error) {
	switch pssID {
	case raftfamily.PSSID:
		return []oracle.Monitor{oracle.Agreement{}, raftfamily.LinearizableRead{}}, nil
	default:
		return nil, fmt.Errorf("no trusted monitor set registered for PSS %q", pssID)
	}
}

// CampaignMonitors returns the reviewed monitor set enabled by a frozen
// Driver Manifest. A Profile cannot select a monitor by name: the manifest
// must expose every observation capability on which an additional monitor
// depends. This keeps the ordinary Campaign path unchanged while allowing a
// separately identity-bound runtime profile to exercise its reviewed facts.
func CampaignMonitors(pssID string, manifest driver.Manifest) ([]oracle.Monitor, error) {
	monitors, err := TrustedMonitors(pssID)
	if err != nil {
		return nil, err
	}
	if pssID != raftfamily.PSSID || !manifestSupports(manifest, "conditional-ready-sync") ||
		!manifestSupports(manifest, "ready-must-sync-observation") {
		return monitors, nil
	}
	return append(monitors, raftfamily.ReadyMustSync{}), nil
}

// RegisteredMonitors returns every reviewed monitor available to trusted
// qualification and defect evaluation. It is intentionally wider than the
// default Campaign set above: adding a monitor must not silently alter frozen
// campaign reports that did not request its Driver observation capability.
func RegisteredMonitors(pssID string) ([]oracle.Monitor, error) {
	switch pssID {
	case raftfamily.PSSID:
		return []oracle.Monitor{oracle.Agreement{}, raftfamily.LinearizableRead{}, raftfamily.ReadyMustSync{}}, nil
	default:
		return nil, fmt.Errorf("no registered monitor set for PSS %q", pssID)
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

func manifestSupports(manifest driver.Manifest, id string) bool {
	for _, capability := range manifest.Capabilities {
		if capability.ID == id {
			return capability.Supported
		}
	}
	return false
}
