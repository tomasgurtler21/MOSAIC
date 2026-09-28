package cliexec_test

import (
	"errors"
	"testing"

	commonharness "mosaic-common/harness"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness/cliexec"
)

type recordedEntry struct {
	event   string
	message string
	fields  []domain.DebugField
}

type recordingLogger struct {
	entries []recordedEntry
}

func (r *recordingLogger) Log(event string, message string, fields ...domain.DebugField) {
	r.entries = append(r.entries, recordedEntry{event: event, message: message, fields: fields})
}

func TestNewSinkMapsEventNamesToDebugLogVocabulary(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"stdout", "spawn.stdout", domain.EventHarnessStdout},
		{"stderr", "spawn.stderr", domain.EventHarnessStderr},
		{"timeout", "spawn.timeout", domain.EventHarnessInvokeError},
		{"error", "spawn.error", domain.EventHarnessInvokeError},
		{"unknown passes through unchanged", "parse.recovered", "parse.recovered"},
		{"empty passes through unchanged", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &recordingLogger{}
			cliexec.NewSink(logger).Log(commonharness.Event{Name: tt.in, Message: "msg"})

			if len(logger.entries) != 1 {
				t.Fatalf("logged %d entries, want 1", len(logger.entries))
			}
			if got := logger.entries[0].event; got != tt.want {
				t.Errorf("event = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewSinkForwardsMessageAndFieldsInOrder(t *testing.T) {
	logger := &recordingLogger{}
	cliexec.NewSink(logger).Log(commonharness.Event{
		Name:    "spawn.stdout",
		Message: "line one\nline two",
		Fields:  []commonharness.Field{{Key: "pid", Value: "42"}, {Key: "path", Value: "/bin/x"}},
	})

	if len(logger.entries) != 1 {
		t.Fatalf("logged %d entries, want 1", len(logger.entries))
	}
	e := logger.entries[0]
	if e.message != "line one\nline two" {
		t.Errorf("message = %q, want it forwarded unchanged", e.message)
	}
	want := []domain.DebugField{{Key: "pid", Value: "42"}, {Key: "path", Value: "/bin/x"}}
	if len(e.fields) != len(want) {
		t.Fatalf("fields = %v, want %v", e.fields, want)
	}
	for i := range want {
		if e.fields[i] != want[i] {
			t.Errorf("field[%d] = %v, want %v", i, e.fields[i], want[i])
		}
	}
}

func TestNewSinkWithoutFieldsForwardsNoFields(t *testing.T) {
	logger := &recordingLogger{}
	cliexec.NewSink(logger).Log(commonharness.Event{Name: "spawn.stderr", Message: "boom"})

	if len(logger.entries) != 1 || len(logger.entries[0].fields) != 0 {
		t.Fatalf("entries = %+v, want one entry without fields", logger.entries)
	}
}

func TestSentinelsAliasSharedHarnessSentinels(t *testing.T) {
	pairs := []struct {
		name   string
		got    error
		shared error
	}{
		{"executable not found", cliexec.ErrExecutableNotFound, commonharness.ErrExecutableNotFound},
		{"non-zero exit", cliexec.ErrNonZeroExit, commonharness.ErrNonZeroExit},
		{"timeout", cliexec.ErrTimeout, commonharness.ErrTimeout},
		{"empty response", cliexec.ErrEmptyResponse, commonharness.ErrEmptyResponse},
		{"malformed json", cliexec.ErrMalformedJSON, commonharness.ErrMalformedJSON},
		{"malformed output", cliexec.ErrMalformedOutput, commonharness.ErrProtocolNotExtractable},
	}
	for _, p := range pairs {
		t.Run(p.name, func(t *testing.T) {
			if !errors.Is(p.got, p.shared) {
				t.Errorf("%s sentinel does not match the shared sentinel under errors.Is", p.name)
			}
		})
	}
}
