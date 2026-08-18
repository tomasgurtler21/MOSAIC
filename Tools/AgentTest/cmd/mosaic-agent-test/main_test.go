package main

// Tests for the binary composition root: interceptor routing, frontend
// dispatch, harness/launcher wiring and exit-code propagation.
//
// These are white-box tests against package main's own unexported dispatch
// surface (selectFrontend, newAdapter, decoderFor, runFrontend) rather than
// against a running process, because that surface — not a spawned binary —
// is what this stage's tests must specify and where "compiles and fails" is
// verifiable without an external process. Whole-pipeline validation through
// the real entry point is the execution pass's e2e suite's job, not this
// file's.

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/harness/claudecode"
)

// alwaysTerminal and neverTerminal are fixed TerminalCheck seams for tests
// that don't care about the terminal outcome one way or the other, so each
// test's isTerminal makes its own dependency on the seam explicit.
func alwaysTerminal() (bool, bool) { return true, true }
func neverTerminal() (bool, bool)  { return false, false }

// ---------------------------------------------------------------------------
// T17.1 — Interceptor routing
// ---------------------------------------------------------------------------

// TestSelectFrontend_InterceptorSubcommand_RoutesToInterceptor verifies the
// interceptor route is selected before anything else, and — critically —
// that isTerminal is never consulted to reach that decision: a terminal
// check ahead of interceptor routing could launch a user interface into the
// pipe the harness is reading its reply from.
func TestSelectFrontend_InterceptorSubcommand_RoutesToInterceptor(t *testing.T) {
	isTerminalCalled := false
	isTerminal := func() (bool, bool) {
		isTerminalCalled = true
		return true, true
	}

	got := selectFrontend([]string{InterceptorSubcommand, "--control-dir", "C:/sandbox/control"}, isTerminal)

	if got != FrontendInterceptor {
		t.Errorf("selectFrontend(%q, ...) = %q, want %q", InterceptorSubcommand, got, FrontendInterceptor)
	}
	if isTerminalCalled {
		t.Error("isTerminal was consulted for an interceptor invocation; terminal detection must never run ahead of interceptor routing")
	}
}

// TestSelectFrontend_InterceptorSubcommand_WinsOverTUIOverride verifies the
// interceptor route is decided before frontend selection even considers the
// explicit "--tui" override: an interceptor invocation must never resolve
// to a frontend under any combination of trailing flags.
func TestSelectFrontend_InterceptorSubcommand_WinsOverTUIOverride(t *testing.T) {
	got := selectFrontend([]string{InterceptorSubcommand, "--tui"}, alwaysTerminal)
	if got != FrontendInterceptor {
		t.Errorf("selectFrontend([intercept --tui], ...) = %q, want %q", got, FrontendInterceptor)
	}
}

// TestSelectFrontend_InterceptorSubcommand_MustBeFirstArgument verifies
// "intercept" only routes to the interceptor when it is the first argument
// — a value that merely equals "intercept" elsewhere in the argument list
// (for example, a flag's value) must not be mistaken for the subcommand.
// This is the inverse of the routing rule: no ordinary invocation may be
// mistaken for an interceptor one.
func TestSelectFrontend_InterceptorSubcommand_MustBeFirstArgument(t *testing.T) {
	cases := [][]string{
		{"run", "--tests", InterceptorSubcommand},
		{"validate", "suite.yaml", "--harness", InterceptorSubcommand},
	}

	for _, args := range cases {
		t.Run("", func(t *testing.T) {
			got := selectFrontend(args, alwaysTerminal)
			if got == FrontendInterceptor {
				t.Errorf("selectFrontend(%v, ...) = %q; %q appearing after the first argument must not route to the interceptor", args, got, InterceptorSubcommand)
			}
		})
	}
}

// TestSelectFrontend_OrdinaryInvocations_NeverRouteToInterceptor is the
// inverse coverage AC17.2 calls for: a plain "run"/"validate" invocation,
// and a bare invocation with no arguments at all, must never resolve to
// FrontendInterceptor.
func TestSelectFrontend_OrdinaryInvocations_NeverRouteToInterceptor(t *testing.T) {
	cases := [][]string{
		{},
		{"run", "suite.yaml"},
		{"validate", "suite.yaml"},
		{"--tui"},
	}

	for _, args := range cases {
		t.Run("", func(t *testing.T) {
			got := selectFrontend(args, alwaysTerminal)
			if got == FrontendInterceptor {
				t.Errorf("selectFrontend(%v, ...) = %q, an ordinary invocation must never route to the interceptor", args, got)
			}
		})
	}
}

// TestOrdinaryHelpSurface_DoesNotMentionInterceptor verifies AC17.3: the
// interceptor entry point is absent from the ordinary help surface, since
// the subject under test may see this binary's usage output and the tool's
// transparency obligation extends to it. A bare invocation (no command) is
// what surfaces that usage text.
func TestOrdinaryHelpSurface_DoesNotMentionInterceptor(t *testing.T) {
	var stdout, stderr bytes.Buffer

	cli.Execute(context.Background(), []string{}, cli.Options{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	if strings.Contains(strings.ToLower(stdout.String()), InterceptorSubcommand) ||
		strings.Contains(strings.ToLower(stderr.String()), InterceptorSubcommand) {
		t.Errorf("ordinary usage output mentions %q; the interceptor route must stay absent from the help surface\nstdout: %s\nstderr: %s", InterceptorSubcommand, stdout.String(), stderr.String())
	}
}

// ---------------------------------------------------------------------------
// T17.2 — Frontend dispatch
// ---------------------------------------------------------------------------

// TestSelectFrontend_TUIOverride_WinsFromAnywhereInArguments verifies the
// explicit "--tui" override selects the interactive frontend regardless of
// where it appears among the arguments, and wins over a positional argument
// that would otherwise select the non-interactive frontend.
func TestSelectFrontend_TUIOverride_WinsFromAnywhereInArguments(t *testing.T) {
	cases := [][]string{
		{"--tui"},
		{"--tui", "run", "suite.yaml"},
		{"run", "suite.yaml", "--tui"},
		{"run", "--tests", "a,b", "--tui"},
	}

	for _, args := range cases {
		t.Run("", func(t *testing.T) {
			got := selectFrontend(args, neverTerminal)
			if got != FrontendTUI {
				t.Errorf("selectFrontend(%v, neverTerminal) = %q, want %q (--tui must win over a positional and not need a terminal)", args, got, FrontendTUI)
			}
		})
	}
}

// TestSelectFrontend_PositionalArgument_SelectsCLI verifies a positional
// argument (a command name) selects the non-interactive frontend without
// consulting the terminal seam.
func TestSelectFrontend_PositionalArgument_SelectsCLI(t *testing.T) {
	isTerminalCalled := false
	isTerminal := func() (bool, bool) {
		isTerminalCalled = true
		return true, true
	}

	got := selectFrontend([]string{"run", "suite.yaml"}, isTerminal)

	if got != FrontendCLI {
		t.Errorf("selectFrontend([run suite.yaml], ...) = %q, want %q", got, FrontendCLI)
	}
	if isTerminalCalled {
		t.Error("isTerminal was consulted although a positional argument was present; a positional must decide dispatch on its own")
	}
}

// TestSelectFrontend_NoOverrideNoPositional_UsesTerminalCheck verifies that
// when neither the override nor a positional is present, both stdin and
// stdout must be terminals for the interactive frontend to be chosen —
// otherwise the non-interactive frontend is selected.
func TestSelectFrontend_NoOverrideNoPositional_UsesTerminalCheck(t *testing.T) {
	cases := []struct {
		name       string
		isTerminal TerminalCheck
		want       Frontend
	}{
		{"both streams are terminals", func() (bool, bool) { return true, true }, FrontendTUI},
		{"stdin only is a terminal", func() (bool, bool) { return true, false }, FrontendCLI},
		{"stdout only is a terminal", func() (bool, bool) { return false, true }, FrontendCLI},
		{"neither stream is a terminal", func() (bool, bool) { return false, false }, FrontendCLI},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := selectFrontend(nil, tc.isTerminal)
			if got != tc.want {
				t.Errorf("selectFrontend(nil, %s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// TestSelectFrontend_FlagAwarePositionalDetection verifies the ecosystem's
// known failure mode: a value token that follows a space-separated
// value-consuming flag (e.g. "--workspace-root C:/sandbox") must not be
// mistaken for a positional subcommand. The equals-separated form
// ("--flag=value") never produces a separate value token and is unaffected.
func TestSelectFrontend_FlagAwarePositionalDetection(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want Frontend
	}{
		{
			name: "space-separated value flag's value is not a positional",
			args: []string{"--workspace-root", "C:/sandbox"},
			want: FrontendTUI, // falls through to the terminal check; alwaysTerminal below
		},
		{
			name: "space-separated value flag followed by a real positional",
			args: []string{"--harness", "claude-code", "run"},
			want: FrontendCLI,
		},
		{
			name: "equals-separated value flag never produces a stray positional",
			args: []string{"--workspace-root=C:/sandbox"},
			want: FrontendTUI,
		},
		{
			name: "boolean flag consumes no following value",
			args: []string{"--tui", "--format", "json"},
			want: FrontendTUI,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := selectFrontend(tc.args, alwaysTerminal)
			if got != tc.want {
				t.Errorf("selectFrontend(%v, alwaysTerminal) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// TestValueConsumingFlags_NamesEveryFlagTheCLIAccepts verifies the named set
// positional detection consults contains every space-separated-value flag
// the CLI frontend's command surface accepts, per the Contracts design's
// command-surface table.
func TestValueConsumingFlags_NamesEveryFlagTheCLIAccepts(t *testing.T) {
	want := []string{"--tests", "--harness", "--format", "--fixtures", "--workspace-root", "--timeout", "--repetitions"}

	for _, flag := range want {
		if !valueConsumingFlags[flag] {
			t.Errorf("valueConsumingFlags[%q] = false, want true", flag)
		}
	}

	// A boolean flag must not be named here: it consumes no following value.
	if valueConsumingFlags["--tui"] {
		t.Error(`valueConsumingFlags["--tui"] = true, want false (--tui is boolean and consumes no value)`)
	}
}

// ---------------------------------------------------------------------------
// T17.3 — Dependency wiring: harness selection and the launcher's decoder
// ---------------------------------------------------------------------------

// TestNewAdapter_KnownHarness_ResolvesToMatchingAdapter verifies a
// recognised harness selection resolves to the corresponding adapter.
func TestNewAdapter_KnownHarness_ResolvesToMatchingAdapter(t *testing.T) {
	adapter, err := newAdapter(claudecode.HarnessID, adapterOptions{})
	if err != nil {
		t.Fatalf("newAdapter(%q, ...) returned an error: %v", claudecode.HarnessID, err)
	}
	if adapter == nil {
		t.Fatal("newAdapter returned a nil adapter with a nil error")
	}
	if got := adapter.ID(); got != claudecode.HarnessID {
		t.Errorf("adapter.ID() = %q, want %q", got, claudecode.HarnessID)
	}
}

// TestNewAdapter_UnrecognisedHarness_IsUsageError verifies AC17.6: an
// unrecognised harness selection is a usage error, never a silent
// substitution of some default harness. A test tool that measures the wrong
// harness produces results about the wrong thing.
func TestNewAdapter_UnrecognisedHarness_IsUsageError(t *testing.T) {
	adapter, err := newAdapter("not-a-real-harness", adapterOptions{})

	if err == nil {
		t.Fatal("newAdapter with an unrecognised harness returned a nil error; an unknown selection must be a usage error")
	}
	if !errors.Is(err, ErrUnknownHarness) {
		t.Errorf("newAdapter error = %v, want it to wrap %v", err, ErrUnknownHarness)
	}
	if adapter != nil {
		t.Errorf("newAdapter with an unrecognised harness returned a non-nil adapter %v; must never silently substitute a default", adapter)
	}
}

// TestNewAdapter_EmptyHarness_IsUsageError verifies the empty string is
// treated as any other unrecognised selection rather than as "use the
// default", which would be exactly the silent substitution AC17.6 forbids.
func TestNewAdapter_EmptyHarness_IsUsageError(t *testing.T) {
	_, err := newAdapter("", adapterOptions{})
	if !errors.Is(err, ErrUnknownHarness) {
		t.Errorf("newAdapter(\"\", ...) error = %v, want it to wrap %v", err, ErrUnknownHarness)
	}
}

// TestDecoderFor_KnownHarness_MatchesTheAdapterPackagesDecoder verifies the
// launcher wiring half of AC17.9: the selected harness's decoder is that
// exact adapter package's exported envelope-decoding function — checked by
// function identity, not merely by behaviour, so a decoder that happens to
// behave the same as claudecode.DecodeEnvelope without being it still fails
// this test.
func TestDecoderFor_KnownHarness_MatchesTheAdapterPackagesDecoder(t *testing.T) {
	dec, err := decoderFor(claudecode.HarnessID)
	if err != nil {
		t.Fatalf("decoderFor(%q) returned an error: %v", claudecode.HarnessID, err)
	}
	if dec == nil {
		t.Fatal("decoderFor returned a nil decoder with a nil error")
	}

	gotName := runtime.FuncForPC(reflect.ValueOf(dec).Pointer()).Name()
	wantName := runtime.FuncForPC(reflect.ValueOf(claudecode.DecodeEnvelope).Pointer()).Name()
	if gotName != wantName {
		t.Errorf("decoderFor(%q) = %s, want the adapter's own claudecode.DecodeEnvelope (%s)", claudecode.HarnessID, gotName, wantName)
	}
}

// TestDecoderFor_UnrecognisedHarness_IsError verifies decoder resolution
// fails the same way adapter resolution does for an unrecognised harness:
// no decoder is available for a harness that resolves to no adapter.
func TestDecoderFor_UnrecognisedHarness_IsError(t *testing.T) {
	dec, err := decoderFor("not-a-real-harness")
	if err == nil {
		t.Fatal("decoderFor with an unrecognised harness returned a nil error")
	}
	if dec != nil {
		t.Error("decoderFor with an unrecognised harness returned a non-nil decoder")
	}
}

// ---------------------------------------------------------------------------
// T17.4 — Exit-code propagation
// ---------------------------------------------------------------------------

// TestRunFrontend_PropagatesEveryContractExitCodeUnchanged verifies every
// exit code a frontend may derive reaches runFrontend's return value
// unchanged, for every code the CLI frontend's contract defines. An exit
// code that is correct inside the frontend and lost in the composition root
// is worse than no exit code at all.
func TestRunFrontend_PropagatesEveryContractExitCodeUnchanged(t *testing.T) {
	codes := []int{cli.ExitSuccess, cli.ExitFailure, cli.ExitTestsFailed, cli.ExitUsage, cli.ExitPreflight}

	for _, code := range codes {
		code := code
		t.Run("", func(t *testing.T) {
			got := runFrontend(func() int { return code })
			if got != code {
				t.Errorf("runFrontend(func() int { return %d }) = %d, want %d unchanged", code, got, code)
			}
		})
	}
}

// TestRunFrontend_CallsTheGivenFunctionExactlyOnce guards against a
// pass-through implementation that retries, memoises across calls, or
// otherwise calls fn more than once — any of which could mask a frontend
// whose exit code is not idempotent to compute.
func TestRunFrontend_CallsTheGivenFunctionExactlyOnce(t *testing.T) {
	calls := 0
	runFrontend(func() int {
		calls++
		return cli.ExitSuccess
	})
	if calls != 1 {
		t.Errorf("fn was called %d times, want exactly 1", calls)
	}
}
