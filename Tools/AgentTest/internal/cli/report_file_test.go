package cli_test

// report_file_test.go covers the persistent JSON report file feature for the
// command-line frontend:
//
//   T7.1 - A JSON report file is written at the default location on a normal
//           run, the override flag redirects it, the suppression flag prevents
//           any write, and the file content is JSON under both console
//           rendering formats.
//
//   T7.2 - An unwritable or invalid report path is reported to the user
//           rather than silently ignored, and the run's exit code still
//           reflects the run's own outcome.
//
// These tests drive cli.Execute through the new WriteFile/DefaultReportPath
// fields on cli.Options and the new --report-path / --no-report flags. No
// real filesystem is touched: WriteFile is an in-memory capture function.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/preflight"
)

// ---------------------------------------------------------------------------
// In-memory WriteFile capture
// ---------------------------------------------------------------------------

// captureWriteFile records every call made to it so a test can assert on the
// path and the bytes without touching the real filesystem.
type captureWriteFile struct {
	mu    sync.Mutex
	calls []writeFileCall
	err   error // returned verbatim on every call when non-nil
}

type writeFileCall struct {
	path string
	data []byte
}

func (c *captureWriteFile) write(path string, data []byte) error {
	c.mu.Lock()
	c.calls = append(c.calls, writeFileCall{path: path, data: data})
	c.mu.Unlock()
	return c.err
}

func (c *captureWriteFile) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func (c *captureWriteFile) lastCall() (writeFileCall, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.calls) == 0 {
		return writeFileCall{}, false
	}
	return c.calls[len(c.calls)-1], true
}

// ---------------------------------------------------------------------------
// Report-options helpers
// ---------------------------------------------------------------------------

// withReportFile returns a copy of o with the given default path and write
// function installed. It is the single point of assembly for report-file
// tests, so both the path field and the WriteFile field are always set
// together and tests cannot accidentally leave one unset.
func withReportFile(o cli.Options, defaultPath string, wf cli.WriteFileFunc) cli.Options {
	o.DefaultReportPath = defaultPath
	o.WriteFile = wf
	return o
}

// ---------------------------------------------------------------------------
// T7.1: default write, override, suppression, JSON content
// ---------------------------------------------------------------------------

// TestRunCommand_WritesJSONReport_AtDefaultPath asserts that a normal run
// writes the report file to Options.DefaultReportPath when --report-path is
// absent and --no-report is not supplied.
func TestRunCommand_WritesJSONReport_AtDefaultPath(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	capture := &captureWriteFile{}
	runner := &fakeSuiteRunner{result: passingResult("default-path-suite")}
	opts := withReportFile(
		baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner),
		"report.json",
		capture.write,
	)

	code := cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	if code == cli.ExitUsage {
		t.Fatalf("Execute returned ExitUsage; --report-path or --no-report flags may not be registered yet (stderr=%s)", stderr.String())
	}
	if n := capture.callCount(); n != 1 {
		t.Fatalf("WriteFile was called %d times, want exactly 1", n)
	}
	call, _ := capture.lastCall()
	if call.path != "report.json" {
		t.Errorf("WriteFile called with path %q, want %q (Options.DefaultReportPath)", call.path, "report.json")
	}
}

// TestRunCommand_ReportPath_OverridesDefault asserts that --report-path
// redirects the write to the specified path, leaving Options.DefaultReportPath
// unused.
func TestRunCommand_ReportPath_OverridesDefault(t *testing.T) {
	forms := map[string][]string{
		"space":  {"run", "suite.yaml", "--report-path", "out/custom.json"},
		"equals": {"run", "suite.yaml", "--report-path=out/custom.json"},
	}

	for name, args := range forms {
		t.Run(name, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			capture := &captureWriteFile{}
			runner := &fakeSuiteRunner{result: passingResult("override-path-suite")}
			opts := withReportFile(
				baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner),
				"report.json",
				capture.write,
			)

			code := cli.Execute(context.Background(), args, opts)

			if code == cli.ExitUsage {
				t.Fatalf("%s form: --report-path was rejected as an unknown flag (stderr=%s)", name, stderr.String())
			}
			if n := capture.callCount(); n != 1 {
				t.Fatalf("%s form: WriteFile called %d times, want 1", name, n)
			}
			call, _ := capture.lastCall()
			if call.path != "out/custom.json" {
				t.Errorf("%s form: WriteFile path = %q, want %q (the --report-path value, not the default)", name, call.path, "out/custom.json")
			}
		})
	}
}

// TestRunCommand_NoReport_SuppressesWrite asserts that --no-report prevents
// any call to WriteFile, so no report file is created.
func TestRunCommand_NoReport_SuppressesWrite(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	capture := &captureWriteFile{}
	runner := &fakeSuiteRunner{result: passingResult("suppressed-suite")}
	opts := withReportFile(
		baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner),
		"report.json",
		capture.write,
	)

	code := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--no-report"}, opts)

	if code == cli.ExitUsage {
		t.Fatalf("--no-report was rejected as an unknown flag (stderr=%s)", stderr.String())
	}
	if n := capture.callCount(); n != 0 {
		t.Errorf("WriteFile was called %d times with --no-report; want 0 (no file must be written)", n)
	}
}

// TestRunCommand_ReportContent_IsJSON_UnderTextFormat asserts that the bytes
// written to WriteFile are valid JSON when --format text is in effect (or when
// --format is absent and defaults to text). The report file must be machine-
// readable regardless of the console rendering format. A content check on the
// "schema_version" field guards against a wiring fault that produces empty or
// zero-valued JSON (e.g. "{}" or "null"), both of which satisfy json.Valid.
func TestRunCommand_ReportContent_IsJSON_UnderTextFormat(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	capture := &captureWriteFile{}
	runner := &fakeSuiteRunner{result: passingResult("text-format-suite")}
	opts := withReportFile(
		baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner),
		"report.json",
		capture.write,
	)

	cli.Execute(context.Background(), []string{"run", "suite.yaml", "--format", "text"}, opts)

	call, ok := capture.lastCall()
	if !ok {
		t.Fatal("WriteFile was never called under --format text")
	}
	if !json.Valid(call.data) {
		t.Errorf("WriteFile bytes are not valid JSON under --format text:\n%s", call.data)
	}
	if !strings.Contains(string(call.data), `"schema_version"`) {
		t.Errorf("WriteFile bytes under --format text do not contain the expected %q field; the content may be empty or zero-valued:\n%s", "schema_version", call.data)
	}
}

// TestRunCommand_ReportContent_IsJSON_UnderJSONFormat asserts that the bytes
// written to WriteFile are valid JSON when --format json is in effect — the
// same guarantee under the other console rendering format.
func TestRunCommand_ReportContent_IsJSON_UnderJSONFormat(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	capture := &captureWriteFile{}
	runner := &fakeSuiteRunner{result: passingResult("json-format-suite")}
	opts := withReportFile(
		baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner),
		"report.json",
		capture.write,
	)

	cli.Execute(context.Background(), []string{"run", "suite.yaml", "--format", "json"}, opts)

	call, ok := capture.lastCall()
	if !ok {
		t.Fatal("WriteFile was never called under --format json")
	}
	if !json.Valid(call.data) {
		t.Errorf("WriteFile bytes are not valid JSON under --format json:\n%s", call.data)
	}
}

// TestRunCommand_ReportContent_SameUnderBothFormats asserts that the bytes
// written to WriteFile are identical under --format text and --format json,
// because both must produce the machine-readable rendering regardless of the
// console format selection.
func TestRunCommand_ReportContent_SameUnderBothFormats(t *testing.T) {
	result := passingResult("format-equivalence-suite")

	collectBytes := func(format string) []byte {
		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		capture := &captureWriteFile{}
		runner := &fakeSuiteRunner{result: result}
		opts := withReportFile(
			baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner),
			"report.json",
			capture.write,
		)
		cli.Execute(context.Background(), []string{"run", "suite.yaml", "--format", format}, opts)
		call, ok := capture.lastCall()
		if !ok {
			t.Fatalf("WriteFile not called under --format %s", format)
		}
		return call.data
	}

	textBytes := collectBytes("text")
	jsonBytes := collectBytes("json")

	if string(textBytes) != string(jsonBytes) {
		t.Errorf("report file bytes differ between --format text and --format json:\ntext: %s\njson: %s", textBytes, jsonBytes)
	}
}

// TestRunCommand_ReportPath_IsRecognisedFlag asserts --report-path never
// surfaces as an unknown flag on the `run` command.
func TestRunCommand_ReportPath_IsRecognisedFlag(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	capture := &captureWriteFile{}
	runner := &fakeSuiteRunner{result: passingResult("flag-recognition-suite")}
	opts := withReportFile(
		baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner),
		"report.json",
		capture.write,
	)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--report-path", "out.json"}, opts)

	if got == cli.ExitUsage {
		t.Errorf("--report-path was rejected as unknown (exit ExitUsage); stderr=%s", stderr.String())
	}
}

// TestValidateCommand_ReportPath_IsRecognisedFlag asserts --report-path never
// surfaces as an unknown flag on the `validate` command. Both commands share
// one flag parser, so both must accept it.
func TestValidateCommand_ReportPath_IsRecognisedFlag(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := withReportFile(
		baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner),
		"report.json",
		nil,
	)

	got := cli.Execute(context.Background(), []string{"validate", "suite.yaml", "--report-path", "out.json"}, opts)

	if got == cli.ExitUsage {
		t.Errorf("--report-path on validate was rejected as unknown (exit ExitUsage); stderr=%s", stderr.String())
	}
}

// TestRunCommand_NoReport_IsRecognisedFlag asserts --no-report never surfaces
// as an unknown flag on the `run` command.
func TestRunCommand_NoReport_IsRecognisedFlag(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("flag-recognition-suite")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--no-report"}, opts)

	if got == cli.ExitUsage {
		t.Errorf("--no-report was rejected as unknown (exit ExitUsage); stderr=%s", stderr.String())
	}
}

// TestValidateCommand_NoReport_IsRecognisedFlag asserts --no-report never
// surfaces as an unknown flag on the `validate` command either.
func TestValidateCommand_NoReport_IsRecognisedFlag(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"validate", "suite.yaml", "--no-report"}, opts)

	if got == cli.ExitUsage {
		t.Errorf("--no-report on validate was rejected as unknown (exit ExitUsage); stderr=%s", stderr.String())
	}
}

// TestFlags_IncludesReportFileFlags asserts the shared flag specification
// (cli.Flags()) documents both --report-path and --no-report, so a user
// running --help learns they exist.
func TestFlags_IncludesReportFileFlags(t *testing.T) {
	names := map[string]bool{}
	for _, f := range cli.Flags() {
		names[f.Name] = true
	}
	for _, want := range []string{"--report-path", "--no-report"} {
		if !names[want] {
			t.Errorf("cli.Flags() does not include %q", want)
		}
	}
}

// ---------------------------------------------------------------------------
// T7.2: failure modes and exit-code preservation
// ---------------------------------------------------------------------------

// TestRunCommand_ReportWriteFailure_IsReportedOnStderr asserts that a
// WriteFile error surfaces as a message on standard error naming the path and
// the cause, so the user is not silently left with a missing report file.
func TestRunCommand_ReportWriteFailure_IsReportedOnStderr(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	capture := &captureWriteFile{err: errors.New("permission denied")}
	runner := &fakeSuiteRunner{result: passingResult("write-failure-suite")}
	opts := withReportFile(
		baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner),
		"report.json",
		capture.write,
	)

	cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	errOut := stderr.String()
	if errOut == "" {
		t.Error("stderr is empty after a WriteFile failure; the user was given no indication the report was not written")
	}
	if !containsAll(errOut, "report.json", "permission denied") {
		t.Errorf("stderr does not name both the path and the error:\n%s", errOut)
	}
}

// TestRunCommand_ReportWriteFailure_DoesNotChangeExitCode_OnPassingRun asserts
// that a WriteFile error does not turn a passing run's exit code into a
// failure. The report file is a record of the run, not part of its verdict.
//
// Note (TDD RED phase): this test passes vacuously before implementation.
// WriteFile is never called (no implementation wires it), so the injected
// error never fires and the exit code is simply what the suite result already
// yields. The test will catch a wrong implementation once WriteFile is wired
// at I7.2.
func TestRunCommand_ReportWriteFailure_DoesNotChangeExitCode_OnPassingRun(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	capture := &captureWriteFile{err: errors.New("disk full")}
	runner := &fakeSuiteRunner{result: passingResult("passing-run-suite")}
	opts := withReportFile(
		baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner),
		"report.json",
		capture.write,
	)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	if got != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d); a report-write failure must not change a passing run's exit code (stderr=%s)", got, cli.ExitSuccess, stderr.String())
	}
}

// TestRunCommand_ReportWriteFailure_DoesNotChangeExitCode_OnFailingRun asserts
// that a WriteFile error does not mask a failing run's exit code. The run's
// own verdict is the authoritative signal.
//
// Note (TDD RED phase): this test passes vacuously before implementation.
// WriteFile is never called (no implementation wires it), so the injected
// error never fires and the exit code is simply what the suite result already
// yields. The test will catch a wrong implementation once WriteFile is wired
// at I7.2.
func TestRunCommand_ReportWriteFailure_DoesNotChangeExitCode_OnFailingRun(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	capture := &captureWriteFile{err: errors.New("disk full")}
	runner := &fakeSuiteRunner{result: failedTestResult("failing-run-suite")}
	opts := withReportFile(
		baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner),
		"report.json",
		capture.write,
	)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	if got != cli.ExitTestsFailed {
		t.Errorf("exit code = %d, want ExitTestsFailed (%d); a report-write failure must not mask a failing run (stderr=%s)", got, cli.ExitTestsFailed, stderr.String())
	}
}

// TestRunCommand_NilWriteFileFunc_IsReportedAsError asserts that a nil
// Options.WriteFile is treated as a report-write failure (reported on stderr)
// rather than silently skipped. A wiring omission must be visible.
func TestRunCommand_NilWriteFileFunc_IsReportedAsError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("nil-wf-suite")}
	opts := withReportFile(
		baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner),
		"report.json",
		nil, // deliberately nil
	)

	cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	if stderr.String() == "" {
		t.Error("stderr is empty after a run with nil WriteFileFunc; the wiring omission must be reported, not silently skipped")
	}
}

// TestRunCommand_ReportNotWritten_WhenPreflightFails asserts that WriteFile
// is not called when pre-flight fails, since no run took place and there is
// no result to record.
//
// Note (TDD RED phase): this test passes vacuously before implementation.
// WriteFile is never called at all before I7.2 lands, so callCount() == 0 is
// trivially true. Once WriteFile is wired, the test becomes a meaningful guard
// that pre-flight failure skips the write, as required.
func TestRunCommand_ReportNotWritten_WhenPreflightFails(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	capture := &captureWriteFile{}
	runner := &fakeSuiteRunner{}
	opts := withReportFile(
		baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, failingReport(), nil), runner),
		"report.json",
		capture.write,
	)

	cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	if n := capture.callCount(); n != 0 {
		t.Errorf("WriteFile called %d times when pre-flight failed; want 0 (no run, no report)", n)
	}
}

// TestRunCommand_ReportPath_MissingValue_IsUsageError asserts that --report-path
// stated as the final argument with no following value is a usage error,
// consistent with every other value-consuming flag.
func TestRunCommand_ReportPath_MissingValue_IsUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--report-path"}, opts)

	if got != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for --report-path with no value; stderr=%s", got, cli.ExitUsage, stderr.String())
	}
}

// TestRunCommand_ReportPath_EmptyEqualsValue_IsUsageError asserts that
// --report-path= (equals form, explicitly empty value) is a usage error. The
// design states that an empty report-path value is not equivalent to "unset":
// present-and-empty must be rejected, not silently treated as a no-op or as
// the default path.
func TestRunCommand_ReportPath_EmptyEqualsValue_IsUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--report-path="}, opts)

	if got != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for --report-path= (empty value); stderr=%s", got, cli.ExitUsage, stderr.String())
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// containsAll reports whether s contains every item in wants.
func containsAll(s string, wants ...string) bool {
	for _, w := range wants {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
}

// Compile-time check that captureWriteFile.write satisfies cli.WriteFileFunc.
var _ cli.WriteFileFunc = (*captureWriteFile)(nil).write
