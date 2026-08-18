//go:build stage3

package app

// hook_probe_test.go covers probeDeployedHookBundle, the directory-oriented presence probe
// for hook bundle artifacts.
//
// Hook bundles deploy to a directory rather than a file, so the file-oriented
// probeDeployedArtifact function (which always yields Present: false for directories) cannot
// report hook bundles as present. probeDeployedHookBundle is a dedicated probe that treats a
// non-empty directory as present.
//
// Covered cases (T3.2):
//
//   Missing target path:
//     - No entry at <workspace>/<targetPath> → Present: false
//
//   File at target path (not a directory):
//     - A regular file at the path → Present: false (hook bundles are directories)
//
//   Empty directory:
//     - Target path exists as a directory but contains no files → Present: false
//
//   Directory with at least one file:
//     - Target path exists as a directory and contains one or more files → Present: true
//
//   ContentHash when present:
//     - Present: true → ContentHash == manifest.Hash(nil) (reproduces the executor's nil hash)
//
//   Version fields when present:
//     - All version fields are empty: a hook bundle carries no in-file version marker
//
//   I/O failure graceful degradation:
//     - Any I/O failure → Present: false, no error returned
//
// All tests use t.TempDir() for filesystem fixtures, matching the existing app test style.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/manifest"
)

// ---------------------------------------------------------------------------
// probeDeployedHookBundle — missing target path
// ---------------------------------------------------------------------------

// TestProbeDeployedHookBundle_MissingPath_PresentIsFalse verifies that when there is no entry
// at the hook bundle's target path (neither a file nor a directory), the probe reports
// Present: false.
func TestProbeDeployedHookBundle_MissingPath_PresentIsFalse(t *testing.T) {
	ws := t.TempDir()

	state := probeDeployedHookBundle(ws, "hooks/my-hooks")

	if state.Present {
		t.Error("expected Present: false for a missing target path, got true")
	}
}

// TestProbeDeployedHookBundle_MissingPath_ContentHashIsEmpty verifies that a missing target
// path yields an empty ContentHash, not a zero-hash or nil-hash value.
func TestProbeDeployedHookBundle_MissingPath_ContentHashIsEmpty(t *testing.T) {
	ws := t.TempDir()

	state := probeDeployedHookBundle(ws, "hooks/my-hooks")

	if state.ContentHash != "" {
		t.Errorf("missing path must have empty ContentHash, got %q", state.ContentHash)
	}
}

// ---------------------------------------------------------------------------
// probeDeployedHookBundle — file at target path (not a directory)
// ---------------------------------------------------------------------------

// TestProbeDeployedHookBundle_FileAtPath_PresentIsFalse verifies that when a regular file
// exists at the target path (rather than a directory), the probe reports Present: false.
// Hook bundles are directories; a regular file at that path is not a valid hook bundle.
func TestProbeDeployedHookBundle_FileAtPath_PresentIsFalse(t *testing.T) {
	ws := t.TempDir()
	// Create a regular file at the target path.
	if err := os.WriteFile(filepath.Join(ws, "hooks-bundle"), []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	state := probeDeployedHookBundle(ws, "hooks-bundle")

	if state.Present {
		t.Error("expected Present: false when target path is a regular file, got true")
	}
}

// ---------------------------------------------------------------------------
// probeDeployedHookBundle — empty directory
// ---------------------------------------------------------------------------

// TestProbeDeployedHookBundle_EmptyDirectory_PresentIsFalse verifies that when the target
// path exists as a directory but contains no files, the probe reports Present: false.
// An empty directory is treated as absent because no hook content has been deployed there.
func TestProbeDeployedHookBundle_EmptyDirectory_PresentIsFalse(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "hooks", "my-hooks"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	state := probeDeployedHookBundle(ws, "hooks/my-hooks")

	if state.Present {
		t.Error("expected Present: false for an empty directory, got true")
	}
}

// TestProbeDeployedHookBundle_DirectoryWithOnlySubdirs_PresentIsFalse verifies that a
// directory that contains only subdirectories (but no regular files directly inside it) is
// also treated as not present. Only regular files count as hook content.
func TestProbeDeployedHookBundle_DirectoryWithOnlySubdirs_PresentIsFalse(t *testing.T) {
	ws := t.TempDir()
	bundleDir := filepath.Join(ws, "hooks", "my-hooks")
	if err := os.MkdirAll(filepath.Join(bundleDir, "subdir"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	state := probeDeployedHookBundle(ws, "hooks/my-hooks")

	if state.Present {
		t.Error("expected Present: false for a directory containing only subdirectories, got true")
	}
}

// ---------------------------------------------------------------------------
// probeDeployedHookBundle — directory with at least one file
// ---------------------------------------------------------------------------

// TestProbeDeployedHookBundle_DirectoryWithOneFile_PresentIsTrue verifies that when the
// target path is a directory and contains at least one regular file directly within it, the
// probe reports Present: true.
func TestProbeDeployedHookBundle_DirectoryWithOneFile_PresentIsTrue(t *testing.T) {
	ws := t.TempDir()
	bundleDir := filepath.Join(ws, "hooks", "my-hooks")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "hook.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile hook.sh: %v", err)
	}

	state := probeDeployedHookBundle(ws, "hooks/my-hooks")

	if !state.Present {
		t.Error("expected Present: true for a directory with at least one file, got false")
	}
}

// TestProbeDeployedHookBundle_DirectoryWithMultipleFiles_PresentIsTrue verifies that a
// directory with several files is treated as present, confirming that the one-file check is
// a minimum threshold, not an exact count.
func TestProbeDeployedHookBundle_DirectoryWithMultipleFiles_PresentIsTrue(t *testing.T) {
	ws := t.TempDir()
	bundleDir := filepath.Join(ws, "hooks", "pre-commit")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for _, name := range []string{"pre-commit.sh", "utils.sh", "README"} {
		if err := os.WriteFile(filepath.Join(bundleDir, name), []byte("content"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	state := probeDeployedHookBundle(ws, "hooks/pre-commit")

	if !state.Present {
		t.Error("expected Present: true for a directory with multiple files, got false")
	}
}

// ---------------------------------------------------------------------------
// probeDeployedHookBundle — ContentHash contract when present
// ---------------------------------------------------------------------------

// TestProbeDeployedHookBundle_PresentBundle_ContentHashIsNilHash verifies that when a hook
// bundle is present (directory with at least one file), ContentHash is set to manifest.Hash(nil).
// This reproduces the executor's convention: hook bundles are recorded in the manifest with a
// nil content hash, so probe and manifest hashes are directly comparable and step 4 of
// classifyHookItem does not classify every bundle as a local modification.
func TestProbeDeployedHookBundle_PresentBundle_ContentHashIsNilHash(t *testing.T) {
	ws := t.TempDir()
	bundleDir := filepath.Join(ws, "hooks", "my-hooks")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "hook.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	state := probeDeployedHookBundle(ws, "hooks/my-hooks")

	want := manifest.Hash(nil)
	if state.ContentHash != want {
		t.Errorf("ContentHash = %q, want manifest.Hash(nil) = %q; present hook bundle must reproduce the executor's nil-hash convention",
			state.ContentHash, want)
	}
}

// TestProbeDeployedHookBundle_AbsentBundle_ContentHashIsEmpty verifies that an absent bundle
// (missing or empty directory) has an empty ContentHash, not the nil-hash value. An empty
// ContentHash distinguishes "absent" from "present with nil hash".
func TestProbeDeployedHookBundle_AbsentBundle_ContentHashIsEmpty(t *testing.T) {
	ws := t.TempDir()
	// No directory at the target path.

	state := probeDeployedHookBundle(ws, "hooks/absent-bundle")

	nilHash := manifest.Hash(nil)
	if state.ContentHash == nilHash {
		t.Errorf("absent bundle must have empty ContentHash, not manifest.Hash(nil) = %q; "+
			"nil-hash must only appear for present bundles", nilHash)
	}
	if state.ContentHash != "" {
		t.Errorf("absent bundle ContentHash = %q, want empty", state.ContentHash)
	}
}

// ---------------------------------------------------------------------------
// probeDeployedHookBundle — version fields always empty
// ---------------------------------------------------------------------------

// TestProbeDeployedHookBundle_PresentBundle_AllVersionFieldsEmpty verifies that all four
// version fields (Version, TransformVersion, InjectionsVersion, OrchestratorInjectionsVersion)
// are empty even when the bundle is present. A hook bundle directory carries no in-file version
// marker, so no version information can be read from the probe.
func TestProbeDeployedHookBundle_PresentBundle_AllVersionFieldsEmpty(t *testing.T) {
	ws := t.TempDir()
	bundleDir := filepath.Join(ws, "hooks", "my-hooks")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "hook.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	state := probeDeployedHookBundle(ws, "hooks/my-hooks")

	if state.Version != "" {
		t.Errorf("Version = %q, want empty; hook bundle probe must leave all version fields empty", state.Version)
	}
	if state.TransformVersion != "" {
		t.Errorf("TransformVersion = %q, want empty", state.TransformVersion)
	}
	if state.InjectionsVersion != "" {
		t.Errorf("InjectionsVersion = %q, want empty", state.InjectionsVersion)
	}
	if state.OrchestratorInjectionsVersion != "" {
		t.Errorf("OrchestratorInjectionsVersion = %q, want empty", state.OrchestratorInjectionsVersion)
	}
}

// TestProbeDeployedHookBundle_PresentBundle_HasVersionInfoReturnsFalse verifies that a present
// hook bundle, despite being Present: true, returns false from HasVersionInfo(). This is the
// load-bearing property for classifyHookItem's step-7 bypass: the probe deliberately leaves all
// four version fields empty, so HasVersionInfo() is always false for hook bundles. The classifier
// must not treat this as evidence of staleness.
func TestProbeDeployedHookBundle_PresentBundle_HasVersionInfoReturnsFalse(t *testing.T) {
	ws := t.TempDir()
	bundleDir := filepath.Join(ws, "hooks", "my-hooks")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "hook.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	state := probeDeployedHookBundle(ws, "hooks/my-hooks")

	if !state.Present {
		t.Fatal("expected Present: true for a directory with one file")
	}
	if state.HasVersionInfo() {
		t.Error("HasVersionInfo() must return false for a present hook bundle; " +
			"the probe leaves all version fields empty by design, and that must not be treated as staleness evidence")
	}
}
