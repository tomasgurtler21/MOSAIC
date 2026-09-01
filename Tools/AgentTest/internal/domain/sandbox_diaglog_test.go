package domain_test

// Tests for the per-run diagnostic log path derivation on domain.Sandbox
// (T6.3 and T6.4).
//
// T6.3: Two runs' diagnostic log paths are distinct and each is under its own
// run's control directory.
//
// T6.4: The diagnostic log path is under ControlDir, so the run's retention
// policy governs the file uniformly alongside the other control files.

import (
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
)

// TestDiagnosticLogPath_IsUnderControlDir asserts that the diagnostic log
// path returned by DiagnosticLogPath is contained within the sandbox's
// ControlDir. The file must live in ControlDir so the run's retention policy
// governs it uniformly alongside the run state, lock, and invocation log.
func TestDiagnosticLogPath_IsUnderControlDir(t *testing.T) {
	sb := domain.Sandbox{
		Key:        domain.RunKey{RunID: "20260101T120000Z-0001"},
		Root:       "/tmp/sandbox",
		SubjectDir: "/tmp/sandbox/subject",
		ControlDir: "/tmp/sandbox/ctl",
	}

	path := sb.DiagnosticLogPath()
	if path == "" {
		t.Fatal("DiagnosticLogPath returned an empty string; must return a non-empty path under ControlDir")
	}

	// The path must be under ControlDir, not under SubjectDir or Root
	// directly. The subject must not be able to discover control files.
	cleanPath := filepath.ToSlash(filepath.Clean(path))
	cleanCtl := filepath.ToSlash(filepath.Clean(sb.ControlDir))
	if !strings.HasPrefix(cleanPath, cleanCtl+"/") && cleanPath != cleanCtl {
		t.Errorf("DiagnosticLogPath = %q, want a path under ControlDir %q", path, sb.ControlDir)
	}
}

// TestDiagnosticLogPath_IsNotUnderSubjectDir asserts that the diagnostic log
// is placed outside the subject's working directory, preserving the invariant
// that the subject cannot discover control files.
func TestDiagnosticLogPath_IsNotUnderSubjectDir(t *testing.T) {
	sb := domain.Sandbox{
		Key:        domain.RunKey{RunID: "20260101T120000Z-0001"},
		Root:       "/tmp/sandbox",
		SubjectDir: "/tmp/sandbox/subject",
		ControlDir: "/tmp/sandbox/ctl",
	}

	path := sb.DiagnosticLogPath()
	if path == "" {
		t.Fatal("DiagnosticLogPath returned empty string; must return a path outside SubjectDir")
	}

	cleanPath := filepath.ToSlash(filepath.Clean(path))
	cleanSubj := filepath.ToSlash(filepath.Clean(sb.SubjectDir))
	if strings.HasPrefix(cleanPath, cleanSubj+"/") || cleanPath == cleanSubj {
		t.Errorf("DiagnosticLogPath = %q is under SubjectDir %q; control files must not be discoverable by the subject", path, sb.SubjectDir)
	}
}

// TestDiagnosticLogPath_TwoRunsHaveDistinctPaths asserts that two sandboxes
// with different control directories produce different diagnostic log paths.
// This is the structural guarantee that two concurrent runs never share a
// diagnostic file.
func TestDiagnosticLogPath_TwoRunsHaveDistinctPaths(t *testing.T) {
	sb1 := domain.Sandbox{
		Key:        domain.RunKey{RunID: "20260101T120000Z-0001"},
		Root:       "/tmp/sandbox-a",
		SubjectDir: "/tmp/sandbox-a/subject",
		ControlDir: "/tmp/sandbox-a/ctl",
	}
	sb2 := domain.Sandbox{
		Key:        domain.RunKey{RunID: "20260101T120000Z-0002"},
		Root:       "/tmp/sandbox-b",
		SubjectDir: "/tmp/sandbox-b/subject",
		ControlDir: "/tmp/sandbox-b/ctl",
	}

	path1 := sb1.DiagnosticLogPath()
	path2 := sb2.DiagnosticLogPath()

	if path1 == "" || path2 == "" {
		t.Fatal("DiagnosticLogPath returned an empty string for at least one sandbox; must return distinct non-empty paths for distinct control directories")
	}
	if filepath.Clean(path1) == filepath.Clean(path2) {
		t.Errorf("DiagnosticLogPath returned the same path %q for two runs with different ControlDirs; paths must be per-run", path1)
	}
}

// TestDiagnosticLogPath_IsDistinctFromOtherControlFiles asserts that
// DiagnosticLogPath returns a path that does not collide with any other
// control file in the sandbox (state, lock, invocation log, etc.).
func TestDiagnosticLogPath_IsDistinctFromOtherControlFiles(t *testing.T) {
	sb := domain.Sandbox{
		Key:        domain.RunKey{RunID: "20260101T120000Z-0001"},
		Root:       "/tmp/sandbox",
		SubjectDir: "/tmp/sandbox/subject",
		ControlDir: "/tmp/sandbox/ctl",
	}

	rawDiagPath := sb.DiagnosticLogPath()
	if rawDiagPath == "" {
		t.Fatal("DiagnosticLogPath returned an empty string; must return a non-empty path distinct from all other control files")
	}
	diagPath := filepath.Clean(rawDiagPath)

	others := map[string]string{
		"StatePath":          filepath.Clean(sb.StatePath()),
		"LockPath":           filepath.Clean(sb.LockPath()),
		"InvocationLogPath":  filepath.Clean(sb.InvocationLogPath()),
		"LedgerPath":         filepath.Clean(sb.LedgerPath()),
		"EarlyExitSentinel":  filepath.Clean(sb.EarlyExitSentinelPath()),
	}
	for name, other := range others {
		if diagPath == other {
			t.Errorf("DiagnosticLogPath = %q collides with %s = %q; each control file must have a unique name", diagPath, name, other)
		}
	}
}
