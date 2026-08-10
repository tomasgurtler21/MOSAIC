package e2e

// T18.10 — the harness-neutrality end-to-end check: the pipeline is driven
// end to end through the scripted adapter with the engine unchanged, and no
// package outside the harness family and the composition root refers to a
// concrete harness.
//
// This is largely already demonstrated by every other scenario in this
// suite: each one drives internal/cli.Execute (the real non-interactive
// entry point) with cli.Options.DefaultHarness set to "fake" and no other
// harness package referenced anywhere in this module's production code
// outside internal/harness/ and cmd/mosaic-agent-test (the one composition
// root that constructs a concrete adapter — see
// cmd/mosaic-agent-test/dispatch.go's newAdapter, which recognises only
// claude-code; "fake" is wired only here, in this test module's own
// RunScenario, precisely because a scripted adapter has no place in the
// shipped binary). What this test adds is a named, explicit assertion
// rather than an implicit property of every other test file: it drives
// tools/importcheck (the module's own import-boundary gate, documented in
// tools/importcheck/main.go) as a subprocess and asserts it passes, then
// runs one full scenario through the real entry point to pin "the pipeline
// runs end to end through the scripted adapter with no engine change" to a
// single, named test rather than leaving it implicit across the rest of
// this package.
//
// tools/importcheck classifies every package under internal/ and cmd/ only
// (see its own doc comment) — this e2e package itself is outside both
// trees, so its own direct import of internal/harness/fake (needed because
// Scenario.Script is typed fake.Options, per the Contracts design itself)
// is not a boundary the tool polices, and does not need a special-cased
// exemption to pass.

import (
	"context"
	"os/exec"
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
)

func TestHarnessNeutrality_ImportGate_PermitsNoConcreteHarnessOutsideCompositionRoot(t *testing.T) {
	root := moduleRoot(t)
	cmd := exec.CommandContext(context.Background(), "go", "run", "./tools/importcheck")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run ./tools/importcheck failed: %v\noutput:\n%s", err, out)
	}
}

func TestHarnessNeutrality_FullPipeline_RunsThroughScriptedAdapterWithNoEngineChange(t *testing.T) {
	sc := Scenario{
		Dir:       "testdata/e2e/happy-path",
		SuitePath: "happy-path.suite.yaml",
		Script: fakeOptionsFor(
			domain.HarnessCapabilities{SupportsDirectSubstitution: true},
			[]subjectStep{
				invoke("dispatch", "greeter"),
				finish(`{"agent_instance_id":"standin-subject#1","status_code":"SUCCESS","status_message":"Done."}`),
			},
		),
	}

	out := RunScenario(t, sc)

	if out.ExitCode != cli.ExitSuccess {
		t.Fatalf("RunScenario: exit code = %d, want %d (ExitSuccess) — the whole pipeline must run cleanly through the scripted adapter alone\nstdout: %s\nstderr: %s",
			out.ExitCode, cli.ExitSuccess, out.Stdout, out.Stderr)
	}
}
