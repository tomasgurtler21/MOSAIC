package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	commonharness "mosaic-common/harness"

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

	// logger-bundle, cost-tool, suites, deploy-tool, mosaic-root and
	// catalog-folder are recognised-and-ignored here, the same way tui is:
	// all are pre-scanned by the composition root ahead of this parser (see
	// cmd/mosaic-agent-test's resolveWiringConfig and runTUIMode), so this
	// package owes them nothing beyond not rejecting them as unknown.
	"logger-bundle":  true,
	"cost-tool":      true,
	"suites":         true,
	"deploy-tool":    true,
	"mosaic-root":    true,
	"catalog-folder": true,

	// report-path overrides the JSON report file location; --no-report is the
	// bool companion that suppresses the write entirely.
	"report-path": true,

	// subject-model and stub-model are the run-time model selections validated
	// against the selected harness's catalog before pre-flight runs.
	"subject-model": true,
	"stub-model":    true,
}

// boolFlags are the flags this package's command surface accepts that take
// no value, other than --tui (handled separately above) and --help/-h
// (handled ahead of parseInvocation).
var boolFlags = map[string]bool{
	"keep-sandbox":            true,
	"keep-sandbox-on-failure": true,
	"no-report":               true,
}

// parsedInvocation is the result of tokenising the command line: the
// command name, the positional suite path, and every recognised flag's raw
// string value.
type parsedInvocation struct {
	command    string
	suitePath  string
	flags      map[string]string
	boolFlags  map[string]bool
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
	if helpRequested(args) {
		if err := Usage(o.Stdout, o); err != nil {
			fmt.Fprintf(o.Stderr, "error: %v\n", err)
			return ExitFailure
		}
		return ExitSuccess
	}

	if len(args) == 0 {
		// A bare invocation shows the full usage surface, not only the
		// terse "no command given" message: an invocation naming no
		// command is still a usage error (ExitUsage), but the response to
		// it must be the same surface --help produces.
		_ = Usage(o.Stderr, o)
		return ExitUsage
	}

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

// helpRequested reports whether args carries an explicit help request
// (--help or -h) anywhere. Checked ahead of parseInvocation so a help
// request is honoured regardless of position, without teaching the
// command/flag parser its own help vocabulary.
func helpRequested(args []string) bool {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			return true
		}
	}
	return false
}

// parseInvocation tokenises the raw command line into a command name, a
// positional suite path and a set of flag values, accepting both
// space-separated ("--flag value") and equals-separated ("--flag=value")
// forms equivalently. An unrecognised flag or a value-flag stated without a
// value is a usageError.
func parseInvocation(args []string) (parsedInvocation, error) {
	inv := parsedInvocation{flags: map[string]string{}, boolFlags: map[string]bool{}}

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

		if boolFlags[name] {
			inv.boolFlags[name] = true
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

	if len(o.Harnesses) > 0 && !harnessRecognised(in.HarnessID, o.Harnesses) {
		return in, usageError{fmt.Sprintf("unrecognised --harness value %q", in.HarnessID)}
	}

	if v, ok := inv.flags["subject-model"]; ok {
		if v == "" {
			return in, usageError{"--subject-model requires a non-empty value"}
		}
		if err := validateModelID("--subject-model", v, in.HarnessID, o.Models); err != nil {
			return in, err
		}
		in.Overrides.SubjectModel = v
	}
	if v, ok := inv.flags["stub-model"]; ok {
		if v == "" {
			return in, usageError{"--stub-model requires a non-empty value"}
		}
		if err := validateModelID("--stub-model", v, in.HarnessID, o.Models); err != nil {
			return in, err
		}
		in.Overrides.StubModel = v
	}

	return in, nil
}

// validateModelID checks that modelID is valid for harnessID in the supplied
// catalog. When the catalog is nil or has no entry for harnessID, validation
// is skipped: an unwired context must never reject every value.
func validateModelID(flag, modelID, harnessID string, catalog []commonharness.ModelCatalog) error {
	for _, mc := range catalog {
		if mc.HarnessID == harnessID {
			for _, id := range mc.IDs {
				if id == modelID {
					return nil
				}
			}
			return usageError{fmt.Sprintf("unrecognised %s value %q for harness %q", flag, modelID, harnessID)}
		}
	}
	// No catalog entry for this harness: skip validation.
	return nil
}

// harnessRecognised reports whether id names an entry in catalog. Validation
// only runs when a catalog was actually supplied: Options.Harnesses is
// empty in every context that has not been wired to a catalog, and an empty
// catalog must never reject every value.
func harnessRecognised(id string, catalog []commonharness.CLIHarness) bool {
	for _, h := range catalog {
		if h.ID == id {
			return true
		}
	}
	return false
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

// resolveRetention resolves --keep-sandbox and --keep-sandbox-on-failure into
// the sandbox retention policy: domain.RetainNever when neither is present,
// domain.RetainOnFailure for --keep-sandbox-on-failure alone, and
// domain.RetainAlways for --keep-sandbox alone or both together (the
// stronger policy wins; specifying both is not an error).
func resolveRetention(inv parsedInvocation) domain.RetentionPolicy {
	if inv.boolFlags["keep-sandbox"] {
		return domain.RetainAlways
	}
	if inv.boolFlags["keep-sandbox-on-failure"] {
		return domain.RetainOnFailure
	}
	return domain.RetainNever
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

// resolveReportPath determines the JSON report file path for this invocation.
// --no-report suppresses the write (returns ""). --report-path overrides the
// default. A --report-path value that is empty (i.e. --report-path=) is a
// usage error: present-and-empty is not equivalent to absent.
func resolveReportPath(inv parsedInvocation, o Options) (string, error) {
	if inv.boolFlags["no-report"] {
		return "", nil
	}
	if v, ok := inv.flags["report-path"]; ok {
		if v == "" {
			return "", usageError{"--report-path requires a non-empty value"}
		}
		return v, nil
	}
	if o.ReportPathFor != nil {
		return o.ReportPathFor(inv.suitePath), nil
	}
	return o.DefaultReportPath, nil
}

// writeReportFile renders result to JSON and writes the bytes through
// o.WriteFile to path. A nil WriteFile or a write error is reported to
// o.Stderr. Neither failure alters the run's own exit code.
func writeReportFile(o Options, path string, result report.Result) {
	if o.WriteFile == nil {
		fmt.Fprintf(o.Stderr, "error: cannot write report to %q: no write function configured\n", path)
		return
	}
	var buf bytes.Buffer
	if err := report.RenderJSON(&buf, result); err != nil {
		fmt.Fprintf(o.Stderr, "error: cannot write report to %q: %v\n", path, err)
		return
	}
	if err := o.WriteFile(path, buf.Bytes()); err != nil {
		fmt.Fprintf(o.Stderr, "error: cannot write report to %q: %v\n", path, err)
	}
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

	reportPath, err := resolveReportPath(inv, o)
	if err != nil {
		fmt.Fprintf(o.Stderr, "error: %v\n", err)
		return ExitUsage
	}

	plan, rep := o.Preflight(in)
	if rep.HasErrors() {
		renderPreflightReport(o.Stderr, rep)
		return ExitPreflight
	}

	cfg := RunConfig{
		WorkspaceRoot: resolveWorkspaceRoot(inv, o),
		Retention:     resolveRetention(inv),
	}
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

	// Record the effective repetitions count in the result so the text renderer
	// can surface it in the report header. This gives the user a confirmation
	// of the configured value without requiring them to examine the suite file.
	result.EffectiveRepetitions = in.Overrides.Repetitions

	if err := renderResult(o.Stdout, format, result); err != nil {
		fmt.Fprintf(o.Stderr, "error: rendering the report: %v\n", err)
		return ExitFailure
	}

	if reportPath != "" {
		writeReportFile(o, reportPath, result)
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

// renderPreflightReport writes the shared rendering of rep to w. Both
// runCommand and validateCommand call it before returning ExitPreflight, so
// the two subcommands cannot report the same rejection differently.
func renderPreflightReport(w io.Writer, rep authoring.Report) {
	rendered := authoring.RenderReport(rep)
	if rendered != "" {
		fmt.Fprintln(w, rendered)
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
