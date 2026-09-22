// SubprocessRunInvoker implementation: shells out to mosaic-run run as a
// subprocess and discovers the resulting run folder.
package testrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mosaic-run/internal/domain"
)

// maxListingEntries is the maximum number of workspace entry names included in
// discoveryError.Listing. Mandatory cap.
const maxListingEntries = 200

// discoveryKind distinguishes the two failure modes of
// discoverNewestOrchestrationDir.
type discoveryKind int

const (
	// discoveryNoNewFolder means the post-invoke ReadDir succeeded but no
	// Orchestration-* directory appeared that was not in the pre-existing set.
	discoveryNoNewFolder discoveryKind = iota

	// discoveryReadDirFailed means the post-invoke os.ReadDir call itself
	// failed (workspace removed, permission denied, etc.).
	discoveryReadDirFailed
)

// discoveryError is the error returned by discoverNewestOrchestrationDir on
// failure. Callers use Kind to select the correct debug-log message body.
type discoveryError struct {
	// Kind distinguishes no-new-folder from ReadDir infrastructure failure.
	Kind discoveryKind

	// Workspace is the path that was scanned (as supplied in WorkingDir).
	Workspace string

	// Listing contains entry names in the workspace directory, one per name
	// (not full paths). Present only when Kind == discoveryNoNewFolder.
	// Capped at maxListingEntries (200). Excess entries are NOT included in
	// the slice; the Truncated field carries the overflow count.
	Listing []string

	// Truncated is the number of workspace entries omitted from Listing due
	// to the maxListingEntries cap. Zero when all entries fit or when
	// Kind == discoveryReadDirFailed.
	Truncated int

	// Cause is the underlying error.
	Cause error
}

// Error implements the error interface. The message includes the workspace
// path and the cause. On discoveryNoNewFolder, it also includes the directory
// listing. When Truncated > 0, appends "[... and <Truncated> more entries]".
func (e *discoveryError) Error() string {
	if e.Kind == discoveryReadDirFailed {
		return fmt.Sprintf("reading workspace %q: %v", e.Workspace, e.Cause)
	}
	// discoveryNoNewFolder
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("no Orchestration-* directory found in workspace %q", e.Workspace))
	if len(e.Listing) > 0 {
		sb.WriteString("\nWorkspace contents:\n")
		for _, name := range e.Listing {
			sb.WriteString("  ")
			sb.WriteString(name)
			sb.WriteString("\n")
		}
		if e.Truncated > 0 {
			sb.WriteString(fmt.Sprintf("[... and %d more entries]", e.Truncated))
		}
	}
	return sb.String()
}

// Unwrap returns the Cause for errors.Is / errors.As compatibility.
func (e *discoveryError) Unwrap() error {
	return e.Cause
}

// errWithStderr appends a bounded excerpt of child stderr to an error message.
// Used by Invoke failure paths to satisfy FR-3 (error includes child stderr).
//
// When stderr is empty or nil, returns the base error unchanged.
// When stderr is non-empty, appends the last MaxStderrErrorBytes bytes
// (cut on a line boundary) with a clear delimiter.
//
// N in the header reflects the byte count of the excerpt after the
// trailing-newline strip, which is the actual content appended.
func errWithStderr(baseErr error, stderr []byte) error {
	if len(stderr) == 0 {
		return baseErr
	}

	var excerpt []byte

	if len(stderr) <= MaxStderrErrorBytes {
		// Step 2: under limit -- use all of stderr. Proceed to step 5.
		excerpt = stderr
		// Step 5: strip one trailing newline.
		if len(excerpt) > 0 && excerpt[len(excerpt)-1] == '\n' {
			excerpt = excerpt[:len(excerpt)-1]
		}
	} else {
		// Step 3a: take last MaxStderrErrorBytes bytes.
		tail := stderr[len(stderr)-MaxStderrErrorBytes:]

		// Step 3b: determine search range -- exclude trailing newline from search.
		searchEnd := len(tail)
		if tail[len(tail)-1] == '\n' {
			searchEnd = len(tail) - 1
		}

		// Step 3c: find first newline in search range.
		nlIdx := bytes.IndexByte(tail[:searchEnd], '\n')
		if nlIdx >= 0 {
			// Candidate is everything after the first newline.
			candidate := tail[nlIdx+1:]

			// Step 3d: strip one trailing newline from candidate.
			if len(candidate) > 0 && candidate[len(candidate)-1] == '\n' {
				candidate = candidate[:len(candidate)-1]
			}

			if len(candidate) > 0 {
				// Non-empty after strip -- use as excerpt, go to step 6 directly.
				n := len(candidate)
				return fmt.Errorf("%w\nChild stderr (last %d bytes):\n%s", baseErr, n, candidate)
			}
			// Empty after strip (step 3d fallback) -- fall through to step 3e.
		}

		// Step 3e: fallback -- use entire tail as-is.
		excerpt = tail

		// Step 5: strip one trailing newline.
		if len(excerpt) > 0 && excerpt[len(excerpt)-1] == '\n' {
			excerpt = excerpt[:len(excerpt)-1]
		}
	}

	// Step 6: N = len(excerpt) after strip.
	n := len(excerpt)
	return fmt.Errorf("%w\nChild stderr (last %d bytes):\n%s", baseErr, n, excerpt)
}

// discoverBinary returns the executable path. It calls discoverBinaryFn when
// set (test injection), otherwise falls back to discoverSelfBinary.
func (s *SubprocessRunInvoker) discoverBinary() (string, error) {
	if s.discoverBinaryFn != nil {
		return s.discoverBinaryFn()
	}
	return discoverSelfBinary()
}

// logger returns the configured DebugLogger, or NopDebugLogger{} when nil.
// All log call sites inside SubprocessRunInvoker use this accessor so callers
// never need to guard a Log call, regardless of construction path.
func (s *SubprocessRunInvoker) logger() domain.DebugLogger {
	if s.opts.DebugLogger != nil {
		return s.opts.DebugLogger
	}
	return domain.NopDebugLogger{}
}

// singleLine replaces embedded newlines in s with " | " so the result can
// be stored in a single-line log field value per the DebugField contract.
func singleLine(s string) string {
	return strings.ReplaceAll(s, "\n", " | ")
}

// Invoke executes a single mosaic-run run invocation as a subprocess.
// It always populates InvokeResult.ChildStderr and InvokeResult.ExitCode,
// regardless of whether an error is returned.
func (s *SubprocessRunInvoker) Invoke(ctx context.Context, inv RunInvocation) (InvokeResult, error) {
	log := s.logger()

	invoke := s.opts.Invoke
	if invoke == nil {
		invoke = execCommandRunnerRun
	}

	execPath := s.opts.ExecutablePath
	if execPath == "" {
		var err error
		execPath, err = s.discoverBinary()
		if err != nil {
			// Path 6: Binary discovery failure. No subprocess ran, ChildStderr empty.
			// Emit invoke.error only (no invoke.start -- binary path is unknown).
			log.Log(domain.EventTestrunInvokeError,
				err.Error(),
				domain.F("exit_code", "0"),
				domain.F("error", singleLine(err.Error())),
			)
			return InvokeResult{}, fmt.Errorf("testrun: cannot discover mosaic-run binary: %w", err)
		}
	}

	args := buildRunArgs(inv)

	runCtx := ctx
	if s.opts.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, s.opts.Timeout)
		defer cancel()
	}

	// Snapshot pre-existing Orchestration-* dirs before invoking so they can
	// be excluded from the post-invoke discovery scan.
	preExisting := snapshotOrchestrationDirs(s.opts.WorkingDir)

	// Binary is known; emit invoke.start before exec attempt.
	log.Log(domain.EventTestrunInvokeStart,
		"starting subprocess",
		domain.F("binary", execPath),
		domain.F("args", strings.Join(args, " ")),
		domain.F("workdir", s.opts.WorkingDir),
	)

	// Execute the subprocess, forwarding WorkingDir as the child process cwd.
	_, stderr, code, invokeErr := invoke(runCtx, s.opts.WorkingDir, execPath, args)
	if invokeErr != nil {
		// Path 3: Start failure. Subprocess did not start (no invoke.done).
		// Emit invoke.error with the OS error in the error field.
		msg := invokeErr.Error()
		if len(stderr) > 0 {
			msg = msg + "\n\n" + string(stderr)
		}
		log.Log(domain.EventTestrunInvokeError,
			msg,
			domain.F("exit_code", "0"),
			domain.F("error", singleLine(invokeErr.Error())),
		)
		baseErr := fmt.Errorf("testrun: subprocess invocation failed: %w", invokeErr)
		return InvokeResult{ExitCode: 0, ChildStderr: stderr}, errWithStderr(baseErr, stderr)
	}

	// Subprocess ran to completion; emit invoke.done.
	log.Log(domain.EventTestrunInvokeDone,
		"subprocess completed",
		domain.F("exit_code", strconv.Itoa(code)),
	)

	rf, rfErr := discoverNewestOrchestrationDir(s.opts.WorkingDir, preExisting)
	if rfErr != nil {
		// Paths 4/5: Discovery failure. Subprocess ran but no run folder found.
		// Emit invoke.error when stderr is non-empty or exit code is non-zero.
		if code != 0 || len(stderr) > 0 {
			log.Log(domain.EventTestrunInvokeError,
				string(stderr),
				domain.F("exit_code", strconv.Itoa(code)),
			)
		}
		// Emit discovery.fail with workspace and the appropriate message body.
		var de *discoveryError
		var discMsg string
		if errors.As(rfErr, &de) {
			switch de.Kind {
			case discoveryNoNewFolder:
				// Path 4: message is the workspace directory listing.
				discMsg = strings.Join(de.Listing, "\n")
				if de.Truncated > 0 {
					discMsg += fmt.Sprintf("\n[... and %d more entries]", de.Truncated)
				}
			case discoveryReadDirFailed:
				// Path 5: message is the ReadDir error text (listing unavailable).
				discMsg = de.Cause.Error()
			}
		}
		log.Log(domain.EventTestrunDiscoveryFail,
			discMsg,
			domain.F("workspace", s.opts.WorkingDir),
		)
		baseErr := fmt.Errorf("testrun: run folder discovery failed: %w", rfErr)
		return InvokeResult{ExitCode: code, ChildStderr: stderr}, errWithStderr(baseErr, stderr)
	}

	// Paths 1/2: Discovery succeeded.
	// Emit invoke.error when exit code is non-zero (Path 2). On Path 1 (exit 0),
	// stderr on a success path is benign and not logged.
	if code != 0 {
		log.Log(domain.EventTestrunInvokeError,
			string(stderr),
			domain.F("exit_code", strconv.Itoa(code)),
		)
	}

	// Extract the run_id from the folder name "Orchestration-{run_id}".
	runID := strings.TrimPrefix(filepath.Base(rf), "Orchestration-")
	dlPath := DispatchLogPath(s.opts.WorkingDir, runID)

	return InvokeResult{ExitCode: code, RunFolder: rf, DispatchLogPath: dlPath, ChildStderr: stderr}, nil
}

// buildRunArgs constructs the argument list for the "mosaic-run run" subcommand.
// The shape is:
//
//	run --workflow <WorkflowID> --mode <Mode> --harness <Harness>
//	    --new-run --input <FixturePath> --task <Task>
//	    --pre-consult=<true|false>
//	    [--infrastructure=<keys>]
//	    --checkpoints <disabled|enabled>
//	    --commits <disabled|enabled>
//	    [--ghcp-permission-mode <GHCPPermissionMode>]
func buildRunArgs(inv RunInvocation) []string {
	args := []string{
		"run",
		"--workflow", inv.WorkflowID,
		"--mode", inv.Mode,
		"--harness", inv.Harness,
		"--new-run",
		"--input", inv.FixturePath,
		"--task", inv.Task,
		"--pre-consult=" + strconv.FormatBool(inv.PreConsult),
	}
	if inv.ExecutablePath != "" {
		args = append(args, "--executable-path", inv.ExecutablePath)
	}

	// --infrastructure: nil=omit, empty=--infrastructure=, populated=--infrastructure=k1,k2
	if inv.InfrastructureKeys != nil {
		args = append(args, "--infrastructure="+strings.Join(inv.InfrastructureKeys, ","))
	}

	// --checkpoints: empty string defaults to "disabled"
	checkpoints := inv.Checkpoints
	if checkpoints == "" {
		checkpoints = "disabled"
	}
	args = append(args, "--checkpoints", checkpoints)

	// --commits: empty string defaults to "disabled"
	commits := inv.Commits
	if commits == "" {
		commits = "disabled"
	}
	args = append(args, "--commits", commits)

	if inv.GHCPPermissionMode != "" {
		args = append(args, "--ghcp-permission-mode", inv.GHCPPermissionMode)
	}

	// Always emit --dev-test-mode: buildRunArgs is only called by the test
	// framework's SubprocessRunInvoker, and every test subprocess must accept
	// --infrastructure. This boolean flag signals to mosaic-run run that the
	// invocation is from the test framework.
	args = append(args, "--dev-test-mode")

	return args
}

// snapshotOrchestrationDirs reads workspace and returns the set of existing
// Orchestration-* directory names (base names, not full paths).
//
// Returns a non-nil empty map on any error (including workspace not existing).
// Only directories whose name starts with "Orchestration-" are included.
func snapshotOrchestrationDirs(workspace string) map[string]struct{} {
	entries, err := os.ReadDir(workspace)
	if err != nil {
		return make(map[string]struct{})
	}
	result := make(map[string]struct{})
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "Orchestration-") {
			result[entry.Name()] = struct{}{}
		}
	}
	return result
}

// discoverNewestOrchestrationDir scans workspace for Orchestration-* directories
// that are NOT in the preExisting set, and returns the path of the one with the
// most recent modification time.
//
// Tie-breaking: when two new directories have equal mtimes, the one that
// appears first in ReadDir order (lexicographic by name) wins.
//
// On failure, returns a *discoveryError with Kind, Listing, and Cause set.
func discoverNewestOrchestrationDir(workspace string, preExisting map[string]struct{}) (string, error) {
	entries, err := os.ReadDir(workspace)
	if err != nil {
		return "", &discoveryError{
			Kind:      discoveryReadDirFailed,
			Workspace: workspace,
			Cause:     err,
		}
	}

	// Build a listing of all workspace entry names for the error case.
	listing := make([]string, 0, len(entries))
	for _, entry := range entries {
		listing = append(listing, entry.Name())
	}

	var newest string
	var newestTime time.Time

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !strings.HasPrefix(entry.Name(), "Orchestration-") {
			continue
		}
		// Skip pre-existing directories (recorded before the subprocess ran).
		if _, ok := preExisting[entry.Name()]; ok {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		// Strictly greater mtime replaces current best; equal mtime keeps first
		// in ReadDir order (lexicographic tie-break).
		if info.ModTime().After(newestTime) {
			newestTime = info.ModTime()
			newest = filepath.Join(workspace, entry.Name())
		}
	}

	if newest == "" {
		// Cap listing at maxListingEntries; overflow count goes in Truncated.
		truncated := 0
		if len(listing) > maxListingEntries {
			truncated = len(listing) - maxListingEntries
			listing = listing[:maxListingEntries]
		}
		return "", &discoveryError{
			Kind:      discoveryNoNewFolder,
			Workspace: workspace,
			Listing:   listing,
			Truncated: truncated,
			Cause:     fmt.Errorf("no Orchestration-* directory found in workspace %q", workspace),
		}
	}
	return newest, nil
}

// discoverSelfBinary returns the path of the currently running executable.
// Used by SubprocessRunInvoker to re-invoke mosaic-run as a subprocess.
func discoverSelfBinary() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("could not determine executable path: %w", err)
	}
	return self, nil
}

// execCommandRunnerRun is the real process-execution implementation of
// CommandRunner used by SubprocessRunInvoker. It captures both stdout and stderr.
func execCommandRunnerRun(ctx context.Context, workDir string, path string, args []string) (stdout, stderr []byte, exitCode int, err error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = workDir
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()
	if runErr == nil {
		return stdoutBuf.Bytes(), stderrBuf.Bytes(), 0, nil
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		// Non-zero exit is a normal outcome, not an error. The caller decides
		// whether the exit code is expected by comparing it to the sidecar.
		return stdoutBuf.Bytes(), stderrBuf.Bytes(), exitErr.ExitCode(), nil
	}

	// Binary could not be started (missing, permission denied, killed by signal).
	return nil, stderrBuf.Bytes(), 0, runErr
}
