// Package cliexec holds the pieces every CLI harness adapter shares: the
// error sentinels aliased onto mosaic-common/harness and the debug-log sink
// that forwards the shared package's diagnostic events to Runner's debug log.
package cliexec

import (
	commonharness "mosaic-common/harness"

	"mosaic-run/internal/domain"
)

// Sentinel errors shared by every CLI harness adapter, aliased onto the
// shared package's sentinels so errors.Is at existing call sites keeps
// working unchanged.
var (
	// ErrExecutableNotFound is returned when the Claude Code CLI binary cannot
	// be found or executed.
	ErrExecutableNotFound = commonharness.ErrExecutableNotFound

	// ErrNonZeroExit is returned when the CLI process exits with a non-zero
	// status. The wrapped error includes the exit code and captured stderr.
	ErrNonZeroExit = commonharness.ErrNonZeroExit

	// ErrTimeout is returned when the invocation exceeds the configured timeout.
	// The subprocess is killed before this error is returned.
	ErrTimeout = commonharness.ErrTimeout

	// ErrEmptyResponse is returned when the CLI produces no stdout output.
	ErrEmptyResponse = commonharness.ErrEmptyResponse

	// ErrMalformedJSON is returned when the CLI output is not valid JSON at all.
	ErrMalformedJSON = commonharness.ErrMalformedJSON

	// ErrMalformedOutput is returned when the CLI output is valid JSON but the
	// Communication Protocol response cannot be located or parsed within it.
	ErrMalformedOutput = commonharness.ErrProtocolNotExtractable
)

// NewSink returns a commonharness.Sink that forwards diagnostic events to
// logger, mapping event names onto Runner's debug-log vocabulary.
func NewSink(logger domain.DebugLogger) commonharness.Sink {
	return &sink{logger: logger}
}

// sink adapts domain.DebugLogger to commonharness.Sink, so the
// shared package's diagnostic events (raw stdout, raw stderr, parse
// recovery/failure) continue to reach Runner's debug log.
type sink struct {
	logger domain.DebugLogger
}

// Log implements commonharness.Sink.
func (s *sink) Log(ev commonharness.Event) {
	event := eventName(ev.Name)
	fields := make([]domain.DebugField, 0, len(ev.Fields))
	for _, f := range ev.Fields {
		fields = append(fields, domain.F(f.Key, f.Value))
	}
	s.logger.Log(event, ev.Message, fields...)
}

// eventName maps a mosaic-common/harness event name to Runner's closed
// debug-log event vocabulary. An unrecognised event name is passed through
// unchanged rather than dropped, so a future shared-package event still
// leaves a trace even if this mapping has not been extended for it yet.
func eventName(name string) string {
	switch name {
	case "spawn.stdout":
		return domain.EventHarnessStdout
	case "spawn.stderr":
		return domain.EventHarnessStderr
	case "spawn.timeout", "spawn.error":
		return domain.EventHarnessInvokeError
	default:
		return name
	}
}
