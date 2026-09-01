package main

// Tests for diagnostic output destination selection by output mode (T6.2).
//
// The goal of these tests is to make the routing decision visible and
// testable rather than implicit in a conditional buried in main:
//
//   - No mode may route subject output to a terminal the TUI owns.
//   - JSON mode must never route subject output to stdout.
//   - Subject output is never discarded silently: every mode retains it as
//     the primary diagnostic artifact for an unexpectedly-behaving run.
//
// The DiagnosticDestination type enforces the first constraint structurally:
// the closed type has no variant that names a process stream, so routing
// subject output to stdout or stderr cannot be expressed.

import (
	"testing"
)

// TestResolveDiagnosticDestination_AllModesRouteToRunSandbox asserts that
// every known output mode resolves to DestRunSandbox. Subject output is the
// primary diagnostic artifact and must not be discarded on any mode a user
// can select today.
func TestResolveDiagnosticDestination_AllModesRouteToRunSandbox(t *testing.T) {
	modes := []OutputMode{OutputModeCLI, OutputModeJSON, OutputModeTUI}
	for _, mode := range modes {
		got := ResolveDiagnosticDestination(mode)
		if got != DestRunSandbox {
			t.Errorf("ResolveDiagnosticDestination(%q) = %q, want %q — subject output must not be discarded on any active mode", mode, got, DestRunSandbox)
		}
	}
}

// TestResolveDiagnosticDestination_JSONMode_DoesNotRouteToStdout asserts that
// under JSON output mode the diagnostic destination is not anything that would
// write to stdout. Under JSON mode stdout carries the machine-readable report
// and must never receive any other content.
//
// The DiagnosticDestination type already makes "stdout" inexpressible, so
// this test specifically asserts that the JSON mode is routed to the run's
// sandbox rather than being discarded — discarding would also be safe from
// stdout's perspective, but would lose the primary diagnostic artifact.
func TestResolveDiagnosticDestination_JSONMode_DoesNotRouteToStdout(t *testing.T) {
	got := ResolveDiagnosticDestination(OutputModeJSON)
	if got != DestRunSandbox {
		t.Errorf("ResolveDiagnosticDestination(%q) = %q, want %q — JSON mode must retain subject output in the run sandbox, not discard it and never route it to stdout", OutputModeJSON, got, DestRunSandbox)
	}
}

// TestResolveDiagnosticDestination_TUIMode_DoesNotRouteToTerminal asserts
// that under TUI mode the diagnostic destination does not route subject output
// to any terminal stream. The TUI owns the terminal; raw text writes to
// stdout or stderr produce garbled frames.
//
// The DiagnosticDestination type makes a terminal destination inexpressible,
// so this test asserts the positive: TUI mode routes to the run's own sandbox.
func TestResolveDiagnosticDestination_TUIMode_DoesNotRouteToTerminal(t *testing.T) {
	got := ResolveDiagnosticDestination(OutputModeTUI)
	if got != DestRunSandbox {
		t.Errorf("ResolveDiagnosticDestination(%q) = %q, want %q — TUI mode must route subject output to the run sandbox, never to a terminal stream the TUI owns", OutputModeTUI, got, DestRunSandbox)
	}
}

// TestResolveDiagnosticDestination_CLIMode_RoutesToRunSandbox asserts that
// under plain CLI mode the destination is the run's own sandbox. This is the
// baseline: if CLI mode discarded diagnostics, the only path where a user
// could conveniently retrieve subject output would be gone.
func TestResolveDiagnosticDestination_CLIMode_RoutesToRunSandbox(t *testing.T) {
	got := ResolveDiagnosticDestination(OutputModeCLI)
	if got != DestRunSandbox {
		t.Errorf("ResolveDiagnosticDestination(%q) = %q, want %q — CLI mode must route subject output to the run sandbox", OutputModeCLI, got, DestRunSandbox)
	}
}

// TestResolveDiagnosticDestination_ReturnsValidDestination asserts that the
// returned value is always one of the declared DiagnosticDestination constants
// and never an empty string or an unrecognised value.
func TestResolveDiagnosticDestination_ReturnsValidDestination(t *testing.T) {
	modes := []OutputMode{OutputModeCLI, OutputModeJSON, OutputModeTUI}
	valid := map[DiagnosticDestination]bool{
		DestRunSandbox: true,
		DestDiscard:    true,
	}
	for _, mode := range modes {
		got := ResolveDiagnosticDestination(mode)
		if got == "" {
			t.Errorf("ResolveDiagnosticDestination(%q) returned an empty string; must return a declared DiagnosticDestination constant", mode)
		}
		if !valid[got] {
			t.Errorf("ResolveDiagnosticDestination(%q) = %q, which is not a declared DiagnosticDestination constant", mode, got)
		}
	}
}
