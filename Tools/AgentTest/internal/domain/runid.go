package domain

import (
	"fmt"
	"regexp"
	"time"
)

// RunIDLayout documents the shape the MOSAIC logger bundle's extraction
// accepts, and which anything else is silently bucketed under "unknown-run":
//
//	{YYYYMMDD}T{HHMMSS}Z-{4 lowercase hex}
//	e.g. 20260809T171229Z-79ca
//
// The bundle learns a run's identity from the prompt text alone. An id of any
// other shape cannot be attributed no matter where it is planted, which is
// why this is a format contract rather than a formatting convenience.
const RunIDLayout = "20060102T150405Z-xxxx"

// runIDPattern is the authoritative shape ValidRunID checks against: eight
// digits, "T", six digits, "Z", "-", four lowercase hex digits.
var runIDPattern = regexp.MustCompile(`^\d{8}T\d{6}Z-[0-9a-f]{4}$`)

// FormatRunID renders t (in UTC) and a four-hex-digit suffix into the layout.
// Pure: the caller supplies both the instant and the entropy, so a test pins
// an exact id without a clock or a random source of its own.
func FormatRunID(t time.Time, suffix string) string {
	return t.UTC().Format("20060102T150405Z") + "-" + suffix
}

// ValidRunID reports whether s matches the layout the bundle accepts.
func ValidRunID(s string) bool {
	return runIDPattern.MatchString(s)
}

// RunIDPrelude returns the text prepended to the subject's opening message so
// the bundle's extraction recovers the run id.
//
// Transparency contract, the same one EchoInstruction carries: the text
// contains no test-revealing vocabulary. It must read as ordinary
// orchestration preamble to a subject that reads it, because a subject able
// to tell it is being exercised invalidates the measurement.
func RunIDPrelude(runID string) string {
	return fmt.Sprintf("run_id: %s\n\n", runID)
}
