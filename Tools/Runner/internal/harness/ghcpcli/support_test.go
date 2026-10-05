package ghcpcli_test

// Fixtures shared with helperprocess_test.go and the adapter tests: a
// definition-file writer and a recording debug logger.

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"mosaic-run/internal/domain"
)

// writeDefFile writes a Claude Code agent definition file to a fresh temp dir
// and returns the absolute path. The file is removed automatically when the
// test ends via t.TempDir's cleanup.
func writeDefFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeDefFile: %v", err)
	}
	return path
}

// harnessLogEntry is one captured call to recordingLogger.Log.
type harnessLogEntry struct {
	Event   string
	Message string
	Fields  []domain.DebugField
}

// recordingLogger is a thread-safe domain.DebugLogger that records every Log
// call. Tests assert on the recorded entries after invoking the adapter.
type recordingLogger struct {
	mu      sync.Mutex
	entries []harnessLogEntry
}

// Log implements domain.DebugLogger.
func (r *recordingLogger) Log(event string, message string, fields ...domain.DebugField) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, harnessLogEntry{
		Event:   event,
		Message: message,
		Fields:  append([]domain.DebugField{}, fields...),
	})
}

// eventLogged reports whether at least one entry with the given event name
// was recorded.
func (r *recordingLogger) eventLogged(event string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.Event == event {
			return true
		}
	}
	return false
}

// fieldValue returns the value for the given field key in the first entry
// with the given event name. Returns ("", false) when not found.
func (r *recordingLogger) fieldValue(event, key string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.Event != event {
			continue
		}
		for _, f := range e.Fields {
			if f.Key == key {
				return f.Value, true
			}
		}
	}
	return "", false
}

// allEvents returns the event names of all recorded entries in order.
func (r *recordingLogger) allEvents() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, len(r.entries))
	for i, e := range r.entries {
		names[i] = e.Event
	}
	return names
}

// messageFor returns the message of the first recorded entry with the given
// event name. Returns ("", false) when no such entry was recorded.
func (r *recordingLogger) messageFor(event string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.Event == event {
			return e.Message, true
		}
	}
	return "", false
}

// countEvent returns how many entries were recorded for the given event name.
func (r *recordingLogger) countEvent(event string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.entries {
		if e.Event == event {
			n++
		}
	}
	return n
}

// validClaudeCodeDef is a realistic deployed Claude Code agent definition with
// a comma-separated tools scalar, matching the format ExtractClaudeCodeTools
// expects. Tool names are the realistic set used by production agent files.
const validClaudeCodeDef = "---\nname: test-agent\nmodel: claude-sonnet-4-6\ntools: Read, Write, Edit, Bash\n---\n\nAgent body.\n"
