// Package debuglog provides the file-backed implementation of domain.DebugLogger.
//
// One Logger corresponds to one process/run and owns exactly one log file
// located at {workingDir}/RunnerLogs/{run_id}/{run_id}.log (or a
// startup-timestamped fallback at the RunnerLogs/ root when no run_id is
// known yet).
//
// The log file is created lazily on the first Log call so that SetRunID can
// influence the file location when identity is resolved after construction.
// Entries logged before identity is known are written immediately to the
// out-of-run fallback file and also retained in memory; the moment SetRunID
// resolves, those entries are replayed verbatim into the run folder and the
// out-of-run file is removed. If the replay cannot complete, the out-of-run
// file and its entries are left untouched and every later entry keeps
// appending there for the rest of the process. Every failure mode — folder
// creation, file open, write, replay — permanently disables the Logger (or,
// for a failed replay, simply leaves it on the out-of-run file) without
// surfacing an error to the caller and without panicking.
//
// Entries are written by opening the file in O_APPEND mode, writing, then
// closing immediately. This avoids holding an open file handle between calls,
// which is important for Windows compatibility (open handles block directory
// removal in t.TempDir cleanup). The mutex ensures entries are never
// interleaved; O_APPEND handles crash-safety at the OS level.
//
// Safe for concurrent use.
package debuglog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mosaic-run/internal/domain"
)

// LogsFolderName is the dedicated top-level folder that holds runner debug
// logs. It is resolved relative to the supplied working directory and is never
// placed inside an Orchestration-{run_id} folder, which is reserved for
// dispatch artifacts.
const LogsFolderName = "RunnerLogs"

// Logger is the file-backed domain.DebugLogger. One Logger corresponds to one
// process/run and owns exactly one log file.
//
// The log file is created lazily on the first Log call, not at construction.
// This allows SetRunID to influence the file name when identity is resolved
// after the logger is built (the common path), while still guaranteeing a log
// exists for failures that occur before identity resolution.
//
// Every failure mode — folder creation, file open, write — permanently
// disables the Logger for the remainder of the process. No error is returned
// and no panic occurs; subsequent calls behave as a no-op.
//
// Safe for concurrent use.
type Logger struct {
	workingDir string

	mu              sync.Mutex
	runID           string   // effective run ID (valid, non-empty); empty if not yet set
	runIDSet        bool     // true once a valid run ID has been accepted
	path            string   // absolute path of the log file; empty until first write succeeds
	opened          bool     // true once file initialisation has been attempted
	disabled        bool     // true after any I/O failure or after Close
	buffered        []string // fully-formatted pre-identity entries, for replay; discarded once a replay is attempted
	replayAttempted bool     // true once a replay has been attempted (successful or not); prevents a repeat attempt
}

// New creates a Logger rooted at workingDir. workingDir must be supplied by
// the caller; the package never calls os.Getwd(). No filesystem access occurs
// here.
//
// Never returns an error: an unusable workingDir surfaces as a silently
// disabled logger on first use.
func New(workingDir string) *Logger {
	return &Logger{
		workingDir: workingDir,
	}
}

// SetRunID associates the log with a run_id.
//
// Called before the first entry is written, the log file is created directly
// at RunnerLogs/{run_id}/{run_id}.log. Called afterwards (entries already
// exist under the out-of-run fallback name), SetRunID attempts a replay:
// every buffered entry is written into RunnerLogs/{run_id}/{run_id}.log, in
// original order with original formatting, and on success the out-of-run
// file is removed and Path begins reporting the new location. If the replay
// cannot complete, the out-of-run file and its entries are left exactly as
// they were, and every entry logged afterward continues to append there for
// the rest of the process — the replay is attempted at most once.
//
// A runID that fails domain.IsValidRunID (including the empty string) is
// ignored entirely: no file is created or moved, and the call has no
// observable effect. Safe to call more than once; only the first effective
// call has any effect.
func (l *Logger) SetRunID(runID string) {
	if !domain.IsValidRunID(runID) {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.runIDSet {
		// First effective call already consumed; subsequent calls are ignored.
		return
	}
	l.runIDSet = true
	l.runID = runID

	// If the file was already opened under a fallback name, attempt to
	// replay the buffered entries into the run folder.
	if l.opened && !l.disabled {
		l.replayLocked()
	}
}

// Log implements domain.DebugLogger. Each entry is written and flushed to the
// operating system before Log returns, so entries survive an os.Exit that
// bypasses Close.
func (l *Logger) Log(event string, message string, fields ...domain.DebugField) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.disabled {
		return
	}

	if !l.opened {
		l.initFileLocked()
	}

	if l.disabled {
		return
	}

	l.appendEntryLocked(event, message, fields...)
}

// Close marks the logger as permanently closed. No further entries will be
// written. Safe to call on a disabled or never-used Logger, and safe to call
// more than once. Never returns an error.
//
// Because entries are written by opening the file for each write and closing
// immediately, there is no persistent file handle to release here.
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Mark as disabled to prevent further writes. path is preserved so callers
	// can locate the log file after Close (e.g. for display or post-run reading).
	l.opened = true
	l.disabled = true
}

// Path returns the absolute path of the log file, or "" if no file has been
// created yet or the logger is disabled. Provided for tests and for surfacing
// the log location to the user.
//
// The returned value can change across the Logger's lifetime: before a
// successful replay it reports the out-of-run fallback file, and after a
// successful replay it reports RunnerLogs/{run_id}/{run_id}.log.
func (l *Logger) Path() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.path
}

// initFileLocked resolves the log file path and creates the necessary
// directories. Must be called with l.mu held. On any error, sets l.disabled =
// true and returns. The file itself is not kept open; initFileLocked only
// creates the directory and records the target path.
func (l *Logger) initFileLocked() {
	l.opened = true

	if l.runIDSet && l.runID != "" {
		runDir := filepath.Join(l.workingDir, LogsFolderName, l.runID)
		if err := os.MkdirAll(runDir, 0755); err != nil {
			l.disabled = true
			return
		}
		l.path = filepath.Join(runDir, l.runID+".log")
		return
	}

	logDir := filepath.Join(l.workingDir, LogsFolderName)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		l.disabled = true
		return
	}

	// Fallback: startup-{timestamp} — cannot match the run_id pattern
	// ^\d{8}T\d{6}Z-[0-9a-f]{4}$ because it begins with "startup-".
	ts := time.Now().UTC().Format("20060102T150405.000000000Z")
	l.path = filepath.Join(logDir, "startup-"+ts+".log")
}

// replayLocked attempts to move every buffered pre-identity entry into the
// run folder, verbatim and in original order. Must be called with l.mu held,
// only once per Logger (guarded by the runIDSet latch in SetRunID), and only
// when a fallback file already exists (l.opened && !l.disabled).
//
// On success, the out-of-run file is removed and l.path is updated to the
// run-folder file. On any failure, l.path and the out-of-run file are left
// untouched, no partially-written file is left at the run-folder path, and
// every entry logged afterward continues to append to the out-of-run file
// via the normal appendEntryLocked path.
func (l *Logger) replayLocked() {
	l.replayAttempted = true
	buffered := l.buffered
	l.buffered = nil

	runDir := filepath.Join(l.workingDir, LogsFolderName, l.runID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		return
	}

	runPath := filepath.Join(runDir, l.runID+".log")
	f, err := os.OpenFile(runPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}

	var writeErr error
	for _, entry := range buffered {
		if _, writeErr = f.WriteString(entry); writeErr != nil {
			break
		}
	}
	if writeErr == nil {
		writeErr = f.Sync()
	}
	_ = f.Close()

	if writeErr != nil {
		_ = os.Remove(runPath)
		return
	}

	oldPath := l.path
	l.path = runPath
	_ = os.Remove(oldPath)
}

// appendEntryLocked formats one log entry, opens the file in O_APPEND mode,
// writes the entry, then closes the file. Must be called with l.mu held.
// On write failure, permanently disables the logger.
func (l *Logger) appendEntryLocked(event string, message string, fields ...domain.DebugField) {
	ts := time.Now().UTC().Format(time.RFC3339)

	// Build field fragment: key=value pairs where values containing spaces or
	// pipe characters are rendered with %q so the wire format remains
	// unambiguous.
	var fieldParts []string
	for _, f := range fields {
		val := f.Value
		if strings.ContainsAny(val, " |") {
			val = fmt.Sprintf("%q", val)
		}
		fieldParts = append(fieldParts, f.Key+"="+val)
	}

	isMultiLine := strings.Contains(message, "\n")

	var entry string
	if isMultiLine {
		// Header line followed by a delimited block that contains the payload
		// verbatim. The delimiter makes multi-line payloads unambiguously
		// attributable to their entry.
		header := "[" + ts + "] " + event
		if len(fieldParts) > 0 {
			header += " " + strings.Join(fieldParts, " ")
		}
		payload := message
		if !strings.HasSuffix(payload, "\n") {
			payload += "\n"
		}
		entry = header + "\n" +
			"--- begin " + event + " ---\n" +
			payload +
			"--- end " + event + " ---\n"
	} else {
		// Single-line format: [TIMESTAMP] EVENT field=val ... | message
		header := "[" + ts + "] " + event
		if len(fieldParts) > 0 {
			header += " " + strings.Join(fieldParts, " ")
		}
		entry = header + " | " + message + "\n"
	}

	if !l.runIDSet {
		// Identity is not yet known: this entry is landing in the out-of-run
		// fallback file, so retain its rendered form for a future replay.
		l.buffered = append(l.buffered, entry)
	}

	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		l.path = ""
		l.disabled = true
		return
	}

	_, writeErr := f.WriteString(entry)
	// Sync before close to flush the entry to the OS so it survives os.Exit.
	_ = f.Sync()
	_ = f.Close()

	if writeErr != nil {
		l.path = ""
		l.disabled = true
	}
}
