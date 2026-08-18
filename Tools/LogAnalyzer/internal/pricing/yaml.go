package pricing

import (
	"fmt"

	"mosaic-log-analyzer/internal/domain"
)

// pricingFileYAML is the top-level wire type for the pricing YAML file.
//
// Wire schema:
//
//	models:
//	  <model-id>:
//	    input: "3.00"
//	    cached_input: "0.30"
//	    cache_write: "3.75"
//	    output_under_threshold: "15.00"
//	    output_at_threshold: "22.50"     # omit when no long-context tier
//	    context_length_threshold: 200000 # omit or null when no long-context tier
//
// All rate values are decimal USD per 1,000,000 tokens stored as quoted strings
// so YAML float parsing cannot silently lose precision.
type pricingFileYAML struct {
	Models map[string]modelEntryYAML `yaml:"models"`
}

// modelEntryYAML is the per-model wire type. Rate fields are quoted strings to
// prevent YAML from interpreting them as floats (which would lose precision on
// numbers with many significant digits). output_at_threshold and
// context_length_threshold are optional: absent means no long-context tier.
type modelEntryYAML struct {
	Input                  string `yaml:"input"`
	CachedInput            string `yaml:"cached_input"`
	CacheWrite             string `yaml:"cache_write"`
	OutputUnderThreshold   string `yaml:"output_under_threshold"`
	OutputAtThreshold      string `yaml:"output_at_threshold,omitempty"`
	ContextLengthThreshold *int64 `yaml:"context_length_threshold,omitempty"`
}

// toDomain converts the wire entry to a domain.ModelPricing. It returns an error
// if any required rate string is unparseable so the caller can emit a per-entry
// finding and skip without aborting the whole load.
func (e modelEntryYAML) toDomain(modelID string) (domain.ModelPricing, error) {
	input, err := domain.ParseRate(e.Input)
	if err != nil {
		return domain.ModelPricing{}, fmt.Errorf("input: %w", err)
	}
	cachedInput, err := domain.ParseRate(e.CachedInput)
	if err != nil {
		return domain.ModelPricing{}, fmt.Errorf("cached_input: %w", err)
	}
	cacheWrite, err := domain.ParseRate(e.CacheWrite)
	if err != nil {
		return domain.ModelPricing{}, fmt.Errorf("cache_write: %w", err)
	}
	outputUnder, err := domain.ParseRate(e.OutputUnderThreshold)
	if err != nil {
		return domain.ModelPricing{}, fmt.Errorf("output_under_threshold: %w", err)
	}

	// A null or absent context_length_threshold means no long-context tier.
	// A zero value is also treated as absent (a zero threshold is meaningless).
	hasThreshold := e.ContextLengthThreshold != nil && *e.ContextLengthThreshold != 0

	var outputAt domain.Rate
	var threshold int64
	if hasThreshold {
		outputAt, err = domain.ParseRate(e.OutputAtThreshold)
		if err != nil {
			return domain.ModelPricing{}, fmt.Errorf("output_at_threshold: %w", err)
		}
		threshold = *e.ContextLengthThreshold
	}

	return domain.ModelPricing{
		Model:                  domain.ModelID(modelID),
		Input:                  input,
		CachedInput:            cachedInput,
		CacheWrite:             cacheWrite,
		OutputUnderThreshold:   outputUnder,
		OutputAtThreshold:      outputAt,
		ContextLengthThreshold: threshold,
		HasThreshold:           hasThreshold,
	}, nil
}

// fromDomain converts a domain.ModelPricing to the wire type for YAML serialisation.
// Rate values are written using Rate.String() so the round-trip through
// ParseRate is lossless in nanodollar terms.
func fromDomain(p domain.ModelPricing) modelEntryYAML {
	e := modelEntryYAML{
		Input:                p.Input.String(),
		CachedInput:          p.CachedInput.String(),
		CacheWrite:           p.CacheWrite.String(),
		OutputUnderThreshold: p.OutputUnderThreshold.String(),
	}
	if p.HasThreshold {
		e.OutputAtThreshold = p.OutputAtThreshold.String()
		threshold := p.ContextLengthThreshold
		e.ContextLengthThreshold = &threshold
	}
	return e
}
