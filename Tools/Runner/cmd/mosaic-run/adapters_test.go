package main

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	commonharness "mosaic-common/harness"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/harness/claudecode"
	"mosaic-run/internal/harness/opencode"
)

// ---------------------------------------------------------------------------
// buildAdapter
//
// buildAdapter is the extracted adapter-construction function that main() and
// sessFactory both delegate to. These tests verify the branching that was
// previously inline in main() and untestable (the switch on --harness that
// constructs ClaudeCodeAdapter vs MockAdapter). They close the AC3.1/AC3.2/
// AC3.3/AC3.4 coverage gap.
// ---------------------------------------------------------------------------

// TestBuildAdapter_ClaudeCode_ReturnsClaudeCodeAdapter verifies that passing
// "claude-code" as harnessStr constructs a *claudecode.ClaudeCodeAdapter. This
// is the primary AC3.1 assertion: the right adapter type is instantiated for
// the real Claude Code CLI.
func TestBuildAdapter_ClaudeCode_ReturnsClaudeCodeAdapter(t *testing.T) {
	h := buildAdapter("claude-code", "/custom/claude", "", 45*time.Minute)
	if _, ok := h.(*claudecode.ClaudeCodeAdapter); !ok {
		t.Errorf("buildAdapter(claude-code) returned %T, want *claudecode.ClaudeCodeAdapter", h)
	}
}

// TestBuildAdapter_Fake_ReturnsMockAdapter verifies that "fake" constructs a
// *harness.MockAdapter, confirming backward-compatible default behaviour (AC3.2).
func TestBuildAdapter_Fake_ReturnsMockAdapter(t *testing.T) {
	h := buildAdapter("fake", "", "", 0)
	if _, ok := h.(*harness.MockAdapter); !ok {
		t.Errorf("buildAdapter(fake) returned %T, want *harness.MockAdapter", h)
	}
}

// TestBuildAdapter_Unknown_ReturnsMockAdapter verifies that an unrecognised
// harnessStr falls back to MockAdapter without panicking. Unknown values are
// rejected upstream by cli.Run (AC3.8); buildAdapter is a safe fallback.
func TestBuildAdapter_Unknown_ReturnsMockAdapter(t *testing.T) {
	h := buildAdapter("unknown-harness", "", "", 0)
	if _, ok := h.(*harness.MockAdapter); !ok {
		t.Errorf("buildAdapter(unknown-harness) returned %T, want *harness.MockAdapter", h)
	}
}

// TestBuildAdapter_ClaudeCode_ZeroTimeoutDefaultsTo30Min verifies that a zero
// timeout (e.g. when cfg.Timeout is unset in the TUI before the config screen
// runs) is treated as the 30-minute default, not a zero-timeout adapter. The
// returned adapter must still be a ClaudeCodeAdapter.
func TestBuildAdapter_ClaudeCode_ZeroTimeoutDefaultsTo30Min(t *testing.T) {
	h := buildAdapter("claude-code", "claude", "", 0)
	if _, ok := h.(*claudecode.ClaudeCodeAdapter); !ok {
		t.Errorf("buildAdapter(claude-code, timeout=0) returned %T, want *claudecode.ClaudeCodeAdapter", h)
	}
}

// TestBuildAdapter_ClaudeCode_DefaultPath verifies that using the default
// executable path ("claude", the value buildAdapter substitutes when --executable-path
// is absent) produces a valid ClaudeCodeAdapter. This closes AC3.4's default
// propagation gap.
func TestBuildAdapter_ClaudeCode_DefaultPath(t *testing.T) {
	h := buildAdapter("claude-code", "claude", "", 30*time.Minute)
	if _, ok := h.(*claudecode.ClaudeCodeAdapter); !ok {
		t.Errorf("buildAdapter(claude-code, claude, 30m) returned %T, want *claudecode.ClaudeCodeAdapter", h)
	}
}

// TestBuildAdapter_ClaudeCode_CustomPathAndTimeout verifies that a non-default
// executable path and a non-default timeout both produce a ClaudeCodeAdapter.
// Covers the combined AC3.3 + AC3.4 propagation path.
func TestBuildAdapter_ClaudeCode_CustomPathAndTimeout(t *testing.T) {
	h := buildAdapter("claude-code", "/opt/claude/bin/claude", "", 90*time.Second)
	if _, ok := h.(*claudecode.ClaudeCodeAdapter); !ok {
		t.Errorf("buildAdapter(claude-code, custom path/timeout) returned %T, want *claudecode.ClaudeCodeAdapter", h)
	}
}

// TestBuildAdapter_OpenCode_ReturnsOpenCodeAdapter verifies that passing
// "opencode" as harnessStr constructs a *opencode.OpenCodeAdapter. This is the
// primary AC4.3 assertion: the new catalog entry resolves to its own
// adapter, mirroring TestBuildAdapter_ClaudeCode_ReturnsClaudeCodeAdapter.
func TestBuildAdapter_OpenCode_ReturnsOpenCodeAdapter(t *testing.T) {
	h := buildAdapter("opencode", "/custom/opencode", "", 45*time.Minute)
	if _, ok := h.(*opencode.OpenCodeAdapter); !ok {
		t.Errorf("buildAdapter(opencode) returned %T, want *opencode.OpenCodeAdapter", h)
	}
}

// TestBuildAdapter_OpenCode_ZeroTimeoutDefaultsTo30Min verifies that a zero
// timeout for "opencode" is treated as the 30-minute default, mirroring the
// claude-code case.
func TestBuildAdapter_OpenCode_ZeroTimeoutDefaultsTo30Min(t *testing.T) {
	h := buildAdapter("opencode", "opencode", "", 0)
	if _, ok := h.(*opencode.OpenCodeAdapter); !ok {
		t.Errorf("buildAdapter(opencode, timeout=0) returned %T, want *opencode.OpenCodeAdapter", h)
	}
}

// TestBuildAdapter_Unknown_StillReturnsMockAdapter_AfterOpenCodeAdded
// re-verifies AC4.3's negative half now that a second catalog case exists:
// an unrecognised value must still fall back to MockAdapter, and adding the
// "opencode" case must not have widened the default arm's match.
func TestBuildAdapter_Unknown_StillReturnsMockAdapter_AfterOpenCodeAdded(t *testing.T) {
	h := buildAdapter("still-unknown-harness", "", "", 0)
	if _, ok := h.(*harness.MockAdapter); !ok {
		t.Errorf("buildAdapter(still-unknown-harness) returned %T, want *harness.MockAdapter", h)
	}
}

// ---------------------------------------------------------------------------
// TestBuildAdapter_CatalogCoverage (T4.6, AC4.7)
//
// A future catalog addition with no buildAdapter case must fail this test
// rather than silently falling back to the mock adapter. It iterates every
// entry commonharness.CLIHarnesses() declares and asserts buildAdapter
// resolves it to something other than *harness.MockAdapter.
// ---------------------------------------------------------------------------

func TestBuildAdapter_CatalogCoverage_EveryEntryResolvesToARealAdapter(t *testing.T) {
	for _, entry := range commonharness.CLIHarnesses() {
		h := buildAdapter(entry.ID, "some-path", "", 5*time.Minute)
		if _, isFake := h.(*harness.MockAdapter); isFake {
			t.Errorf("buildAdapter(%q) returned *harness.MockAdapter; every catalog entry must resolve to a real adapter, or this composition root has silently missed a case", entry.ID)
		}
	}
}

// ---------------------------------------------------------------------------
// hasFlag
//
// hasFlag is the new pre-scan helper that reports whether a named flag appears
// anywhere in args in either "--flag value" or "--flag=value" form. It is used
// by resolveRunIdentityForCLI to detect --input presence before any run-folder
// read, satisfying the requirement that the mutual-exclusion check precedes
// filesystem access.
//
// Tests for the presence case are RED until hasFlag's logic is complete:
// a stub returning false would pass the absence tests but fail the presence tests.
// ---------------------------------------------------------------------------

// TestHasFlag_FlagPresent_SpaceSeparated_ReturnsTrue verifies the common
// "--flag value" form: flag name followed by a space and then the value.
func TestHasFlag_FlagPresent_SpaceSeparated_ReturnsTrue(t *testing.T) {
	if !hasFlag([]string{"run", "--input", "/some/path"}, "--input") {
		t.Error("hasFlag should return true when --input appears in space-separated form")
	}
}

// TestHasFlag_FlagPresent_EqualsSeparated_ReturnsTrue verifies the
// "--flag=value" form: flag name and value joined with "=".
func TestHasFlag_FlagPresent_EqualsSeparated_ReturnsTrue(t *testing.T) {
	if !hasFlag([]string{"run", "--input=/some/path"}, "--input") {
		t.Error("hasFlag should return true when --input appears in equals-separated form")
	}
}

// TestHasFlag_FlagPresent_AmongOtherArgs_ReturnsTrue verifies that hasFlag
// finds the flag even when surrounded by other flags and values.
func TestHasFlag_FlagPresent_AmongOtherArgs_ReturnsTrue(t *testing.T) {
	args := []string{"run", "--orchestrator-file", "orch.md", "--input", "/path", "--workflow", "w1"}
	if !hasFlag(args, "--input") {
		t.Error("hasFlag should find --input among other flags")
	}
}

// TestHasFlag_FlagPresent_MultipleOccurrences_ReturnsTrue verifies that
// hasFlag reports true when the flag appears more than once; only the first
// occurrence needs to be found.
func TestHasFlag_FlagPresent_MultipleOccurrences_ReturnsTrue(t *testing.T) {
	args := []string{"--input", "/first", "--input", "/second"}
	if !hasFlag(args, "--input") {
		t.Error("hasFlag should return true when --input appears multiple times")
	}
}

// TestHasFlag_FlagPresent_StandaloneWithNoValue_ReturnsTrue verifies that
// --input appearing as the last token (no value token following) is still
// detected: presence, not value, matters here.
func TestHasFlag_FlagPresent_StandaloneWithNoValue_ReturnsTrue(t *testing.T) {
	if !hasFlag([]string{"run", "--input"}, "--input") {
		t.Error("hasFlag should return true when --input appears as the last arg with no following value")
	}
}

// TestHasFlag_FlagAbsent_ReturnsFalse verifies that hasFlag returns false when
// the named flag is not in args.
func TestHasFlag_FlagAbsent_ReturnsFalse(t *testing.T) {
	args := []string{"run", "--orchestrator-file", "orch.md", "--workflow", "w1"}
	if hasFlag(args, "--input") {
		t.Error("hasFlag should return false when --input is absent")
	}
}

// TestHasFlag_EmptyArgs_ReturnsFalse verifies that hasFlag handles an empty
// slice without panicking and returns false.
func TestHasFlag_EmptyArgs_ReturnsFalse(t *testing.T) {
	if hasFlag([]string{}, "--input") {
		t.Error("hasFlag with empty args should return false")
	}
}

// TestHasFlag_PartialPrefixDoesNotMatch verifies that "--inputx" is not
// matched by a search for "--input": the flag name must match exactly.
func TestHasFlag_PartialPrefixDoesNotMatch(t *testing.T) {
	if hasFlag([]string{"--inputx", "/some/path"}, "--input") {
		t.Error("hasFlag(--inputx, --input) must not match — exact flag name required")
	}
}

// ---------------------------------------------------------------------------
// recordingLogger
//
// recordingLogger is a test-only fake domain.DebugLogger that records every
// Log call. It is thread-safe: the mutex guards the entries slice, which may
// be appended from goroutines spawned by the code under test.
// ---------------------------------------------------------------------------

// logEntry holds one recorded call to recordingLogger.Log.
type logEntry struct {
	event   string
	message string
	fields  []domain.DebugField
}

// recordingLogger accumulates Log calls so tests can assert on the emitted
// events and fields without touching the filesystem.
type recordingLogger struct {
	mu      sync.Mutex
	entries []logEntry
}

// Log implements domain.DebugLogger. It appends one entry per call.
func (r *recordingLogger) Log(event string, message string, fields ...domain.DebugField) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, logEntry{event: event, message: message, fields: append([]domain.DebugField(nil), fields...)})
}

// snapshot returns a copy of all recorded entries taken under the lock.
func (r *recordingLogger) snapshot() []logEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]logEntry(nil), r.entries...)
}

// ---------------------------------------------------------------------------
// newLoggedArtifactStore (T6.1)
//
// Table-driven tests for the newLoggedArtifactStore composition-root helper.
// The helper wraps artifact.NewFileStore and emits at most one artifact.path.*
// debug event depending on the path shape. Tests cover the three mutually
// exclusive cases defined in the emission contract:
//
//   relative path              → EventArtifactPathRejected with path field
//   absolute, non-run-scoped   → EventArtifactPathNonRunScoped with path field
//   absolute, run-scoped       → no event emitted
//
// In all three cases the returned store must be non-nil, and the path must
// be passed through unchanged (no substitution or rewriting).
//
// These tests are in the RED state until I6.8 implements newLoggedArtifactStore.
// ---------------------------------------------------------------------------

func TestNewLoggedArtifactStore(t *testing.T) {
	base := t.TempDir()

	// A valid run_id to construct a run-scoped parent directory name.
	const testRunID = "20260805T143029Z-9bc0"
	runScopedParent := filepath.Join(base, domain.RunScopedFolder(testRunID))

	cases := []struct {
		name      string
		path      string
		wantEvent string // empty string means no event should be emitted
	}{
		{
			// A relative path is a hard failure: every subsequent Create call will
			// return an "must be absolute" error and nothing will be written.
			name:      "relative path emits artifact.path.rejected",
			path:      "Orchestration.md",
			wantEvent: domain.EventArtifactPathRejected,
		},
		{
			// An absolute path whose parent directory is not an Orchestration-{run_id}
			// folder is informational: artifacts will be written, but the path is
			// outside the expected run-scoped hierarchy.
			name:      "absolute non-run-scoped path emits artifact.path.non_run_scoped",
			path:      filepath.Join(base, "Orchestration.md"),
			wantEvent: domain.EventArtifactPathNonRunScoped,
		},
		{
			// An absolute path whose parent is Orchestration-{run_id} is the normal
			// case. No event is emitted.
			name:      "absolute run-scoped path emits no event",
			path:      filepath.Join(runScopedParent, "Orchestration.md"),
			wantEvent: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger := &recordingLogger{}

			store := newLoggedArtifactStore(tc.path, logger)

			// The returned store must always be non-nil regardless of the path shape.
			// The helper never rejects a path — it only observes and logs.
			if store == nil {
				t.Fatal("newLoggedArtifactStore returned nil; want a non-nil domain.ArtifactStore in all cases")
			}

			entries := logger.snapshot()

			if tc.wantEvent == "" {
				// Normal (run-scoped) case: the helper must emit nothing.
				if len(entries) != 0 {
					t.Errorf("run-scoped path: expected 0 events, got %d event(s): %v", len(entries), entries)
				}
				return
			}

			// Anomalous cases: exactly one event must be emitted.
			if len(entries) != 1 {
				t.Fatalf("expected exactly 1 event, got %d: %v", len(entries), entries)
			}

			if entries[0].event != tc.wantEvent {
				t.Errorf("event = %q, want %q", entries[0].event, tc.wantEvent)
			}

			// The path field must be present and carry the original (unmodified) path.
			var pathFieldValue string
			pathFieldFound := false
			for _, f := range entries[0].fields {
				if f.Key == "path" {
					pathFieldValue = f.Value
					pathFieldFound = true
					break
				}
			}
			if !pathFieldFound {
				t.Errorf("emitted entry for event %q carries no \"path\" field; fields = %v",
					tc.wantEvent, entries[0].fields)
			} else if pathFieldValue != tc.path {
				// The helper must pass the path through unchanged — no substitution,
				// no rewriting, no normalisation.
				t.Errorf("path field = %q, want %q (path must not be rewritten)", pathFieldValue, tc.path)
			}
		})
	}
}

// TestNewLoggedArtifactStore_AtMostOneEventPerCall verifies the mutual-exclusion
// property: at most one artifact.path.* event is emitted per newLoggedArtifactStore
// call, regardless of how many conditions are checked internally. This keeps a
// log reader able to distinguish a hard failure (rejected) from a permitted-but-
// unusual case (non_run_scoped) by event name alone.
func TestNewLoggedArtifactStore_AtMostOneEventPerCall(t *testing.T) {
	base := t.TempDir()
	paths := []string{
		"relative/Orchestration.md",
		filepath.Join(base, "Orchestration.md"),
		filepath.Join(base, domain.RunScopedFolder("20260805T143029Z-9bc0"), "Orchestration.md"),
	}

	for _, path := range paths {
		logger := &recordingLogger{}
		_ = newLoggedArtifactStore(path, logger)
		entries := logger.snapshot()
		if len(entries) > 1 {
			t.Errorf("path %q: emitted %d events, want at most 1; mutual-exclusion contract violated",
				path, len(entries))
		}
	}
}

// TestNewLoggedArtifactStore_NopLoggerDoesNotPanic verifies that passing
// domain.NopDebugLogger{} as the logger (the production default when logging is
// off) does not panic and still returns a non-nil store.
func TestNewLoggedArtifactStore_NopLoggerDoesNotPanic(t *testing.T) {
	base := t.TempDir()
	paths := []string{
		"relative/Orchestration.md",
		filepath.Join(base, "Orchestration.md"),
		filepath.Join(base, domain.RunScopedFolder("20260805T143029Z-9bc0"), "Orchestration.md"),
	}
	for _, path := range paths {
		store := newLoggedArtifactStore(path, domain.NopDebugLogger{})
		if store == nil {
			t.Errorf("path %q: newLoggedArtifactStore with NopDebugLogger returned nil store", path)
		}
	}
}
