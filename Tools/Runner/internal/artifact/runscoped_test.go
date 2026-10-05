package artifact_test

// Tests for artifact.IsRunScopedArtifactPath.

import (
	"path/filepath"
	"testing"

	"mosaic-run/internal/artifact"
)

// TestIsRunScopedArtifactPath_RunScopedParent_ReturnsTrue verifies that a path
// whose parent directory is named "Orchestration-{valid_run_id}" is recognised
// as run-scoped.
func TestIsRunScopedArtifactPath_RunScopedParent_ReturnsTrue(t *testing.T) {
	// Construct a synthetic absolute path under a run-scoped folder.
	// filepath.Join normalises separators for the platform.
	base := t.TempDir()
	runScopedDir := filepath.Join(base, "Orchestration-20260805T143029Z-9bc0")
	path := filepath.Join(runScopedDir, "Orchestration.md")

	got := artifact.IsRunScopedArtifactPath(path)

	if !got {
		t.Errorf("IsRunScopedArtifactPath(%q) = false; want true (run-scoped parent directory)",
			path)
	}
}

// TestIsRunScopedArtifactPath_NonRunScopedAbsoluteParent_ReturnsFalse verifies
// that an absolute path whose parent directory is a plain temp directory (not an
// Orchestration-* folder) is not recognised as run-scoped.
func TestIsRunScopedArtifactPath_NonRunScopedAbsoluteParent_ReturnsFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Orchestration.md")

	got := artifact.IsRunScopedArtifactPath(path)

	if got {
		t.Errorf("IsRunScopedArtifactPath(%q) = true; want false (non-run-scoped absolute parent)",
			path)
	}
}

// TestIsRunScopedArtifactPath_InvalidRunIDShape_ReturnsFalse verifies that a path
// whose parent directory starts with "Orchestration-" but is followed by a
// suffix that does not match the canonical run_id format is not recognised.
func TestIsRunScopedArtifactPath_InvalidRunIDShape_ReturnsFalse(t *testing.T) {
	base := t.TempDir()
	// "invalid-id" does not match ^\d{8}T\d{6}Z-[0-9a-f]{4}$
	path := filepath.Join(base, "Orchestration-invalid-id", "Orchestration.md")

	got := artifact.IsRunScopedArtifactPath(path)

	if got {
		t.Errorf("IsRunScopedArtifactPath(%q) = true; want false (invalid run_id shape after prefix)",
			path)
	}
}

// TestIsRunScopedArtifactPath_RelativePath_ReturnsFalse verifies that a relative
// path is never reported as run-scoped, even if its directory component looks
// like an Orchestration-* folder.
func TestIsRunScopedArtifactPath_RelativePath_ReturnsFalse(t *testing.T) {
	path := filepath.Join("Orchestration-20260805T143029Z-9bc0", "Orchestration.md")

	got := artifact.IsRunScopedArtifactPath(path)

	if got {
		t.Errorf("IsRunScopedArtifactPath(%q) = true; want false (relative path must never be run-scoped)",
			path)
	}
}
