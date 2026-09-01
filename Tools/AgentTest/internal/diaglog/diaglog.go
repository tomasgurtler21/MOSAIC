// Package diaglog is the file-backed diagnostic sink for one run's subject
// process output.
//
// The shared spawner logs the subject's whole captured stdout and stderr as
// two diagnostic events when the spawn finishes — tens of kilobytes per run,
// including raw task prompts and provider usage records. That output is the
// primary artifact for diagnosing an unexpectedly-behaving run, so it is
// retained; what changes is where. One Sink corresponds to one run.
//
// The two most important properties:
//   - An open Sink does not write to the process's own stdout or stderr.
//     Those streams belong to the TUI or to the machine-readable report,
//     and this package has no mechanism to reach them.
//   - The caller MUST Close the Sink before the run's sandbox is torn down.
//     Windows blocks directory removal while a file handle is open, which
//     blocks sandbox teardown. Close releases that handle.
package diaglog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	commonharness "mosaic-common/harness"
)

// Sink implements mosaic-common/harness.Sink by writing each event to one
// run's own file.
//
// Each Log call opens the file in append mode, writes the entry, and closes
// the file immediately. This avoids holding an open file handle between calls,
// which matters on Windows where an open handle blocks directory removal during
// sandbox teardown. Close collects the first write error encountered and
// returns it, so a caller can surface write-failure degradation without that
// degradation failing the run itself.
type Sink struct {
	mu       sync.Mutex
	path     string
	firstErr error
}

// Ensure Sink satisfies the Sink interface at compile time.
var _ commonharness.Sink = (*Sink)(nil)

// Open creates or truncates path and returns a Sink writing to it, attributed
// to runID. The parent directory is created on demand.
//
// The caller MUST Close the returned Sink before that run's sandbox is torn
// down: an open handle blocks directory deletion on Windows, which is the
// working platform.
func Open(path, runID string) (*Sink, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("diaglog.Open: creating parent directory: %w", err)
	}

	// Create or truncate the file and write the attribution header, then close
	// immediately so no handle is held between this call and the first Log.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("diaglog.Open: %w", err)
	}
	_, werr := fmt.Fprintf(f, "# run: %s\n", runID)
	closeErr := f.Close()
	if werr != nil {
		return nil, fmt.Errorf("diaglog.Open: writing header: %w", werr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("diaglog.Open: closing after header: %w", closeErr)
	}

	return &Sink{path: path}, nil
}

// Log implements mosaic-common/harness.Sink. Per that contract it never
// returns an error, never panics and never blocks: a write failure after Open
// succeeded is recorded and reported by Close, because losing a diagnostic
// line must not fail a run whose subject behaved correctly.
//
// Each call opens the file in append mode, writes the event, and closes
// immediately so no file handle is held between calls.
func (s *Sink) Log(ev commonharness.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		if s.firstErr == nil {
			s.firstErr = err
		}
		return
	}

	_, werr := fmt.Fprintf(f, "%s: %s\n", ev.Name, ev.Message)
	_ = f.Close()

	if werr != nil && s.firstErr == nil {
		s.firstErr = werr
	}
}

// Close returns the first write error encountered during logging, if any, so a
// caller that cares can surface the degradation; a caller that does not may
// ignore it. Because each Log call opens and closes the file independently,
// there is no persistent handle to release here; Close is solely the error-
// reporting rendezvous point.
func (s *Sink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.firstErr
}

// Discard is the named no-op sink: it drops every event and is what a run
// with no diagnostic destination is given. It exists so "no destination" is a
// named value rather than a nil check repeated at every call site.
var Discard commonharness.Sink = discardSink{}

type discardSink struct{}

func (discardSink) Log(_ commonharness.Event) {}
