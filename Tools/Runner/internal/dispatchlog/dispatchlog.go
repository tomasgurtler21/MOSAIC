// Package dispatchlog provides the file-backed implementation of domain.DispatchLogger.
//
// One Logger corresponds to one process/run and owns exactly one log file
// located at {workingDir}/RunnerLogs/{run_id}/{run_id}-dispatch.log (or a
// RunnerLogs/startup-{timestamp}-dispatch.log fallback when no run_id is
// known at first write).
//
// Each LogRequest, LogResponse, and LogError call serialises its envelope
// struct to a single JSON line (JSONL format) and appends it to the file.
// The logger uses the same architectural patterns as debuglog.Logger: New
// constructor, lazy file init on first write, SetRunID for filename
// resolution, mutex-guarded append-mode writes, permanent silent disable on
// any I/O failure.
//
// Safe for concurrent use.
package dispatchlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"mosaic-run/internal/domain"
)

// LogsFolderName is the folder that holds dispatch logs, resolved relative
// to the supplied working directory.
const LogsFolderName = "RunnerLogs"

// Logger is the file-backed domain.DispatchLogger. One Logger corresponds to
// one process/run and owns exactly one log file.
//
// The log file is created lazily on the first LogRequest/LogResponse/LogError
// call. SetRunID influences the file name when called before the first write.
// Every I/O failure permanently and silently disables the logger.
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

// requestEntry is the JSONL envelope for a dispatch request.
type requestEntry struct {
	Type      string                `json:"type"`      // always "request"
	Timestamp string                `json:"timestamp"` // RFC 3339 UTC
	Request   domain.ProtocolRequest `json:"request"`
}

// responseEntry is the JSONL envelope for a dispatch response.
type responseEntry struct {
	Type            string                  `json:"type"`              // always "response"
	Timestamp       string                  `json:"timestamp"`         // RFC 3339 UTC
	AgentInstanceID string                  `json:"agent_instance_id"` // ties to the preceding request
	Response        domain.ProtocolResponse `json:"response"`
}

// errorEntry is the JSONL envelope for a harness-level invocation error.
type errorEntry struct {
	Type            string `json:"type"`              // always "error"
	Timestamp       string `json:"timestamp"`         // RFC 3339 UTC
	AgentInstanceID string `json:"agent_instance_id"` // ties to the preceding request
	Error           string `json:"error"`             // err.Error() text
}

// correlationEntry records a late-bound run_id association.
type correlationEntry struct {
	Type      string `json:"type"`      // always "correlation"
	Timestamp string `json:"timestamp"` // RFC 3339 UTC
	RunID     string `json:"run_id"`
}

// New creates a Logger rooted at workingDir. No filesystem access occurs here.
//
// Never returns an error: an unusable workingDir surfaces as a silently
// disabled logger on first use.
func New(workingDir string) *Logger {
	return &Logger{workingDir: workingDir}
}

// SetRunID associates the log with a run_id.
//
// Called before the first entry is written, the log file is named
// "{run_id}-dispatch.log" inside a run subfolder. Called afterwards (the file
// already exists under its fallback name), the name is left alone and a
// correlation entry is recorded instead, so the file can still be tied to
// its run.
//
// A runID that fails domain.IsValidRunID (including the empty string) is
// ignored entirely. Safe to call more than once; only the first effective
// call names the file.
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
		entry := correlationEntry{
			Type:      "correlation",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			RunID:     runID,
		}
		l.appendJSONLocked(entry)
	}
}

// LogRequest implements domain.DispatchLogger. Records the full protocol
// request dispatched to a subagent as a single JSONL line.
func (l *Logger) LogRequest(req domain.ProtocolRequest) {
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

	entry := requestEntry{
		Type:      "request",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Request:   req,
	}
	l.appendJSONLocked(entry)
}

// LogResponse implements domain.DispatchLogger. Records the full protocol
// response returned by a subagent as a single JSONL line.
func (l *Logger) LogResponse(resp domain.ProtocolResponse) {
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

	entry := responseEntry{
		Type:            "response",
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		AgentInstanceID: resp.AgentInstanceID,
		Response:        resp,
	}
	l.appendJSONLocked(entry)
}

// LogError implements domain.DispatchLogger. Records a harness-level error
// for an invocation that never produced a ProtocolResponse.
func (l *Logger) LogError(agentInstanceID string, errText string) {
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

	entry := errorEntry{
		Type:            "error",
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		AgentInstanceID: agentInstanceID,
		Error:           errText,
	}
	l.appendJSONLocked(entry)
}

// Close permanently disables the logger. No further entries will be written.
// Safe to call on a disabled or never-used Logger, and safe to call more
// than once. Never returns an error.
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.opened = true
	l.disabled = true
}

// Path returns the absolute path of the log file, or "" if no file has been
// created yet or the logger is disabled due to an I/O error.
//
// Once a file has been created, Path continues to return that path even after
// Close — Close disables future writes but does not clear the path.
func (l *Logger) Path() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.path
}

// initFileLocked resolves the log file path and creates the RunnerLogs/
// directory (and, when a run_id is known, its run subfolder). Must be called
// with l.mu held. On any error, sets l.disabled = true and returns. The file
// itself is not kept open; initFileLocked only creates the directory and
// records the target path.
func (l *Logger) initFileLocked() {
	l.opened = true

	var logDir, filename string
	if l.runIDSet && l.runID != "" {
		logDir = filepath.Join(l.workingDir, LogsFolderName, l.runID)
		filename = l.runID + "-dispatch.log"
	} else {
		logDir = filepath.Join(l.workingDir, LogsFolderName)
		// Fallback: startup-{timestamp} — cannot match the run_id pattern
		// ^\d{8}T\d{6}Z-[0-9a-f]{4}$ because it begins with "startup-".
		ts := time.Now().UTC().Format("20060102T150405.000000000Z")
		filename = "startup-" + ts + "-dispatch.log"
	}

	if err := os.MkdirAll(logDir, 0755); err != nil {
		l.disabled = true
		return
	}

	l.path = filepath.Join(logDir, filename)
}

// appendJSONLocked marshals v as compact JSON, appends it as a single line to
// the log file, and closes the file handle immediately. Must be called with
// l.mu held. On any failure, permanently disables the logger.
func (l *Logger) appendJSONLocked(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		// json.Marshal should never fail for these struct types, but handle it
		// defensively by permanently disabling the logger.
		l.path = ""
		l.disabled = true
		return
	}

	// Append newline to complete the JSONL line.
	line := append(data, '\n')

	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		l.path = ""
		l.disabled = true
		return
	}

	_, writeErr := f.Write(line)
	_ = f.Sync()
	_ = f.Close()

	if writeErr != nil {
		l.path = ""
		l.disabled = true
	}
}
