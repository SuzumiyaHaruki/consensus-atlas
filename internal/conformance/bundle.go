package conformance

import (
	"errors"
	"sort"

	"github.com/SuzumiyaHaruki/consensus-atlas/internal/control"
)

const QualificationBundleSchemaVersion = "consensus-atlas/adapter-qualification-bundle/v1"

// QualificationBundle binds every mechanical qualification input to its
// derived report. It is shared by all Adapter-specific composition roots.
type QualificationBundle struct {
	SchemaVersion      string                   `json:"schema_version"`
	Profile            QualificationProfile     `json:"profile"`
	Manifest           control.AdapterManifest  `json:"manifest"`
	ConformanceReports []Report                 `json:"conformance_reports"`
	Unsupported        []UnsupportedDeclaration `json:"unsupported,omitempty"`
	Qualification      QualificationReport      `json:"qualification"`
	Digest             string                   `json:"digest"`
}

func (bundle QualificationBundle) Seal() (QualificationBundle, error) {
	bundle.SchemaVersion = QualificationBundleSchemaVersion
	sort.Slice(bundle.ConformanceReports, func(i, j int) bool {
		return bundle.ConformanceReports[i].Digest < bundle.ConformanceReports[j].Digest
	})
	sort.Slice(bundle.Unsupported, func(i, j int) bool {
		return bundle.Unsupported[i].CapabilityID < bundle.Unsupported[j].CapabilityID
	})
	bundle.Digest = ""
	digest, err := control.CanonicalDigest(bundle)
	if err != nil {
		return QualificationBundle{}, err
	}
	bundle.Digest = digest
	return bundle, nil
}

func (bundle QualificationBundle) Validate() error {
	if bundle.SchemaVersion != QualificationBundleSchemaVersion {
		return errors.New("QUALIFICATION_BUNDLE_SCHEMA_MISMATCH")
	}
	if err := bundle.Profile.Validate(); err != nil {
		return err
	}
	if err := bundle.Manifest.Validate(); err != nil {
		return err
	}
	for _, report := range bundle.ConformanceReports {
		if err := report.Validate(); err != nil {
			return err
		}
	}
	if err := bundle.Qualification.Validate(); err != nil {
		return err
	}
	recomputed, err := Qualify(bundle.Manifest, bundle.Profile, bundle.Unsupported, bundle.ConformanceReports)
	if err != nil {
		return err
	}
	if recomputed.Digest != bundle.Qualification.Digest {
		return errors.New("QUALIFICATION_BUNDLE_REPORT_MISMATCH")
	}
	sealed, err := bundle.Seal()
	if err != nil {
		return err
	}
	if sealed.Digest != bundle.Digest {
		return errors.New("QUALIFICATION_BUNDLE_DIGEST_MISMATCH")
	}
	return nil
}
