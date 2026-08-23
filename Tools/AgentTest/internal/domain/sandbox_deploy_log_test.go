package domain_test

// Tests for the per-run deploy log directory derivation on domain.Sandbox.
//
// The deploy log directory is derived from the sandbox so that the deployment
// tool writes its run logs within the run's own sandbox tree — not to a shared
// path under the repository root. Two runs therefore produce two distinct
// destinations by construction, which is the isolation guarantee that makes
// concurrent deploy invocations safe.

import (
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
)

// TestDeployLogDir_IsNonEmpty asserts that DeployLogDir returns a non-empty
// path. An empty return value would be treated as "do not override" by the
// deploy argument builder, causing the delegate to fall back to its default
// shared log location — exactly the shared-write problem this method exists
// to solve.
func TestDeployLogDir_IsNonEmpty(t *testing.T) {
	sb := domain.Sandbox{
		Key:        domain.RunKey{RunID: "20260101T120000Z-0001"},
		Root:       "/tmp/sandbox",
		SubjectDir: "/tmp/sandbox/subject",
		ControlDir: "/tmp/sandbox/ctl",
	}

	dir := sb.DeployLogDir()
	if dir == "" {
		t.Fatal("DeployLogDir returned an empty string; must return a non-empty path so --log-dir is not treated as absent")
	}
}

// TestDeployLogDir_IsUnderSandboxRoot asserts that the deploy log directory
// is contained within the sandbox's Root. This ensures the deployment tool's
// logs are governed by the same retention policy as all other per-run evidence:
// when the sandbox is torn down, the deploy logs go with it.
func TestDeployLogDir_IsUnderSandboxRoot(t *testing.T) {
	sb := domain.Sandbox{
		Key:        domain.RunKey{RunID: "20260101T120000Z-0001"},
		Root:       "/tmp/sandbox-a",
		SubjectDir: "/tmp/sandbox-a/subject",
		ControlDir: "/tmp/sandbox-a/ctl",
	}

	dir := sb.DeployLogDir()
	if dir == "" {
		t.Fatal("DeployLogDir returned an empty string; cannot assert containment within Root")
	}

	cleanDir := filepath.ToSlash(filepath.Clean(dir))
	cleanRoot := filepath.ToSlash(filepath.Clean(sb.Root))
	if !strings.HasPrefix(cleanDir, cleanRoot+"/") && cleanDir != cleanRoot {
		t.Errorf("DeployLogDir = %q, want a path under sandbox Root %q so that the run's retention policy governs the deploy logs uniformly", dir, sb.Root)
	}
}

// TestDeployLogDir_IsNotUnderSubjectDir asserts that the deploy log directory
// is placed outside the subject's working directory. The subject must not be
// able to discover deployment tool logs, which contain infrastructure detail
// about its own invocation and would reveal that it is being exercised.
func TestDeployLogDir_IsNotUnderSubjectDir(t *testing.T) {
	sb := domain.Sandbox{
		Key:        domain.RunKey{RunID: "20260101T120000Z-0001"},
		Root:       "/tmp/sandbox",
		SubjectDir: "/tmp/sandbox/subject",
		ControlDir: "/tmp/sandbox/ctl",
	}

	dir := sb.DeployLogDir()
	if dir == "" {
		t.Fatal("DeployLogDir returned empty string; must return a path outside SubjectDir")
	}

	cleanDir := filepath.ToSlash(filepath.Clean(dir))
	cleanSubj := filepath.ToSlash(filepath.Clean(sb.SubjectDir))
	if strings.HasPrefix(cleanDir, cleanSubj+"/") || cleanDir == cleanSubj {
		t.Errorf("DeployLogDir = %q is under SubjectDir %q; deploy logs must not be discoverable by the subject", dir, sb.SubjectDir)
	}
}

// TestDeployLogDir_TwoRunsHaveDistinctPaths asserts that two sandboxes with
// different roots produce different deploy log directories. This is the core
// isolation guarantee: two concurrent runs write their deploy logs to different
// directories and can neither truncate nor read each other's logs.
func TestDeployLogDir_TwoRunsHaveDistinctPaths(t *testing.T) {
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

	dir1 := sb1.DeployLogDir()
	dir2 := sb2.DeployLogDir()

	if dir1 == "" || dir2 == "" {
		t.Fatal("DeployLogDir returned an empty string for at least one sandbox; must return distinct non-empty paths for distinct sandboxes")
	}
	if filepath.Clean(dir1) == filepath.Clean(dir2) {
		t.Errorf("DeployLogDir returned the same path %q for two distinct sandboxes; each run must have its own deploy log directory so concurrent calls cannot truncate each other's latest.log", dir1)
	}
}

// TestDeployLogDir_IsDeterministic asserts that calling DeployLogDir twice on
// the same sandbox returns the same path. A non-deterministic path would mean
// each call produces a different --log-dir value for the same run, scattering
// the delegate's log files across multiple locations.
func TestDeployLogDir_IsDeterministic(t *testing.T) {
	sb := domain.Sandbox{
		Key:        domain.RunKey{RunID: "20260101T120000Z-0001"},
		Root:       "/tmp/sandbox",
		SubjectDir: "/tmp/sandbox/subject",
		ControlDir: "/tmp/sandbox/ctl",
	}

	dir1 := sb.DeployLogDir()
	dir2 := sb.DeployLogDir()

	if filepath.Clean(dir1) != filepath.Clean(dir2) {
		t.Errorf("DeployLogDir returned %q on first call and %q on second call for the same sandbox; must be deterministic", dir1, dir2)
	}
}

// TestDeployLogDir_IsDistinctFromDiagnosticLogPath asserts that the deploy
// log directory does not collide with the diagnostic log file path. A collision
// would mean the deployment tool and the diagnostic sink try to use the same
// filesystem entry, one expecting a directory and the other a file.
func TestDeployLogDir_IsDistinctFromDiagnosticLogPath(t *testing.T) {
	sb := domain.Sandbox{
		Key:        domain.RunKey{RunID: "20260101T120000Z-0001"},
		Root:       "/tmp/sandbox",
		SubjectDir: "/tmp/sandbox/subject",
		ControlDir: "/tmp/sandbox/ctl",
	}

	deployLogDir := sb.DeployLogDir()
	diagLogPath := sb.DiagnosticLogPath()

	if deployLogDir == "" || diagLogPath == "" {
		t.Fatal("DeployLogDir or DiagnosticLogPath returned empty string; cannot assert they are distinct")
	}
	if filepath.Clean(deployLogDir) == filepath.Clean(diagLogPath) {
		t.Errorf("DeployLogDir = %q collides with DiagnosticLogPath = %q; the deploy log directory and the subject diagnostic log must not share a filesystem entry", deployLogDir, diagLogPath)
	}
}
