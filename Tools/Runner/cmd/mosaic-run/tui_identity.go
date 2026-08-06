// tui_identity.go declares the types, sentinel errors, and helper functions
// for TUI entry-point run-identity resolution. The helpers are extracted so
// they are testable independently of runTUIMode (which calls os.Exit and
// launches Bubble Tea and therefore cannot be unit-tested directly).
package main

import (
	"errors"
	"fmt"
	"path/filepath"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
	"mosaic-run/internal/tui"
)

// tuiRunIdentity is the resolved (or deliberately deferred) run identity for a
// TUI launch. Exactly one of two shapes is valid:
//
//	resolved: RunFolder != "" and ScanResult == nil
//	deferred: RunID == "" and RunFolder == "" and ScanResult != nil
type tuiRunIdentity struct {
	RunID      string
	RunFolder  string
	IsNewRun   bool
	ScanResult *runscan.ScanResult
}

// errTUIUsage marks argument-usage errors. The caller reports them on stderr
// and exits with code 2. All other errors exit with code 1.
var errTUIUsage = errors.New("usage")

// errUnresolvedRunFolder signals that a run folder was required but not supplied.
var errUnresolvedRunFolder = errors.New("run folder is unresolved; cannot derive an artifact path")

// resolveRunIdentityForTUI resolves run identity for the TUI entry point before
// the session is constructed. It mirrors resolveRunIdentityForCLI, except that
// the multi-candidate case is not an error: it is deferred to the run-select
// screen by returning a non-nil ScanResult with empty identity fields.
//
// workDir is the directory used to root scoped run folders and to scan for
// resumable candidates. Callers pass the process working directory; tests pass
// a temporary directory.
func resolveRunIdentityForTUI(args []string, workDir string) (tuiRunIdentity, error) {
	runIDFlag := scanFlag(args, "--run")
	isNewRunFlag := scanBoolFlag(args, "--new-run")

	if runIDFlag != "" && isNewRunFlag {
		return tuiRunIdentity{}, fmt.Errorf("%w: --run and --new-run are mutually exclusive", errTUIUsage)
	}

	switch {
	case runIDFlag != "":
		// --run <run_id>: validate format and resolve the run folder.
		if !domain.IsValidRunID(runIDFlag) {
			return tuiRunIdentity{}, fmt.Errorf("%w: invalid run_id format %q; expected {YYYYMMDD}T{HHMMSS}Z-{4-hex}", errTUIUsage, runIDFlag)
		}
		return tuiRunIdentity{
			RunID:      runIDFlag,
			RunFolder:  filepath.Join(workDir, domain.RunScopedFolder(runIDFlag)),
			IsNewRun:   false,
			ScanResult: nil,
		}, nil

	case isNewRunFlag:
		// --new-run: mint a fresh run identity now, before the session is constructed.
		newID := domain.NewRunID(&realClock{}, domain.DefaultRandomSource())
		return tuiRunIdentity{
			RunID:      newID,
			RunFolder:  filepath.Join(workDir, domain.RunScopedFolder(newID)),
			IsNewRun:   true,
			ScanResult: nil,
		}, nil

	default:
		// Neither flag: scan the working directory for resumable candidates.
		scanner := runscan.NewDirScanner()
		result, scanErr := scanner.Scan(workDir)
		if scanErr != nil {
			return tuiRunIdentity{}, fmt.Errorf("scanning for runs: %w", scanErr)
		}
		switch len(result.Candidates) {
		case 0:
			// No candidates: mint a fresh run identity.
			newID := domain.NewRunID(&realClock{}, domain.DefaultRandomSource())
			return tuiRunIdentity{
				RunID:      newID,
				RunFolder:  filepath.Join(workDir, domain.RunScopedFolder(newID)),
				IsNewRun:   true,
				ScanResult: nil,
			}, nil
		case 1:
			// Single candidate: auto-resume.
			return tuiRunIdentity{
				RunID:      result.Candidates[0].RunID,
				RunFolder:  result.Candidates[0].FolderPath,
				IsNewRun:   false,
				ScanResult: nil,
			}, nil
		default:
			// Multiple candidates: defer to the run-select screen.
			return tuiRunIdentity{
				RunID:      "",
				RunFolder:  "",
				IsNewRun:   false,
				ScanResult: &result,
			}, nil
		}
	}
}

// resolveTUIArtifactPath returns the artifact path for a resolved run folder.
// It never returns a bare, working-directory-relative path: an empty runFolder
// is a contract violation by the caller and yields errUnresolvedRunFolder.
func resolveTUIArtifactPath(runFolder string) (string, error) {
	if runFolder == "" {
		return "", errUnresolvedRunFolder
	}
	return filepath.Join(runFolder, "Orchestration.md"), nil
}

// newTUIRunIdentityMinter returns a RunIdentityMinter that mints run IDs with
// the real clock and the default random source, rooting scoped run folders at
// workDir.
func newTUIRunIdentityMinter(workDir string) tui.RunIdentityMinter {
	return func() (string, string) {
		runID := domain.NewRunID(&realClock{}, domain.DefaultRandomSource())
		runFolder := filepath.Join(workDir, domain.RunScopedFolder(runID))
		return runID, runFolder
	}
}
