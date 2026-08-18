package pricing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
	"mosaic-log-analyzer/internal/domain"
)

// ConfigRelPath is the pricing file's location relative to the MOSAIC runtime root.
const ConfigRelPath = "MosaicLogAnalyzer/config/pricing.yaml"

// Store owns the external YAML pricing file end to end.
type Store struct {
	path string
}

// NewStore returns a store rooted at the given MOSAIC runtime root.
// The pricing file is resolved as <mosaicRoot>/MosaicLogAnalyzer/config/pricing.yaml.
func NewStore(mosaicRoot string) *Store {
	return &Store{
		path: filepath.Join(mosaicRoot, ConfigRelPath),
	}
}

// Path returns the config file's absolute location so a frontend can tell the user
// exactly which file to edit or which file was written.
func (s *Store) Path() string {
	return s.path
}

// Load reads the pricing table from the YAML file.
//
// Absent file contract: a missing file yields an empty table and a nil error,
// following the same convention as the Deployment tool's config store.
//
// Per-entry isolation: an entry that fails rate parsing or domain.ModelPricing.Validate
// is skipped and reported as a domain.FindingInvalidPricingEntry naming the model;
// the remaining entries still load. An error is returned only when the document
// itself cannot be parsed.
func (s *Store) Load(_ context.Context) (domain.PricingTable, []domain.Finding, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return domain.EmptyPricingTable(), nil, nil
		}
		return domain.EmptyPricingTable(), nil, fmt.Errorf("read pricing file: %w", err)
	}

	var raw pricingFileYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return domain.EmptyPricingTable(), nil, fmt.Errorf("parse pricing file: %w", err)
	}

	if len(raw.Models) == 0 {
		// Comments-only file or genuinely empty file — both are valid empty tables.
		return domain.EmptyPricingTable(), nil, nil
	}

	var entries []domain.ModelPricing
	var findings []domain.Finding

	for modelID, wireEntry := range raw.Models {
		entry, convErr := wireEntry.toDomain(modelID)
		if convErr != nil {
			findings = append(findings, domain.Finding{
				Kind:     domain.FindingInvalidPricingEntry,
				Severity: domain.SeverityWarning,
				Detail:   fmt.Sprintf("invalid pricing entry for %s: %v", modelID, convErr),
			})
			continue
		}
		if valErr := entry.Validate(); valErr != nil {
			findings = append(findings, domain.Finding{
				Kind:     domain.FindingInvalidPricingEntry,
				Severity: domain.SeverityWarning,
				Detail:   fmt.Sprintf("invalid pricing entry for %s: %v", modelID, valErr),
			})
			continue
		}
		entries = append(entries, entry)
	}

	return domain.NewPricingTable(entries), findings, nil
}
