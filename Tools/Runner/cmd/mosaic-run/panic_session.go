package main

import (
	"context"
	"fmt"
	"runtime/debug"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/session"
)

// maxPanicErrorStack bounds the stack text carried in the returned error. The
// debug log always receives the full stack.
const maxPanicErrorStack = 4096

// panicLoggingSession converts a panic in the inner session into an error.
type panicLoggingSession struct {
	inner session.Session
	debug domain.DebugLogger
}

// newPanicLoggingSession returns a session.Session whose Start delegates to
// inner.Start. If inner.Start panics, Start recovers, records the panic value
// and full stack in debug as a runner.error entry and returns a RunFailed
// outcome with an error beginning "panic in session: <value>". Without a panic,
// inner's result is returned unchanged.
func newPanicLoggingSession(inner session.Session, debugLog domain.DebugLogger) session.Session {
	return &panicLoggingSession{inner: inner, debug: debugLog}
}

func (s *panicLoggingSession) Start(ctx context.Context, cfg domain.RunConfig) (outcome domain.RunOutcome, err error) {
	defer func() {
		p := recover()
		if p == nil {
			return
		}
		full := debug.Stack()
		if s.debug != nil {
			s.debug.Log(domain.EventRunnerError,
				fmt.Sprintf("panic in session: %v\n%s", p, full),
				domain.F("site", "session"))
		}
		short := full
		if len(short) > maxPanicErrorStack {
			short = short[:maxPanicErrorStack]
		}
		err = fmt.Errorf("panic in session: %v\n%s", p, short)
		outcome = domain.RunOutcome{Status: domain.RunFailed, Message: err.Error()}
	}()
	return s.inner.Start(ctx, cfg)
}
