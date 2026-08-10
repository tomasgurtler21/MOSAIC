package domain_test

// Run identity contract tests (T10.1, T10.3): FormatRunID renders the
// logger bundle's required shape from an injected instant and entropy
// suffix rather than reading ambient time, ValidRunID accepts exactly that
// shape and rejects everything else, and RunIDPrelude carries the run id
// into the subject's prompt without test-revealing vocabulary.

import (
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
)

// TestFormatRunID_RendersTheLoggerBundlesRequiredShape pins the exact
// rendering: {YYYYMMDD}T{HHMMSS}Z-{suffix}, UTC, matching the layout
// ContractsDesign.md's "Run identity contract" documents
// (RunIDLayout = "20060102T150405Z-xxxx").
func TestFormatRunID_RendersTheLoggerBundlesRequiredShape(t *testing.T) {
	at := time.Date(2026, 8, 9, 17, 12, 29, 0, time.UTC)

	got := domain.FormatRunID(at, "79ca")

	want := "20260809T171229Z-79ca"
	if got != want {
		t.Errorf("FormatRunID(%v, %q) = %q, want %q", at, "79ca", got, want)
	}
}

// TestFormatRunID_ConvertsANonUTCInstantToUTCBeforeRendering asserts the
// rendering is always in UTC regardless of the input instant's own
// location, since the bundle's extraction has no notion of timezone.
func TestFormatRunID_ConvertsANonUTCInstantToUTCBeforeRendering(t *testing.T) {
	loc := time.FixedZone("UTC-5", -5*60*60)
	at := time.Date(2026, 8, 9, 12, 12, 29, 0, loc) // 17:12:29 UTC

	got := domain.FormatRunID(at, "79ca")

	want := "20260809T171229Z-79ca"
	if got != want {
		t.Errorf("FormatRunID with a non-UTC instant = %q, want %q (converted to UTC)", got, want)
	}
}

// TestFormatRunID_IsPureAndDeterministic asserts that FormatRunID depends
// on nothing but its two arguments: the same (instant, suffix) pair always
// renders the same id, so a test can pin an exact id with no clock or
// random source of its own.
func TestFormatRunID_IsPureAndDeterministic(t *testing.T) {
	at := time.Date(2026, 8, 9, 17, 12, 29, 0, time.UTC)

	first := domain.FormatRunID(at, "79ca")
	second := domain.FormatRunID(at, "79ca")

	if first != second {
		t.Errorf("FormatRunID(%v, %q) returned %q then %q, want the same value both times", at, "79ca", first, second)
	}
}

// TestValidRunID_AcceptsWhatFormatRunIDProduces closes the loop between the
// two functions: whatever FormatRunID renders, ValidRunID must accept —
// otherwise the suite's own default generator could produce an id its own
// validity check rejects.
func TestValidRunID_AcceptsWhatFormatRunIDProduces(t *testing.T) {
	at := time.Date(2026, 8, 9, 17, 12, 29, 0, time.UTC)
	id := domain.FormatRunID(at, "79ca")

	if !domain.ValidRunID(id) {
		t.Errorf("ValidRunID(%q) = false, want true — this is exactly what FormatRunID produced", id)
	}
}

// TestValidRunID_AcceptRejectTable pins the exact shape ValidRunID checks:
// eight digits, "T", six digits, "Z", "-", four lowercase hex digits — the
// only shape the logger bundle's extraction accepts. Anything else is
// silently bucketed under "unknown-run".
func TestValidRunID_AcceptRejectTable(t *testing.T) {
	cases := []struct {
		name string
		id   string
		want bool
	}{
		{"well-formed", "20260809T171229Z-79ca", true},
		{"well-formed, all-numeric suffix", "20260809T171229Z-0000", true},
		{"empty string", "", false},
		{"old testID-runNumber shape", "one-dispatch-1", false},
		{"uppercase hex suffix rejected", "20260809T171229Z-79CA", false},
		{"missing Z", "20260809T171229-79ca", false},
		{"missing T", "20260809171229Z-79ca", false},
		{"missing dash before suffix", "20260809T171229Z79ca", false},
		{"suffix too short", "20260809T171229Z-79c", false},
		{"suffix too long", "20260809T171229Z-79caa", false},
		{"date too short", "2026809T171229Z-79ca", false},
		{"trailing garbage", "20260809T171229Z-79ca-extra", false},
		{"leading garbage", "extra-20260809T171229Z-79ca", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := domain.ValidRunID(tc.id); got != tc.want {
				t.Errorf("ValidRunID(%q) = %v, want %v", tc.id, got, tc.want)
			}
		})
	}
}

// TestRunIDPrelude_ContainsTheRunID asserts the prelude actually carries the
// id it is given, since the bundle's extraction can only recover a run id
// that is present in the prompt text.
func TestRunIDPrelude_ContainsTheRunID(t *testing.T) {
	const runID = "20260809T171229Z-79ca"

	prelude := domain.RunIDPrelude(runID)

	if !strings.Contains(prelude, runID) {
		t.Errorf("RunIDPrelude(%q) = %q, want it to contain the run id", runID, prelude)
	}
}

// TestRunIDPrelude_IsNeverEmpty asserts the prelude is always non-empty text
// for a non-empty run id, so a caller prepending it to the subject's opening
// message always actually injects something.
func TestRunIDPrelude_IsNeverEmpty(t *testing.T) {
	prelude := domain.RunIDPrelude("20260809T171229Z-79ca")

	if prelude == "" {
		t.Error("RunIDPrelude returned an empty string, want non-empty prelude text")
	}
}

// TestRunIDPrelude_ContainsNoTestRevealingVocabulary is the transparency
// obligation ContractsDesign.md binds RunIDPrelude to: the same one
// intercept.EchoInstruction carries. A subject able to tell the prelude was
// injected to make it testable invalidates the measurement.
func TestRunIDPrelude_ContainsNoTestRevealingVocabulary(t *testing.T) {
	prelude := domain.RunIDPrelude("20260809T171229Z-79ca")

	lower := strings.ToLower(prelude)
	banned := []string{"test", "stub", "mock", "fixture", "fake", "harness", "assert", "verdict", "mosaic-agent-test"}
	for _, word := range banned {
		if strings.Contains(lower, word) {
			t.Errorf("RunIDPrelude contains test-revealing vocabulary %q: %q", word, prelude)
		}
	}
}
