package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
	"mosaic-run/internal/runselect"
)

// resolveRunIdentity resolves run identity from flags or scanner.
//
// When a pre-resolved identity is provided (production path via main.go),
// the resolution step is skipped entirely and the caller-supplied values are
// used. This ensures the RunConfig's RunFolder matches the store path that
// was constructed in main.go before the session was created. Tests pass a
// nil identity to exercise the full internal resolution logic below.
func resolveRunIdentity(identity *RunIdentity, f runFlags, errOut io.Writer) (runID, runFolder string, isNewRun bool, position *runselect.Position, exitCode int, ok bool) {
	if identity != nil {
		// Pre-resolved by caller: use directly, no scanning or minting.
		return identity.RunID, identity.RunFolder, identity.IsNewRun, identity.Position, ExitSuccess, true
	}

	workDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(errOut, "error: getting working directory: %v\n", err)
		return "", "", false, nil, ExitUsage, false
	}

	switch {
	case f.runID != "":
		return resolveRunIdentityByRunID(workDir, f.runID, errOut)
	case f.isNewRun:
		newRunID := domain.NewRunID(cliClock{}, domain.DefaultRandomSource())
		return newRunID, filepath.Join(workDir, domain.RunScopedFolder(newRunID)), true, nil, ExitSuccess, true
	default:
		return resolveRunIdentityByScan(workDir, errOut)
	}
}

// resolveRunIdentityByRunID handles the --run <run_id> path: validate format,
// verify existence, and reject if the run is completed.
func resolveRunIdentityByRunID(workDir, runIDFlag string, errOut io.Writer) (runID, runFolder string, isNewRun bool, position *runselect.Position, exitCode int, ok bool) {
	if !domain.IsValidRunID(runIDFlag) {
		fmt.Fprintf(errOut, "error: invalid run_id format %q; expected {YYYYMMDD}T{HHMMSS}Z-{4-hex}\n", runIDFlag)
		return "", "", false, nil, ExitUsage, false
	}

	folderPath := filepath.Join(workDir, domain.RunScopedFolder(runIDFlag))
	artifactPath := filepath.Join(folderPath, "Orchestration.md")

	data, readErr := os.ReadFile(artifactPath)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			fmt.Fprintf(errOut, "error: no run found with id %s\n", runIDFlag)
		} else {
			fmt.Fprintf(errOut, "error: reading run artifact for %s: %v\n", runIDFlag, readErr)
		}
		return "", "", false, nil, ExitUsage, false
	}

	// An unusable run identity is refused here, naming the problem. Other
	// parse errors are treated as resumable (the session layer will surface
	// any real parse issues). A successful parse also yields the recorded
	// position directly, so the announcement need not read the artifact again.
	state, parseErr := artifact.Parse(data)
	var idErr *domain.RunIdentityError
	if errors.As(parseErr, &idErr) {
		reason := idErr.Error()
		var refErr *domain.RefusalError
		if errors.As(parseErr, &refErr) {
			reason = refErr.Reason
		}
		fmt.Fprintf(errOut, "error: run %s cannot be resumed: %s\n", runIDFlag, reason)
		return "", "", false, nil, ExitUsage, false
	}
	if parseErr == nil {
		if state.RunID != runIDFlag {
			fmt.Fprintf(errOut, "error: run %s cannot be resumed: the artifact's run_id %q does not match its run folder %q\n",
				runIDFlag, state.RunID, domain.RunScopedFolder(runIDFlag))
			return "", "", false, nil, ExitUsage, false
		}
		if strings.EqualFold(state.CurrentState.Phase, "COMPLETED") {
			fmt.Fprintf(errOut, "error: run %s is completed and cannot be resumed\n", runIDFlag)
			return "", "", false, nil, ExitUsage, false
		}
		position = &runselect.Position{
			Phase:       state.CurrentState.Phase,
			Stage:       state.CurrentState.Stage,
			LastAgent:   state.CurrentState.LastAgent,
			LastUpdated: state.LastUpdated,
		}
	}

	return runIDFlag, folderPath, false, position, ExitSuccess, true
}

// resolveRunIdentityByScan handles the path where neither --run nor --new-run
// was given. The selection is never inferred from how many runs exist: scan
// the working directory and ask runselect for the decision. A Question means
// the CLI refuses and names the available choices rather than guessing
// (AC2.2, AC2.6).
func resolveRunIdentityByScan(workDir string, errOut io.Writer) (runID, runFolder string, isNewRun bool, position *runselect.Position, exitCode int, ok bool) {
	scanner := runscan.NewDirScanner()
	scanResult, scanErr := scanner.Scan(workDir)
	if scanErr != nil {
		fmt.Fprintf(errOut, "error: scanning for runs: %v\n", scanErr)
		return "", "", false, nil, ExitUsage, false
	}

	dec, resErr := runselect.Resolve(runselect.Request{Scan: scanResult, WorkDir: workDir}, cliMinter(workDir))
	if resErr != nil {
		fmt.Fprintf(errOut, "error: %v\n", resErr)
		return "", "", false, nil, ExitUsage, false
	}
	if dec.Question != nil {
		fmt.Fprintf(errOut, "error: %s\n", formatSelectionRefusal(*dec.Question))
		return "", "", false, nil, ExitUsage, false
	}

	return dec.Resolved.RunID, dec.Resolved.RunFolder, dec.Resolved.IsNewRun, dec.Resolved.Position, ExitSuccess, true
}

// cliClock implements domain.Clock using the real system clock.
type cliClock struct{}

func (cliClock) Now() time.Time { return time.Now().UTC() }

// cliMinter returns a runselect.Minter that mints a new run_id rooted at workDir.
func cliMinter(workDir string) runselect.Minter {
	return func() (string, string) {
		newRunID := domain.NewRunID(cliClock{}, domain.DefaultRandomSource())
		return newRunID, filepath.Join(workDir, domain.RunScopedFolder(newRunID))
	}
}

// formatSelectionRefusal renders the non-interactive refusal message for an
// unsettled selection: every resumable run_id the caller may pass to --run,
// every unresumable run with the reason it cannot be resumed (AC2.5), and
// --new-run as the always-available way to start fresh (AC2.3, AC2.6).
func formatSelectionRefusal(q runselect.Question) string {
	var resumable []string
	var unresumable []string
	for _, c := range q.Choices {
		switch c.Kind {
		case runselect.ChoiceResume:
			resumable = append(resumable, c.ID)
		case runselect.ChoiceUnresumable:
			why := c.Reason.Description()
			if c.Detail != "" {
				why += ": " + c.Detail
			}
			unresumable = append(unresumable, fmt.Sprintf("%s (%s)", c.ID, why))
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

// announceIdentity builds the runselect.Identity used to render the
// chosen-run announcement. When position is already known to the caller
// (the artifact was already read to resolve identity), it is used directly.
// Otherwise, for a resumed run, the artifact is read once here to recover
// it; a read or parse failure simply leaves Position nil, which
// runselect.Announce handles without panicking.
func announceIdentity(runID, runFolder string, isNewRun bool, position *runselect.Position) runselect.Identity {
	id := runselect.Identity{RunID: runID, RunFolder: runFolder, IsNewRun: isNewRun, Position: position}
	if isNewRun || position != nil {
		return id
	}
	data, err := os.ReadFile(filepath.Join(runFolder, "Orchestration.md"))
	if err != nil {
		return id
	}
	state, err := artifact.Parse(data)
	if err != nil {
		return id
	}
	id.Position = &runselect.Position{
		Phase:       state.CurrentState.Phase,
		Stage:       state.CurrentState.Stage,
		LastAgent:   state.CurrentState.LastAgent,
		LastUpdated: state.LastUpdated,
	}
	return id
}
