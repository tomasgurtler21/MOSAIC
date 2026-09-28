// Tests for ResolveHarnessBinaries per-harness binary names, fake skipping, fail-fast and duplicates.
package resolve_test

import (
	"fmt"
	"strings"
	"testing"

	commonharness "mosaic-common/harness"
	"mosaic-run/internal/testrun/resolve"
)

func TestResolveHarnessBinaries_FakeHarness_IsSkippedWithoutError(t *testing.T) {
	// Arrange: only "fake" harness; lookPath should never be called.
	var lookPathCalled bool
	lookPath := func(binary string) (string, error) {
		lookPathCalled = true
		return "/some/path", nil
	}

	// Act
	result, err := resolve.ResolveHarnessBinaries([]string{"fake"}, lookPath)

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
	result, err := resolve.ResolveHarnessBinaries([]string{"fake", "claude-code"}, lookPath)

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
	_, err := resolve.ResolveHarnessBinaries(harnesses, lookPath)

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
	_, err := resolve.ResolveHarnessBinaries([]string{"unknown-first", "claude-code"}, lookPath)

	// Assert
	if err == nil {
		t.Fatal("expected error for unknown harness ID, got nil")
	}
	if !strings.Contains(err.Error(), "unknown-first") {
		t.Errorf("error does not identify the failing harness %q: %v", "unknown-first", err)
	}
}

func TestResolveHarnessBinaries_UnknownHarnessID_ReturnsErrorWithID(t *testing.T) {
	// Arrange
	const unknownID = "not-a-real-harness"
	lookPath := fixedLookPath("/some/path", nil)

	// Act
	_, err := resolve.ResolveHarnessBinaries([]string{unknownID}, lookPath)

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
	_, err := resolve.ResolveHarnessBinaries([]string{"Claude-Code"}, lookPath)

	// Assert: the wrong-case ID should be treated as unknown and error must name it.
	if err == nil {
		t.Fatal("expected error for wrong-case harness ID, got nil")
	}
	if !strings.Contains(err.Error(), "Claude-Code") {
		t.Errorf("error does not contain the unrecognized ID %q: %v", "Claude-Code", err)
	}
}

func TestResolveHarnessBinaries_ClaudeCode_CallsLookPathWithClaude(t *testing.T) {
	// Arrange
	rec := &recordingLookPath{result: "/usr/local/bin/claude"}

	// Act
	_, err := resolve.ResolveHarnessBinaries([]string{"claude-code"}, rec.fn())

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
	_, err := resolve.ResolveHarnessBinaries([]string{"opencode"}, rec.fn())

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
	_, err := resolve.ResolveHarnessBinaries([]string{"ghcp-cli"}, rec.fn())

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

func TestResolveHarnessBinaries_AlreadyAbsolutePath_ReturnedUnchanged(t *testing.T) {
	// Arrange: lookPath returns a path that is already absolute.
	const absPath = "/usr/local/bin/claude"
	lookPath := fixedLookPath(absPath, nil)

	// Act
	result, err := resolve.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

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
	result, err := resolve.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error for already-absolute .cmd path: %v", err)
	}
	if result["claude-code"] != cmdPath {
		t.Errorf("result = %q, want %q", result["claude-code"], cmdPath)
	}
}

func TestResolveHarnessBinaries_DuplicateHarnessID_ResolvedOnce(t *testing.T) {
	// Arrange: same harness ID twice; lookPath must be called only once.
	callCount := 0
	const absPath = "/usr/local/bin/claude"
	lookPath := func(binary string) (string, error) {
		callCount++
		return absPath, nil
	}

	// Act
	result, err := resolve.ResolveHarnessBinaries([]string{"claude-code", "claude-code"}, lookPath)

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
			result, err := resolve.ResolveHarnessBinaries([]string{h.ID}, lookPath)

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
