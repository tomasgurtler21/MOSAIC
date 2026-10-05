// Package mosaiclog writes the MOSAIC log events the Runner owns itself:
// run_start / run_end for a Runner session, and the OpenCode invocation_end
// fallback. Every method swallows failures; logging never affects a run.
package mosaiclog

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	commonharness "mosaic-common/harness"

	"mosaic-run/internal/domain"
)

// SchemaVersion is the envelope schema_version the Runner writes.
const SchemaVersion = "1.1.0"

const (
	logsDirName          = "OrchestrationLogs"
	orchestratorFileName = "00_orchestrator_events.jsonl"
	invocationFileName   = "03_events.jsonl"
	timestampLayout      = "2006-01-02T15:04:05.000Z"
	eventInvocationEnd   = "invocation_end"
	debugWriteFailed     = "mosaiclog.write.failed"
)

// runIDPattern is the canonical run id shape; anything else (including
// traversal attempts and the "unknown-run" placeholder) is never written.
var runIDPattern = regexp.MustCompile(`^\d{8}T\d{6}Z-[0-9a-f]{4}$`)

// harnessIDs is the harness vocabulary of the envelope.
var harnessIDs = map[string]bool{
	commonharness.HarnessIDClaudeCode: true,
	commonharness.HarnessIDOpenCode:   true,
	commonharness.HarnessIDGHCPCLI:    true,
}

// Writer appends Runner-owned events under {workspaceRoot}/OrchestrationLogs/.
type Writer struct {
	root    string
	harness string
	clock   domain.Clock
	debug   domain.DebugLogger
}

// Option configures a Writer.
type Option func(*Writer)

// WithClock sets the timestamp source (default: UTC wall clock).
func WithClock(c domain.Clock) Option {
	return func(w *Writer) {
		if c != nil {
			w.clock = c
		}
	}
}

// WithDebugLogger sets where swallowed write failures are recorded.
func WithDebugLogger(l domain.DebugLogger) Option {
	return func(w *Writer) {
		if l != nil {
			w.debug = l
		}
	}
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now().UTC() }

// NewWriter binds a writer to a workspace root and a harness id. A harness id
// outside claude-code / opencode / ghcp-cli makes every write a no-op.
func NewWriter(workspaceRoot, harness string, opts ...Option) *Writer {
	w := &Writer{root: workspaceRoot, harness: harness, clock: wallClock{}, debug: domain.NopDebugLogger{}}
	for _, o := range opts {
		o(w)
	}
	return w
}

// event is one JSON line: the common envelope plus the field blocks of
// run_start, run_end and invocation_end. Optional fields are omitted when empty.
type event struct {
	SchemaVersion   string `json:"schema_version"`
	Event           string `json:"event"`
	Timestamp       string `json:"timestamp"`
	Harness         string `json:"harness"`
	RunID           string `json:"run_id"`
	Cwd             string `json:"cwd,omitempty"`
	Outcome         string `json:"outcome,omitempty"`
	AgentInstanceID string `json:"agent_instance_id,omitempty"`
	StatusCode      string `json:"status_code,omitempty"`
	Response        string `json:"response,omitempty"`
}

// RunStart appends one run_start to {runID}/00_orchestrator_events.jsonl.
func (w *Writer) RunStart(runID, cwd string) {
	w.appendRunEvent(runID, event{Event: "run_start", Cwd: cwd})
}

// RunEnd appends one run_end with outcome to the same file.
func (w *Writer) RunEnd(runID string, outcome Outcome) {
	w.appendRunEvent(runID, event{Event: "run_end", Outcome: string(outcome)})
}

func (w *Writer) appendRunEvent(runID string, ev event) {
	if !w.usable(runID) {
		return
	}
	path := filepath.Join(w.runDir(runID), orchestratorFileName)
	w.append(path, runID, ev)
}

// InvocationEnd carries the fields native OpenCode invocation_end carries.
type InvocationEnd struct {
	AgentInstanceID string // required
	StatusCode      string // optional; omitted when ""
	Response        string // optional; omitted when ""
}

// EnsureResult reports what EnsureInvocationEnd did.
type EnsureResult string

const (
	EnsureAppended       EnsureResult = "appended"
	EnsureAlreadyPresent EnsureResult = "already-present"
	EnsureNoFolder       EnsureResult = "no-folder"
	EnsureFailed         EnsureResult = "failed"
)

// EnsureInvocationEnd appends one invocation_end to
// {runID}/{folder}/03_events.jsonl only when the folder exists and the file
// holds no invocation_end event. It never creates the invocation folder.
func (w *Writer) EnsureInvocationEnd(runID string, end InvocationEnd) EnsureResult {
	if !w.usable(runID) || end.AgentInstanceID == "" {
		return EnsureNoFolder
	}
	dir := filepath.Join(w.runDir(runID), sanitizeComponent(end.AgentInstanceID))
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return EnsureNoFolder
	}
	if err != nil || !info.IsDir() {
		w.fail(dir, err)
		return EnsureFailed
	}
	path := filepath.Join(dir, invocationFileName)
	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		w.fail(path, err)
		return EnsureFailed
	}
	if hasInvocationEnd(content) {
		return EnsureAlreadyPresent
	}
	ev := event{
		Event:           eventInvocationEnd,
		AgentInstanceID: end.AgentInstanceID,
		StatusCode:      end.StatusCode,
		Response:        end.Response,
	}
	if !w.append(path, runID, ev) {
		return EnsureFailed
	}
	return EnsureAppended
}

// hasInvocationEnd reports whether any line of a JSON Lines file is an
// invocation_end event. Lines that merely mention the name do not count.
func hasInvocationEnd(content []byte) bool {
	for _, line := range bytes.Split(content, []byte("\n")) {
		if !bytes.Contains(line, []byte(eventInvocationEnd)) {
			continue
		}
		var probe struct {
			Event string `json:"event"`
		}
		if json.Unmarshal(line, &probe) == nil && probe.Event == eventInvocationEnd {
			return true
		}
	}
	return false
}

// usable reports whether the writer may write for runID.
func (w *Writer) usable(runID string) bool {
	return harnessIDs[w.harness] && runIDPattern.MatchString(runID)
}

func (w *Writer) runDir(runID string) string {
	return filepath.Join(w.root, logsDirName, runID)
}

// append stamps the envelope on ev and appends it as one line, creating
// missing directories. It reports success; failures are only debug-logged.
func (w *Writer) append(path, runID string, ev event) bool {
	ev.SchemaVersion = SchemaVersion
	ev.Timestamp = w.clock.Now().UTC().Format(timestampLayout)
	ev.Harness = w.harness
	ev.RunID = runID

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(ev); err != nil {
		w.fail(path, err)
		return false
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		w.fail(path, err)
		return false
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		w.fail(path, err)
		return false
	}
	_, err = f.Write(buf.Bytes())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		w.fail(path, err)
		return false
	}
	return true
}

func (w *Writer) fail(path string, err error) {
	msg := "log write skipped"
	if err != nil {
		msg = err.Error()
	}
	w.debug.Log(debugWriteFailed, msg, domain.F("path", path))
}

// sanitizeComponent mirrors the mosaic-logger hook's folder naming so the
// Runner finds the folder the hook created: each of <>:"/\|?* and every
// control character becomes "_", trailing dots and spaces are stripped, and an
// empty result becomes "_".
func sanitizeComponent(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || strings.ContainsRune(`<>:"/\|?*`, r) {
			b.WriteByte('_')
		} else {
			b.WriteRune(r)
		}
	}
	out := strings.TrimRight(b.String(), ". ")
	if out == "" {
		return "_"
	}
	return out
}
