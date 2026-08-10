package domain

import "sort"

// FindingKind classifies why data was skipped, missing or suspect.
type FindingKind string

const (
	FindingMalformedLine       FindingKind = "malformed_line"
	FindingTruncatedLine       FindingKind = "truncated_line"
	FindingUnreadableFile      FindingKind = "unreadable_file"
	FindingUnknownSchema       FindingKind = "unknown_schema_version"
	FindingUnrecognisedHarness FindingKind = "unrecognised_harness"
	FindingMissingTokenUsage   FindingKind = "missing_token_usage"
	FindingMissingModel        FindingKind = "missing_model"
	FindingUnidentifiedEvent   FindingKind = "unidentified_event"
	FindingUnrecognisedFolder  FindingKind = "unrecognised_folder"
	FindingMissingEventFile    FindingKind = "missing_event_file"
	FindingInvalidPricingEntry FindingKind = "invalid_pricing_entry"

	// FindingCrossStreamRecord reports a usage_record whose record_id was
	// observed on more than one stream within a single run, violating the
	// format's single-stream invariant. The data is suspect, not partial:
	// this kind is deliberately NOT a member of dataLossKinds.
	FindingCrossStreamRecord FindingKind = "cross_stream_record"
)

// FindingSeverity lets a frontend decide prominence without parsing kinds.
type FindingSeverity uint8

const (
	SeverityInfo FindingSeverity = iota
	SeverityWarning
)

// Finding records something the tool could not use, so incompleteness is always
// communicable rather than merely survivable.
type Finding struct {
	Kind     FindingKind
	Severity FindingSeverity
	Path     string // file or directory, "" when not file-scoped
	Line     int    // 1-based; 0 when not line-scoped
	Run      RunRef
	Actor    ActorRef
	Detail   string // short human-readable explanation, no payload content
}

// QualitySummary is the report-level rollup a frontend renders.
type QualitySummary struct {
	Findings []Finding // deterministically sorted by Path, then Line, then Kind
	Counts   map[FindingKind]int
}

// IsClean reports whether there are no findings.
func (q QualitySummary) IsClean() bool { return len(q.Findings) == 0 }

// dataLossKinds is the set of finding kinds that indicate the underlying data
// is known to be partial (i.e. events were irrecoverably lost).
var dataLossKinds = map[FindingKind]bool{
	FindingMalformedLine:  true,
	FindingTruncatedLine:  true,
	FindingUnreadableFile: true,
	FindingMissingEventFile: true,
	FindingUnrecognisedFolder: true,
}

// Incomplete reports whether the data behind the report is known to be partial.
func (q QualitySummary) Incomplete() bool {
	for _, f := range q.Findings {
		if dataLossKinds[f.Kind] {
			return true
		}
	}
	return false
}

// NewQualitySummary builds a QualitySummary from a slice of findings.
// Findings are sorted deterministically by Path, then Line, then Kind.
func NewQualitySummary(findings []Finding) QualitySummary {
	if len(findings) == 0 {
		return QualitySummary{}
	}

	// Copy the slice so callers cannot mutate the original.
	sorted := make([]Finding, len(findings))
	copy(sorted, findings)

	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return string(a.Kind) < string(b.Kind)
	})

	counts := make(map[FindingKind]int, len(sorted))
	for _, f := range sorted {
		counts[f.Kind]++
	}

	return QualitySummary{
		Findings: sorted,
		Counts:   counts,
	}
}
