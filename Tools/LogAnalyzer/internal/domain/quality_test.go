package domain_test

// Tests for the data-quality finding type and QualitySummary shape.
//
// Design invariants under test:
//   - NewQualitySummary builds Counts from the finding slice and sorts Findings
//     deterministically by Path, then Line, then Kind (as specified by the design).
//   - QualitySummary.IsClean reports true iff there are no findings.
//   - QualitySummary.Incomplete reports whether the data behind the report is
//     known to be partial. A summary built from findings that indicate data loss
//     (e.g. malformed lines, missing files) must be incomplete; an empty summary
//     must not be.
//
// All assertions here call stub methods that panic("not implemented") until
// implementation is complete — this is the expected TDD RED-phase behavior.

import (
	"testing"

	"mosaic-log-analyzer/internal/domain"
)

// makeFinding constructs a minimal Finding for testing purposes.
// Severity is set to Warning because the test cases focus on data-loss scenarios.
func makeFinding(kind domain.FindingKind, path string, line int) domain.Finding {
	return domain.Finding{
		Kind:     kind,
		Severity: domain.SeverityWarning,
		Path:     path,
		Line:     line,
		Run:      domain.NamedRun("20260802T074635Z-480e"),
	}
}

// ---------------------------------------------------------------------------
// NewQualitySummary — construction from nil / empty slice
// ---------------------------------------------------------------------------

func TestNewQualitySummary_NilFindings_IsClean(t *testing.T) {
	q := domain.NewQualitySummary(nil)
	if !q.IsClean() {
		t.Error("NewQualitySummary(nil) must produce a clean summary")
	}
}

func TestNewQualitySummary_NilFindings_IsNotIncomplete(t *testing.T) {
	q := domain.NewQualitySummary(nil)
	if q.Incomplete() {
		t.Error("NewQualitySummary(nil) must not be incomplete")
	}
}

func TestNewQualitySummary_EmptySlice_IsClean(t *testing.T) {
	q := domain.NewQualitySummary([]domain.Finding{})
	if !q.IsClean() {
		t.Error("NewQualitySummary with empty findings slice must produce a clean summary")
	}
}

// ---------------------------------------------------------------------------
// NewQualitySummary — construction with findings
// ---------------------------------------------------------------------------

func TestNewQualitySummary_SingleFinding_NotClean(t *testing.T) {
	f := makeFinding(domain.FindingMalformedLine, "events.jsonl", 3)
	q := domain.NewQualitySummary([]domain.Finding{f})
	if q.IsClean() {
		t.Error("NewQualitySummary with one finding must not be clean")
	}
}

func TestNewQualitySummary_FindingsPreserved(t *testing.T) {
	f := makeFinding(domain.FindingMalformedLine, "events.jsonl", 1)
	q := domain.NewQualitySummary([]domain.Finding{f})
	if len(q.Findings) != 1 {
		t.Fatalf("NewQualitySummary preserved %d findings, want 1", len(q.Findings))
	}
	if q.Findings[0].Kind != domain.FindingMalformedLine {
		t.Errorf("Finding.Kind = %q, want FindingMalformedLine", q.Findings[0].Kind)
	}
}

func TestNewQualitySummary_CountsByKind_SingleKind(t *testing.T) {
	findings := []domain.Finding{
		makeFinding(domain.FindingMalformedLine, "a.jsonl", 1),
		makeFinding(domain.FindingMalformedLine, "a.jsonl", 2),
	}
	q := domain.NewQualitySummary(findings)
	if q.Counts[domain.FindingMalformedLine] != 2 {
		t.Errorf("Counts[FindingMalformedLine] = %d, want 2", q.Counts[domain.FindingMalformedLine])
	}
}

func TestNewQualitySummary_CountsByKind_MultipleKinds(t *testing.T) {
	findings := []domain.Finding{
		makeFinding(domain.FindingMalformedLine, "a.jsonl", 1),
		makeFinding(domain.FindingMalformedLine, "a.jsonl", 2),
		makeFinding(domain.FindingMissingTokenUsage, "a.jsonl", 5),
	}
	q := domain.NewQualitySummary(findings)
	if q.Counts[domain.FindingMalformedLine] != 2 {
		t.Errorf("Counts[FindingMalformedLine] = %d, want 2", q.Counts[domain.FindingMalformedLine])
	}
	if q.Counts[domain.FindingMissingTokenUsage] != 1 {
		t.Errorf("Counts[FindingMissingTokenUsage] = %d, want 1", q.Counts[domain.FindingMissingTokenUsage])
	}
}

func TestNewQualitySummary_CountsByKind_AbsentKind_IsZero(t *testing.T) {
	q := domain.NewQualitySummary([]domain.Finding{
		makeFinding(domain.FindingMalformedLine, "a.jsonl", 1),
	})
	// A kind not present in any finding should not appear in Counts (or be zero).
	if got := q.Counts[domain.FindingUnreadableFile]; got != 0 {
		t.Errorf("Counts[FindingUnreadableFile] = %d for absent kind, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// NewQualitySummary — deterministic sort order
// ---------------------------------------------------------------------------

func TestNewQualitySummary_FindingsSorted_ByPath(t *testing.T) {
	// Findings with different paths must be sorted ascending by path.
	findings := []domain.Finding{
		makeFinding(domain.FindingMalformedLine, "z.jsonl", 1),
		makeFinding(domain.FindingMalformedLine, "a.jsonl", 1),
	}
	q := domain.NewQualitySummary(findings)
	if len(q.Findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(q.Findings))
	}
	if q.Findings[0].Path != "a.jsonl" {
		t.Errorf("first finding path = %q, want a.jsonl", q.Findings[0].Path)
	}
	if q.Findings[1].Path != "z.jsonl" {
		t.Errorf("second finding path = %q, want z.jsonl", q.Findings[1].Path)
	}
}

func TestNewQualitySummary_FindingsSorted_ByPathThenLine(t *testing.T) {
	// Within the same path, findings must be sorted ascending by line number.
	findings := []domain.Finding{
		makeFinding(domain.FindingMalformedLine, "b.jsonl", 1),
		makeFinding(domain.FindingMalformedLine, "a.jsonl", 5),
		makeFinding(domain.FindingMalformedLine, "a.jsonl", 2),
	}
	q := domain.NewQualitySummary(findings)
	if len(q.Findings) != 3 {
		t.Fatalf("expected 3 findings, got %d", len(q.Findings))
	}
	// Expected order: a.jsonl:2, a.jsonl:5, b.jsonl:1
	if q.Findings[0].Path != "a.jsonl" || q.Findings[0].Line != 2 {
		t.Errorf("first finding = %q:%d, want a.jsonl:2", q.Findings[0].Path, q.Findings[0].Line)
	}
	if q.Findings[1].Path != "a.jsonl" || q.Findings[1].Line != 5 {
		t.Errorf("second finding = %q:%d, want a.jsonl:5", q.Findings[1].Path, q.Findings[1].Line)
	}
	if q.Findings[2].Path != "b.jsonl" || q.Findings[2].Line != 1 {
		t.Errorf("third finding = %q:%d, want b.jsonl:1", q.Findings[2].Path, q.Findings[2].Line)
	}
}

func TestNewQualitySummary_FindingsSorted_ByPathThenLineThenKind(t *testing.T) {
	// When path and line are identical, findings must be sorted by kind.
	findings := []domain.Finding{
		{Kind: domain.FindingMissingTokenUsage, Path: "a.jsonl", Line: 1,
			Severity: domain.SeverityWarning, Run: domain.NamedRun("20260802T074635Z-480e")},
		{Kind: domain.FindingMalformedLine, Path: "a.jsonl", Line: 1,
			Severity: domain.SeverityWarning, Run: domain.NamedRun("20260802T074635Z-480e")},
	}
	q := domain.NewQualitySummary(findings)
	if len(q.Findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(q.Findings))
	}
	// FindingKind is a string; "malformed_line" < "missing_token_usage" lexicographically.
	if q.Findings[0].Kind != domain.FindingMalformedLine {
		t.Errorf("first finding kind = %q, want FindingMalformedLine", q.Findings[0].Kind)
	}
	if q.Findings[1].Kind != domain.FindingMissingTokenUsage {
		t.Errorf("second finding kind = %q, want FindingMissingTokenUsage", q.Findings[1].Kind)
	}
}

// ---------------------------------------------------------------------------
// QualitySummary.IsClean
// ---------------------------------------------------------------------------

func TestQualitySummary_IsClean_ZeroValue_True(t *testing.T) {
	// The zero value of QualitySummary (no findings) must be clean.
	var q domain.QualitySummary
	if !q.IsClean() {
		t.Error("zero-value QualitySummary must be clean")
	}
}

func TestQualitySummary_IsClean_WithFindings_False(t *testing.T) {
	q := domain.QualitySummary{
		Findings: []domain.Finding{
			makeFinding(domain.FindingMalformedLine, "a.jsonl", 1),
		},
	}
	if q.IsClean() {
		t.Error("QualitySummary with findings must not be clean")
	}
}

// ---------------------------------------------------------------------------
// QualitySummary.Incomplete
// ---------------------------------------------------------------------------

func TestQualitySummary_Incomplete_NoFindings_False(t *testing.T) {
	q := domain.NewQualitySummary(nil)
	if q.Incomplete() {
		t.Error("QualitySummary with no findings must not be incomplete")
	}
}

func TestQualitySummary_Incomplete_MalformedLineFindings_True(t *testing.T) {
	// A malformed line means event data was irrecoverably lost; the data behind
	// the report is partial. A QualitySummary built from such findings must be
	// incomplete.
	findings := []domain.Finding{
		makeFinding(domain.FindingMalformedLine, "events.jsonl", 7),
	}
	q := domain.NewQualitySummary(findings)
	if !q.Incomplete() {
		t.Error("QualitySummary with malformed-line findings must be incomplete")
	}
}

func TestQualitySummary_Incomplete_UnreadableFileFindings_True(t *testing.T) {
	// An unreadable file means all events in that file were lost.
	findings := []domain.Finding{
		{
			Kind:     domain.FindingUnreadableFile,
			Severity: domain.SeverityWarning,
			Path:     "03_events.jsonl",
			Run:      domain.NamedRun("20260802T074635Z-480e"),
		},
	}
	q := domain.NewQualitySummary(findings)
	if !q.Incomplete() {
		t.Error("QualitySummary with unreadable-file findings must be incomplete")
	}
}
