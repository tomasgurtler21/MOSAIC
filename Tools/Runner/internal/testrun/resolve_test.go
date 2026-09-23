// Tests for ResolveHarnessBinaries and the harness binary-name coverage guard.
//
// Test coverage:
//
//   - Empty input returns a non-nil empty map and nil error.
//   - A single harness resolves to an absolute path when lookPath returns an
//     absolute path.
//   - Multiple harnesses all resolve when lookPath succeeds for each.
//   - Absolute path guarantee: a relative path returned by lookPath is
//     absolutized via filepath.Abs; the result is guaranteed absolute.
//   - ErrDot handling: a path returned alongside exec.ErrDot is treated as
//     non-fatal and absolutized; the function succeeds.
//   - Resolution failure: error contains the harness binary name, the searched
//     PATH value, and the underlying failure reason.
//   - errors.Is chain: the returned resolution error wraps the lookPath error.
//   - "fake" harness is silently skipped: not present in the result map, no
//     error, no lookPath call.
//   - "fake" alongside real harnesses: only the real harnesses are resolved.
//   - Fail-fast: when the first harness fails, the function returns immediately
//     without calling lookPath for subsequent harnesses.
//   - Unknown harness ID: error identifies the unrecognized ID.
//   - Binary name per harness ID: claude-code -> "claude", opencode ->
//     "opencode", ghcp-cli -> "copilot".
//   - Already-absolute path (including a Windows .cmd path with spaces) is
//     returned unchanged without modification.
//   - Duplicate harness IDs in the input slice are resolved once; lookPath is
//     called only for the first occurrence.
//   - Coverage guard: every harness ID returned by harness.CLIHarnesses() has
//     a binary-name mapping in ResolveHarnessBinaries; the test fails when a
//     new harness is added to the catalog without updating the mapping.
package testrun_test

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	commonharness "mosaic-common/harness"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/testrun"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// fixedLookPath returns a lookPath function that always returns the given path
// and error, regardless of the binary name.
func fixedLookPath(path string, err error) func(string) (string, error) {
	return func(_ string) (string, error) {
		return path, err
	}
}

// recordingLookPath is a lookPath that records every binary name it was called
// with and returns a fixed path for all calls.
type recordingLookPath struct {
	called []string
	result string
}

func (r *recordingLookPath) fn() func(string) (string, error) {
	return func(binary string) (string, error) {
		r.called = append(r.called, binary)
		return r.result, nil
	}
}

// perBinaryLookPath returns a lookPath that dispatches per binary name.
// Known names return the provided path; unknown names return an error.
func perBinaryLookPath(m map[string]string) func(string) (string, error) {
	return func(binary string) (string, error) {
		if p, ok := m[binary]; ok {
			return p, nil
		}
		return "", fmt.Errorf("binary %q not found", binary)
	}
}

// ---------------------------------------------------------------------------
// Empty input
// ---------------------------------------------------------------------------

func TestResolveHarnessBinaries_EmptyHarnessesSlice_ReturnsNonNilEmptyMap(t *testing.T) {
	// Arrange
	lookPath := fixedLookPath("/irrelevant/path", nil)

	// Act
	result, err := testrun.ResolveHarnessBinaries(nil, lookPath)

	// Assert
	if err != nil {
		t.Fatalf("expected nil error for empty input, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil map for empty input, got nil")
	}
	if len(result) != 0 {
		t.Fatalf("expected empty map for empty input, got %d entries", len(result))
	}
}

func TestResolveHarnessBinaries_EmptySlice_ReturnsNonNilEmptyMap(t *testing.T) {
	// Arrange
	lookPath := fixedLookPath("/irrelevant/path", nil)

	// Act
	result, err := testrun.ResolveHarnessBinaries([]string{}, lookPath)

	// Assert
	if err != nil {
		t.Fatalf("expected nil error for empty slice, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil map for empty slice, got nil")
	}
}

// ---------------------------------------------------------------------------
// Successful resolution
// ---------------------------------------------------------------------------

func TestResolveHarnessBinaries_SingleHarness_ResolvesToAbsolutePath(t *testing.T) {
	// Arrange: lookPath returns an absolute path.
	const absPath = "/usr/local/bin/claude"
	lookPath := fixedLookPath(absPath, nil)

	// Act
	result, err := testrun.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := result["claude-code"]
	if !ok {
		t.Fatal("result map missing key claude-code")
	}
	if got != absPath {
		t.Errorf("result[claude-code] = %q, want %q", got, absPath)
	}
}

func TestResolveHarnessBinaries_AllThreeHarnesses_AllResolve(t *testing.T) {
	// Arrange: each harness gets its own absolute path.
	paths := map[string]string{
		"claude":   "/usr/local/bin/claude",
		"opencode": "/usr/local/bin/opencode",
		"copilot":  "/usr/local/bin/copilot",
	}
	lookPath := perBinaryLookPath(paths)

	// Act
	harnesses := []string{"claude-code", "opencode", "ghcp-cli"}
	result, err := testrun.ResolveHarnessBinaries(harnesses, lookPath)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, id := range harnesses {
		if _, ok := result[id]; !ok {
			t.Errorf("result map missing key %q", id)
		}
	}
}

// ---------------------------------------------------------------------------
// Absolute path guarantee
// ---------------------------------------------------------------------------

func TestResolveHarnessBinaries_RelativePathFromLookPath_IsAbsolutized(t *testing.T) {
	// Arrange: lookPath returns a relative path (no error).
	// The function must absolutize it via filepath.Abs.
	lookPath := fixedLookPath("./claude", nil)

	// Act
	result, err := testrun.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := result["claude-code"]
	if !filepath.IsAbs(got) {
		t.Errorf("expected absolute path, got %q", got)
	}
}

func TestResolveHarnessBinaries_RelativePathFromLookPath_ContainsOriginalBasename(t *testing.T) {
	// Arrange: lookPath returns a relative path.
	lookPath := fixedLookPath("./claude", nil)

	// Act
	result, err := testrun.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := result["claude-code"]
	if filepath.Base(got) != "claude" {
		t.Errorf("absolutized path basename = %q, want %q", filepath.Base(got), "claude")
	}
}

// ---------------------------------------------------------------------------
// ErrDot handling
// ---------------------------------------------------------------------------

func TestResolveHarnessBinaries_ErrDot_IsNonFatalAndPathIsAbsolutized(t *testing.T) {
	// Arrange: lookPath returns a relative path WITH exec.ErrDot.
	// ErrDot must be treated as non-fatal; the function must absolutize.
	lookPath := func(binary string) (string, error) {
		return "./claude", exec.ErrDot
	}

	// Act
	result, err := testrun.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

	// Assert
	if err != nil {
		t.Fatalf("ErrDot with non-empty path should not cause an error, got: %v", err)
	}
	got := result["claude-code"]
	if !filepath.IsAbs(got) {
		t.Errorf("expected absolute path after ErrDot handling, got %q", got)
	}
}

func TestResolveHarnessBinaries_ErrDotWithEmptyPath_ReturnsError(t *testing.T) {
	// Arrange: lookPath returns empty path WITH exec.ErrDot.
	// An empty path is treated as a resolution failure.
	lookPath := func(binary string) (string, error) {
		return "", exec.ErrDot
	}

	// Act
	_, err := testrun.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

	// Assert
	if err == nil {
		t.Fatal("expected error for empty path with ErrDot, got nil")
	}
	// Error must name the binary so the user can diagnose the problem.
	if !strings.Contains(err.Error(), "claude") {
		t.Errorf("error for empty-path-with-ErrDot must name the binary, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Resolution failure errors
// ---------------------------------------------------------------------------

func TestResolveHarnessBinaries_MissingBinary_ErrorContainsBinaryName(t *testing.T) {
	// Arrange
	lookErr := fmt.Errorf("executable file not found in $PATH")
	lookPath := fixedLookPath("", lookErr)

	// Act
	_, err := testrun.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

	// Assert
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
	// The binary name "claude" (not the harness ID) must appear in the error.
	if !strings.Contains(err.Error(), "claude") {
		t.Errorf("error message does not contain binary name %q: %v", "claude", err)
	}
}

func TestResolveHarnessBinaries_MissingBinary_ErrorContainsPATH(t *testing.T) {
	// Arrange: set a deterministic PATH value for assertion.
	t.Setenv("PATH", "/deterministic/test/path")
	lookErr := fmt.Errorf("executable file not found in $PATH")
	lookPath := fixedLookPath("", lookErr)

	// Act
	_, err := testrun.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

	// Assert
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
	if !strings.Contains(err.Error(), "/deterministic/test/path") {
		t.Errorf("error message does not contain searched PATH: %v", err)
	}
}

func TestResolveHarnessBinaries_MissingBinary_ErrorContainsUnderlyingReason(t *testing.T) {
	// Arrange: lookPath returns a descriptive error.
	const errReason = "lookup failed: no such executable"
	lookErr := fmt.Errorf(errReason)
	lookPath := fixedLookPath("", lookErr)

	// Act
	_, err := testrun.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

	// Assert
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
	if !strings.Contains(err.Error(), errReason) {
		t.Errorf("error message does not contain underlying reason %q: %v", errReason, err)
	}
}

func TestResolveHarnessBinaries_MissingBinary_WrapsLookPathError(t *testing.T) {
	// Arrange: lookPath returns a sentinel error.
	sentinel := fmt.Errorf("sentinel lookup error")
	lookPath := fixedLookPath("", sentinel)

	// Act
	_, err := testrun.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

	// Assert
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("returned error does not wrap lookPath error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// "fake" harness
// ---------------------------------------------------------------------------

func TestResolveHarnessBinaries_FakeHarness_IsSkippedWithoutError(t *testing.T) {
	// Arrange: only "fake" harness; lookPath should never be called.
	var lookPathCalled bool
	lookPath := func(binary string) (string, error) {
		lookPathCalled = true
		return "/some/path", nil
	}

	// Act
	result, err := testrun.ResolveHarnessBinaries([]string{"fake"}, lookPath)

	// Assert
	if err != nil {
		t.Fatalf("expected nil error for fake harness, got: %v", err)
	}
	if _, ok := result["fake"]; ok {
		t.Error("fake harness should not appear in result map")
	}
	if lookPathCalled {
		t.Error("lookPath should not be called for the fake harness")
	}
}

func TestResolveHarnessBinaries_FakeAlongsideRealHarness_RealHarnessIsResolved(t *testing.T) {
	// Arrange: fake + claude-code; only claude-code should be resolved.
	const absPath = "/usr/local/bin/claude"
	lookPath := fixedLookPath(absPath, nil)

	// Act
	result, err := testrun.ResolveHarnessBinaries([]string{"fake", "claude-code"}, lookPath)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result["fake"]; ok {
		t.Error("fake harness should not appear in result map")
	}
	if got := result["claude-code"]; got != absPath {
		t.Errorf("result[claude-code] = %q, want %q", got, absPath)
	}
}

// ---------------------------------------------------------------------------
// Fail-fast behavior
// ---------------------------------------------------------------------------

func TestResolveHarnessBinaries_FailFast_StopsAtFirstFailure(t *testing.T) {
	// Arrange: first harness fails; second harness's lookPath must not be
	// called. Fail-fast: return immediately on first error.
	lookErr := fmt.Errorf("binary not found")
	callCount := 0
	lookPath := func(binary string) (string, error) {
		callCount++
		return "", lookErr
	}

	// Act
	harnesses := []string{"claude-code", "opencode"}
	_, err := testrun.ResolveHarnessBinaries(harnesses, lookPath)

	// Assert
	if err == nil {
		t.Fatal("expected error when first harness fails, got nil")
	}
	if callCount != 1 {
		t.Errorf("lookPath called %d times, want exactly 1 (fail-fast)", callCount)
	}
}

func TestResolveHarnessBinaries_FailFast_UnknownIDFirst_ErrorIdentifiesUnknownID(t *testing.T) {
	// Arrange: unknown ID at index 0; the error must name it.
	// Order matters: unknown-ID check is inline (not a pre-pass), so the first
	// harness in the slice that is unknown produces the error.
	lookPath := fixedLookPath("/some/path", nil)

	// Act
	_, err := testrun.ResolveHarnessBinaries([]string{"unknown-first", "claude-code"}, lookPath)

	// Assert
	if err == nil {
		t.Fatal("expected error for unknown harness ID, got nil")
	}
	if !strings.Contains(err.Error(), "unknown-first") {
		t.Errorf("error does not identify the failing harness %q: %v", "unknown-first", err)
	}
}

// ---------------------------------------------------------------------------
// Unknown harness ID
// ---------------------------------------------------------------------------

func TestResolveHarnessBinaries_UnknownHarnessID_ReturnsErrorWithID(t *testing.T) {
	// Arrange
	const unknownID = "not-a-real-harness"
	lookPath := fixedLookPath("/some/path", nil)

	// Act
	_, err := testrun.ResolveHarnessBinaries([]string{unknownID}, lookPath)

	// Assert
	if err == nil {
		t.Fatal("expected error for unknown harness ID, got nil")
	}
	if !strings.Contains(err.Error(), unknownID) {
		t.Errorf("error does not contain unknown harness ID %q: %v", unknownID, err)
	}
}

func TestResolveHarnessBinaries_UnknownHarnessID_CaseSensitive(t *testing.T) {
	// Arrange: harness IDs are case-sensitive; "Claude-Code" is unknown.
	lookPath := fixedLookPath("/some/path", nil)

	// Act
	_, err := testrun.ResolveHarnessBinaries([]string{"Claude-Code"}, lookPath)

	// Assert: the wrong-case ID should be treated as unknown and error must name it.
	if err == nil {
		t.Fatal("expected error for wrong-case harness ID, got nil")
	}
	if !strings.Contains(err.Error(), "Claude-Code") {
		t.Errorf("error does not contain the unrecognized ID %q: %v", "Claude-Code", err)
	}
}

// ---------------------------------------------------------------------------
// Binary name per harness ID
// ---------------------------------------------------------------------------

func TestResolveHarnessBinaries_ClaudeCode_CallsLookPathWithClaude(t *testing.T) {
	// Arrange
	rec := &recordingLookPath{result: "/usr/local/bin/claude"}

	// Act
	_, err := testrun.ResolveHarnessBinaries([]string{"claude-code"}, rec.fn())

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rec.called) == 0 {
		t.Fatal("lookPath was not called")
	}
	if rec.called[0] != "claude" {
		t.Errorf("lookPath called with %q, want %q", rec.called[0], "claude")
	}
}

func TestResolveHarnessBinaries_OpenCode_CallsLookPathWithOpencode(t *testing.T) {
	// Arrange
	rec := &recordingLookPath{result: "/usr/local/bin/opencode"}

	// Act
	_, err := testrun.ResolveHarnessBinaries([]string{"opencode"}, rec.fn())

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rec.called) == 0 {
		t.Fatal("lookPath was not called")
	}
	if rec.called[0] != "opencode" {
		t.Errorf("lookPath called with %q, want %q", rec.called[0], "opencode")
	}
}

func TestResolveHarnessBinaries_GHCPCli_CallsLookPathWithCopilot(t *testing.T) {
	// Arrange
	rec := &recordingLookPath{result: "/usr/local/bin/copilot"}

	// Act
	_, err := testrun.ResolveHarnessBinaries([]string{"ghcp-cli"}, rec.fn())

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rec.called) == 0 {
		t.Fatal("lookPath was not called")
	}
	if rec.called[0] != "copilot" {
		t.Errorf("lookPath called with %q, want %q", rec.called[0], "copilot")
	}
}

// ---------------------------------------------------------------------------
// Already-absolute path (including .cmd paths with spaces)
// ---------------------------------------------------------------------------

func TestResolveHarnessBinaries_AlreadyAbsolutePath_ReturnedUnchanged(t *testing.T) {
	// Arrange: lookPath returns a path that is already absolute.
	const absPath = "/usr/local/bin/claude"
	lookPath := fixedLookPath(absPath, nil)

	// Act
	result, err := testrun.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["claude-code"] != absPath {
		t.Errorf("result = %q, want %q", result["claude-code"], absPath)
	}
}

func TestResolveHarnessBinaries_WindowsCmdPathWithSpaces_ReturnedUnchanged(t *testing.T) {
	// Arrange: Windows .cmd shim path that is already absolute.
	// A path with spaces must be returned as-is.
	const cmdPath = `C:\Users\test user\AppData\Roaming\npm\claude.cmd`
	lookPath := fixedLookPath(cmdPath, nil)

	// Act
	result, err := testrun.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error for already-absolute .cmd path: %v", err)
	}
	if result["claude-code"] != cmdPath {
		t.Errorf("result = %q, want %q", result["claude-code"], cmdPath)
	}
}

// ---------------------------------------------------------------------------
// Duplicate harness IDs
// ---------------------------------------------------------------------------

func TestResolveHarnessBinaries_DuplicateHarnessID_ResolvedOnce(t *testing.T) {
	// Arrange: same harness ID twice; lookPath must be called only once.
	callCount := 0
	const absPath = "/usr/local/bin/claude"
	lookPath := func(binary string) (string, error) {
		callCount++
		return absPath, nil
	}

	// Act
	result, err := testrun.ResolveHarnessBinaries([]string{"claude-code", "claude-code"}, lookPath)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("lookPath called %d times for duplicate IDs, want 1", callCount)
	}
	if result["claude-code"] != absPath {
		t.Errorf("result[claude-code] = %q, want %q", result["claude-code"], absPath)
	}
}

// ---------------------------------------------------------------------------
// HarnessDisplayOrder
// ---------------------------------------------------------------------------

// TestHarnessDisplayOrder_EmptySlice_ReturnsEmpty verifies that a nil or
// empty slice returns an empty (or nil) slice with no entries.
func TestHarnessDisplayOrder_EmptySlice_ReturnsEmpty(t *testing.T) {
	result := testrun.HarnessDisplayOrder(nil)
	if len(result) != 0 {
		t.Errorf("HarnessDisplayOrder(nil) returned %v, want empty", result)
	}
}

// TestHarnessDisplayOrder_FakeOnly_ReturnsEmpty verifies that a slice
// containing only "fake" entries yields an empty result.
func TestHarnessDisplayOrder_FakeOnly_ReturnsEmpty(t *testing.T) {
	result := testrun.HarnessDisplayOrder([]string{"fake", "fake"})
	if len(result) != 0 {
		t.Errorf("HarnessDisplayOrder([fake, fake]) = %v, want empty", result)
	}
}

// TestHarnessDisplayOrder_SkipsFakeEntries verifies that "fake" entries are
// excluded from the result.
func TestHarnessDisplayOrder_SkipsFakeEntries(t *testing.T) {
	input := []string{"fake", "claude-code", "fake", "opencode"}
	result := testrun.HarnessDisplayOrder(input)
	for _, id := range result {
		if id == "fake" {
			t.Errorf("HarnessDisplayOrder result contains 'fake': %v", result)
		}
	}
}

// TestHarnessDisplayOrder_DeduplicatesEntries verifies that duplicate harness
// IDs appear at most once in the result.
func TestHarnessDisplayOrder_DeduplicatesEntries(t *testing.T) {
	input := []string{"claude-code", "opencode", "claude-code"}
	result := testrun.HarnessDisplayOrder(input)
	seen := make(map[string]int)
	for _, id := range result {
		seen[id]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("HarnessDisplayOrder duplicated %q (%d times); want each ID at most once", id, count)
		}
	}
}

// TestHarnessDisplayOrder_PreservesInputOrder verifies that the output slice
// preserves the order of first occurrence from the input slice.
func TestHarnessDisplayOrder_PreservesInputOrder(t *testing.T) {
	input := []string{"opencode", "claude-code", "ghcp-cli"}
	result := testrun.HarnessDisplayOrder(input)
	if len(result) != 3 {
		t.Fatalf("HarnessDisplayOrder(%v) returned %d entries, want 3", input, len(result))
	}
	want := []string{"opencode", "claude-code", "ghcp-cli"}
	for i, id := range result {
		if id != want[i] {
			t.Errorf("result[%d] = %q, want %q (order must be preserved)", i, id, want[i])
		}
	}
}

// TestHarnessDisplayOrder_MixedFakeAndReal_ReturnsOnlyRealInOrder verifies
// that mixed "fake" and real entries yield only the real IDs in input order.
func TestHarnessDisplayOrder_MixedFakeAndReal_ReturnsOnlyRealInOrder(t *testing.T) {
	input := []string{"claude-code", "fake", "opencode"}
	result := testrun.HarnessDisplayOrder(input)
	if len(result) != 2 {
		t.Fatalf("HarnessDisplayOrder(%v) returned %d entries, want 2", input, len(result))
	}
	if result[0] != "claude-code" || result[1] != "opencode" {
		t.Errorf("result = %v, want [claude-code opencode]", result)
	}
}

// ---------------------------------------------------------------------------
// recordingDebugLogger records debug log calls for assertion in
// ResolveAndAnnounce tests.
// ---------------------------------------------------------------------------

type recordingDebugLogger struct {
	events  []string
	fields  [][]domain.DebugField
	messages []string
}

func (r *recordingDebugLogger) Log(event string, message string, fields ...domain.DebugField) {
	r.events = append(r.events, event)
	r.messages = append(r.messages, message)
	r.fields = append(r.fields, append([]domain.DebugField(nil), fields...))
}

// hasEventField returns true if the i-th log call has a field with the given
// key and value.
func (r *recordingDebugLogger) hasEventField(i int, key, value string) bool {
	if i >= len(r.fields) {
		return false
	}
	for _, f := range r.fields[i] {
		if f.Key == key && f.Value == value {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// ResolveAndAnnounce: CLI-mode (non-nil io.Writer)
// ---------------------------------------------------------------------------

// TestResolveAndAnnounce_CLIMode_WritesFormattedLinePerHarness verifies that
// in CLI mode (non-nil cliOutput), ResolveAndAnnounce writes one formatted
// line per resolved harness, in the format "  <harnessID>: <path>\n".
func TestResolveAndAnnounce_CLIMode_WritesFormattedLinePerHarness(t *testing.T) {
	const absPath = "/usr/local/bin/claude"
	lookPath := fixedLookPath(absPath, nil)
	logger := &recordingDebugLogger{}
	var out bytes.Buffer

	_, err := testrun.ResolveAndAnnounce(
		[]string{"claude-code"},
		lookPath,
		&out,
		logger,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	want := "  claude-code: " + absPath + "\n"
	if !strings.Contains(got, want) {
		t.Errorf("writer output = %q, want it to contain %q", got, want)
	}
}

// TestResolveAndAnnounce_CLIMode_LogsResolvePathEventPerHarness verifies that
// each resolved harness produces a log entry with EventTestrunResolvePath.
func TestResolveAndAnnounce_CLIMode_LogsResolvePathEventPerHarness(t *testing.T) {
	const absPath = "/usr/local/bin/claude"
	lookPath := fixedLookPath(absPath, nil)
	logger := &recordingDebugLogger{}
	var out bytes.Buffer

	_, err := testrun.ResolveAndAnnounce(
		[]string{"claude-code"},
		lookPath,
		&out,
		logger,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(logger.events) == 0 {
		t.Fatal("expected at least one log call, got none")
	}
	if logger.events[0] != domain.EventTestrunResolvePath {
		t.Errorf("first log event = %q, want %q", logger.events[0], domain.EventTestrunResolvePath)
	}
}

// TestResolveAndAnnounce_CLIMode_LogFieldsContainHarnessAndPath verifies that
// the log entry fields include the harness ID and resolved absolute path.
func TestResolveAndAnnounce_CLIMode_LogFieldsContainHarnessAndPath(t *testing.T) {
	const (
		harnessID = "claude-code"
		absPath   = "/usr/local/bin/claude"
	)
	lookPath := fixedLookPath(absPath, nil)
	logger := &recordingDebugLogger{}
	var out bytes.Buffer

	_, err := testrun.ResolveAndAnnounce(
		[]string{harnessID},
		lookPath,
		&out,
		logger,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(logger.fields) == 0 {
		t.Fatal("expected log fields, got none")
	}
	if !logger.hasEventField(0, "harness", harnessID) {
		t.Errorf("log fields missing harness=%q; fields[0] = %v", harnessID, logger.fields[0])
	}
	if !logger.hasEventField(0, "path", absPath) {
		t.Errorf("log fields missing path=%q; fields[0] = %v", absPath, logger.fields[0])
	}
}

// TestResolveAndAnnounce_CLIMode_MultipleHarnesses_WritesAllLines verifies
// that all resolved harnesses are written to the writer.
func TestResolveAndAnnounce_CLIMode_MultipleHarnesses_WritesAllLines(t *testing.T) {
	paths := map[string]string{
		"claude":  "/usr/local/bin/claude",
		"opencode": "/usr/local/bin/opencode",
	}
	lookPath := perBinaryLookPath(paths)
	var out bytes.Buffer

	_, err := testrun.ResolveAndAnnounce(
		[]string{"claude-code", "opencode"},
		lookPath,
		&out,
		nil, // nil logger: no logging, no panic
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "claude-code:") {
		t.Errorf("writer output missing \"claude-code:\"; got: %q", got)
	}
	if !strings.Contains(got, "opencode:") {
		t.Errorf("writer output missing \"opencode:\"; got: %q", got)
	}
}

// TestResolveAndAnnounce_CLIMode_SkipsFakeHarness_NoOutputLine verifies that
// the "fake" harness produces no writer output (it is silently skipped).
func TestResolveAndAnnounce_CLIMode_SkipsFakeHarness_NoOutputLine(t *testing.T) {
	lookPath := fixedLookPath("/usr/local/bin/claude", nil)
	var out bytes.Buffer

	_, err := testrun.ResolveAndAnnounce(
		[]string{"fake"},
		lookPath,
		&out,
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Len() != 0 {
		t.Errorf("writer output for fake-only harnesses should be empty, got: %q", out.String())
	}
}

// TestResolveAndAnnounce_CLIMode_MultipleHarnesses_LogsEachHarness verifies
// that one log entry is produced per real harness.
func TestResolveAndAnnounce_CLIMode_MultipleHarnesses_LogsEachHarness(t *testing.T) {
	paths := map[string]string{
		"claude":  "/usr/local/bin/claude",
		"opencode": "/usr/local/bin/opencode",
	}
	lookPath := perBinaryLookPath(paths)
	logger := &recordingDebugLogger{}

	_, err := testrun.ResolveAndAnnounce(
		[]string{"claude-code", "opencode"},
		lookPath,
		nil,
		logger,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(logger.events) != 2 {
		t.Errorf("expected 2 log calls for 2 harnesses, got %d: %v", len(logger.events), logger.events)
	}
	for i, event := range logger.events {
		if event != domain.EventTestrunResolvePath {
			t.Errorf("logger.events[%d] = %q, want %q", i, event, domain.EventTestrunResolvePath)
		}
	}
}

// ---------------------------------------------------------------------------
// ResolveAndAnnounce: TUI-mode (nil writer)
// ---------------------------------------------------------------------------

// TestResolveAndAnnounce_TUIMode_NilWriter_LogsWithoutPanic verifies that
// when cliOutput is nil (TUI mode), ResolveAndAnnounce logs paths via the
// debug logger without panicking.
func TestResolveAndAnnounce_TUIMode_NilWriter_LogsWithoutPanic(t *testing.T) {
	const absPath = "/usr/local/bin/claude"
	lookPath := fixedLookPath(absPath, nil)
	logger := &recordingDebugLogger{}

	_, err := testrun.ResolveAndAnnounce(
		[]string{"claude-code"},
		lookPath,
		nil, // TUI mode: no stdout output
		logger,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logger.events) == 0 {
		t.Error("TUI mode: expected logger to receive calls, got none")
	}
}

// TestResolveAndAnnounce_NilLogger_NoPanic verifies that a nil logger is
// tolerated: logging is skipped without panicking.
func TestResolveAndAnnounce_NilLogger_NoPanic(t *testing.T) {
	lookPath := fixedLookPath("/usr/local/bin/claude", nil)
	var out bytes.Buffer

	// Must not panic.
	_, err := testrun.ResolveAndAnnounce(
		[]string{"claude-code"},
		lookPath,
		&out,
		nil, // nil interface value: logging must be skipped
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ResolveAndAnnounce: failure path
// ---------------------------------------------------------------------------

// TestResolveAndAnnounce_Failure_ReturnsError verifies that a resolution
// failure is propagated as a non-nil error.
func TestResolveAndAnnounce_Failure_ReturnsError(t *testing.T) {
	lookErr := fmt.Errorf("binary not found")
	lookPath := fixedLookPath("", lookErr)
	logger := &recordingDebugLogger{}
	var out bytes.Buffer

	_, err := testrun.ResolveAndAnnounce(
		[]string{"claude-code"},
		lookPath,
		&out,
		logger,
	)
	if err == nil {
		t.Fatal("expected error on resolution failure, got nil")
	}
}

// TestResolveAndAnnounce_Failure_DoesNotLog verifies that on resolution
// failure, no log entries are emitted (the error is returned before logging).
func TestResolveAndAnnounce_Failure_DoesNotLog(t *testing.T) {
	lookErr := fmt.Errorf("binary not found")
	lookPath := fixedLookPath("", lookErr)
	logger := &recordingDebugLogger{}

	_, _ = testrun.ResolveAndAnnounce(
		[]string{"claude-code"},
		lookPath,
		nil,
		logger,
	)
	if len(logger.events) != 0 {
		t.Errorf("expected no log calls on failure, got %d event(s): %v", len(logger.events), logger.events)
	}
}

// TestResolveAndAnnounce_Failure_DoesNotWriteToWriter verifies that on
// resolution failure, nothing is written to the cli output writer.
func TestResolveAndAnnounce_Failure_DoesNotWriteToWriter(t *testing.T) {
	lookErr := fmt.Errorf("binary not found")
	lookPath := fixedLookPath("", lookErr)
	var out bytes.Buffer

	_, _ = testrun.ResolveAndAnnounce(
		[]string{"claude-code"},
		lookPath,
		&out,
		nil,
	)
	if out.Len() != 0 {
		t.Errorf("expected no writer output on failure, got %q", out.String())
	}
}

// ---------------------------------------------------------------------------
// ResolveAndAnnounce: return value
// ---------------------------------------------------------------------------

// TestResolveAndAnnounce_Success_ReturnsResolvedMap verifies that on success
// the returned map contains the harness-to-path entries.
func TestResolveAndAnnounce_Success_ReturnsResolvedMap(t *testing.T) {
	const absPath = "/usr/local/bin/claude"
	lookPath := fixedLookPath(absPath, nil)

	result, err := testrun.ResolveAndAnnounce(
		[]string{"claude-code"},
		lookPath,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, ok := result["claude-code"]; !ok {
		t.Error("result map missing key claude-code")
	} else if got != absPath {
		t.Errorf("result[claude-code] = %q, want %q", got, absPath)
	}
}

// ---------------------------------------------------------------------------
// Coverage guard: every CLIHarnesses() ID must have a mapping
// ---------------------------------------------------------------------------

// TestResolveHarnessBinaries_AllCLIHarnessesHaveMappings verifies that every
// harness ID returned by harness.CLIHarnesses() has a binary-name mapping in
// ResolveHarnessBinaries. This test fails when a new CLI harness is added to
// the catalog without updating the binary-name table, guarding against drift
// from the mapping duplicated in buildAdapter (cmd/mosaic-run/main.go).
func TestResolveHarnessBinaries_AllCLIHarnessesHaveMappings(t *testing.T) {
	// lookPath always succeeds, so the only failure path is "unknown harness ID".
	lookPath := func(binary string) (string, error) {
		return "/usr/bin/" + binary, nil
	}

	for _, h := range commonharness.CLIHarnesses() {
		t.Run(h.ID, func(t *testing.T) {
			// Arrange: single harness ID from the catalog.
			// Act
			result, err := testrun.ResolveHarnessBinaries([]string{h.ID}, lookPath)

			// Assert: no error means the mapping exists.
			if err != nil {
				t.Fatalf("harness %q has no binary-name mapping in ResolveHarnessBinaries: %v", h.ID, err)
			}
			if _, ok := result[h.ID]; !ok {
				t.Fatalf("harness %q resolved without error but was not in result map", h.ID)
			}
		})
	}
}
