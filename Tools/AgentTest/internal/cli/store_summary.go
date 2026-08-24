package cli

import (
	"fmt"

	"mosaic-agent-test/internal/resultstore"
	"mosaic-agent-test/internal/resultsummary"
)

// StoreFunc processes report files into the TestResults tree. The
// composition root supplies a function that calls
// resultstore.StoreFromPaths with a real filesystem and the resolved
// TestResults root.
//
// The function receives a StoreFromPathsRequest (with file paths from
// positional args or a directory from --dir) and returns the result.
// The CLI command maps the result to output text and an exit code.
//
// Following the PreflightFunc precedent, this type references
// resultstore types directly rather than defining mirror types.
type StoreFunc func(req resultstore.StoreFromPathsRequest) (resultstore.StoreResult, error)

// SummaryFunc generates summary Markdown files from stored reports.
// The composition root supplies a function that calls
// resultsummary.Generate with a real filesystem and the resolved
// TestResults root.
type SummaryFunc func(req resultsummary.SummaryRequest) (resultsummary.SummaryResult, error)

// storeCommand implements the `store` command: validates input, builds a
// StoreFromPathsRequest from parsed flags and positional args, calls the
// Store func, and renders the summary line.
func storeCommand(inv parsedInvocation, o Options) int {
	dir := inv.flags["dir"]
	files := inv.positionals

	// Enforce mutual exclusion between --dir and positional file arguments
	// at the CLI layer before dispatching, so fakes and tests can observe
	// that Store is never called with an invalid combination.
	if dir != "" && len(files) > 0 {
		fmt.Fprintf(o.Stderr, "error: --dir and positional file arguments are mutually exclusive\n")
		return ExitUsage
	}

	// Require at least one input source.
	if dir == "" && len(files) == 0 {
		fmt.Fprintf(o.Stderr, "error: store requires at least one report file or --dir\n")
		return ExitUsage
	}

	if o.Store == nil {
		fmt.Fprintf(o.Stderr, "error: store command is not available (not wired by composition root)\n")
		return ExitFailure
	}

	req := resultstore.StoreFromPathsRequest{
		TestResultsRoot: o.TestResultsRoot,
		Files:           files,
		Dir:             dir,
	}

	result, err := o.Store(req)
	if err != nil {
		fmt.Fprintf(o.Stderr, "error: %v\n", err)
		return ExitFailure
	}

	fmt.Fprintln(o.Stdout, result.SummaryLine())
	return ExitSuccess
}

// summaryCommand implements the `summary` command: builds a SummaryRequest
// from parsed flags, calls the Summary func, and lists written/updated files.
func summaryCommand(inv parsedInvocation, o Options) int {
	if o.Summary == nil {
		fmt.Fprintf(o.Stderr, "error: summary command is not available (not wired by composition root)\n")
		return ExitFailure
	}

	req := resultsummary.SummaryRequest{
		TestResultsRoot: o.TestResultsRoot,
		VersionFilter:   inv.flags["for-version"],
	}

	result, err := o.Summary(req)
	if err != nil {
		fmt.Fprintf(o.Stderr, "error: %v\n", err)
		return ExitFailure
	}

	for _, f := range result.FilesWritten {
		fmt.Fprintf(o.Stdout, "wrote %s\n", f)
	}
	for _, f := range result.FilesUpdated {
		fmt.Fprintf(o.Stdout, "updated %s\n", f)
	}
	return ExitSuccess
}
