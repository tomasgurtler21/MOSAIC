package domain_test

// Tests for run_id minting and run-scoped folder helpers.
//
// Coverage:
//
//   NewRunID — format and determinism:
//   - Minted run_id matches the format ^\d{8}T\d{6}Z-[0-9a-f]{4}$.
//   - With fixed clock and fixed random source, NewRunID returns the expected value.
//   - Date part (YYYYMMDD) reflects the clock's calendar date (UTC).
//   - Time part (HHMMSS) reflects the clock's time-of-day (UTC).
//   - Hex suffix comes from the RandomSource.HexSuffix() value.
//   - Two calls with the same clock but different random sources return different run_ids.
//
//   RunScopedFolder:
//   - Returns "Orchestration-{run_id}" for any non-empty run_id.
//
//   ParseRunFolder:
//   - "Orchestration-{run_id}" → returns (run_id, true).
//   - Name not starting with "Orchestration-" → returns ("", false).
//   - Empty string → returns ("", false).
//   - "Orchestration-" with no suffix → returns ("", false).
//   - "Orchestration-{run_id}" round-trips with RunScopedFolder.

import (
	"regexp"
	"testing"
	"time"

	"mosaic-run/internal/domain"
)

// ---- test doubles ----

// fixedClockForRunID is a Clock implementation that always returns a fixed time.
// Used to produce deterministic NewRunID output in tests.
type fixedClockForRunID struct {
	t time.Time
}

func (c fixedClockForRunID) Now() time.Time { return c.t }

// fixedRand is a RandomSource that always returns the same suffix.
// Used to produce deterministic NewRunID output in tests.
type fixedRand struct {
	suffix string
}

func (r fixedRand) HexSuffix() string { return r.suffix }

// ---- helpers ----

// runIDRegex is the canonical run_id format pattern.
var runIDRegex = regexp.MustCompile(`^\d{8}T\d{6}Z-[0-9a-f]{4}$`)

// testClock returns a fixed clock for 2026-07-27 17:00:00 UTC.
func testClock() fixedClockForRunID {
	return fixedClockForRunID{t: time.Date(2026, 7, 27, 17, 0, 0, 0, time.UTC)}
}

// ---- NewRunID: format ----

func TestNewRunID_MatchesFormat(t *testing.T) {
	// A minted run_id must match ^\d{8}T\d{6}Z-[0-9a-f]{4}$.
	got := domain.NewRunID(testClock(), fixedRand{suffix: "a3f9"})

	if !runIDRegex.MatchString(got) {
		t.Errorf("NewRunID() = %q, does not match expected format %q", got, runIDRegex.String())
	}
}

func TestNewRunID_Deterministic_WithFixedInputs(t *testing.T) {
	// With fixed clock and fixed random source, NewRunID must return
	// the deterministic expected value.
	clock := fixedClockForRunID{t: time.Date(2026, 7, 27, 17, 0, 0, 0, time.UTC)}
	rand := fixedRand{suffix: "a3f9"}
	want := "20260727T170000Z-a3f9"

	got := domain.NewRunID(clock, rand)

	if got != want {
		t.Errorf("NewRunID() = %q, want %q", got, want)
	}
}

func TestNewRunID_DatePart_MatchesClock(t *testing.T) {
	// The YYYYMMDD part of the run_id must reflect the clock's calendar date in UTC.
	clock := fixedClockForRunID{t: time.Date(2026, 7, 27, 17, 0, 0, 0, time.UTC)}
	rand := fixedRand{suffix: "0000"}

	got := domain.NewRunID(clock, rand)

	// First 8 characters must be "20260727".
	if len(got) < 8 || got[:8] != "20260727" {
		t.Errorf("NewRunID() = %q, want date part %q", got, "20260727")
	}
}

func TestNewRunID_TimePart_MatchesClock(t *testing.T) {
	// The HHMMSS part (characters 9-14, after "T") must reflect the clock's
	// time-of-day in UTC.
	clock := fixedClockForRunID{t: time.Date(2026, 7, 27, 17, 30, 45, 0, time.UTC)}
	rand := fixedRand{suffix: "0000"}

	got := domain.NewRunID(clock, rand)

	// Characters at positions 9-14 (0-indexed) must be "173045".
	// Format: "20260727T173045Z-0000"
	//          01234567890123456789
	if len(got) < 15 || got[9:15] != "173045" {
		t.Errorf("NewRunID() = %q, want time part %q at positions 9-14", got, "173045")
	}
}

func TestNewRunID_HexSuffix_ComesFromRandomSource(t *testing.T) {
	// The 4-char hex suffix must come from RandomSource.HexSuffix().
	clock := testClock()
	rand := fixedRand{suffix: "b7e2"}

	got := domain.NewRunID(clock, rand)

	// The suffix is the last 4 characters, after the "-".
	if len(got) < 4 || got[len(got)-4:] != "b7e2" {
		t.Errorf("NewRunID() = %q, want hex suffix %q", got, "b7e2")
	}
}

func TestNewRunID_DifferentSuffix_WhenRandomDiffers(t *testing.T) {
	// Two calls with the same clock but different random sources must produce
	// different run_ids. This verifies that the random suffix is actually used
	// and not ignored.
	clock := testClock()
	id1 := domain.NewRunID(clock, fixedRand{suffix: "a3f9"})
	id2 := domain.NewRunID(clock, fixedRand{suffix: "b7e2"})

	if id1 == id2 {
		t.Errorf("NewRunID with different suffixes must differ, but both returned %q", id1)
	}
}

// ---- RunScopedFolder ----

func TestRunScopedFolder_ProducesOrchestrationPrefix(t *testing.T) {
	// RunScopedFolder must prefix the run_id with "Orchestration-".
	const runID = "20260727T170000Z-a3f9"
	want := "Orchestration-" + runID

	got := domain.RunScopedFolder(runID)

	if got != want {
		t.Errorf("RunScopedFolder(%q) = %q, want %q", runID, got, want)
	}
}

func TestRunScopedFolder_AnotherRunID(t *testing.T) {
	// Verify RunScopedFolder works for a different run_id value (not a one-time fluke).
	const runID = "20260101T000000Z-ffff"
	want := "Orchestration-" + runID

	got := domain.RunScopedFolder(runID)

	if got != want {
		t.Errorf("RunScopedFolder(%q) = %q, want %q", runID, got, want)
	}
}

// ---- ParseRunFolder ----

func TestParseRunFolder_ValidPattern_ReturnsRunID(t *testing.T) {
	// ParseRunFolder must extract the run_id from a valid folder name.
	const runID = "20260727T170000Z-a3f9"
	folderName := "Orchestration-" + runID

	gotRunID, _ := domain.ParseRunFolder(folderName)

	if gotRunID != runID {
		t.Errorf("ParseRunFolder(%q) runID = %q, want %q", folderName, gotRunID, runID)
	}
}

func TestParseRunFolder_ValidPattern_ReturnsTrue(t *testing.T) {
	// ParseRunFolder must return ok=true for a valid folder name.
	const runID = "20260727T170000Z-a3f9"
	folderName := "Orchestration-" + runID

	_, ok := domain.ParseRunFolder(folderName)

	if !ok {
		t.Errorf("ParseRunFolder(%q): want ok=true, got false", folderName)
	}
}

func TestParseRunFolder_NotOrchestrationPrefix_ReturnsFalse(t *testing.T) {
	// A folder name that does not start with "Orchestration-" must not match.
	folderName := "SomethingElse-20260727T170000Z-a3f9"

	gotRunID, ok := domain.ParseRunFolder(folderName)

	if ok {
		t.Errorf("ParseRunFolder(%q): want ok=false for non-matching name, got true with runID=%q", folderName, gotRunID)
	}
}

func TestParseRunFolder_EmptyString_ReturnsFalse(t *testing.T) {
	// An empty string must not match.
	gotRunID, ok := domain.ParseRunFolder("")

	if ok {
		t.Errorf("ParseRunFolder(%q): want ok=false, got true with runID=%q", "", gotRunID)
	}
}

func TestParseRunFolder_JustPrefix_ReturnsFalse(t *testing.T) {
	// "Orchestration-" with no run_id suffix must not match.
	// An empty run_id is not valid.
	folderName := "Orchestration-"

	gotRunID, ok := domain.ParseRunFolder(folderName)

	if ok {
		t.Errorf("ParseRunFolder(%q): want ok=false (no run_id after prefix), got true with runID=%q", folderName, gotRunID)
	}
}

func TestParseRunFolder_PrefixWithoutDash_ReturnsFalse(t *testing.T) {
	// "Orchestration" alone (no dash) must not match.
	folderName := "Orchestration"

	gotRunID, ok := domain.ParseRunFolder(folderName)

	if ok {
		t.Errorf("ParseRunFolder(%q): want ok=false, got true with runID=%q", folderName, gotRunID)
	}
}

// ---- RunScopedFolder / ParseRunFolder round-trip ----

func TestRunScopedFolder_ParseRunFolder_RoundTrip(t *testing.T) {
	// ParseRunFolder(RunScopedFolder(id)) must return (id, true) for any
	// valid run_id. This verifies that the two functions are genuine inverses.
	const runID = "20260727T170000Z-a3f9"

	folder := domain.RunScopedFolder(runID)
	gotRunID, ok := domain.ParseRunFolder(folder)

	if !ok {
		t.Errorf("ParseRunFolder(RunScopedFolder(%q)): want ok=true, got false", runID)
	}
	if gotRunID != runID {
		t.Errorf("ParseRunFolder(RunScopedFolder(%q)) = %q, want %q", runID, gotRunID, runID)
	}
}
