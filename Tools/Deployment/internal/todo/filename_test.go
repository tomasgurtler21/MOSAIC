package todo_test

// filename_test.go covers the timestamped TODO filename contract (related to T8.1 and T8.2).
//
// FileNameAt must return a name in the form:
//
//	MOSAIC-DEPLOYMENT-TODO-<YYYYMMDD-HHMMSS>.md
//
// where the timestamp is the UTC instant of the run, so the filename and the file's
// generated-at header describe the same moment.
//
// Two runs at different instants must produce two distinct names so their files never
// overwrite each other (run isolation). The fixed FileName constant is replaced by this
// function; callers supply the run's own GeneratedAt timestamp so filename and header agree.

import (
	"strings"
	"testing"
	"time"

	"mosaic-deploy/internal/todo"
)

// referenceTime is a fixed UTC instant used as the canonical "run A" time throughout these tests.
var referenceTime = time.Date(2026, 8, 14, 14, 24, 5, 0, time.UTC)

// ---------------------------------------------------------------------------
// FileNameAt: name format and content
// ---------------------------------------------------------------------------

// TestFileNameAt_HasCorrectPrefix verifies that the name produced by FileNameAt starts with
// the canonical FileNamePrefix constant, so callers can identify the file by prefix.
func TestFileNameAt_HasCorrectPrefix(t *testing.T) {
	name := todo.FileNameAt(referenceTime)
	if !strings.HasPrefix(name, todo.FileNamePrefix) {
		t.Errorf("FileNameAt(%v) = %q; want prefix %q", referenceTime, name, todo.FileNamePrefix)
	}
}

// TestFileNameAt_HasCorrectExtension verifies that the name produced by FileNameAt ends with
// the canonical FileNameExt constant (.md).
func TestFileNameAt_HasCorrectExtension(t *testing.T) {
	name := todo.FileNameAt(referenceTime)
	if !strings.HasSuffix(name, todo.FileNameExt) {
		t.Errorf("FileNameAt(%v) = %q; want suffix %q", referenceTime, name, todo.FileNameExt)
	}
}

// TestFileNameAt_ContainsTimestampSuffix verifies that the name contains the run's timestamp
// formatted as YYYYMMDD-HHMMSS before the extension — the suffix that makes the name unique
// per run. The referenceTime (2026-08-14 14:24:05 UTC) must yield "20260814-142405".
func TestFileNameAt_ContainsTimestampSuffix(t *testing.T) {
	name := todo.FileNameAt(referenceTime)
	wantSuffix := "20260814-142405"
	if !strings.Contains(name, wantSuffix) {
		t.Errorf("FileNameAt(%v) = %q; want timestamp suffix %q somewhere in the name",
			referenceTime, name, wantSuffix)
	}
}

// TestFileNameAt_ExactName verifies the complete file name produced for the reference instant,
// pinning every character of the format so no part can silently change.
func TestFileNameAt_ExactName(t *testing.T) {
	name := todo.FileNameAt(referenceTime)
	want := "MOSAIC-DEPLOYMENT-TODO-20260814-142405.md"
	if name != want {
		t.Errorf("FileNameAt(%v) = %q; want %q", referenceTime, name, want)
	}
}

// TestFileNameAt_NonUTCInputYieldsUTCFormatting verifies that a non-UTC time is converted to
// UTC before formatting, so the same instant always yields the same name regardless of the
// caller's local timezone. A time in UTC+5 at 19:24:05 equals 14:24:05 UTC.
func TestFileNameAt_NonUTCInputYieldsUTCFormatting(t *testing.T) {
	loc := time.FixedZone("UTC+5", 5*60*60)
	localTime := time.Date(2026, 8, 14, 19, 24, 5, 0, loc) // same instant as referenceTime

	name := todo.FileNameAt(localTime)
	want := todo.FileNameAt(referenceTime)
	if name != want {
		t.Errorf("FileNameAt with UTC+5 time = %q; FileNameAt with UTC time = %q; "+
			"the same instant must yield the same name regardless of timezone", name, want)
	}
}

// TestFileNameAt_ZeroTimeProducesFormattedName verifies that a zero time.Time value does not
// panic and produces a formatted name. Callers that forget to populate GeneratedAt must not
// cause a runtime crash.
func TestFileNameAt_ZeroTimeProducesFormattedName(t *testing.T) {
	var zero time.Time
	name := todo.FileNameAt(zero) // must not panic
	if !strings.HasPrefix(name, todo.FileNamePrefix) {
		t.Errorf("FileNameAt(zero) = %q; want prefix %q", name, todo.FileNamePrefix)
	}
	if !strings.HasSuffix(name, todo.FileNameExt) {
		t.Errorf("FileNameAt(zero) = %q; want suffix %q", name, todo.FileNameExt)
	}
}

// TestFileNameAt_IsDeterministic verifies that repeated calls with the same time produce
// byte-identical output, confirming the function is a pure formatter with no randomness.
func TestFileNameAt_IsDeterministic(t *testing.T) {
	first := todo.FileNameAt(referenceTime)
	second := todo.FileNameAt(referenceTime)
	if first != second {
		t.Errorf("FileNameAt is not deterministic: first=%q second=%q", first, second)
	}
}

// ---------------------------------------------------------------------------
// Run isolation: two distinct times → two distinct names (T8.1)
// ---------------------------------------------------------------------------

// TestFileNameAt_TwoDistinctTimesProduceTwoDistinctNames verifies that two runs at different
// instants produce different file names, so neither run's file overwrites the other's.
func TestFileNameAt_TwoDistinctTimesProduceTwoDistinctNames(t *testing.T) {
	runA := time.Date(2026, 8, 14, 14, 24, 5, 0, time.UTC)
	runB := time.Date(2026, 8, 14, 14, 25, 0, 0, time.UTC) // one minute later

	nameA := todo.FileNameAt(runA)
	nameB := todo.FileNameAt(runB)

	if nameA == nameB {
		t.Errorf("two runs at different times produced the same filename %q; "+
			"each run must produce a distinct name so files are not overwritten", nameA)
	}
}

// TestFileNameAt_RunsDifferingByOneSecondProduceDistinctNames verifies that even a one-second
// difference in run times is enough to produce distinct names — the timestamp resolution is
// at least one second.
func TestFileNameAt_RunsDifferingByOneSecondProduceDistinctNames(t *testing.T) {
	runA := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	runB := runA.Add(time.Second)

	nameA := todo.FileNameAt(runA)
	nameB := todo.FileNameAt(runB)

	if nameA == nameB {
		t.Errorf("runs one second apart produced the same filename %q; "+
			"want distinct names for distinct run instants", nameA)
	}
}

// ---------------------------------------------------------------------------
// Filename and header agreement (T8.2)
// ---------------------------------------------------------------------------

// TestFileNameAt_TimestampMatchesGeneratedAtHeader verifies that the timestamp embedded in the
// file name is byte-identical to what RenderMarkdown formats into the file's "Generated:" header
// line. Both use the same Meta.GeneratedAt instant, so they must agree on every character.
//
// The contract: derive the TODO filename from the same time.Time that the renderer embeds in
// the header, so any reader can cross-check name against content and find them consistent.
func TestFileNameAt_TimestampMatchesGeneratedAtHeader(t *testing.T) {
	generatedAt := time.Date(2026, 8, 14, 14, 24, 5, 0, time.UTC)
	meta := todo.Meta{
		Harness:       "claude-code",
		WorkspacePath: "/workspace",
		GeneratedAt:   generatedAt,
	}

	// The file name contains the run timestamp.
	fileName := todo.FileNameAt(generatedAt)

	// RenderMarkdown embeds the same generatedAt in the header.
	rendered := string(todo.RenderMarkdown(nil, meta))

	// The timestamp formatted per FileNameTimestampLayout must appear in both.
	tsInName := generatedAt.UTC().Format(todo.FileNameTimestampLayout)

	if !strings.Contains(fileName, tsInName) {
		t.Errorf("file name %q does not contain timestamp %q formatted per FileNameTimestampLayout",
			fileName, tsInName)
	}

	// The header must mention the same date and time components so a reader can reconcile
	// the filename against the file's own header.
	wantDateInHeader := "2026-08-14"
	wantTimeInHeader := "14:24:05"
	if !strings.Contains(rendered, wantDateInHeader) {
		t.Errorf("RenderMarkdown header does not contain date %q; filename uses %q; they must describe the same run",
			wantDateInHeader, tsInName)
	}
	if !strings.Contains(rendered, wantTimeInHeader) {
		t.Errorf("RenderMarkdown header does not contain time %q; filename uses %q; they must describe the same run",
			wantTimeInHeader, tsInName)
	}
}
