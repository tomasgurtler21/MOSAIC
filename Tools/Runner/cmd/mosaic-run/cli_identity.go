package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/cli"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
	"mosaic-run/internal/runselect"
)

// resolveRunIdentityForCLI pre-scans --run / --new-run and the working directory
// to determine the run folder before the session is constructed. It mirrors the
// pre-scan pattern used for --mode and --manual-resolution.
//
// When --run <run_id> is present, the folder is Orchestration-{run_id} under the
// working directory. When --new-run is present, a new run_id is minted here so
// that the same id is used by both the session's store and cli.Run's RunConfig.
// When neither flag is present, the working directory is scanned for resumable
// candidates; zero candidates mints a new run_id, one candidate uses that folder,
// and multiple candidates return an error (the multi-candidate rejection cannot be
// deferred to cli.Run because a non-nil identity skips cli.Run's internal check).
//
// The second return value is always nil. Callers are responsible for constructing
// the artifact store after identity is resolved, using the process logger so that
// path anomalies are captured in the debug log. The nil return preserves the
// three-value signature for call-site compatibility.
//
// An optional workDir may be passed as the last argument to avoid a redundant
// os.Getwd syscall when the caller has already resolved the working directory.
// When omitted, os.Getwd is called internally.
func resolveRunIdentityForCLI(args []string, workDirs ...string) (*cli.RunIdentity, domain.ArtifactStore, error) {
	var workDir string
	if len(workDirs) > 0 {
		workDir = workDirs[0]
	} else {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return nil, nil, fmt.Errorf("getting working directory: %w", err)
		}
	}

	runIDFlag := scanFlag(args, "--run")
	isNewRunFlag := scanBoolFlag(args, "--new-run")

	// Refuse --input together with --run before any run-folder access. This check
	// must precede the switch so the refusal cannot come from reading Orchestration.md.
	if hasFlag(args, "--input") && runIDFlag != "" {
		return nil, nil, fmt.Errorf("--input and --run are mutually exclusive")
	}

	var runID, runFolder string
	var isNewRun bool
	var position *runselect.Position

	switch {
	case runIDFlag != "":
		// --run <run_id>: validate format, verify the run folder exists on disk,
		// and reject completed runs. These checks must be done here rather than
		// deferred to cli.Run, because cli.Run skips its own resolution step
		// whenever a non-nil identity is supplied (which is always the case in
		// production). Omitting them here would silently bypass AC5.3.
		if !domain.IsValidRunID(runIDFlag) {
			return nil, nil, fmt.Errorf("invalid run_id format %q; expected {YYYYMMDD}T{HHMMSS}Z-{4-hex}", runIDFlag)
		}
		folderPath := filepath.Join(workDir, domain.RunScopedFolder(runIDFlag))
		artifactPath := filepath.Join(folderPath, "Orchestration.md")
		data, readErr := os.ReadFile(artifactPath)
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				return nil, nil, fmt.Errorf("no run found with id %s", runIDFlag)
			}
			return nil, nil, fmt.Errorf("reading run artifact for %s: %w", runIDFlag, readErr)
		}
		// Treat parse errors as resumable: the session layer will surface real
		// format problems when it calls store.Read. Only reject when we can
		// confirm the run is completed. A successful parse also yields the
		// recorded position directly, so cli.Run's announcement need not read
		// the artifact a second time.
		if state, parseErr := artifact.Parse(data); parseErr == nil {
			if strings.EqualFold(state.CurrentState.Phase, "COMPLETED") {
				return nil, nil, fmt.Errorf("run %s is completed and cannot be resumed", runIDFlag)
			}
			position = &runselect.Position{
				Phase:       state.CurrentState.Phase,
				Stage:       state.CurrentState.Stage,
				LastAgent:   state.CurrentState.LastAgent,
				LastUpdated: state.LastUpdated,
			}
		}
		runID = runIDFlag
		runFolder = folderPath
		isNewRun = false

	case isNewRunFlag:
		// --new-run: mint the run_id here so both the session's store and the
		// RunConfig use the same path. cli.Run receives the identity and skips
		// its own mint.
		newID := domain.NewRunID(&realClock{}, domain.DefaultRandomSource())
		runID = newID
		runFolder = filepath.Join(workDir, domain.RunScopedFolder(newID))
		isNewRun = true

	default:
		// Neither flag: the selection is never inferred from what the
		// workspace happens to contain, whatever that is -- zero candidates
		// included (Plan.md: "Minting a new run because none existed is
		// exactly the inference this stage removes."). Scan the working
		// directory and ask runselect for the decision; a non-nil identity
		// returned to cli.Run would bypass cli.Run's own resolution step, so
		// the refusal must happen here rather than being deferred (AC2.2,
		// AC2.6, AC2.9).
		scanner := runscan.NewDirScanner()
		result, scanErr := scanner.Scan(workDir)
		if scanErr != nil {
			return nil, nil, fmt.Errorf("scanning for runs: %w", scanErr)
		}
		dec, resErr := runselect.Resolve(runselect.Request{Scan: result, WorkDir: workDir}, mainMinter(workDir))
		if resErr != nil {
			return nil, nil, resErr
		}
		if dec.Question != nil {
			return nil, nil, fmt.Errorf("%s", formatSelectionRefusal(*dec.Question))
		}
		runID = dec.Resolved.RunID
		runFolder = dec.Resolved.RunFolder
		isNewRun = dec.Resolved.IsNewRun
		position = dec.Resolved.Position
	}

	identity := &cli.RunIdentity{
		RunID:     runID,
		RunFolder: runFolder,
		IsNewRun:  isNewRun,
		Position:  position,
	}
	// Return nil for the store. The caller constructs the authoritative store
	// via newLoggedArtifactStore with the process logger, ensuring path anomalies
	// are captured in the debug log. Store construction here would require a
	// no-op logger (process logger not in scope) and would be immediately
	// discarded at the call site anyway.
	return identity, nil, nil
}

// mainMinter returns a runselect.Minter that mints a new run_id rooted at workDir.
func mainMinter(workDir string) runselect.Minter {
	return func() (string, string) {
		newID := domain.NewRunID(&realClock{}, domain.DefaultRandomSource())
		return newID, filepath.Join(workDir, domain.RunScopedFolder(newID))
	}
}

// formatSelectionRefusal renders the non-interactive refusal message for an
// unsettled selection: every resumable run_id the caller may pass to --run,
// every unresumable run with the reason it cannot be resumed (AC2.5), and
// --new-run as the always-available way to start fresh (AC2.3, AC2.6). This
// mirrors internal/cli.formatSelectionRefusal; it is duplicated here rather
// than exported across the package boundary because cli.Run and
// resolveRunIdentityForCLI are independent resolution sites that share the
// runselect decision but not their output plumbing.
func formatSelectionRefusal(q runselect.Question) string {
	var resumable []string
	var unresumable []string
	for _, c := range q.Choices {
		switch c.Kind {
		case runselect.ChoiceResume:
			resumable = append(resumable, c.ID)
		case runselect.ChoiceUnresumable:
			unresumable = append(unresumable, fmt.Sprintf("%s (%s)", c.ID, c.Reason.Description()))
		}
	}
	var sb strings.Builder
	sb.WriteString("run selection is required; ")
	if len(resumable) > 0 {
		sb.WriteString("use --run <run_id> to resume one of: ")
		sb.WriteString(strings.Join(resumable, ", "))
		sb.WriteString(", or ")
	}
	sb.WriteString("use --new-run to start a new run")
	if len(unresumable) > 0 {
		sb.WriteString("; cannot be resumed: ")
		sb.WriteString(strings.Join(unresumable, ", "))
	}
	return sb.String()
}
