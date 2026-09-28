// Tests for ResolveHarnessBinaries path resolution and lookup failure reporting.
package resolve_test

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/testrun/resolve"
)

func TestResolveHarnessBinaries_EmptyHarnessesSlice_ReturnsNonNilEmptyMap(t *testing.T) {
	// Arrange
	lookPath := fixedLookPath("/irrelevant/path", nil)

	// Act
	result, err := resolve.ResolveHarnessBinaries(nil, lookPath)

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
	result, err := resolve.ResolveHarnessBinaries([]string{}, lookPath)

	// Assert
	if err != nil {
		t.Fatalf("expected nil error for empty slice, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil map for empty slice, got nil")
	}
}

func TestResolveHarnessBinaries_SingleHarness_ResolvesToAbsolutePath(t *testing.T) {
	// Arrange: lookPath returns an absolute path.
	const absPath = "/usr/local/bin/claude"
	lookPath := fixedLookPath(absPath, nil)

	// Act
	result, err := resolve.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

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
	result, err := resolve.ResolveHarnessBinaries(harnesses, lookPath)

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

func TestResolveHarnessBinaries_RelativePathFromLookPath_IsAbsolutized(t *testing.T) {
	// Arrange: lookPath returns a relative path (no error).
	// The function must absolutize it via filepath.Abs.
	lookPath := fixedLookPath("./claude", nil)

	// Act
	result, err := resolve.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

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
	result, err := resolve.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := result["claude-code"]
	if filepath.Base(got) != "claude" {
		t.Errorf("absolutized path basename = %q, want %q", filepath.Base(got), "claude")
	}
}

func TestResolveHarnessBinaries_ErrDot_IsNonFatalAndPathIsAbsolutized(t *testing.T) {
	// Arrange: lookPath returns a relative path WITH exec.ErrDot.
	// ErrDot must be treated as non-fatal; the function must absolutize.
	lookPath := func(binary string) (string, error) {
		return "./claude", exec.ErrDot
	}

	// Act
	result, err := resolve.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

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
	_, err := resolve.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

	// Assert
	if err == nil {
		t.Fatal("expected error for empty path with ErrDot, got nil")
	}
	// Error must name the binary so the user can diagnose the problem.
	if !strings.Contains(err.Error(), "claude") {
		t.Errorf("error for empty-path-with-ErrDot must name the binary, got: %v", err)
	}
}

func TestResolveHarnessBinaries_MissingBinary_ErrorContainsBinaryName(t *testing.T) {
	// Arrange
	lookErr := fmt.Errorf("executable file not found in $PATH")
	lookPath := fixedLookPath("", lookErr)

	// Act
	_, err := resolve.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

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
	_, err := resolve.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

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
	_, err := resolve.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

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
	_, err := resolve.ResolveHarnessBinaries([]string{"claude-code"}, lookPath)

	// Assert
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("returned error does not wrap lookPath error: %v", err)
	}
}
