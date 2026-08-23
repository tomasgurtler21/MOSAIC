package main

// DiagnosticDestination names where a run's subject diagnostics are written.
//
// The type is deliberately closed and deliberately has no variant naming a
// process stream. Under JSON output mode standard output carries the
// machine-readable report and must never receive diagnostics; under TUI mode
// the frontend owns the terminal that standard error writes to. Rather than
// condition the routing on the mode and rely on the condition being right,
// the destinations that could violate either rule are not expressible.
type DiagnosticDestination string

const (
	// DestRunSandbox writes each run's diagnostics into that run's own
	// sandbox, where the run's retention policy governs them uniformly.
	DestRunSandbox DiagnosticDestination = "run_sandbox"
	// DestDiscard drops them.
	DestDiscard DiagnosticDestination = "discard"
)

// OutputMode names the active output presentation mode. It is the sole input
// to ResolveDiagnosticDestination: the destination must be derivable from the
// mode alone, with no ambient state.
//
// The type is closed. A mode not in this list cannot be passed to
// ResolveDiagnosticDestination, so the resolution table is exhaustive by
// construction.
type OutputMode string

const (
	// OutputModeCLI names the human-readable CLI output mode: lines written
	// to stderr as they arrive, no TUI, no machine-readable JSON.
	OutputModeCLI OutputMode = "cli"
	// OutputModeJSON names the JSON output mode: the machine-readable report
	// is written to stdout on completion. Stdout must never receive anything
	// else while this mode is active.
	OutputModeJSON OutputMode = "json"
	// OutputModeTUI names the TUI mode: the program owns the terminal it
	// started with, and neither stdout nor stderr may receive raw text while
	// the TUI is painting frames.
	OutputModeTUI OutputMode = "tui"
)

// ResolveDiagnosticDestination picks the destination for an output mode.
// Every mode currently resolves to DestRunSandbox: subject output is the
// primary diagnostic artifact for an unexpectedly-behaving run and is not
// discarded on any path a user can select today. DestDiscard exists so that
// remains a decision this function records rather than an absence of one.
func ResolveDiagnosticDestination(_ OutputMode) DiagnosticDestination {
	return DestRunSandbox
}
