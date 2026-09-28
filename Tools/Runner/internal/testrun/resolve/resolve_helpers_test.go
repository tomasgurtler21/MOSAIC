// Shared lookPath and logger test doubles for the harness binary resolution tests.
package resolve_test

import (
	"fmt"

	"mosaic-run/internal/domain"
)

// fixedLookPath returns a lookPath function that always returns the given path
// and error, regardless of the binary name.
func fixedLookPath(path string, err error) func(string) (string, error) {
	return func(_ string) (string, error) {
		return path, err
	}
}

// recordingLookPath is a lookPath that records every binary name it was called
// with and returns a fixed path for all calls.
type recordingLookPath struct {
	called []string
	result string
}

func (r *recordingLookPath) fn() func(string) (string, error) {
	return func(binary string) (string, error) {
		r.called = append(r.called, binary)
		return r.result, nil
	}
}

// perBinaryLookPath returns a lookPath that dispatches per binary name.
// Known names return the provided path; unknown names return an error.
func perBinaryLookPath(m map[string]string) func(string) (string, error) {
	return func(binary string) (string, error) {
		if p, ok := m[binary]; ok {
			return p, nil
		}
		return "", fmt.Errorf("binary %q not found", binary)
	}
}

type recordingDebugLogger struct {
	events  []string
	fields  [][]domain.DebugField
	messages []string
}

func (r *recordingDebugLogger) Log(event string, message string, fields ...domain.DebugField) {
	r.events = append(r.events, event)
	r.messages = append(r.messages, message)
	r.fields = append(r.fields, append([]domain.DebugField(nil), fields...))
}

// hasEventField returns true if the i-th log call has a field with the given
// key and value.
func (r *recordingDebugLogger) hasEventField(i int, key, value string) bool {
	if i >= len(r.fields) {
		return false
	}
	for _, f := range r.fields[i] {
		if f.Key == key && f.Value == value {
			return true
		}
	}
	return false
}
