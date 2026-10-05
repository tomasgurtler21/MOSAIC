// Shared test doubles and builders for the SubprocessRunInvoker whitebox tests.
package invoker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
)

// fakeCommandRunner is an injectable CommandRunner that records the arguments
// it was called with and returns configured outputs.
type fakeCommandRunner struct {
	capturedWorkDir string
	capturedPath    string
	capturedArgs    []string
	stdout          []byte
	stderr          []byte
	exitCode        int
	err             error
}

func (f *fakeCommandRunner) run(ctx context.Context, workDir string, path string, args []string) ([]byte, []byte, int, error) {
	f.capturedWorkDir = workDir
	f.capturedPath = path
	f.capturedArgs = args
	return f.stdout, f.stderr, f.exitCode, f.err
}

// newInvokerWithFakeRunner creates a SubprocessRunInvoker with the given
// fakeCommandRunner wired in, WorkingDir set, and a fixed ExecutablePath so
// the binary-discovery step is bypassed.
func newInvokerWithFakeRunner(workDir string, runner *fakeCommandRunner) *SubprocessRunInvoker {
	return &SubprocessRunInvoker{
		opts: RunInvokerOptions{
			ExecutablePath: "/fake/mosaic-run",
			WorkingDir:     workDir,
			Invoke:         runner.run,
		},
	}
}

// newTestOrchestrationDir creates an Orchestration-* subdirectory inside dir and
// returns its full path.
func newTestOrchestrationDir(t *testing.T, dir string, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.Mkdir(p, 0755); err != nil {
		t.Fatalf("newTestOrchestrationDir: %v", err)
	}
	return p
}

// fakeDebugLogger records Log calls so tests can assert on the events, messages,
// and fields emitted by SubprocessRunInvoker.Invoke.
type fakeDebugLogger struct {
	entries []fakeLogEntry
}

type fakeLogEntry struct {
	event   string
	message string
	fields  []domain.DebugField
}

// Log implements domain.DebugLogger. It records every call.
func (f *fakeDebugLogger) Log(event string, message string, fields ...domain.DebugField) {
	f.entries = append(f.entries, fakeLogEntry{
		event:   event,
		message: message,
		fields:  append([]domain.DebugField(nil), fields...),
	})
}

// hasEvent reports whether the fake received a Log call with the given event name.
func (f *fakeDebugLogger) hasEvent(event string) bool {
	for _, e := range f.entries {
		if e.event == event {
			return true
		}
	}
	return false
}

// fieldValue returns the value for key in the first entry matching event.
// Returns ("", false) when no matching entry or no matching field is found.
func (f *fakeDebugLogger) fieldValue(event string, key string) (string, bool) {
	for _, e := range f.entries {
		if e.event != event {
			continue
		}
		for _, fld := range e.fields {
			if fld.Key == key {
				return fld.Value, true
			}
		}
	}
	return "", false
}

// messageFor returns the message of the first entry matching event.
// Returns ("", false) when no matching entry is found.
func (f *fakeDebugLogger) messageFor(event string) (string, bool) {
	for _, e := range f.entries {
		if e.event == event {
			return e.message, true
		}
	}
	return "", false
}

// newInvokerWithLogger creates a SubprocessRunInvoker with the given
// DebugLogger and fakeCommandRunner wired in.
func newInvokerWithLogger(workDir string, runner *fakeCommandRunner, logger domain.DebugLogger) *SubprocessRunInvoker {
	return &SubprocessRunInvoker{
		opts: RunInvokerOptions{
			ExecutablePath: "/fake/mosaic-run",
			WorkingDir:     workDir,
			Invoke:         runner.run,
			DebugLogger:    logger,
		},
	}
}

// containsArg reports whether args contains the exact token s.
func containsArg(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}
