package cli_test

// Tests for Stage 3: CLI Per-Command Flag Scoping.
//
// Each command must validate only its own flag set. Pre-scanned flags
// (consumed by the composition root before CLI dispatch) are accepted on every
// command. Flags specific to one command are rejected on all others. The
// store command enforces mutual exclusion between --dir and positional file
// arguments. The --help surface documents store and summary under a visible
// "Process Test Reports" group.

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/resultstore"
	"mosaic-agent-test/internal/resultsummary"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// fakeStoreFunc returns a StoreFunc that records whether it was called and
// always succeeds with an empty result.
func fakeStoreFunc(called *bool) cli.StoreFunc {
	return func(req resultstore.StoreFromPathsRequest) (resultstore.StoreResult, error) {
		if called != nil {
			*called = true
		}
		return resultstore.StoreResult{}, nil
	}
}

// fakeSummaryFunc returns a SummaryFunc that records whether it was called
// and always succeeds with an empty result.
func fakeSummaryFunc(called *bool) cli.SummaryFunc {
	return func(req resultsummary.SummaryRequest) (resultsummary.SummaryResult, error) {
		if called != nil {
			*called = true
		}
		return resultsummary.SummaryResult{}, nil
	}
}

// storeBaseOpts returns an Options wired with the given Store and Summary
// funcs, plus the standard preflight/suite fakes the other cli tests use so
// store/summary tests inherit a consistent baseline.
func storeBaseOpts(stdout, stderr *bytes.Buffer, store cli.StoreFunc, summary cli.SummaryFunc) cli.Options {
	opts := baseOptions(
		stdout, stderr,
		scriptedPreflight(preflight.Plan{}, cleanReport(), nil),
		&fakeSuiteRunner{},
	)
	opts.Store = store
	opts.Summary = summary
	return opts
}

// ---------------------------------------------------------------------------
// T3.2: Pre-scanned flags are accepted on every command
// ---------------------------------------------------------------------------

// TestStore_PreScannedFlags_AreNotRejectedAsUnknown verifies that every
// pre-scanned flag (consumed by the composition root before CLI dispatch)
// does not produce an "unknown flag" usage error when passed to store.
// Pre-scanned flags must be in every command's recognised set.
func TestStore_PreScannedFlags_AreNotRejectedAsUnknown(t *testing.T) {
	cases := []struct {
		flag string
		args []string
	}{
		{"harness", []string{"store", "--harness", "claude-code", "file.json"}},
		{"logger-bundle", []string{"store", "--logger-bundle", "/path/to/bundle", "file.json"}},
		{"cost-tool", []string{"store", "--cost-tool", "/path/to/tool", "file.json"}},
		{"suites", []string{"store", "--suites", "/suites", "file.json"}},
		{"deploy-tool", []string{"store", "--deploy-tool", "/tool", "file.json"}},
		{"mosaic-root", []string{"store", "--mosaic-root", "/root", "file.json"}},
		{"catalog-folder", []string{"store", "--catalog-folder", "/catalog", "file.json"}},
		{"tui", []string{"store", "--tui", "file.json"}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.flag, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			var storeCalled bool
			opts := storeBaseOpts(stdout, stderr, fakeStoreFunc(&storeCalled), nil)

			code := cli.Execute(context.Background(), tc.args, opts)

			// A pre-scanned flag must not cause ExitUsage (unknown flag).
			// After implementation the store command runs and returns ExitSuccess;
			// any non-ExitUsage code is acceptable here because the RED condition
			// is the current ExitUsage from "unknown command: store".
			if code == cli.ExitUsage {
				t.Errorf("store --%s = ExitUsage, want any other code; stderr=%q",
					tc.flag, stderr.String())
			}
		})
	}
}

// TestSummary_PreScannedFlags_AreNotRejectedAsUnknown verifies the same
// property for the summary command.
func TestSummary_PreScannedFlags_AreNotRejectedAsUnknown(t *testing.T) {
	cases := []struct {
		flag string
		args []string
	}{
		{"harness", []string{"summary", "--harness", "claude-code"}},
		{"logger-bundle", []string{"summary", "--logger-bundle", "/path/to/bundle"}},
		{"cost-tool", []string{"summary", "--cost-tool", "/path/to/tool"}},
		{"suites", []string{"summary", "--suites", "/suites"}},
		{"deploy-tool", []string{"summary", "--deploy-tool", "/tool"}},
		{"mosaic-root", []string{"summary", "--mosaic-root", "/root"}},
		{"catalog-folder", []string{"summary", "--catalog-folder", "/catalog"}},
		{"tui", []string{"summary", "--tui"}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.flag, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			var summaryCalled bool
			opts := storeBaseOpts(stdout, stderr, nil, fakeSummaryFunc(&summaryCalled))

			code := cli.Execute(context.Background(), tc.args, opts)

			if code == cli.ExitUsage {
				t.Errorf("summary --%s = ExitUsage, want any other code; stderr=%q",
					tc.flag, stderr.String())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T3.1: store rejects run-only flags
// ---------------------------------------------------------------------------

// TestStore_RunOnlyFlags_AreRejected verifies that flags valid only on run
// and validate produce a usage error when passed to store, and that the error
// message identifies the flag rather than just saying "unknown command".
func TestStore_RunOnlyFlags_AreRejected(t *testing.T) {
	cases := []struct {
		flag string
		args []string
	}{
		{flag: "tests", args: []string{"store", "--tests", "a,b", "file.json"}},
		{flag: "timeout", args: []string{"store", "--timeout", "5m", "file.json"}},
		{flag: "repetitions", args: []string{"store", "--repetitions", "3", "file.json"}},
		{flag: "format", args: []string{"store", "--format", "json", "file.json"}},
		{flag: "fixtures", args: []string{"store", "--fixtures", "/fixtures", "file.json"}},
		{flag: "workspace-root", args: []string{"store", "--workspace-root", "/ws", "file.json"}},
		{flag: "keep-sandbox", args: []string{"store", "--keep-sandbox", "file.json"}},
		{flag: "keep-sandbox-on-failure", args: []string{"store", "--keep-sandbox-on-failure", "file.json"}},
		{flag: "no-report", args: []string{"store", "--no-report", "file.json"}},
		{flag: "max-concurrent-runs", args: []string{"store", "--max-concurrent-runs", "4", "file.json"}},
		{flag: "report-path", args: []string{"store", "--report-path", "/out.json", "file.json"}},
		{flag: "subject-model", args: []string{"store", "--subject-model", "claude-3", "file.json"}},
		{flag: "stub-model", args: []string{"store", "--stub-model", "claude-3", "file.json"}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.flag, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			opts := storeBaseOpts(stdout, stderr, fakeStoreFunc(nil), nil)

			code := cli.Execute(context.Background(), tc.args, opts)

			if code != cli.ExitUsage {
				t.Errorf("store --%s = %d, want ExitUsage (%d); stderr=%q",
					tc.flag, code, cli.ExitUsage, stderr.String())
			}
			// The error message must mention the flag name, not only "unknown command".
			// Currently (before implementation) the error says "unknown command 'store'"
			// which does NOT mention the flag. After implementation it says "unknown flag: --<name>".
			if !strings.Contains(stderr.String(), tc.flag) {
				t.Errorf("store --%s: stderr=%q does not mention flag name %q; got wrong rejection reason",
					tc.flag, stderr.String(), tc.flag)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T3.1: summary rejects run-only flags
// ---------------------------------------------------------------------------

// TestSummary_RunOnlyFlags_AreRejected verifies that flags valid only on run
// and validate produce a usage error when passed to summary.
func TestSummary_RunOnlyFlags_AreRejected(t *testing.T) {
	cases := []struct {
		flag string
		args []string
	}{
		{flag: "tests", args: []string{"summary", "--tests", "a,b"}},
		{flag: "timeout", args: []string{"summary", "--timeout", "5m"}},
		{flag: "repetitions", args: []string{"summary", "--repetitions", "3"}},
		{flag: "format", args: []string{"summary", "--format", "json"}},
		{flag: "fixtures", args: []string{"summary", "--fixtures", "/fixtures"}},
		{flag: "workspace-root", args: []string{"summary", "--workspace-root", "/ws"}},
		{flag: "keep-sandbox", args: []string{"summary", "--keep-sandbox"}},
		{flag: "keep-sandbox-on-failure", args: []string{"summary", "--keep-sandbox-on-failure"}},
		{flag: "no-report", args: []string{"summary", "--no-report"}},
		{flag: "max-concurrent-runs", args: []string{"summary", "--max-concurrent-runs", "4"}},
		{flag: "report-path", args: []string{"summary", "--report-path", "/out.json"}},
		{flag: "subject-model", args: []string{"summary", "--subject-model", "claude-3"}},
		{flag: "stub-model", args: []string{"summary", "--stub-model", "claude-3"}},
		{flag: "dir", args: []string{"summary", "--dir", "/reports"}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.flag, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			opts := storeBaseOpts(stdout, stderr, nil, fakeSummaryFunc(nil))

			code := cli.Execute(context.Background(), tc.args, opts)

			if code != cli.ExitUsage {
				t.Errorf("summary --%s = %d, want ExitUsage (%d); stderr=%q",
					tc.flag, code, cli.ExitUsage, stderr.String())
			}
			// The error message must mention the flag name so that the rejection
			// reason is identifiable (not just "unknown command"). Currently
			// (before implementation) summary is an unknown command so the flag
			// name is not mentioned; after implementation it will say "unknown
			// flag: --<name>".
			if !strings.Contains(stderr.String(), tc.flag) {
				t.Errorf("summary --%s: stderr=%q does not mention flag name %q; got wrong rejection reason",
					tc.flag, stderr.String(), tc.flag)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T3.1: run and validate reject store/summary-only flags
// ---------------------------------------------------------------------------

// TestRun_StoreOnlyFlag_IsRejected verifies that --dir, which is valid only
// for store, produces ExitUsage when passed to run.
func TestRun_StoreOnlyFlag_IsRejected(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("test-suite")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	code := cli.Execute(context.Background(), []string{"run", "--dir", "/reports", "suite.yaml"}, opts)

	if code != cli.ExitUsage {
		t.Errorf("run --dir = %d, want ExitUsage (%d); stderr=%q", code, cli.ExitUsage, stderr.String())
	}
}

// TestRun_SummaryOnlyFlag_IsRejected verifies that --for-version, which is
// valid only for summary, produces ExitUsage when passed to run.
func TestRun_SummaryOnlyFlag_IsRejected(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{result: passingResult("test-suite")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	code := cli.Execute(context.Background(), []string{"run", "--for-version", "v1.0", "suite.yaml"}, opts)

	if code != cli.ExitUsage {
		t.Errorf("run --for-version = %d, want ExitUsage (%d); stderr=%q", code, cli.ExitUsage, stderr.String())
	}
}

// TestValidate_StoreOnlyFlag_IsRejected verifies that --dir produces
// ExitUsage when passed to validate.
func TestValidate_StoreOnlyFlag_IsRejected(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), &fakeSuiteRunner{})

	code := cli.Execute(context.Background(), []string{"validate", "--dir", "/reports", "suite.yaml"}, opts)

	if code != cli.ExitUsage {
		t.Errorf("validate --dir = %d, want ExitUsage (%d); stderr=%q", code, cli.ExitUsage, stderr.String())
	}
}

// TestValidate_SummaryOnlyFlag_IsRejected verifies that --for-version
// produces ExitUsage when passed to validate.
func TestValidate_SummaryOnlyFlag_IsRejected(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), &fakeSuiteRunner{})

	code := cli.Execute(context.Background(), []string{"validate", "--for-version", "v1.0", "suite.yaml"}, opts)

	if code != cli.ExitUsage {
		t.Errorf("validate --for-version = %d, want ExitUsage (%d); stderr=%q", code, cli.ExitUsage, stderr.String())
	}
}

// ---------------------------------------------------------------------------
// T3.1: store accepts its own flags
// ---------------------------------------------------------------------------

// TestStore_DirFlag_IsAccepted verifies that --dir is accepted by the store
// command and the Store func is called. This is a TDD RED test: currently
// store is an unknown command and returns ExitUsage; after implementation
// store is recognised and ExitSuccess is returned.
func TestStore_DirFlag_IsAccepted(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	var storeCalled bool
	opts := storeBaseOpts(stdout, stderr, fakeStoreFunc(&storeCalled), nil)

	code := cli.Execute(context.Background(), []string{"store", "--dir", "/reports"}, opts)

	if code != cli.ExitSuccess {
		t.Errorf("store --dir = %d, want ExitSuccess (%d); stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}
	if !storeCalled {
		t.Error("store --dir: Store func was not called")
	}
}

// TestStore_PositionalFiles_AreAccepted verifies that positional file
// arguments (without --dir) are accepted by store and the Store func is
// called. This is a TDD RED test.
func TestStore_PositionalFiles_AreAccepted(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	var storeCalled bool
	opts := storeBaseOpts(stdout, stderr, fakeStoreFunc(&storeCalled), nil)

	code := cli.Execute(context.Background(), []string{"store", "file1.json", "file2.json"}, opts)

	if code != cli.ExitSuccess {
		t.Errorf("store file1.json file2.json = %d, want ExitSuccess (%d); stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}
	if !storeCalled {
		t.Error("store file1.json file2.json: Store func was not called")
	}
}

// TestSummary_ForVersionFlag_IsAccepted verifies that --for-version is
// accepted by the summary command and the Summary func is called. This is a
// TDD RED test.
func TestSummary_ForVersionFlag_IsAccepted(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	var summaryCalled bool
	opts := storeBaseOpts(stdout, stderr, nil, fakeSummaryFunc(&summaryCalled))

	code := cli.Execute(context.Background(), []string{"summary", "--for-version", "v1.2.3"}, opts)

	if code != cli.ExitSuccess {
		t.Errorf("summary --for-version = %d, want ExitSuccess (%d); stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}
	if !summaryCalled {
		t.Error("summary --for-version: Summary func was not called")
	}
}

// TestSummary_NoFlags_IsAccepted verifies that summary with no flags (scan
// all versions) is accepted and the Summary func is called.
func TestSummary_NoFlags_IsAccepted(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	var summaryCalled bool
	opts := storeBaseOpts(stdout, stderr, nil, fakeSummaryFunc(&summaryCalled))

	code := cli.Execute(context.Background(), []string{"summary"}, opts)

	if code != cli.ExitSuccess {
		t.Errorf("summary (no flags) = %d, want ExitSuccess (%d); stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}
	if !summaryCalled {
		t.Error("summary (no flags): Summary func was not called")
	}
}

// ---------------------------------------------------------------------------
// T3.3: store rejects --dir combined with positional file arguments
// ---------------------------------------------------------------------------

// TestStore_DirAndPositionals_IsUsageError verifies that specifying both
// --dir and positional file paths returns ExitUsage and does not invoke the
// Store func (mutual exclusion is caught before dispatch).
func TestStore_DirAndPositionals_IsUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	var storeCalled bool
	opts := storeBaseOpts(stdout, stderr, fakeStoreFunc(&storeCalled), nil)

	code := cli.Execute(context.Background(),
		[]string{"store", "--dir", "/reports", "file.json"},
		opts)

	if code != cli.ExitUsage {
		t.Errorf("store --dir + positional = %d, want ExitUsage (%d); stderr=%q",
			code, cli.ExitUsage, stderr.String())
	}
	if storeCalled {
		t.Error("store --dir + positional: Store func was called but mutual exclusion should have prevented it")
	}
}

// TestStore_DirAndMultiplePositionals_IsUsageError verifies the same mutual
// exclusion constraint with multiple positional arguments.
func TestStore_DirAndMultiplePositionals_IsUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	var storeCalled bool
	opts := storeBaseOpts(stdout, stderr, fakeStoreFunc(&storeCalled), nil)

	code := cli.Execute(context.Background(),
		[]string{"store", "--dir", "/reports", "a.json", "b.json", "c.json"},
		opts)

	if code != cli.ExitUsage {
		t.Errorf("store --dir + multiple positionals = %d, want ExitUsage (%d); stderr=%q",
			code, cli.ExitUsage, stderr.String())
	}
	if storeCalled {
		t.Error("store --dir + multiple positionals: Store func was called but mutual exclusion should have prevented it")
	}
}

// ---------------------------------------------------------------------------
// AC3.6: store and summary in --help with visible group
// ---------------------------------------------------------------------------

// TestHelp_IncludesStoreCommand verifies that --help output contains the
// "store" command. This is a TDD RED test: currently commandSpecs has only
// run and validate.
func TestHelp_IncludesStoreCommand(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, nil, nil)

	code := cli.Execute(context.Background(), []string{"--help"}, opts)

	if code != cli.ExitSuccess {
		t.Errorf("--help = %d, want ExitSuccess; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "store") {
		t.Errorf("--help output does not contain \"store\": %q", stdout.String())
	}
}

// TestHelp_IncludesSummaryCommand verifies that --help output contains the
// "summary" command.
func TestHelp_IncludesSummaryCommand(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, nil, nil)

	code := cli.Execute(context.Background(), []string{"--help"}, opts)

	if code != cli.ExitSuccess {
		t.Errorf("--help = %d, want ExitSuccess; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "summary") {
		t.Errorf("--help output does not contain \"summary\": %q", stdout.String())
	}
}

// TestHelp_ShowsProcessTestReportsGroupHeader verifies that --help output
// contains a visible "Process Test Reports" section header that presents
// store and summary as a distinct capability group.
func TestHelp_ShowsProcessTestReportsGroupHeader(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, nil, nil)

	code := cli.Execute(context.Background(), []string{"--help"}, opts)

	if code != cli.ExitSuccess {
		t.Errorf("--help = %d, want ExitSuccess; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Process Test Reports") {
		t.Errorf("--help output does not contain \"Process Test Reports\" group header: %q",
			stdout.String())
	}
}

// TestHelp_StoreSummaryGroupedAfterRunValidate verifies that the help output
// shows run and validate before the "Process Test Reports" group header,
// meaning ungrouped commands appear first.
func TestHelp_StoreSummaryGroupedAfterRunValidate(t *testing.T) {
	stdout, _ := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, &bytes.Buffer{}, nil, nil)
	cli.Execute(context.Background(), []string{"--help"}, opts)

	out := stdout.String()
	runIdx := strings.Index(out, "run ")
	groupIdx := strings.Index(out, "Process Test Reports")
	storeIdx := strings.Index(out, "store")

	if runIdx < 0 {
		t.Fatal("--help output does not mention 'run'")
	}
	if groupIdx < 0 {
		t.Fatal("--help output does not contain 'Process Test Reports' group header")
	}
	if storeIdx < 0 {
		t.Fatal("--help output does not mention 'store'")
	}
	if runIdx >= groupIdx {
		t.Errorf("'run' appears at index %d, 'Process Test Reports' at %d: ungrouped commands should appear before the group header",
			runIdx, groupIdx)
	}
	if storeIdx <= groupIdx {
		t.Errorf("'store' appears at index %d, 'Process Test Reports' at %d: grouped commands should appear after the group header",
			storeIdx, groupIdx)
	}
}

// TestHelp_IncludesDirFlag verifies that --help documents the --dir flag
// (used by store to specify a report directory).
func TestHelp_IncludesDirFlag(t *testing.T) {
	stdout, _ := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, &bytes.Buffer{}, nil, nil)
	cli.Execute(context.Background(), []string{"--help"}, opts)

	if !strings.Contains(stdout.String(), "--dir") {
		t.Errorf("--help output does not document --dir flag: %q", stdout.String())
	}
}

// TestHelp_IncludesForVersionFlag verifies that --help documents the
// --for-version flag (used by summary to restrict summary to one version).
func TestHelp_IncludesForVersionFlag(t *testing.T) {
	stdout, _ := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, &bytes.Buffer{}, nil, nil)
	cli.Execute(context.Background(), []string{"--help"}, opts)

	if !strings.Contains(stdout.String(), "--for-version") {
		t.Errorf("--help output does not document --for-version flag: %q", stdout.String())
	}
}

// ---------------------------------------------------------------------------
// cli.Commands() introspection
// ---------------------------------------------------------------------------

// TestCommands_IncludesStore verifies that cli.Commands() returns an entry
// for the store command.
func TestCommands_IncludesStore(t *testing.T) {
	cmds := cli.Commands()
	for _, c := range cmds {
		if c.Name == "store" {
			return // found
		}
	}
	t.Errorf("cli.Commands() = %+v, want an entry named \"store\"", cmds)
}

// TestCommands_IncludesSummary verifies that cli.Commands() returns an entry
// for the summary command.
func TestCommands_IncludesSummary(t *testing.T) {
	cmds := cli.Commands()
	for _, c := range cmds {
		if c.Name == "summary" {
			return // found
		}
	}
	t.Errorf("cli.Commands() = %+v, want an entry named \"summary\"", cmds)
}

// TestCommands_StoreHasGroupField verifies that the store entry in
// cli.Commands() has a non-empty Group field identifying the "Process Test
// Reports" capability group.
func TestCommands_StoreHasGroupField(t *testing.T) {
	cmds := cli.Commands()
	for _, c := range cmds {
		if c.Name == "store" {
			if c.Group == "" {
				t.Errorf("cli.Commands() entry \"store\" has empty Group, want \"Process Test Reports\"")
			}
			return
		}
	}
	t.Errorf("cli.Commands() has no \"store\" entry")
}

// TestCommands_SummaryHasGroupField verifies that the summary entry in
// cli.Commands() has a non-empty Group field.
func TestCommands_SummaryHasGroupField(t *testing.T) {
	cmds := cli.Commands()
	for _, c := range cmds {
		if c.Name == "summary" {
			if c.Group == "" {
				t.Errorf("cli.Commands() entry \"summary\" has empty Group, want \"Process Test Reports\"")
			}
			return
		}
	}
	t.Errorf("cli.Commands() has no \"summary\" entry")
}

// TestCommands_StoreAndSummaryShareGroup verifies that store and summary
// share the same Group value (they are presented together as one capability).
func TestCommands_StoreAndSummaryShareGroup(t *testing.T) {
	cmds := cli.Commands()
	groups := map[string]string{}
	for _, c := range cmds {
		if c.Name == "store" || c.Name == "summary" {
			groups[c.Name] = c.Group
		}
	}
	if len(groups) < 2 {
		t.Skipf("need both store and summary in Commands(); found %v", groups)
	}
	if groups["store"] != groups["summary"] {
		t.Errorf("store Group=%q, summary Group=%q; want them equal", groups["store"], groups["summary"])
	}
}

// TestCommands_RunAndValidateHaveNoGroup verifies that the existing run and
// validate commands have an empty Group field (they are ungrouped).
func TestCommands_RunAndValidateHaveNoGroup(t *testing.T) {
	cmds := cli.Commands()
	for _, c := range cmds {
		if c.Name == "run" || c.Name == "validate" {
			if c.Group != "" {
				t.Errorf("cli.Commands() entry %q has Group=%q, want empty (ungrouped)",
					c.Name, c.Group)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// AC3.1 / AC3.7: existing run/validate behavior is preserved
// ---------------------------------------------------------------------------

// TestRun_ExistingFlags_StillAccepted verifies that refactoring parseInvocation
// for per-command scoping does not break acceptance of any flag run already
// accepts. This documents the invariant: the run flag set is unchanged.
func TestRun_ExistingFlags_StillAccepted(t *testing.T) {
	cases := []struct {
		flag string
		args []string
	}{
		{"tests", []string{"run", "--tests", "a,b", "suite.yaml"}},
		{"format", []string{"run", "--format", "text", "suite.yaml"}},
		{"fixtures", []string{"run", "--fixtures", "/f", "suite.yaml"}},
		{"workspace-root", []string{"run", "--workspace-root", "/ws", "suite.yaml"}},
		{"timeout", []string{"run", "--timeout", "5m", "suite.yaml"}},
		{"repetitions", []string{"run", "--repetitions", "2", "suite.yaml"}},
		{"keep-sandbox", []string{"run", "--keep-sandbox", "suite.yaml"}},
		{"keep-sandbox-on-failure", []string{"run", "--keep-sandbox-on-failure", "suite.yaml"}},
		{"no-report", []string{"run", "--no-report", "suite.yaml"}},
		{"logger-bundle", []string{"run", "--logger-bundle", "/bundle", "suite.yaml"}},
		{"cost-tool", []string{"run", "--cost-tool", "/tool", "suite.yaml"}},
		{"tui", []string{"run", "--tui", "suite.yaml"}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.flag, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			runner := &fakeSuiteRunner{result: passingResult("suite")}
			opts := baseOptions(stdout, stderr,
				scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

			code := cli.Execute(context.Background(), tc.args, opts)

			if code == cli.ExitUsage {
				t.Errorf("run --%s = ExitUsage, want accepted; stderr=%q",
					tc.flag, stderr.String())
			}
		})
	}
}

// TestValidate_ExistingFlags_StillAccepted verifies that the same flags run
// accepts are also accepted by validate after the per-command refactor.
// Run and validate share the same flag set; this is a direct regression guard
// for the parseInvocation refactor (AC3.7).
func TestValidate_ExistingFlags_StillAccepted(t *testing.T) {
	cases := []struct {
		flag string
		args []string
	}{
		{"tests", []string{"validate", "--tests", "a,b", "suite.yaml"}},
		{"format", []string{"validate", "--format", "text", "suite.yaml"}},
		{"fixtures", []string{"validate", "--fixtures", "/f", "suite.yaml"}},
		{"workspace-root", []string{"validate", "--workspace-root", "/ws", "suite.yaml"}},
		{"timeout", []string{"validate", "--timeout", "5m", "suite.yaml"}},
		{"repetitions", []string{"validate", "--repetitions", "2", "suite.yaml"}},
		{"keep-sandbox", []string{"validate", "--keep-sandbox", "suite.yaml"}},
		{"keep-sandbox-on-failure", []string{"validate", "--keep-sandbox-on-failure", "suite.yaml"}},
		{"no-report", []string{"validate", "--no-report", "suite.yaml"}},
		{"logger-bundle", []string{"validate", "--logger-bundle", "/bundle", "suite.yaml"}},
		{"cost-tool", []string{"validate", "--cost-tool", "/tool", "suite.yaml"}},
		{"tui", []string{"validate", "--tui", "suite.yaml"}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.flag, func(t *testing.T) {
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			opts := baseOptions(stdout, stderr,
				scriptedPreflight(preflight.Plan{}, cleanReport(), nil), &fakeSuiteRunner{})

			code := cli.Execute(context.Background(), tc.args, opts)

			if code == cli.ExitUsage {
				t.Errorf("validate --%s = ExitUsage, want accepted; stderr=%q",
					tc.flag, stderr.String())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Edge case: store with no arguments and no --dir
// ---------------------------------------------------------------------------

// TestStore_NoArgs_Behavior pins down the contract for store invoked with zero
// positional arguments and no --dir flag. This is a usage error: the caller
// has not told store what to process. The Store func must not be called.
//
// This is a TDD RED test: currently store is an unknown command and returns
// ExitUsage for the wrong reason ("unknown command"), but the exit code matches.
// After implementation, ExitUsage must still be returned because no input was
// provided.
func TestStore_NoArgs_Behavior(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	var storeCalled bool
	opts := storeBaseOpts(stdout, stderr, fakeStoreFunc(&storeCalled), nil)

	code := cli.Execute(context.Background(), []string{"store"}, opts)

	if code != cli.ExitUsage {
		t.Errorf("store (no args) = %d, want ExitUsage (%d); stderr=%q",
			code, cli.ExitUsage, stderr.String())
	}
	if storeCalled {
		t.Error("store (no args): Store func was called but should not be invoked with no input")
	}
}
