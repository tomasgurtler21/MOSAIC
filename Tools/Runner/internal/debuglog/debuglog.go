// Package debuglog provides the file-backed implementation of domain.DebugLogger.
//
// One Logger corresponds to one process/run and owns exactly one log file
// located at {workingDir}/RunnerLogs/{run_id}.log (or a startup-timestamped
// fallback when no run_id is known at first write).
//
// The log file is created lazily on the first Log call so that SetRunID can
// influence the file name when identity is resolved after construction. Every
// failure mode — folder creation, file open, write — permanently disables the
// Logger without surfacing an error to the caller and without panicking.
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

	mu       sync.Mutex
	runID    string // effective run ID (valid, non-empty); empty if not yet set
	runIDSet bool   // true once a valid run ID has been accepted
	path     string // absolute path of the log file; empty until first write succeeds
	opened   bool   // true once file initialisation has been attempted
	disabled bool   // true after any I/O failure or after Close
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
// Called before the first entry is written, the log file is named
// "{run_id}.log". Called afterwards (the file already exists under its
// fallback name), the name is left alone and a correlation entry is recorded
// instead, so the file can still be tied to its run.
//
// A runID that fails domain.IsValidRunID (including the empty string) is
// ignored entirely: the file is not named, no correlation entry is written,
// and the call has no observable effect. Safe to call more than once; only the
// first effective call names the file.
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

	// If the file was already opened under a fallback name, append a
	// correlation entry so the file can be tied to this run_id.
	if l.opened && !l.disabled {
		l.appendEntryLocked(domain.EventRunnerRunID, runID)
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
func (l *Logger) Path() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.path
}

// initFileLocked resolves the log file path and creates the RunnerLogs/
// directory. Must be called with l.mu held. On any error, sets l.disabled =
// true and returns. The file itself is not kept open; initFileLocked only
// creates the directory and records the target path.
func (l *Logger) initFileLocked() {
	l.opened = true

	logDir := filepath.Join(l.workingDir, LogsFolderName)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		l.disabled = true
		return
	}

	var filename string
	if l.runIDSet && l.runID != "" {
		filename = l.runID + ".log"
	} else {
		// Fallback: startup-{timestamp} — cannot match the run_id pattern
		// ^\d{8}T\d{6}Z-[0-9a-f]{4}$ because it begins with "startup-".
		ts := time.Now().UTC().Format("20060102T150405.000000000Z")
		filename = "startup-" + ts + ".log"
	}

	l.path = filepath.Join(logDir, filename)
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
