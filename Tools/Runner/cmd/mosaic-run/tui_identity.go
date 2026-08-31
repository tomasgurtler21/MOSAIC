// tui_identity.go declares the types, sentinel errors, and helper functions
// for TUI entry-point run-identity resolution. The helpers are extracted so
// they are testable independently of runTUIMode (which calls os.Exit and
// launches Bubble Tea and therefore cannot be unit-tested directly).
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
	"mosaic-run/internal/runselect"
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

	// Selection is the runselect.Question built via runselect.Resolve for
	// the deferred shape -- the same decision the CLI refuses on. Non-nil
	// exactly when ScanResult is non-nil. tui.Options.Selection is built
	// from it directly, so the run-select screen is not a second copy of
	// the question-building rules.
	Selection *runselect.Question

	// Workflow is the workflow ID a resumed run recorded when it was
	// created, read from the run's own artifact. It is carried here because
	// the resolved shape bypasses the run-select screen, which is where a
	// chosen run picks its recorded workflow up. Empty for the new-run and
	// deferred shapes, and for a resumed run whose artifact records no
	// workflow or cannot be read.
	Workflow string
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
		runFolder := filepath.Join(workDir, domain.RunScopedFolder(runIDFlag))
		return tuiRunIdentity{
			RunID:      runIDFlag,
			RunFolder:  runFolder,
			IsNewRun:   false,
			ScanResult: nil,
			// This branch bypasses the run-select screen, which is where a
			// chosen run picks up the workflow it recorded, and the setup
			// sequence no longer asks a resumed run which workflow to run. So
			// the workflow is read from the run's own artifact here or it is
			// never supplied at all.
			Workflow: recordedWorkflowOf(runFolder),
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
		if len(result.Candidates) == 0 {
			// No candidates: mint a fresh run identity. Zero candidates offers
			// no real choice besides "start new" either way, so this is not the
			// auto-resume defect the one-or-more-candidate case below removes.
			newID := domain.NewRunID(&realClock{}, domain.DefaultRandomSource())
			return tuiRunIdentity{
				RunID:      newID,
				RunFolder:  filepath.Join(workDir, domain.RunScopedFolder(newID)),
				IsNewRun:   true,
				ScanResult: nil,
			}, nil
		}
		// One or more resumable candidates: defer to the run-select screen.
		// The number of candidates never decides whether the screen is shown --
		// a single candidate must not be auto-resumed, exactly like many.
		//
		// The deferred question is built via runselect.Resolve (neither flag
		// set, so it always yields Decision.Question) rather than a local
		// switch, so the TUI's run-select screen is built from the same
		// decision the CLI refuses on. mint is never invoked here (Resolve
		// only calls it for a resolved new-run outcome).
		mint := newTUIRunIdentityMinter(workDir)
		dec, resErr := runselect.Resolve(runselect.Request{Scan: result, WorkDir: workDir}, runselect.Minter(mint))
		if resErr != nil {
			return tuiRunIdentity{}, fmt.Errorf("%w: %v", errTUIUsage, resErr)
		}
		return tuiRunIdentity{
			RunID:      "",
			RunFolder:  "",
			IsNewRun:   false,
			ScanResult: &result,
			Selection:  dec.Question,
		}, nil
	}
}

// recordedWorkflowOf reads the workflow a run recorded when it was created from
// the run folder's own artifact. It returns "" when the artifact is missing or
// unreadable, which is the honest answer: the run does not say. Substituting a
// plausible workflow would resume the run as something it never was, so the
// absence travels on and the session layer refuses the run by name.
func recordedWorkflowOf(runFolder string) string {
	artifactPath, err := resolveTUIArtifactPath(runFolder)
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		return ""
	}
	state, err := artifact.Parse(data)
	if err != nil {
		return ""
	}
	return string(state.Workflow)
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
