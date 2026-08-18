package domain

import (
	crand "crypto/rand"
	"fmt"
	"regexp"
	"strings"
)

// runIDPattern is the canonical run_id format: {YYYYMMDD}T{HHMMSS}Z-{4-hex}.
var runIDPattern = regexp.MustCompile(`^\d{8}T\d{6}Z-[0-9a-f]{4}$`)

// IsValidRunID reports whether runID matches the canonical run_id format
// {YYYYMMDD}T{HHMMSS}Z-{4-hex}, e.g. "20260727T170000Z-a3f9".
func IsValidRunID(runID string) bool {
	return runIDPattern.MatchString(runID)
}

// RandomSource provides random bytes for run_id minting.
// Injected so tests can produce deterministic output.
type RandomSource interface {
	// HexSuffix returns a 4-character lowercase hex string.
	HexSuffix() string
}

// NewRunID mints a new run_id in the format {YYYYMMDD}T{HHMMSS}Z-{4-hex}.
// The clock and random source are injected for deterministic testing.
//
// Format: {YYYYMMDD}T{HHMMSS}Z-{4-hex}, e.g. "20260727T170000Z-a3f9".
// This matches the regex: ^\d{8}T\d{6}Z-[0-9a-f]{4}$
func NewRunID(clock Clock, rand RandomSource) string {
	t := clock.Now().UTC()
	return t.Format("20060102T150405Z") + "-" + rand.HexSuffix()
}

// cryptoRandSource is a RandomSource backed by crypto/rand.
type cryptoRandSource struct{}

func (cryptoRandSource) HexSuffix() string {
	var b [2]byte
	_, _ = crand.Read(b[:])
	return fmt.Sprintf("%02x%02x", b[0], b[1])
}

// DefaultRandomSource returns a RandomSource backed by crypto/rand.
func DefaultRandomSource() RandomSource {
	return cryptoRandSource{}
}

// RunScopedFolder returns the folder name for a given run_id.
// Example: RunScopedFolder("20260727T170000Z-a3f9") returns "Orchestration-20260727T170000Z-a3f9".
func RunScopedFolder(runID string) string {
	return "Orchestration-" + runID
}

// ParseRunFolder extracts the run_id from a folder name matching
// the Orchestration-{run_id} pattern. Returns ("", false) if the name
// does not match.
func ParseRunFolder(name string) (runID string, ok bool) {
	const prefix = "Orchestration-"
	if !strings.HasPrefix(name, prefix) {
		return "", false
	}
	suffix := name[len(prefix):]
	if suffix == "" {
		return "", false
	}
	return suffix, true
}
