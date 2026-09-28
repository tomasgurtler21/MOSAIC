package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

// ---------------------------------------------------------------------------
// resolveRunIdentityForCLI: --input + --run pre-scan mutual-exclusion (T2.3)
//
// These tests prove that the mutual-exclusion check fires inside
// resolveRunIdentityForCLI *before* any run-folder read. The technique: supply
// a valid-format run_id whose folder does not exist on disk. If the refusal
// came from the folder read, the error would say "no run found"; if it came
// from the pre-scan check, the error must describe the mutual exclusion.
// ---------------------------------------------------------------------------

// mainTestRunID is a valid-format run_id whose folder is never created on disk
// in these tests, so any attempt to read it would produce "no run found".
const mainTestRunID = "20260727T170000Z-a3f9"

// TestResolveRunIdentityForCLI_InputWithRun_RefusesMutuallyExclusive is the
// core T2.3 assertion. It verifies that providing --input together with --run
// causes resolveRunIdentityForCLI to return a mutual-exclusion error without
// reading the run folder. The named run folder is absent from disk; if the
// function reads the folder before checking the flags, it returns
// "no run found" instead of the mutual-exclusion message — which fails this
// test and proves the check came too late.
func TestResolveRunIdentityForCLI_InputWithRun_RefusesMutuallyExclusive(t *testing.T) {
	args := []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--input", "/some/seed.md",
		"--run", mainTestRunID,
	}

	_, _, err := resolveRunIdentityForCLI(args)
	if err == nil {
		t.Fatal("resolveRunIdentityForCLI returned nil error; want a mutual-exclusion refusal")
	}
	// A "no run found" error means the folder was read before the flag check.
	if strings.Contains(err.Error(), "no run found") {
		t.Errorf("error %q suggests the run folder was read before the pre-scan check fired; "+
			"the mutual-exclusion check must precede any os.ReadFile call", err.Error())
	}
	// The error must identify the conflict between --input and --run.
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error %q does not contain \"mutually exclusive\"; "+
			"the pre-scan check must produce a clear mutual-exclusion message", err.Error())
	}
}

// TestResolveRunIdentityForCLI_InputAloneWithoutRun_DoesNotRefuse verifies
// that --input by itself (no --run) does not trigger the --input/--run
// mutual-exclusion check. --new-run is passed explicitly so the assertion
// exercises only that mutual-exclusion check, not the separate zero-candidate
// selection refusal AC2.6 requires when neither --run nor --new-run is given.
func TestResolveRunIdentityForCLI_InputAloneWithoutRun_DoesNotRefuse(t *testing.T) {
	rootDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	args := []string{
		"run",
		"--orchestrator-file", "orch.md",
		"--workflow", "w1",
		"--task", "do work",
		"--input", "/some/seed.md",
		"--new-run",
		// no --run flag
	}

	identity, _, err := resolveRunIdentityForCLI(args)
	if err != nil {
		t.Fatalf("resolveRunIdentityForCLI returned unexpected error: %v", err)
	}
	if identity == nil {
		t.Fatal("resolveRunIdentityForCLI returned nil identity; want a new-run identity")
	}
	if !identity.IsNewRun {
		t.Error("IsNewRun = false, want true when --input is given without --run (--new-run mints a new run)")
	}
}

// ---------------------------------------------------------------------------
// resolveRunIdentityForTUI
//
// These tests drive the TDD RED phase for the new resolveRunIdentityForTUI
// helper. All tests compile against the stub declarations in
// tui_identity_stub.go and fail at runtime because the stubs return zero
// values. They become GREEN when the real implementation is written.
// ---------------------------------------------------------------------------

// TestResolveRunIdentityForTUI_NewRunFlag_YieldsValidRunID verifies that
// --new-run produces a non-empty run ID matching the canonical format.
func TestResolveRunIdentityForTUI_NewRunFlag_YieldsValidRunID(t *testing.T) {
	workDir := t.TempDir()
	identity, err := resolveRunIdentityForTUI([]string{"--new-run"}, workDir)
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(--new-run) error = %v, want nil", err)
	}
	if identity.RunID == "" {
		t.Error("RunID is empty; want a non-empty run ID in canonical format")
	}
	if !domain.IsValidRunID(identity.RunID) {
		t.Errorf("RunID %q does not match canonical format {YYYYMMDD}T{HHMMSS}Z-{4-hex}", identity.RunID)
	}
}

// TestResolveRunIdentityForTUI_NewRunFlag_YieldsAbsoluteScopedFolder verifies
// that --new-run produces an absolute run folder whose final path element is
// Orchestration-{run_id}.
func TestResolveRunIdentityForTUI_NewRunFlag_YieldsAbsoluteScopedFolder(t *testing.T) {
	workDir := t.TempDir()
	identity, err := resolveRunIdentityForTUI([]string{"--new-run"}, workDir)
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(--new-run) error = %v, want nil", err)
	}
	if !filepath.IsAbs(identity.RunFolder) {
		t.Errorf("RunFolder %q is not absolute", identity.RunFolder)
	}
	wantSuffix := domain.RunScopedFolder(identity.RunID)
	if !strings.HasSuffix(identity.RunFolder, wantSuffix) {
		t.Errorf("RunFolder %q does not end in %q", identity.RunFolder, wantSuffix)
	}
}

// TestResolveRunIdentityForTUI_NewRunFlag_SetsIsNewRun verifies that --new-run
// sets IsNewRun = true in the returned identity.
func TestResolveRunIdentityForTUI_NewRunFlag_SetsIsNewRun(t *testing.T) {
	workDir := t.TempDir()
	identity, err := resolveRunIdentityForTUI([]string{"--new-run"}, workDir)
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(--new-run) error = %v, want nil", err)
	}
	if !identity.IsNewRun {
		t.Error("IsNewRun = false, want true for --new-run")
	}
}

// TestResolveRunIdentityForTUI_NewRunFlag_ScanResultIsNil verifies that the
// --new-run branch returns nil ScanResult: identity is resolved, not deferred
// to the run-select screen.
func TestResolveRunIdentityForTUI_NewRunFlag_ScanResultIsNil(t *testing.T) {
	workDir := t.TempDir()
	identity, err := resolveRunIdentityForTUI([]string{"--new-run"}, workDir)
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(--new-run) error = %v, want nil", err)
	}
	if identity.ScanResult != nil {
		t.Errorf("ScanResult = %v, want nil for --new-run (resolved, not deferred)", identity.ScanResult)
	}
}

// TestResolveRunIdentityForTUI_ZeroCandidates_YieldsValidRunID verifies that
// when the directory scan finds zero candidates, a run ID is minted in canonical
// format — mirroring the --new-run branch.
func TestResolveRunIdentityForTUI_ZeroCandidates_YieldsValidRunID(t *testing.T) {
	workDir := t.TempDir() // empty directory → zero candidates
	identity, err := resolveRunIdentityForTUI([]string{}, workDir)
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(zero candidates) error = %v, want nil", err)
	}
	if identity.RunID == "" {
		t.Error("RunID is empty; want a non-empty run ID in canonical format")
	}
	if !domain.IsValidRunID(identity.RunID) {
		t.Errorf("RunID %q does not match canonical format {YYYYMMDD}T{HHMMSS}Z-{4-hex}", identity.RunID)
	}
}

// TestResolveRunIdentityForTUI_ZeroCandidates_YieldsAbsoluteScopedFolder verifies
// that the zero-candidate branch produces an absolute scoped folder under workDir.
func TestResolveRunIdentityForTUI_ZeroCandidates_YieldsAbsoluteScopedFolder(t *testing.T) {
	workDir := t.TempDir()
	identity, err := resolveRunIdentityForTUI([]string{}, workDir)
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(zero candidates) error = %v, want nil", err)
	}
	if !filepath.IsAbs(identity.RunFolder) {
		t.Errorf("RunFolder %q is not absolute", identity.RunFolder)
	}
	wantSuffix := domain.RunScopedFolder(identity.RunID)
	if !strings.HasSuffix(identity.RunFolder, wantSuffix) {
		t.Errorf("RunFolder %q does not end in %q", identity.RunFolder, wantSuffix)
	}
}

// TestResolveRunIdentityForTUI_ZeroCandidates_SetsIsNewRun verifies that the
// zero-candidate branch sets IsNewRun = true and ScanResult = nil.
func TestResolveRunIdentityForTUI_ZeroCandidates_SetsIsNewRun(t *testing.T) {
	workDir := t.TempDir()
	identity, err := resolveRunIdentityForTUI([]string{}, workDir)
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(zero candidates) error = %v, want nil", err)
	}
	if !identity.IsNewRun {
		t.Error("IsNewRun = false, want true when zero scan candidates")
	}
	if identity.ScanResult != nil {
		t.Errorf("ScanResult = %v, want nil for zero-candidate new run", identity.ScanResult)
	}
}

// TestResolveRunIdentityForTUI_ConsecutiveNewRuns_ProduceDistinctIdentities
// verifies that two consecutive --new-run resolutions yield distinct run IDs
// and distinct run folders, satisfying the successive-minting contract.
func TestResolveRunIdentityForTUI_ConsecutiveNewRuns_ProduceDistinctIdentities(t *testing.T) {
	workDir := t.TempDir()
	id1, err := resolveRunIdentityForTUI([]string{"--new-run"}, workDir)
	if err != nil {
		t.Fatalf("first resolveRunIdentityForTUI call error = %v", err)
	}
	id2, err := resolveRunIdentityForTUI([]string{"--new-run"}, workDir)
	if err != nil {
		t.Fatalf("second resolveRunIdentityForTUI call error = %v", err)
	}
	if id1.RunID == id2.RunID {
		t.Errorf("consecutive mints produced the same RunID %q; each call must yield a distinct ID", id1.RunID)
	}
	if id1.RunFolder == id2.RunFolder {
		t.Errorf("consecutive mints produced the same RunFolder %q; each call must yield a distinct folder", id1.RunFolder)
	}
}

// TestResolveRunIdentityForTUI_RunFlag_ResolvesToNamedFolder verifies that
// --run <valid_id> resolves to the named run folder without minting, and
// does not set IsNewRun.
func TestResolveRunIdentityForTUI_RunFlag_ResolvesToNamedFolder(t *testing.T) {
	workDir := t.TempDir()
	const runID = "20260727T170000Z-a3f9"
	identity, err := resolveRunIdentityForTUI([]string{"--run", runID}, workDir)
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(--run %s) error = %v, want nil", runID, err)
	}
	if identity.RunID != runID {
		t.Errorf("RunID = %q, want %q", identity.RunID, runID)
	}
	wantFolder := filepath.Join(workDir, domain.RunScopedFolder(runID))
	if identity.RunFolder != wantFolder {
		t.Errorf("RunFolder = %q, want %q", identity.RunFolder, wantFolder)
	}
	if identity.IsNewRun {
		t.Error("IsNewRun = true, want false for --run <id>")
	}
	if identity.ScanResult != nil {
		t.Errorf("ScanResult = %v, want nil for --run", identity.ScanResult)
	}
}

// TestResolveRunIdentityForTUI_RunAndNewRunTogether_ReturnsUsageError verifies
// that providing both --run and --new-run returns a usage error wrapping errTUIUsage.
func TestResolveRunIdentityForTUI_RunAndNewRunTogether_ReturnsUsageError(t *testing.T) {
	workDir := t.TempDir()
	const runID = "20260727T170000Z-a3f9"
	_, err := resolveRunIdentityForTUI([]string{"--run", runID, "--new-run"}, workDir)
	if err == nil {
		t.Fatal("resolveRunIdentityForTUI(--run + --new-run) returned nil error, want usage error")
	}
	if !errors.Is(err, errTUIUsage) {
		t.Errorf("error %v does not wrap errTUIUsage; got errors.Is = false", err)
	}
}

// TestResolveRunIdentityForTUI_InvalidRunID_ReturnsUsageError verifies that
// --run with a malformed run ID returns an error wrapping errTUIUsage.
func TestResolveRunIdentityForTUI_InvalidRunID_ReturnsUsageError(t *testing.T) {
	workDir := t.TempDir()
	_, err := resolveRunIdentityForTUI([]string{"--run", "not-a-valid-id"}, workDir)
	if err == nil {
		t.Fatal("resolveRunIdentityForTUI(--run <invalid>) returned nil error, want usage error")
	}
	if !errors.Is(err, errTUIUsage) {
		t.Errorf("error %v does not wrap errTUIUsage; got errors.Is = false", err)
	}
}

// TestResolveRunIdentityForTUI_MultipleCandidate_DefersToRunSelectScreen verifies
// that when the scan finds multiple candidates, resolveRunIdentityForTUI returns
// empty identity fields and a non-nil ScanResult, deferring to the run-select screen.
func TestResolveRunIdentityForTUI_MultipleCandidate_DefersToRunSelectScreen(t *testing.T) {
	workDir := t.TempDir()
	// Create two directories with valid Orchestration-{run_id} names. Missing
	// Orchestration.md causes the scanner to classify both as resumable candidates.
	for _, id := range []string{"20260727T170000Z-a3f9", "20260727T180000Z-b1c2"} {
		folder := filepath.Join(workDir, domain.RunScopedFolder(id))
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", folder, err)
		}
	}

	identity, err := resolveRunIdentityForTUI([]string{}, workDir)
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(multi-candidate) error = %v, want nil", err)
	}
	if identity.RunID != "" {
		t.Errorf("RunID = %q, want empty string for multi-candidate deferral", identity.RunID)
	}
	if identity.RunFolder != "" {
		t.Errorf("RunFolder = %q, want empty string for multi-candidate deferral", identity.RunFolder)
	}
	if identity.IsNewRun {
		t.Error("IsNewRun = true, want false for multi-candidate deferral")
	}
	if identity.ScanResult == nil {
		t.Error("ScanResult is nil, want non-nil scan result for multi-candidate deferral")
	}
}

// TestResolveRunIdentityForTUI_SingleCandidate_DefersToRunSelectScreen is the
// TUI entry-point expression of the core defect this stage removes (AC2.2):
// exactly one resumable run must no longer be auto-resumed before the TUI
// even launches. resolveRunIdentityForTUI must defer to the run-select
// screen exactly as it already does for the multi-candidate case, returning
// empty identity fields and a non-nil ScanResult.
//
// Currently fails (RED): resolveRunIdentityForTUI still auto-resumes the
// single candidate (the `case 1` branch this stage removes).
func TestResolveRunIdentityForTUI_SingleCandidate_DefersToRunSelectScreen(t *testing.T) {
	workDir := t.TempDir()
	const runID = "20260727T170000Z-a3f9"
	folder := filepath.Join(workDir, domain.RunScopedFolder(runID))
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", folder, err)
	}

	identity, err := resolveRunIdentityForTUI([]string{}, workDir)
	if err != nil {
		t.Fatalf("resolveRunIdentityForTUI(single candidate) error = %v, want nil", err)
	}
	if identity.RunID != "" {
		t.Errorf("RunID = %q, want empty string; a single candidate must defer to the run-select screen, not auto-resume", identity.RunID)
	}
	if identity.RunFolder != "" {
		t.Errorf("RunFolder = %q, want empty string for single-candidate deferral", identity.RunFolder)
	}
	if identity.IsNewRun {
		t.Error("IsNewRun = true, want false for single-candidate deferral")
	}
	if identity.ScanResult == nil {
		t.Fatal("ScanResult = nil, want non-nil scan result for single-candidate deferral")
	}
	if len(identity.ScanResult.Candidates) != 1 || identity.ScanResult.Candidates[0].RunID != runID {
		t.Errorf("ScanResult.Candidates = %+v, want exactly one candidate with RunID %q", identity.ScanResult.Candidates, runID)
	}
}

// TestResolveRunIdentityForTUI_ScanFails_ReturnsNonUsageError verifies that
// when no flags are provided and the directory scan itself fails, the returned
// error is non-nil and does not wrap errTUIUsage. A scan error is an
// unexpected I/O failure, not a user-argument mistake, so it must exit with
// code 1 rather than code 2 at the call site.
//
// Fixture: a regular file is supplied as workDir. Attempting to list directory
// entries from a file path fails on all supported platforms (Linux, macOS,
// Windows), making this a portable scan-failure fixture without requiring
// OS-specific permission manipulation.
func TestResolveRunIdentityForTUI_ScanFails_ReturnsNonUsageError(t *testing.T) {
	dir := t.TempDir()
	notADir := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(notADir, []byte("placeholder"), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", notADir, err)
	}

	_, err := resolveRunIdentityForTUI([]string{}, notADir)
	if err == nil {
		t.Fatal("resolveRunIdentityForTUI(scan fails) returned nil error, want a non-nil error")
	}
	if errors.Is(err, errTUIUsage) {
		t.Errorf("error %v wraps errTUIUsage; scan errors must not be classified as usage errors", err)
	}
}

// ---------------------------------------------------------------------------
// resolveTUIArtifactPath
//
// Tests drive the TDD RED phase for the resolveTUIArtifactPath helper.
// The stub returns ("", nil) for all inputs; the real implementation will
// return the joined path for non-empty folders and errUnresolvedRunFolder for
// empty folders.
// ---------------------------------------------------------------------------

// TestResolveTUIArtifactPath_NonEmptyFolder_ReturnsJoinedPath verifies that a
// non-empty run folder produces filepath.Join(runFolder, "Orchestration.md").
func TestResolveTUIArtifactPath_NonEmptyFolder_ReturnsJoinedPath(t *testing.T) {
	const runFolder = "/runs/Orchestration-20260727T170000Z-a3f9"
	got, err := resolveTUIArtifactPath(runFolder)
	if err != nil {
		t.Fatalf("resolveTUIArtifactPath(%q) error = %v, want nil", runFolder, err)
	}
	want := filepath.Join(runFolder, "Orchestration.md")
	if got != want {
		t.Errorf("resolveTUIArtifactPath(%q) = %q, want %q", runFolder, got, want)
	}
}

// TestResolveTUIArtifactPath_EmptyFolder_ReturnsUnresolvedError verifies that
// an empty run folder returns errUnresolvedRunFolder. This is the contract
// violation path that replaces the implicit "Orchestration.md" fallback.
func TestResolveTUIArtifactPath_EmptyFolder_ReturnsUnresolvedError(t *testing.T) {
	got, err := resolveTUIArtifactPath("")
	if err == nil {
		t.Fatalf("resolveTUIArtifactPath(\"\") returned nil error and %q, want errUnresolvedRunFolder", got)
	}
	if !errors.Is(err, errUnresolvedRunFolder) {
		t.Errorf("error %v does not wrap errUnresolvedRunFolder; got errors.Is = false", err)
	}
}

// TestResolveTUIArtifactPath_EmptyFolder_NeverReturnsBareRelativePath verifies
// the core safety property: the function must never return the bare relative path
// "Orchestration.md" under any input, eliminating the implicit CWD-relative fallback.
func TestResolveTUIArtifactPath_EmptyFolder_NeverReturnsBareRelativePath(t *testing.T) {
	got, _ := resolveTUIArtifactPath("")
	if got == "Orchestration.md" {
		t.Error("resolveTUIArtifactPath(\"\") returned bare \"Orchestration.md\"; this is the forbidden CWD-relative fallback")
	}
}

// ---------------------------------------------------------------------------
// newTUIRunIdentityMinter
//
// Tests drive the TDD RED phase for the newTUIRunIdentityMinter helper.
// The stub returns a minter that always returns ("", ""); the real
// implementation returns valid, distinct pairs rooted at workDir.
// ---------------------------------------------------------------------------

// TestNewTUIRunIdentityMinter_YieldsValidRunID verifies that the minter
// returned by newTUIRunIdentityMinter produces a run ID satisfying
// domain.IsValidRunID.
func TestNewTUIRunIdentityMinter_YieldsValidRunID(t *testing.T) {
	workDir := t.TempDir()
	minter := newTUIRunIdentityMinter(workDir)
	runID, _ := minter()
	if !domain.IsValidRunID(runID) {
		t.Errorf("minter() runID %q does not satisfy domain.IsValidRunID", runID)
	}
}

// TestNewTUIRunIdentityMinter_YieldsScopedFolderUnderWorkDir verifies that
// the run folder equals filepath.Join(workDir, domain.RunScopedFolder(runID)).
func TestNewTUIRunIdentityMinter_YieldsScopedFolderUnderWorkDir(t *testing.T) {
	workDir := t.TempDir()
	minter := newTUIRunIdentityMinter(workDir)
	runID, runFolder := minter()
	want := filepath.Join(workDir, domain.RunScopedFolder(runID))
	if runFolder != want {
		t.Errorf("minter() runFolder = %q, want %q", runFolder, want)
	}
}

// TestNewTUIRunIdentityMinter_SuccessiveCallsProduceDistinctPairs verifies
// that calling the minter twice yields distinct (runID, runFolder) pairs,
// satisfying the successive-minting contract.
func TestNewTUIRunIdentityMinter_SuccessiveCallsProduceDistinctPairs(t *testing.T) {
	workDir := t.TempDir()
	minter := newTUIRunIdentityMinter(workDir)
	runID1, folder1 := minter()
	runID2, folder2 := minter()
	if runID1 == runID2 {
		t.Errorf("successive mints produced the same runID %q; each call must yield a distinct ID", runID1)
	}
	if folder1 == folder2 {
		t.Errorf("successive mints produced the same runFolder %q; each call must yield a distinct folder", folder1)
	}
}
