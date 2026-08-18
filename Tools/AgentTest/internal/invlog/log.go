// Package invlog implements the append-only JSONL invocation log written by
// many short-lived interceptor processes.
package invlog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mosaic-agent-test/internal/domain"
)

// Log is the append-only invocation log. One JSON record per line.
type Log interface {
	// Append writes records durably. Concurrent appends from separate
	// processes must not interleave or truncate records.
	Append(records ...domain.LogRecord) error

	// Read parses the log. A trailing partial record left by a crash
	// mid-append is tolerated and reported, never an error.
	Read() ([]domain.LogRecord, ReadReport, error)
}

// ReadReport describes tolerated anomalies found while reading the log.
type ReadReport struct {
	TrailingPartialRecord bool
	MalformedLines        []int // 1-based line numbers skipped
}

// log is the default Log implementation.
type log struct {
	path string
}

// NewLog constructs a Log backed by the JSONL file at path.
func NewLog(path string) Log {
	return &log{path: path}
}

// Append writes every record as one JSON line and issues a single Write
// call for the whole batch. A single write to a file opened for append is
// atomic with respect to other writers on the same path — the mechanism
// that lets many independent, short-lived interceptor processes append
// concurrently without ever interleaving or truncating each other's
// records.
func (l *log) Append(records ...domain.LogRecord) error {
	if len(records) == 0 {
		return nil
	}

	var buf bytes.Buffer
	for _, r := range records {
		data, err := json.Marshal(r)
		if err != nil {
			return fmt.Errorf("invlog: failed to marshal record: %w", err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}

	if dir := filepath.Dir(l.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("invlog: failed to create %s: %w", dir, err)
		}
	}

	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("invlog: failed to open %s: %w", l.path, err)
	}
	defer f.Close()

	if _, err := f.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("invlog: failed to append to %s: %w", l.path, err)
	}
	return nil
}

// Read parses every line as a JSON record. A line that fails to parse is
// tolerated rather than treated as an error: if it is the last line, it is
// reported as a trailing partial record (the shape a crash mid-append
// leaves behind); otherwise it is skipped and its 1-based line number is
// reported as malformed.
func (l *log) Read() ([]domain.LogRecord, ReadReport, error) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ReadReport{}, nil
		}
		return nil, ReadReport{}, fmt.Errorf("invlog: failed to read %s: %w", l.path, err)
	}

	lines := strings.Split(string(data), "\n")
	lastIndex := len(lines) - 1

	var records []domain.LogRecord
	var report ReadReport
	for i, line := range lines {
		if line == "" {
			continue
		}
		var rec domain.LogRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			if i == lastIndex {
				report.TrailingPartialRecord = true
			} else {
				report.MalformedLines = append(report.MalformedLines, i+1)
			}
			continue
		}
		records = append(records, rec)
	}
	return records, report, nil
}

// Filter selects the records belonging to one attempt of one test.
// Pure; exported so evaluation can slice a merged log without re-reading it.
func Filter(records []domain.LogRecord, testID string, runNumber int) []domain.LogRecord {
	var out []domain.LogRecord
	for _, r := range records {
		if r.TestID == testID && r.RunNumber == runNumber {
			out = append(out, r)
		}
	}
	return out
}
