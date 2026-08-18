package claudecode_test

// Tests for interpreter resolution, substitution and validation failure
// (T10.3).
//
// The bundle's published fragment invokes a Python interpreter that is
// frequently absent under the name it uses, so the interpreter is a
// resolved, validated setting substituted into the generated configuration
// rather than pasted through. A wrong interpreter yields a run with no logs
// and therefore no cost — a silent failure that must surface before any
// agent is spawned, never after.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"mosaic-agent-test/internal/harness/claudecode"
)

// TestResolveInterpreter_ConfiguredIsHonoured asserts that an explicitly
// configured interpreter is used as given, and reported with source
// InterpreterConfigured rather than resolved from candidates.
func TestResolveInterpreter_ConfiguredIsHonoured(t *testing.T) {
	got, err := claudecode.ResolveInterpreter(context.Background(), "py")
	if err != nil {
		t.Fatalf("ResolveInterpreter: %v", err)
	}
	if got.Command != "py" {
		t.Errorf("ResolveInterpreter: Command = %q, want %q", got.Command, "py")
	}
	if got.Source != claudecode.InterpreterConfigured {
		t.Errorf("ResolveInterpreter: Source = %q, want %q", got.Source, claudecode.InterpreterConfigured)
	}
}

// TestResolveInterpreter_ResolvesAcrossCandidates asserts that, with no
// interpreter configured, resolution tries the plausible candidate names in
// order and succeeds with the first one that actually runs.
func TestResolveInterpreter_ResolvesAcrossCandidates(t *testing.T) {
	got, err := claudecode.ResolveInterpreter(context.Background(), "")
	if err != nil {
		t.Fatalf("ResolveInterpreter: %v", err)
	}
	if got.Source != claudecode.InterpreterResolved {
		t.Errorf("ResolveInterpreter: Source = %q, want %q", got.Source, claudecode.InterpreterResolved)
	}
	found := false
	for _, candidate := range claudecode.InterpreterCandidates {
		if got.Command == candidate {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ResolveInterpreter: resolved command %q is not one of the declared candidates %v", got.Command, claudecode.InterpreterCandidates)
	}
	if len(got.Tried) == 0 {
		t.Errorf("ResolveInterpreter: Tried is empty; the diagnostic must name every candidate examined")
	}
}

// TestResolveInterpreter_UnresolvableFailsWithDiagnosticMessage asserts that
// an interpreter that cannot be resolved or does not run fails the
// environment check with ErrInterpreterUnresolved and a message naming what
// was tried.
func TestResolveInterpreter_UnresolvableFailsWithDiagnosticMessage(t *testing.T) {
	const bogus = "definitely-not-a-real-interpreter-binary-xyz"

	_, err := claudecode.ResolveInterpreter(context.Background(), bogus)
	if err == nil {
		t.Fatalf("ResolveInterpreter: want error for an interpreter that cannot run, got nil")
	}
	if !errors.Is(err, claudecode.ErrInterpreterUnresolved) {
		t.Errorf("ResolveInterpreter: error %v does not wrap ErrInterpreterUnresolved", err)
	}
	if !strings.Contains(err.Error(), bogus) {
		t.Errorf("ResolveInterpreter: error message %q does not name what was tried (%q)", err.Error(), bogus)
	}
}

// TestResolveInterpreter_ShimThatFailsToRunIsRejected asserts that a
// candidate is accepted only after it has been executed successfully, not
// merely found on the search path: a shim that resolves and then fails to
// run is exactly the failure this check exists to catch. This test names
// the property; the fixture for a failing shim is provided by the
// implementation pass's own test harness once ResolveInterpreter shells out.
func TestResolveInterpreter_ShimThatFailsToRunIsRejected(t *testing.T) {
	t.Skip("claudecode: exercised once ResolveInterpreter executes candidates via a seam the test can substitute (I10.4)")
}

// TestSubstituteInterpreter_ReplacesEveryInvokingEntry asserts that the
// resolved interpreter is substituted into every generated entry that
// invokes the published command, and that the count of changed entries is
// reported so a substitution that silently matched nothing is detectable.
func TestSubstituteInterpreter_ReplacesEveryInvokingEntry(t *testing.T) {
	timeout := 30
	async := true
	s := claudecode.Settings{
		Hooks: map[string][]claudecode.Matcher{
			"PreToolUse": {
				{Hooks: []claudecode.Entry{{Type: "command", Command: claudecode.PublishedInterpreter, Args: []string{"hook.py"}, Async: &async, Timeout: &timeout}}},
			},
			"SessionStart": {
				{Hooks: []claudecode.Entry{{Type: "command", Command: claudecode.PublishedInterpreter, Args: []string{"hook.py"}, Async: &async, Timeout: &timeout}}},
			},
			"Stop": {
				// Not an interpreter invocation; must be left untouched.
				{Hooks: []claudecode.Entry{{Type: "command", Command: "some-other-tool"}}},
			},
		},
	}

	got, changed := claudecode.SubstituteInterpreter(s, claudecode.PublishedInterpreter, "py")

	if changed != 2 {
		t.Fatalf("SubstituteInterpreter: changed = %d, want 2 (PreToolUse and SessionStart entries invoke the published interpreter)", changed)
	}
	if got.Hooks["PreToolUse"][0].Hooks[0].Command != "py" {
		t.Errorf("SubstituteInterpreter: PreToolUse entry command = %q, want %q", got.Hooks["PreToolUse"][0].Hooks[0].Command, "py")
	}
	if got.Hooks["SessionStart"][0].Hooks[0].Command != "py" {
		t.Errorf("SubstituteInterpreter: SessionStart entry command = %q, want %q", got.Hooks["SessionStart"][0].Hooks[0].Command, "py")
	}
	if got.Hooks["Stop"][0].Hooks[0].Command != "some-other-tool" {
		t.Errorf("SubstituteInterpreter: non-interpreter entry was modified: %q", got.Hooks["Stop"][0].Hooks[0].Command)
	}
}

// TestSubstituteInterpreter_NoMatchingEntriesReportsZero asserts that a
// substitution that matches nothing is detectable via the returned count,
// rather than silently reporting success.
func TestSubstituteInterpreter_NoMatchingEntriesReportsZero(t *testing.T) {
	s := claudecode.Settings{
		Hooks: map[string][]claudecode.Matcher{
			"Stop": {{Hooks: []claudecode.Entry{{Type: "command", Command: "already-resolved-interpreter"}}}},
		},
	}

	_, changed := claudecode.SubstituteInterpreter(s, claudecode.PublishedInterpreter, "py")
	if changed != 0 {
		t.Errorf("SubstituteInterpreter: changed = %d, want 0 when no entry invokes the published command", changed)
	}
}
