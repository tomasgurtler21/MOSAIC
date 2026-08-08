package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/report"
)

// valueFlags are the flags this package's command surface accepts that
// require a value, in the flag names used on the command line (without the
// leading "--"). --tui is handled separately: it takes no value and is
// recognised-and-ignored rather than resolved into anything below.
var valueFlags = map[string]bool{
	"tests":          true,
	"harness":        true,
	"format":         true,
	"fixtures":       true,
	"workspace-root": true,
	"timeout":        true,
	"repetitions":    true,
}

// parsedInvocation is the result of tokenising the command line: the
// command name, the positional suite path, and every recognised flag's raw
// string value.
type parsedInvocation struct {
	command    string
	suitePath  string
	flags      map[string]string
	tuiPresent bool
}

// usageError is returned by parsing steps that must produce ExitUsage. It
// carries the message written to Options.Stderr.
type usageError struct {
	msg string
}

func (e usageError) Error() string { return e.msg }

// execute is Execute's implementation. Kept separate from the exported
// Execute in cli.go so the file layout matches the plan: command tree, flag
// parsing and the run/validate paths live here.
func execute(ctx context.Context, args []string, o Options) int {
	inv, err := parseInvocation(args)
	if err != nil {
		fmt.Fprintf(o.Stderr, "error: %v\n", err)
		return ExitUsage
	}

	switch inv.command {
	case "run":
		return runCommand(ctx, inv, o)
	case "validate":
		return validateCommand(inv, o)
	default:
		fmt.Fprintf(o.Stderr, "error: unknown command %q; want \"run\" or \"validate\"\n", inv.command)
		return ExitUsage
	}
}

// parseInvocation tokenises the raw command line into a command name, a
// positional suite path and a set of flag values, accepting both
// space-separated ("--flag value") and equals-separated ("--flag=value")
// forms equivalently. An unrecognised flag or a value-flag stated without a
// value is a usageError.
func parseInvocation(args []string) (parsedInvocation, error) {
	inv := parsedInvocation{flags: map[string]string{}}

	if len(args) == 0 {
		return inv, usageError{"no command given; want \"run\" or \"validate\""}
	}
	inv.command = args[0]

	rest := args[1:]
	var positionals []string

	for i := 0; i < len(rest); i++ {
		tok := rest[i]
		if !strings.HasPrefix(tok, "--") {
			positionals = append(positionals, tok)
			continue
		}

		name := tok[2:]
		var value string
		hasValue := false
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			value = name[eq+1:]
			name = name[:eq]
			hasValue = true
		}

		if name == "tui" {
			inv.tuiPresent = true
			continue
		}

		if !valueFlags[name] {
			return inv, usageError{fmt.Sprintf("unknown flag: --%s", name)}
		}

		if !hasValue {
			if i+1 >= len(rest) {
				return inv, usageError{fmt.Sprintf("flag --%s requires a value", name)}
			}
			i++
			value = rest[i]
		}

		inv.flags[name] = value
	}

	if len(positionals) > 0 {
		inv.suitePath = positionals[0]
	}

	return inv, nil
}

// resolveInput builds a preflight.Input from the parsed invocation and the
// defaults Options carries, applying flag values where present. A malformed
// --timeout or --repetitions value is a usageError, distinct from either
// flag being stated without a value at all.
func resolveInput(inv parsedInvocation, o Options) (preflight.Input, error) {
	in := preflight.Input{
		SuitePath:   inv.suitePath,
		FixtureRoot: o.FixtureRoot,
		HarnessID:   o.DefaultHarness,
	}

	if v, ok := inv.flags["fixtures"]; ok {
		in.FixtureRoot = v
	}
	if v, ok := inv.flags["harness"]; ok {
		in.HarnessID = v
	}
	if v, ok := inv.flags["tests"]; ok && v != "" {
		in.Overrides.TestIDs = strings.Split(v, ",")
	}
	if v, ok := inv.flags["timeout"]; ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return in, usageError{fmt.Sprintf("invalid --timeout value %q: %v", v, err)}
		}
		in.Overrides.Timeout = &d
	}
	if v, ok := inv.flags["repetitions"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return in, usageError{fmt.Sprintf("invalid --repetitions value %q: %v", v, err)}
		}
		in.Overrides.Repetitions = &n
	}

	return in, nil
}

// resolveFormat validates the --format flag against the documented
// text|json surface, defaulting to text when absent.
func resolveFormat(inv parsedInvocation) (string, error) {
	format, ok := inv.flags["format"]
	if !ok || format == "" {
		return "text", nil
	}
	switch format {
	case "text", "json":
		return format, nil
	default:
		return "", usageError{fmt.Sprintf("invalid --format value %q; want \"text\" or \"json\"", format)}
	}
}

// resolveWorkspaceRoot resolves --workspace-root against Options.WorkspaceRoot
// at the single flag-parse site, so both flag forms and the no-flag default
// all funnel through one resolution.
func resolveWorkspaceRoot(inv parsedInvocation, o Options) string {
	if v, ok := inv.flags["workspace-root"]; ok {
		return v
	}
	return o.WorkspaceRoot
}

// runCommand implements `run <suite>`: pre-flight, then — only on a clean
// report — execute the suite.
func runCommand(ctx context.Context, inv parsedInvocation, o Options) int {
	in, err := resolveInput(inv, o)
	if err != nil {
		fmt.Fprintf(o.Stderr, "error: %v\n", err)
		return ExitUsage
	}

	format, err := resolveFormat(inv)
	if err != nil {
		fmt.Fprintf(o.Stderr, "error: %v\n", err)
		return ExitUsage
	}

	plan, rep := o.Preflight(in)
	if rep.HasErrors() {
		renderPreflightReport(o.Stderr, rep)
		return ExitPreflight
	}

	cfg := RunConfig{WorkspaceRoot: resolveWorkspaceRoot(inv, o)}
	runner, err := o.Suite(cfg)
	if err != nil {
		fmt.Fprintf(o.Stderr, "error: could not build the suite runner: %v\n", err)
		return ExitFailure
	}

	progressWriter := o.Stdout
	if format == "json" {
		progressWriter = o.Stderr
	}
	sink := NewLineSink(progressWriter)

	result, err := runner.Run(ctx, plan, sink)
	if err != nil {
		fmt.Fprintf(o.Stderr, "error: %v\n", err)
		return ExitFailure
	}

	if err := renderResult(o.Stdout, format, result); err != nil {
		fmt.Fprintf(o.Stderr, "error: rendering the report: %v\n", err)
		return ExitFailure
	}

	return exitCodeForResult(result)
}

// validateCommand implements `validate <suite>`: pre-flight only. It never
// calls Options.Suite — pre-flight creates no sandbox, so nothing on this
// path may construct a sandbox manager — and shares runCommand's pre-flight
// rendering so the two never drift into different diagnostic vocabularies.
func validateCommand(inv parsedInvocation, o Options) int {
	in, err := resolveInput(inv, o)
	if err != nil {
		fmt.Fprintf(o.Stderr, "error: %v\n", err)
		return ExitUsage
	}

	if _, err := resolveFormat(inv); err != nil {
		fmt.Fprintf(o.Stderr, "error: %v\n", err)
		return ExitUsage
	}

	_, rep := o.Preflight(in)
	if rep.HasErrors() {
		renderPreflightReport(o.Stderr, rep)
		return ExitPreflight
	}

	fmt.Fprintln(o.Stdout, "no errors found")
	return ExitSuccess
}

// renderPreflightReport writes every diagnostic in rep.Sorted() order, one
// per line, to w — the single rendering both `run`'s pre-flight failure and
// `validate` use, so they cannot report the same rejection differently.
func renderPreflightReport(w io.Writer, rep authoring.Report) {
	for _, d := range rep.Sorted() {
		fmt.Fprintf(w, "%s: %s: %s\n", d.Path, d.Code, d.Message)
	}
}

// renderResult writes result through the renderer the selected format
// names — the only two renderings this package ever writes, both owned by
// internal/report.
func renderResult(w io.Writer, format string, result report.Result) error {
	if format == "json" {
		return report.RenderJSON(w, result)
	}
	return report.RenderText(w, result)
}

// exitCodeForResult classifies a finished run's result into the exit-code
// contract. An infrastructure fault always reads as ExitFailure — a
// statement about the tool, never about the subject — even when the same
// result also carries a FAIL verdict.
func exitCodeForResult(result report.Result) int {
	if result.InfrastructureFailures > 0 {
		return ExitFailure
	}
	if result.Counts[domain.VerdictFail] > 0 {
		return ExitTestsFailed
	}
	return ExitSuccess
}
