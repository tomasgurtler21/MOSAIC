package main

// panic_session_test.go covers the CLI panic-logging session wrapper: a panic
// in the inner session becomes a returned error, is recorded with its stack in
// the debug log, and makes cli.Run exit with the failure code.

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"mosaic-run/internal/cli"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/session"
)

func panickingInner(value any) *mlSession {
	return &mlSession{start: func(context.Context, domain.RunConfig) (domain.RunOutcome, error) {
		panic(value)
	}}
}

// startNoPanic calls sess.Start and fails the test when a panic escapes.
func startNoPanic(t *testing.T, sess session.Session) (out domain.RunOutcome, err error) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("panic escaped Start: %v", p)
		}
	}()
	return sess.Start(context.Background(), domain.RunConfig{})
}

func TestPanicLoggingSession_PanicBecomesErrorAndFailedOutcome(t *testing.T) {
	rec := &recordingLogger{}

	out, err := startNoPanic(t, newPanicLoggingSession(panickingInner("cli-exploded"), rec))

	if err == nil || !strings.Contains(err.Error(), "panic in session: cli-exploded") {
		t.Fatalf("err = %v, want it to name the session and panic value", err)
	}
	if out.Status != domain.RunFailed || out.Message != err.Error() {
		t.Errorf("outcome = %+v, want RunFailed carrying the error text", out)
	}
}

func TestPanicLoggingSession_NonStringPanicValueIsReported(t *testing.T) {
	rec := &recordingLogger{}

	_, err := startNoPanic(t, newPanicLoggingSession(panickingInner(42), rec))

	if err == nil || !strings.Contains(err.Error(), "42") {
		t.Fatalf("err = %v, want the integer panic value", err)
	}
}

func TestPanicLoggingSession_RecordsValueAndFullStackInDebugLog(t *testing.T) {
	rec := &recordingLogger{}

	_, _ = startNoPanic(t, newPanicLoggingSession(panickingInner("cli-exploded"), rec))

	var found []logEntry
	for _, e := range rec.snapshot() {
		if e.event == domain.EventRunnerError {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("runner.error entries = %d, want 1: %+v", len(found), rec.snapshot())
	}
	e := found[0]
	if !strings.Contains(e.message, "cli-exploded") || !strings.Contains(e.message, "goroutine") {
		t.Errorf("debug message = %q, want panic value and stack", e.message)
	}
	site := ""
	for _, f := range e.fields {
		if f.Key == "site" {
			site = f.Value
		}
	}
	if site != "session" {
		t.Errorf("site field = %q, want session", site)
	}
}

func TestPanicLoggingSession_WithoutPanicPassesResultThroughAndLogsNothing(t *testing.T) {
	rec := &recordingLogger{}
	want := domain.RunOutcome{Status: domain.RunStopped, Message: "m"}
	boom := context.Canceled
	inner := &mlSession{start: func(context.Context, domain.RunConfig) (domain.RunOutcome, error) {
		return want, boom
	}}

	got, err := startNoPanic(t, newPanicLoggingSession(inner, rec))

	if got != want || err != boom || inner.calls != 1 {
		t.Fatalf("got (%+v, %v), calls=%d; want the inner result unchanged", got, err, inner.calls)
	}
	if n := len(rec.snapshot()); n != 0 {
		t.Errorf("debug entries = %d, want 0", n)
	}
}

func TestPanicLoggingSession_CLIRunExitsWithFailureAndReportsPanic(t *testing.T) {
	rec := &recordingLogger{}
	sess := newPanicLoggingSession(panickingInner("cli-exploded"), rec)
	var out, errOut bytes.Buffer

	code := cli.Run(context.Background(), []string{
		"run",
		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
		"--review-loop-limit", "3",
	}, nil, nil, sess, &out, &errOut)

	if code != cli.ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d); stderr: %q", code, cli.ExitFailure, errOut.String())
	}
	if !strings.Contains(errOut.String(), "cli-exploded") {
		t.Errorf("stderr = %q, want the panic value", errOut.String())
	}
	logged := false
	for _, e := range rec.snapshot() {
		if e.event == domain.EventRunnerError && strings.Contains(e.message, "cli-exploded") {
			logged = true
		}
	}
	if !logged {
		t.Errorf("debug log has no runner.error carrying the panic: %+v", rec.snapshot())
	}
}
