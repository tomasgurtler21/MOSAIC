package cli_test

// Tests for the usage surface: --help and a bare invocation must both
// produce a real usage surface — the documented commands and flags — rather
// than the bare "unknown command" error the command surface currently
// produces for either input. The interceptor route must appear in neither.
//
// cli.Commands, cli.Flags, cli.ValueConsumingFlags and cli.Usage are the
// single source three consumers currently duplicate (this package's own
// flag parser, the composition root's positional detection, and the usage
// text itself); these tests pin their content directly, independent of how
// Execute wires them in.

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"mosaic-agent-test/internal/cli"
)

// documentedFlagNames are every flag this stage's command surface must
// document, per ContractsDesign.md's command-surface table: the flags that
// existed before this stage, plus the two path overrides this stage adds
// (--logger-bundle, --cost-tool) and the help flag itself. Retention flags
// (--keep-sandbox, --keep-sandbox-on-failure) are a later stage's addition
// and are deliberately not asserted here.
var documentedFlagNames = []string{
	"--tests", "--harness", "--format", "--fixtures", "--workspace-root",
	"--timeout", "--repetitions", "--tui",
	"--logger-bundle", "--cost-tool",
	"--help",
}

// ---------------------------------------------------------------------------
// cli.Commands
// ---------------------------------------------------------------------------

// TestCommands_DocumentsRunAndValidate asserts the two documented commands
// appear, each with its argument form.
func TestCommands_DocumentsRunAndValidate(t *testing.T) {
	cmds := cli.Commands()

	byName := map[string]cli.CommandSpec{}
	for _, c := range cmds {
		byName[c.Name] = c
	}

	for _, name := range []string{"run", "validate"} {
		c, ok := byName[name]
		if !ok {
			t.Errorf("cli.Commands() = %+v, want an entry named %q", cmds, name)
			continue
		}
		if c.Args == "" {
			t.Errorf("cli.Commands() entry %q has empty Args, want an argument form such as \"<suite>\"", name)
		}
	}
}

// TestCommands_NeverDocumentsTheInterceptorRoute asserts the interceptor
// subcommand ("intercept") is absent from every command entry's Name — the
// one hard constraint on help: the subject under test may read this
// binary's own help text.
func TestCommands_NeverDocumentsTheInterceptorRoute(t *testing.T) {
	for _, c := range cli.Commands() {
		if strings.Contains(strings.ToLower(c.Name), "intercept") {
			t.Errorf("cli.Commands() documents %+v; the interceptor route must never appear in the help surface", c)
		}
	}
}

// ---------------------------------------------------------------------------
// cli.Flags
// ---------------------------------------------------------------------------

// TestFlags_DocumentsEveryFlagThisStageAdds asserts every flag named in
// documentedFlagNames appears exactly once.
func TestFlags_DocumentsEveryFlagThisStageAdds(t *testing.T) {
	flags := cli.Flags()

	counts := map[string]int{}
	for _, f := range flags {
		counts[f.Name]++
	}

	for _, name := range documentedFlagNames {
		if counts[name] != 1 {
			t.Errorf("cli.Flags() names %q %d times, want exactly 1; flags=%+v", name, counts[name], flags)
		}
	}
}

// TestFlags_BooleanFlagsConsumeNoValue asserts --tui and --help are boolean:
// ConsumesValue false and no placeholder, matching the flags this package's
// own parser must not treat as value-consuming.
func TestFlags_BooleanFlagsConsumeNoValue(t *testing.T) {
	byName := map[string]cli.FlagSpec{}
	for _, f := range cli.Flags() {
		byName[f.Name] = f
	}

	for _, name := range []string{"--tui", "--help"} {
		f, ok := byName[name]
		if !ok {
			t.Fatalf("cli.Flags() has no entry named %q", name)
		}
		if f.ConsumesValue {
			t.Errorf("cli.Flags() entry %q has ConsumesValue = true, want false (boolean flag)", name)
		}
	}
}

// TestFlags_PathOverrideFlagsConsumeAValue asserts --logger-bundle and
// --cost-tool are documented as value-consuming flags with a placeholder,
// consistent with every other path/id/duration flag on the surface.
func TestFlags_PathOverrideFlagsConsumeAValue(t *testing.T) {
	byName := map[string]cli.FlagSpec{}
	for _, f := range cli.Flags() {
		byName[f.Name] = f
	}

	for _, name := range []string{"--logger-bundle", "--cost-tool"} {
		f, ok := byName[name]
		if !ok {
			t.Fatalf("cli.Flags() has no entry named %q", name)
		}
		if !f.ConsumesValue {
			t.Errorf("cli.Flags() entry %q has ConsumesValue = false, want true", name)
		}
		if f.Placeholder == "" {
			t.Errorf("cli.Flags() entry %q has an empty Placeholder, want a value placeholder such as \"<dir>\"", name)
		}
	}
}

// ---------------------------------------------------------------------------
// cli.ValueConsumingFlags
// ---------------------------------------------------------------------------

// TestValueConsumingFlags_IsDerivedFromFlags asserts ValueConsumingFlags
// names exactly the flags Flags reports with ConsumesValue true — neither
// more nor fewer — so the two cannot drift into two different lists.
func TestValueConsumingFlags_IsDerivedFromFlags(t *testing.T) {
	want := map[string]bool{}
	for _, f := range cli.Flags() {
		if f.ConsumesValue {
			want[f.Name] = true
		}
	}

	got := map[string]bool{}
	for _, name := range cli.ValueConsumingFlags() {
		got[name] = true
	}

	for name := range want {
		if !got[name] {
			t.Errorf("cli.ValueConsumingFlags() is missing %q, which cli.Flags() reports as ConsumesValue", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("cli.ValueConsumingFlags() names %q, which cli.Flags() does not report as ConsumesValue", name)
		}
	}
}

// TestValueConsumingFlags_IncludesTheNewPathOverrides asserts the two flags
// this stage adds are recognised by positional detection — AC6.6 — stated
// directly against the shared list rather than only through the derived
// property above.
func TestValueConsumingFlags_IncludesTheNewPathOverrides(t *testing.T) {
	names := map[string]bool{}
	for _, n := range cli.ValueConsumingFlags() {
		names[n] = true
	}

	for _, want := range []string{"--logger-bundle", "--cost-tool"} {
		if !names[want] {
			t.Errorf("cli.ValueConsumingFlags() = %v, want it to include %q", cli.ValueConsumingFlags(), want)
		}
	}
}

// ---------------------------------------------------------------------------
// cli.Usage
// ---------------------------------------------------------------------------

// TestUsage_WritesEveryDocumentedCommandAndFlag asserts the rendered usage
// text names every documented command and flag.
func TestUsage_WritesEveryDocumentedCommandAndFlag(t *testing.T) {
	var buf bytes.Buffer

	if err := cli.Usage(&buf, cli.Options{}); err != nil {
		t.Fatalf("cli.Usage: %v", err)
	}
	out := buf.String()

	for _, name := range []string{"run", "validate"} {
		if !strings.Contains(out, name) {
			t.Errorf("cli.Usage output does not mention command %q\noutput:\n%s", name, out)
		}
	}
	for _, name := range documentedFlagNames {
		if !strings.Contains(out, name) {
			t.Errorf("cli.Usage output does not mention flag %q\noutput:\n%s", name, out)
		}
	}
}

// TestUsage_NeverMentionsTheInterceptorRoute asserts the rendered usage
// text contains no mention of the interceptor route, under any casing.
func TestUsage_NeverMentionsTheInterceptorRoute(t *testing.T) {
	var buf bytes.Buffer

	if err := cli.Usage(&buf, cli.Options{}); err != nil {
		t.Fatalf("cli.Usage: %v", err)
	}

	if strings.Contains(strings.ToLower(buf.String()), "intercept") {
		t.Errorf("cli.Usage output mentions the interceptor route\noutput:\n%s", buf.String())
	}
}

// ---------------------------------------------------------------------------
// cli.Execute — the observable surface a real invocation drives
// ---------------------------------------------------------------------------

// TestExecute_HelpFlag_ShowsUsageAndExitsSuccess asserts --help produces the
// usage surface and a success exit code — an explicit request for help is
// not a malformed invocation.
func TestExecute_HelpFlag_ShowsUsageAndExitsSuccess(t *testing.T) {
	var stdout, stderr bytes.Buffer

	got := cli.Execute(context.Background(), []string{"--help"}, cli.Options{Stdout: &stdout, Stderr: &stderr})

	if got != cli.ExitSuccess {
		t.Errorf("Execute([--help], ...) = %d, want ExitSuccess (%d); stdout=%s stderr=%s", got, cli.ExitSuccess, stdout.String(), stderr.String())
	}
	combined := stdout.String() + stderr.String()
	for _, name := range []string{"run", "validate", "--timeout", "--logger-bundle", "--cost-tool"} {
		if !strings.Contains(combined, name) {
			t.Errorf("Execute([--help], ...) output does not mention %q\nstdout:\n%s\nstderr:\n%s", name, stdout.String(), stderr.String())
		}
	}
}

// TestExecute_ShortHelpFlag_ShowsUsageAndExitsSuccess asserts -h is
// equivalent to --help.
func TestExecute_ShortHelpFlag_ShowsUsageAndExitsSuccess(t *testing.T) {
	var stdout, stderr bytes.Buffer

	got := cli.Execute(context.Background(), []string{"-h"}, cli.Options{Stdout: &stdout, Stderr: &stderr})

	if got != cli.ExitSuccess {
		t.Errorf("Execute([-h], ...) = %d, want ExitSuccess (%d); stdout=%s stderr=%s", got, cli.ExitSuccess, stdout.String(), stderr.String())
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "run") || !strings.Contains(combined, "validate") {
		t.Errorf("Execute([-h], ...) output does not document both commands\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
}

// TestExecute_BareInvocation_ShowsFullUsageSurface asserts a bare invocation
// (no arguments) shows the same usage surface --help does, rather than only
// the terse "no command given" error the command surface produces today.
func TestExecute_BareInvocation_ShowsFullUsageSurface(t *testing.T) {
	var stdout, stderr bytes.Buffer

	got := cli.Execute(context.Background(), []string{}, cli.Options{Stdout: &stdout, Stderr: &stderr})

	if got != cli.ExitUsage {
		t.Errorf("Execute([], ...) = %d, want ExitUsage (%d) for an invocation naming no command", got, cli.ExitUsage)
	}
	combined := stdout.String() + stderr.String()
	for _, name := range []string{"run", "validate", "--timeout", "--repetitions", "--logger-bundle", "--cost-tool"} {
		if !strings.Contains(combined, name) {
			t.Errorf("Execute([], ...) output does not mention %q — a bare invocation must show usage, not only a bare error\nstdout:\n%s\nstderr:\n%s", name, stdout.String(), stderr.String())
		}
	}
}

// TestExecute_HelpFlagOutput_NeverMentionsTheInterceptorRoute asserts the
// hard constraint on help holds through the full Execute path for an
// explicit --help request, not only through cli.Usage in isolation.
func TestExecute_HelpFlagOutput_NeverMentionsTheInterceptorRoute(t *testing.T) {
	var stdout, stderr bytes.Buffer

	cli.Execute(context.Background(), []string{"--help"}, cli.Options{Stdout: &stdout, Stderr: &stderr})

	combined := strings.ToLower(stdout.String() + stderr.String())
	if strings.Contains(combined, "intercept") {
		t.Errorf("Execute([--help], ...) output mentions the interceptor route\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
}
